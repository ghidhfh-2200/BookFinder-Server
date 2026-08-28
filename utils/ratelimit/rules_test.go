package ratelimit

import (
	"os"
	"path/filepath"
	"testing"

	"bookfinder-backend/types"
)

// validRules 一份合法的规则文件内容
const validRules = `{
  "enabled": true,
  "limits": {
    "read":   {"daily": 3000, "burst": 120, "burst_window_seconds": 60},
    "create": {"daily": 30,   "burst": 5,   "burst_window_seconds": 60},
    "update": {"daily": 60,   "burst": 10,  "burst_window_seconds": 60},
    "report": {"daily": 50,   "burst": 10,  "burst_window_seconds": 60},
    "auth":   {"daily": 20,   "burst": 5,   "burst_window_seconds": 300},
    "appeal": {"daily": 10,   "burst": 3,   "burst_window_seconds": 300}
  },
  "probation": {"daily": 20, "burst": 5, "burst_window_seconds": 60},
  "auto_ban": {
    "enabled": true,
    "daily_overflow_multiplier": 3,
    "burst_violations": 5,
    "duplicate_reports": 10,
    "probation_overflow_multiplier": 5
  }
}`

// writeRules 把规则写入临时文件并加载
func writeRules(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "rate_rules.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入临时规则失败: %v", err)
	}
	if err := Load(path); err != nil {
		t.Fatalf("加载规则失败: %v", err)
	}
	return path
}

// TestLoadValidRules 合法规则应加载成功且取值正确
func TestLoadValidRules(t *testing.T) {
	writeRules(t, validRules)

	if !Enabled() {
		t.Error("规则中 enabled 为 true，Enabled() 应返回 true")
	}

	limit, ok := LimitFor(types.CategoryReport)
	if !ok {
		t.Fatal("report 类别应存在")
	}
	if limit.Daily != 50 || limit.Burst != 10 || limit.BurstWindowSeconds != 60 {
		t.Errorf("report 配额有误: %+v", limit)
	}

	autoBan := AutoBan()
	if !autoBan.Enabled || autoBan.DailyOverflowMultiplier != 3 {
		t.Errorf("自动封禁规则有误: %+v", autoBan)
	}
}

// TestGetReturnsCopy Get 返回副本，改动它不应影响生效中的规则
func TestGetReturnsCopy(t *testing.T) {
	writeRules(t, validRules)

	copied := Get()
	copied.Limits[types.CategoryRead] = types.CategoryLimit{Daily: 1, Burst: 1, BurstWindowSeconds: 1}

	limit, _ := LimitFor(types.CategoryRead)
	if limit.Daily != 3000 {
		t.Errorf("改动 Get() 的返回值影响了生效规则，read 每日配额变为 %d", limit.Daily)
	}
}

