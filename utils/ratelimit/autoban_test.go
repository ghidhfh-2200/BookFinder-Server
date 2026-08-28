package ratelimit

import (
	"testing"

	"bookfinder-backend/types"
)

// setAutoBan 直接设置生效中的自动封禁规则，省去写文件
func setAutoBan(t *testing.T, autoBan types.AutoBanRules) {
	t.Helper()

	mu.Lock()
	rules = types.RateRules{
		Enabled: true,
		Limits: map[types.LimitCategory]types.CategoryLimit{
			types.CategoryReport: {Daily: 50, Burst: 10, BurstWindowSeconds: 60},
		},
		AutoBan: autoBan,
	}
	mu.Unlock()
}

// defaultAutoBan 与默认配置一致的规则
func defaultAutoBan() types.AutoBanRules {
	return types.AutoBanRules{
		Enabled:                     true,
		DailyOverflowMultiplier:     3,
		BurstViolations:             5,
		DuplicateReports:            10,
		ProbationOverflowMultiplier: 5,
	}
}

// TestUsingFullQuotaDoesNotBan 用满当日配额不应封禁。
// 连续多日用满也不封：天天用满额度是重度用户的正常特征，
// 这类访问者每天照常被限流拦到次日零点，但不升级为封禁。
//
// 注意 DailyUsed 计的是尝试次数（含被限流拒绝的）：用满配额后被拒的请求仍会累加，
// 所以此处 51~149 的取值代表「配额用尽后又试了几次」，仍在容忍范围内。
func TestUsingFullQuotaDoesNotBan(t *testing.T) {
	setAutoBan(t, defaultAutoBan())

	for _, used := range []int{50, 51, 100, 149} {
		verdict := EvaluateBan(Signals{
			Category:   types.CategoryReport,
			DailyUsed:  used,
			DailyLimit: 50,
		})
		if verdict.ShouldBan {
			t.Errorf("当日尝试 %d 次（配额 50，阈值 150）不应封禁，实际封了: %s",
				used, verdict.Detail)
		}
	}
}

// TestDailyOverflowBans 当日尝试次数达配额倍数阈值才封禁。
// 达到这个量级意味着配额用尽后仍在反复叩门——正常用户看到提示就停了。
func TestDailyOverflowBans(t *testing.T) {
	setAutoBan(t, defaultAutoBan())

	verdict := EvaluateBan(Signals{
		Category:   types.CategoryReport,
		DailyUsed:  150, // 50 × 3
		DailyLimit: 50,
	})

	if !verdict.ShouldBan {
		t.Fatal("当日尝试次数达配额 3 倍应封禁")
	}
	if verdict.Reason == "" || verdict.Detail == "" {
		t.Error("封禁结论应带原因与详情，便于复核误判")
	}
}

// TestBurstViolationsBans 反复触发突发限制应封禁
func TestBurstViolationsBans(t *testing.T) {
	setAutoBan(t, defaultAutoBan())

	if verdict := EvaluateBan(Signals{BurstViolations: 4}); verdict.ShouldBan {
		t.Error("突发违规 4 次（阈值 5）不应封禁")
	}
	if verdict := EvaluateBan(Signals{BurstViolations: 5}); !verdict.ShouldBan {
		t.Error("突发违规达 5 次应封禁")
	}
}

// TestDuplicateReportsBans 大量疑似重复报告应封禁
func TestDuplicateReportsBans(t *testing.T) {
	setAutoBan(t, defaultAutoBan())

	if verdict := EvaluateBan(Signals{DuplicateReports: 9}); verdict.ShouldBan {
		t.Error("重复报告 9 次（阈值 10）不应封禁")
	}
	if verdict := EvaluateBan(Signals{DuplicateReports: 10}); !verdict.ShouldBan {
		t.Error("重复报告达 10 次应封禁")
	}
}

// TestDisabledAutoBanNeverBans 关闭自动封禁后任何信号都不触发
func TestDisabledAutoBanNeverBans(t *testing.T) {
	setAutoBan(t, types.AutoBanRules{Enabled: false, DailyOverflowMultiplier: 3})

	verdict := EvaluateBan(Signals{
		Category:         types.CategoryReport,
		DailyUsed:        99999,
		DailyLimit:       50,
		BurstViolations:  99,
		DuplicateReports: 99,
	})

	if verdict.ShouldBan {
		t.Error("关闭自动封禁后不应触发任何封禁")
	}
}

// TestZeroThresholdDisablesRule 阈值为 0 表示不启用该条规则
func TestZeroThresholdDisablesRule(t *testing.T) {
	setAutoBan(t, types.AutoBanRules{
		Enabled: true,
		// 只留突发违规一条，其余为 0
		BurstViolations: 5,
	})

	if verdict := EvaluateBan(Signals{
		Category:         types.CategoryReport,
		DailyUsed:        99999,
		DailyLimit:       50,
		DuplicateReports: 99,
	}); verdict.ShouldBan {
		t.Errorf("阈值为 0 的规则不应触发，实际封了: %s", verdict.Detail)
	}

	if verdict := EvaluateBan(Signals{BurstViolations: 5}); !verdict.ShouldBan {
		t.Error("阈值非 0 的规则应正常生效")
	}
}

// TestNoDailyLimitSkipsOverflowRule 配额缺失时跳过超额判定，避免除零式误判
func TestNoDailyLimitSkipsOverflowRule(t *testing.T) {
	setAutoBan(t, defaultAutoBan())

	if verdict := EvaluateBan(Signals{DailyUsed: 99999, DailyLimit: 0}); verdict.ShouldBan {
		t.Error("未配置每日配额时不应触发超额封禁")
	}
}
