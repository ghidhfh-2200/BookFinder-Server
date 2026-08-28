package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"bookfinder-backend/logger"
	"bookfinder-backend/models"
	"bookfinder-backend/types"
	"bookfinder-backend/utils/sysconfig"
)

// tickInterval 调度检查的间隔。
//
// 用「每分钟比对一次当前时刻」而非「睡到下次执行时刻」：后者在配置改动后
// 仍按旧时刻醒来，改了执行时间要等一整天才生效。每分钟一次 tick 的开销
// 可以忽略，换来配置改动一分钟内生效。
const tickInterval = time.Minute

// cleanupResult 一次清理的结果，写入日志用。
//
// 不导出、也不做 JSON 序列化：清理是后台维护动作，结果只体现为一条操作日志
// （含删除行数与耗时）与日志表规模的变化，没有接口把它回给前端。
type cleanupResult struct {
	// operationLogs 删除的操作日志行数
	operationLogs int64
	// appLogs 删除的运行日志行数
	appLogs int64
	// duration 耗时
	duration time.Duration
}

var (
	// runMu 保护 lastRunDate，并保证同一时刻只有一次清理在跑。
	runMu sync.Mutex
	// lastRunDate 本日已触发过定时清理的日期（YYYYMMDD）。由 runMu 保护。
	lastRunDate string
)

// StartMaintenance 启动定期清理协程，返回一个等待其退出的函数。
//
// 生命周期与 logger 的写入协程一致：随服务启动，关闭时由 ctx 取消并等它退出。
// 必须等——清理中途被强杀会留下一半删完的状态（虽然无害，但日志里会缺一条结果）。
func StartMaintenance(ctx context.Context) (wait func()) {
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer logger.RecoverPanic("Maintenance Goroutine")

		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()

		logger.Infof("定期清理任务已启动，检查间隔 %s", tickInterval)

		for {
			select {
			case <-ctx.Done():
				logger.Infof("定期清理任务已停止")
				return
			case <-ticker.C:
				tick(ctx, time.Now())
			}
		}
	}()

	return func() { <-done }
}

// tick 判断当前时刻是否该执行清理。
// now 作为参数传入而非直接取，便于测试。
func tick(ctx context.Context, now time.Time) {
	maintenance := sysconfig.Get().Maintenance
	if !maintenance.Enabled {
		return
	}

	target, ok := sysconfig.DailyAtMinutes(maintenance.DailyAt)
	if !ok {
		// 配置校验会拦住非法取值，走到这里说明文件被手工改坏了
		logger.Warnf("清理任务的执行时刻 %q 不合法，本次跳过", maintenance.DailyAt)
		return
	}

	current := now.Hour()*60 + now.Minute()
	// 只在到点的那一分钟触发。tick 间隔恰为一分钟，故不会漏。
	if current != target {
		return
	}

	runMu.Lock()
	defer runMu.Unlock()

	// 同一天只触发一次。执行前就记下日期：若清理失败，当天不再自动重试——
	// 失败多半是数据库不可用，一分钟后重试同样会失败，只会把日志刷满；
	// 次日到点会自然重试，届时过期数据一并清掉。
	date := now.Format("20060102")
	if lastRunDate == date {
		return
	}
	lastRunDate = date

	if _, err := cleanup(ctx, now); err != nil {
		logger.Errorf("定期清理失败: %v", err)
	}
}

// cleanup 按当前配置清理两张日志表。调用方须已持有 runMu。
//
// 两张表分别按自己的保留天数算 cutoff：操作日志是审计证据（复核误封、处理申诉
// 时要用），保留得比运行日志久。
func cleanup(ctx context.Context, now time.Time) (cleanupResult, error) {
	maintenance := sysconfig.Get().Maintenance
	started := time.Now()

	var result cleanupResult

	operationCutoff := now.AddDate(0, 0, -maintenance.OperationLogRetentionDays)
	appCutoff := now.AddDate(0, 0, -maintenance.AppLogRetentionDays)

	operations, opErr := models.DeleteOperationLogsBefore(ctx, operationCutoff, 0)
	result.operationLogs = operations

	appLogs, appErr := models.DeleteAppLogsBefore(ctx, appCutoff, 0)
	result.appLogs = appLogs

	result.duration = time.Since(started)

	// 两张表的错误都要报出去，不能只报第一个——两张都失败时，丢掉的那一个
	// 可能正是真正的原因。已删除的行数照常返回：一张失败不该让另一张的成果归零。
	if err := errors.Join(opErr, appErr); err != nil {
		return result, err
	}

	// 记一条操作日志。没有它的话，任务是否真的在跑无从确认——
	// 而清理本身是静默的，不产生任何用户可见的变化。
	logger.Infof("定期清理完成：操作日志 %d 行、运行日志 %d 行，耗时 %s",
		result.operationLogs, result.appLogs, result.duration)
	logger.Operation(&types.OperationLog{
		User:   "system",
		Action: types.ActionMaintenanceCleanup,
		Level:  types.LevelInfo,
		Detail: fmt.Sprintf("清理过期日志：操作日志 %d 行（保留 %d 天）、"+
			"运行日志 %d 行（保留 %d 天），耗时 %s",
			result.operationLogs, maintenance.OperationLogRetentionDays,
			result.appLogs, maintenance.AppLogRetentionDays, result.duration),
		IP: "-",
	})

	return result, nil
}