// TestValidateRejectsInvalid 不合法的规则应被拒绝
func TestValidateRejectsInvalid(t *testing.T) {
	base := func() *types.RateRules {
		return &types.RateRules{
			Enabled: true,
			Limits: map[types.LimitCategory]types.CategoryLimit{
				types.CategoryRead:   {Daily: 3000, Burst: 120, BurstWindowSeconds: 60},
				types.CategoryCreate: {Daily: 30, Burst: 5, BurstWindowSeconds: 60},
				types.CategoryUpdate: {Daily: 60, Burst: 10, BurstWindowSeconds: 60},
				types.CategoryReport: {Daily: 50, Burst: 10, BurstWindowSeconds: 60},
				types.CategoryAuth:   {Daily: 20, Burst: 5, BurstWindowSeconds: 300},
				types.CategoryAppeal: {Daily: 10, Burst: 3, BurstWindowSeconds: 300},
			},
			Probation: types.ProbationRules{Daily: 20, Burst: 5, BurstWindowSeconds: 60},
			AutoBan:   types.AutoBanRules{Enabled: true, DailyOverflowMultiplier: 3},
		}
	}

	tests := map[string]func(*types.RateRules){
		"缺少类别": func(r *types.RateRules) { delete(r.Limits, types.CategoryReport) },
		"每日配额为零": func(r *types.RateRules) {
			r.Limits[types.CategoryReport] = types.CategoryLimit{Daily: 0, Burst: 1, BurstWindowSeconds: 60}
		},
		"突发次数为零": func(r *types.RateRules) {
			r.Limits[types.CategoryReport] = types.CategoryLimit{Daily: 50, Burst: 0, BurstWindowSeconds: 60}
		},
		"突发超过每日": func(r *types.RateRules) {
			r.Limits[types.CategoryReport] = types.CategoryLimit{Daily: 5, Burst: 10, BurstWindowSeconds: 60}
		},
		// 相等时每日配额先耗尽，突发规则永远轮不到生效
		"突发等于每日": func(r *types.RateRules) {
			r.Limits[types.CategoryReport] = types.CategoryLimit{Daily: 20, Burst: 20, BurstWindowSeconds: 60}
		},
		"突发窗口过长": func(r *types.RateRules) {
			r.Limits[types.CategoryReport] = types.CategoryLimit{Daily: 50, Burst: 10, BurstWindowSeconds: 7200}
		},
		"未知类别": func(r *types.RateRules) {
			r.Limits[types.LimitCategory("bogus")] = types.CategoryLimit{Daily: 1, Burst: 1, BurstWindowSeconds: 1}
		},
		// 倍数为 1 意味着用满配额即被封禁，会误伤重度用户
		"超额倍数为一": func(r *types.RateRules) { r.AutoBan.DailyOverflowMultiplier = 1 },
		// 重复报告同样按 IP 累计，阈值过低会让共用出口的多人各报一次即触发封禁
		"重复报告阈值过低": func(r *types.RateRules) {
			r.AutoBan.DuplicateReports = 2
		},
		"启用自动封禁但无规则生效": func(r *types.RateRules) {
			r.AutoBan = types.AutoBanRules{Enabled: true}
		},
		// 见习额度是无令牌来源的唯一闸门，置零等于放任不带 Cookie 的请求刷接口
		"见习配额为零": func(r *types.RateRules) {
			r.Probation = types.ProbationRules{Daily: 0, Burst: 5, BurstWindowSeconds: 60}
		},
		"见习配额过大": func(r *types.RateRules) {
			r.Probation.Daily = 5000
		},
		"见习突发为零": func(r *types.RateRules) {
			r.Probation.Burst = 0
		},
		"见习突发超过每日": func(r *types.RateRules) {
			r.Probation = types.ProbationRules{Daily: 5, Burst: 10, BurstWindowSeconds: 60}
		},
		"见习突发窗口过长": func(r *types.RateRules) {
			r.Probation.BurstWindowSeconds = 7200
		},
		// 倍数为 1 意味着刚用完见习额度就封禁，会误伤禁用 Cookie 的正常用户
		"见习超额倍数为一": func(r *types.RateRules) {
			r.AutoBan.ProbationOverflowMultiplier = 1
		},
	}

	for name, mutate := range tests {
		candidate := base()
		mutate(candidate)
		if err := Validate(candidate); err == nil {
			t.Errorf("规则「%s」应校验失败，实际通过了", name)
		}
	}
}

// TestValidateAllowsDisabledAutoBan 关闭自动封禁时不校验其各项阈值
func TestValidateAllowsDisabledAutoBan(t *testing.T) {
	candidate := &types.RateRules{
		Enabled: true,
		Limits: map[types.LimitCategory]types.CategoryLimit{
			types.CategoryRead:   {Daily: 100, Burst: 10, BurstWindowSeconds: 60},
			types.CategoryCreate: {Daily: 100, Burst: 10, BurstWindowSeconds: 60},
			types.CategoryUpdate: {Daily: 100, Burst: 10, BurstWindowSeconds: 60},
			types.CategoryReport: {Daily: 100, Burst: 10, BurstWindowSeconds: 60},
			types.CategoryAuth:   {Daily: 20, Burst: 5, BurstWindowSeconds: 300},
			types.CategoryAppeal: {Daily: 10, Burst: 3, BurstWindowSeconds: 300},
		},
		Probation: types.ProbationRules{Daily: 20, Burst: 5, BurstWindowSeconds: 60},
		AutoBan:   types.AutoBanRules{Enabled: false},
	}

	if err := Validate(candidate); err != nil {
		t.Errorf("关闭自动封禁时应校验通过，实际报错: %v", err)
	}
}

// TestValidateProbationRequiredEvenWhenAutoBanOff 见习配额与自动封禁无关，
// 关掉自动封禁也仍须配置：它是无令牌来源的限流闸门，不是封禁规则
func TestValidateProbationRequiredEvenWhenAutoBanOff(t *testing.T) {
	candidate := &types.RateRules{
		Enabled: true,
		Limits: map[types.LimitCategory]types.CategoryLimit{
			types.CategoryRead:   {Daily: 100, Burst: 10, BurstWindowSeconds: 60},
			types.CategoryCreate: {Daily: 100, Burst: 10, BurstWindowSeconds: 60},
			types.CategoryUpdate: {Daily: 100, Burst: 10, BurstWindowSeconds: 60},
			types.CategoryReport: {Daily: 100, Burst: 10, BurstWindowSeconds: 60},
			types.CategoryAuth:   {Daily: 20, Burst: 5, BurstWindowSeconds: 300},
			types.CategoryAppeal: {Daily: 10, Burst: 3, BurstWindowSeconds: 300},
		},
		AutoBan: types.AutoBanRules{Enabled: false},
	}

	if err := Validate(candidate); err == nil {
		t.Error("缺少见习配额应校验失败，即便自动封禁已关闭")
	}
}

