package models

import (
	"context"
	"time"

	"bookfinder-backend/logger"
	"bookfinder-backend/types"
)

// GetOperationLogs 分页查询用户操作日志，按时间倒序
func GetOperationLogs(query *types.OperationLogQuery) ([]types.OperationLog, int64, error) {
	var (
		logs  []types.OperationLog
		total int64
	)

	db := logger.GetDB().Model(&types.OperationLog{})

	if query.User != "" {
		db = db.Where("user = ?", query.User)
	}
	if query.Action != "" {
		db = db.Where("action = ?", query.Action)
	}
	if query.Level != "" {
		db = db.Where("level = ?", query.Level)
	}

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (query.Page - 1) * query.Size
	if err := db.Order("timestamp DESC, id DESC").
		Offset(offset).Limit(query.Size).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// GetLogs 分页查询应用运行日志，按时间倒序
func GetLogs(query *types.LogQuery) ([]types.LogEntry, int64, error) {
	var (
		logs  []types.LogEntry
		total int64
	)

	db := logger.GetDB().Model(&types.LogEntry{})

	if query.Level != "" {
		db = db.Where("level = ?", query.Level)
	}
	if query.Keyword != "" {
		db = db.Where("message LIKE ?", "%"+query.Keyword+"%")
	}

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (query.Page - 1) * query.Size
	if err := db.Order("timestamp DESC, id DESC").
		Offset(offset).Limit(query.Size).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// LogStats 两张日志表的规模，供管理页展示保留策略的实际效果
type LogStats struct {
	// OperationLogs 操作日志行数
	OperationLogs int64 `json:"operation_logs"`
	// AppLogs 运行日志行数
	AppLogs int64 `json:"app_logs"`
	// OldestOperationLog 最早一条操作日志的时间，表空时为零值
	OldestOperationLog *time.Time `json:"oldest_operation_log"`
	// OldestAppLog 最早一条运行日志的时间，表空时为零值
	OldestAppLog *time.Time `json:"oldest_app_log"`
}

// GetLogStats 统计两张日志表的规模与最早记录时间。
// 只看天数看不出保留策略是否真的在生效，故管理页一并展示这些。
func GetLogStats() (LogStats, error) {
	var stats LogStats

	db := logger.GetDB()

	if err := db.Model(&types.OperationLog{}).Count(&stats.OperationLogs).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&types.LogEntry{}).Count(&stats.AppLogs).Error; err != nil {
		return stats, err
	}

	if stats.OperationLogs > 0 {
		var oldest types.OperationLog
		if err := db.Order("timestamp ASC").First(&oldest).Error; err == nil {
			stats.OldestOperationLog = &oldest.Timestamp
		}
	}
	if stats.AppLogs > 0 {
		var oldest types.LogEntry
		if err := db.Order("timestamp ASC").First(&oldest).Error; err == nil {
			stats.OldestAppLog = &oldest.Timestamp
		}
	}

	return stats, nil
}

// DeleteOperationLogsBefore 删除给定时刻之前的操作日志，返回删除行数。
//
// 分批删除：这两张表在远程 MySQL 上，一次删几十万行会长时间持有锁、
// 把写入（每个请求都可能写日志）堵在后面。batch 为单批上限，循环到删不动为止。
//
// ctx 取消时立即停止并返回已删行数——清理任务在服务关闭时会被取消，
// 不能让它拖住退出流程。
func DeleteOperationLogsBefore(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	return deleteLogsBefore(ctx, &types.OperationLog{}, cutoff, batch)
}

// DeleteAppLogsBefore 删除给定时刻之前的运行日志，返回删除行数
func DeleteAppLogsBefore(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	return deleteLogsBefore(ctx, &types.LogEntry{}, cutoff, batch)
}

// deleteLogsBefore 按 timestamp 分批删除，两张表共用。
// timestamp 上有索引（见 types/log.go），故不会全表扫。
func deleteLogsBefore(ctx context.Context, model any,
	cutoff time.Time, batch int) (int64, error) {

	if batch < 1 {
		batch = defaultDeleteBatch
	}

	var deleted int64

	for {
		// 每批之间检查取消，避免大表清理拖住服务退出
		if err := ctx.Err(); err != nil {
			return deleted, err
		}

		result := logger.GetDB().
			Where("timestamp < ?", cutoff).
			Limit(batch).
			Delete(model)
		if result.Error != nil {
			return deleted, result.Error
		}

		deleted += result.RowsAffected

		// 不足一批说明已删完
		if result.RowsAffected < int64(batch) {
			return deleted, nil
		}
	}
}

// defaultDeleteBatch 单批删除的行数。
// 太大则一次事务持锁过久，太小则往返次数过多。
const defaultDeleteBatch = 1000

// ListOperationLogActions 列出已出现过的操作类型，供前端筛选下拉。
// 从数据里取而非硬编码，新增操作类型时前端无需同步改动。
func ListOperationLogActions() ([]string, error) {
	var actions []string
	if err := logger.GetDB().Model(&types.OperationLog{}).
		Distinct().Pluck("action", &actions).Error; err != nil {
		return nil, err
	}
	return actions, nil
}
