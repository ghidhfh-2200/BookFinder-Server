package services

import (
	"context"
	"testing"
	"time"

	"bookfinder-backend/types"
	"bookfinder-backend/utils/sysconfig"
)

// setMaintenance 设置清理配置。
// 直接经 Commit 走一遍校验，确保测试用的取值本身是合法的。
func setMaintenance(t *testing.T, m types.MaintenanceConfig) {
	t.Helper()

	config := types.DefaultSystemConfig()
	config.Maintenance = m

	// Commit 会写文件，故先把路径指向临时目录
	if err := sysconfig.Load(t.TempDir() + "/system_config.json"); err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	if err := sysconfig.Commit(&config); err != nil {
		t.Fatalf("提交配置失败: %v", err)
	}
}

// resetLastRun 清掉「今日已执行」的记录，避免用例之间互相影响
func resetLastRun(t *testing.T) {
	t.Helper()

	runMu.Lock()
	lastRunDate = ""
	runMu.Unlock()
}

// TestTickSkipsWhenDisabled 关闭清理时不应执行。
// 走到 cleanup 会碰数据库，而本用例没有数据库——故若真的执行了会 panic，
// 这也正是它能验证「确实没执行」的方式。
func TestTickSkipsWhenDisabled(t *testing.T) {
	resetLastRun(t)
	setMaintenance(t, types.MaintenanceConfig{
		Enabled:                   false,
		DailyAt:                   "03:30",
		OperationLogRetentionDays: 180,
		AppLogRetentionDays:       30,
	})

	// 恰好在执行时刻，但开关关着
	tick(context.Background(), time.Date(2026, 8, 27, 3, 30, 0, 0, time.Local))

	runMu.Lock()
	defer runMu.Unlock()
	if lastRunDate != "" {
		t.Error("清理已关闭，不该记下执行日期")
	}
}

// TestTickSkipsOutsideWindow 未到执行时刻不应执行
func TestTickSkipsOutsideWindow(t *testing.T) {
	resetLastRun(t)
	setMaintenance(t, types.MaintenanceConfig{
		Enabled:                   true,
		DailyAt:                   "03:30",
		OperationLogRetentionDays: 180,
		AppLogRetentionDays:       30,
	})

	// 差一分钟、晚一分钟都不该触发
	for _, at := range []time.Time{
		time.Date(2026, 8, 27, 3, 29, 0, 0, time.Local),
		time.Date(2026, 8, 27, 3, 31, 0, 0, time.Local),
		time.Date(2026, 8, 27, 15, 30, 0, 0, time.Local),
	} {
		tick(context.Background(), at)

		runMu.Lock()
		recorded := lastRunDate
		runMu.Unlock()

		if recorded != "" {
			t.Errorf("%s 不在执行时刻，不该触发", at.Format("15:04"))
		}
	}
}

// TestCommitRejectsInvalidDailyAt 非法的执行时刻进不了内存。
//
// tick 里那条「时刻非法则跳过」的分支是给「配置文件被手工改坏后重启」兜底的，
// 正常路径下走不到——因为 Commit 会先拒绝。此处验证的是这道拦截。
func TestCommitRejectsInvalidDailyAt(t *testing.T) {
	if err := sysconfig.Load(t.TempDir() + "/system_config.json"); err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	for _, bad := range []string{"25:99", "3:30", "", "abc"} {
		config := types.DefaultSystemConfig()
		config.Maintenance.DailyAt = bad

		if err := sysconfig.Commit(&config); err == nil {
			t.Errorf("执行时刻 %q 应当被拒绝", bad)
		}
	}

	// 被拒绝之后内存里仍是合法的默认值，没有被污染
	if _, ok := sysconfig.DailyAtMinutes(sysconfig.Get().Maintenance.DailyAt); !ok {
		t.Error("提交失败后内存里的配置不应被污染")
	}
}

// TestDailyAtMinutes 时刻解析
func TestDailyAtMinutes(t *testing.T) {
	cases := map[string]int{
		"00:00": 0,
		"03:30": 210,
		"23:59": 1439,
	}
	for at, want := range cases {
		got, ok := sysconfig.DailyAtMinutes(at)
		if !ok || got != want {
			t.Errorf("DailyAtMinutes(%q) = %d, %v；期望 %d", at, got, ok, want)
		}
	}

	for _, bad := range []string{"", "3:30", "24:00", "12:60", "abc", "12:34:56"} {
		if _, ok := sysconfig.DailyAtMinutes(bad); ok {
			t.Errorf("DailyAtMinutes(%q) 应判为非法", bad)
		}
	}
}

// TestStartMaintenanceStopsOnCancel 取消 context 后协程应退出。
// 不退出的话，关闭流程会卡在 waitMaintenance() 上。
func TestStartMaintenanceStopsOnCancel(t *testing.T) {
	setMaintenance(t, types.MaintenanceConfig{
		Enabled:                   false,
		DailyAt:                   "03:30",
		OperationLogRetentionDays: 180,
		AppLogRetentionDays:       30,
	})

	ctx, cancel := context.WithCancel(context.Background())
	wait := StartMaintenance(ctx)

	cancel()

	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("取消后协程应立即退出，否则关闭流程会卡住")
	}
}