// TestCommitWritesFileAndHotReloads 保存规则应立即生效并写回文件
func TestCommitWritesFileAndHotReloads(t *testing.T) {
	file := writeRules(t, validRules)

	next := Get()
	next.Limits[types.CategoryReport] = types.CategoryLimit{
		Daily: 88, Burst: 9, BurstWindowSeconds: 30,
	}
	if err := Commit(&next); err != nil {
		t.Fatalf("保存规则失败: %v", err)
	}

	// 热生效：无需重启
	limit, _ := LimitFor(types.CategoryReport)
	if limit.Daily != 88 {
		t.Errorf("保存后 report 每日配额应为 88，实际为 %d", limit.Daily)
	}

	// 落盘：重新加载同一文件应得到相同内容
	if err := Load(file); err != nil {
		t.Fatalf("重新加载规则失败: %v", err)
	}
	limit, _ = LimitFor(types.CategoryReport)
	if limit.Daily != 88 || limit.BurstWindowSeconds != 30 {
		t.Errorf("重新加载后配额有误: %+v，说明未正确写回文件", limit)
	}
}

// TestCommitRejectsInvalidAndRollsBack 非法规则不应生效，也不应破坏当前规则
func TestCommitRejectsInvalidAndRollsBack(t *testing.T) {
	writeRules(t, validRules)

	invalid := Get()
	delete(invalid.Limits, types.CategoryRead)

	if err := Commit(&invalid); err == nil {
		t.Fatal("缺少类别的规则应被拒绝")
	}

	if _, ok := LimitFor(types.CategoryRead); !ok {
		t.Error("拒绝保存后原规则应保持不变")
	}
}

// TestLoadRejectsMissingFile 文件不存在时应报错，而非静默使用空规则
func TestLoadRejectsMissingFile(t *testing.T) {
	if err := Load(filepath.Join(t.TempDir(), "not_exist.json")); err == nil {
		t.Error("规则文件不存在时应返回错误")
	}
}

// TestWarnsWhenNetworkRuleUnreachable IPv4 网段判定在算术上轮不到触发时应提示。
//
// 认出异常设备需单个令牌达「网段预算 × 每日超额倍数」，而预算是各类配额之和，
// 故这个门槛必然高于「某一类配额 × 同一倍数」——规则一会先把人封掉。
// 配置是自洽的、能保存，但那条规则对 IPv4 实际上不生效，故只提示不拒绝。
func TestWarnsWhenNetworkRuleUnreachable(t *testing.T) {
	rules := &types.RateRules{
		Enabled: true,
		Limits: map[types.LimitCategory]types.CategoryLimit{
			types.CategoryRead:   {Daily: 300, Burst: 120, BurstWindowSeconds: 60},
			types.CategoryReport: {Daily: 10, Burst: 2, BurstWindowSeconds: 60},
		},
		Probation: types.ProbationRules{Daily: 20, Burst: 5, BurstWindowSeconds: 60},
		AutoBan: types.AutoBanRules{
			Enabled:                     true,
			DailyOverflowMultiplier:     3,
			NetworkOverflowMultiplier:   5,
			NetworkTopVisitors:          5,
			NetworkConcentrationPercent: 80,
		},
	}

	warnings := Warnings(rules)
	if len(warnings) == 0 {
		t.Error("预算 310×3=930 远高于 report 类的 10×3=30，应给出提示")
	}

	// 关掉网段判定后不该再提示
	rules.AutoBan.NetworkOverflowMultiplier = 0
	if warnings := Warnings(rules); len(warnings) > 0 {
		t.Errorf("网段判定已关闭，不应提示：%v", warnings)
	}

	// 自动封禁整体关闭时同样不提示
	rules.AutoBan.NetworkOverflowMultiplier = 5
	rules.AutoBan.Enabled = false
	if warnings := Warnings(rules); len(warnings) > 0 {
		t.Errorf("自动封禁已关闭，不应提示：%v", warnings)
	}
}

// TestNoWarningWhenNetworkRuleReachable 只有一个类别时门槛与规则一持平，
// 网段判定并非不可达，不该提示
func TestNoWarningWhenNetworkRuleReachable(t *testing.T) {
	rules := &types.RateRules{
		Enabled: true,
		Limits: map[types.LimitCategory]types.CategoryLimit{
			types.CategoryRead: {Daily: 300, Burst: 120, BurstWindowSeconds: 60},
		},
		Probation: types.ProbationRules{Daily: 20, Burst: 5, BurstWindowSeconds: 60},
		AutoBan: types.AutoBanRules{
			Enabled:                     true,
			DailyOverflowMultiplier:     3,
			NetworkOverflowMultiplier:   5,
			NetworkTopVisitors:          5,
			NetworkConcentrationPercent: 80,
		},
	}

	if warnings := Warnings(rules); len(warnings) > 0 {
		t.Errorf("门槛与规则一持平时不应提示：%v", warnings)
	}

	// 规则未启用自动封禁时不做任何判断
	if warnings := Warnings(nil); warnings != nil {
		t.Errorf("空规则应返回 nil，实际为 %v", warnings)
	}
}
