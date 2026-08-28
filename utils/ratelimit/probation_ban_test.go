package ratelimit

import (
	"testing"

	"bookfinder-backend/types"
)

// setProbationBanRules 设置见习额度与自动封禁规则
func setProbationBanRules(t *testing.T, daily int, autoBan types.AutoBanRules) {
	t.Helper()

	mu.Lock()
	rules = types.RateRules{
		Enabled: true,
		Limits: map[types.LimitCategory]types.CategoryLimit{
			types.CategoryRead: {Daily: 300, Burst: 120, BurstWindowSeconds: 60},
		},
		Probation: types.ProbationRules{Daily: daily, Burst: 5, BurstWindowSeconds: 60},
		AutoBan:   autoBan,
	}
	mu.Unlock()
}

// probationAutoBan 与默认配置一致的见习超额规则
func probationAutoBan() types.AutoBanRules {
	return types.AutoBanRules{
		Enabled:                     true,
		ProbationOverflowMultiplier: 5,
	}
}

// TestProbationOverflowBans 反复消耗见习额度应封禁。
//
// 这是「不带 Cookie 刷接口」的特征：正常用户的首个请求就换到了正式令牌，
// 根本走不到反复消耗见习额度这一步。
//
// 判定所在的位置是这条规则能成立的前提：它在见习路径上执行，故打中的正是那个
// 不保存令牌的客户端。放在限流中间件上时，额度耗尽的请求早已被拦下、走不到那里，
// 这条规则对真正的目标就是死代码，反而会用邻居的用量封掉持有效令牌的正常用户。
func TestProbationOverflowBans(t *testing.T) {
	setProbationBanRules(t, 20, probationAutoBan())

	// 额度 20，倍数 5，故阈值为 100
	if verdict := EvaluateProbationBan(99); verdict.ShouldBan {
		t.Error("见习请求 99 次（阈值 100）不应封禁")
	}

	verdict := EvaluateProbationBan(100)
	if !verdict.ShouldBan {
		t.Fatal("见习请求达 100 次应封禁")
	}
	if verdict.Reason == "" || verdict.Detail == "" {
		t.Error("封禁判定应带上原因与详情，便于复核误判")
	}
}

// TestProbationWithinLimitDoesNotBan 禁用 Cookie 的正常用户会一直停留在见习状态，
// 只要没有远超额度就不该被封
func TestProbationWithinLimitDoesNotBan(t *testing.T) {
	setProbationBanRules(t, 20, probationAutoBan())

	for _, used := range []int{0, 1, 20, 21, 60, 99} {
		if verdict := EvaluateProbationBan(used); verdict.ShouldBan {
			t.Errorf("见习请求 %d 次不应封禁，实际封了: %s", used, verdict.Detail)
		}
	}
}

// TestProbationRuleDisabled 倍数为 0 表示不启用该条
func TestProbationRuleDisabled(t *testing.T) {
	autoBan := probationAutoBan()
	autoBan.ProbationOverflowMultiplier = 0
	setProbationBanRules(t, 20, autoBan)

	if verdict := EvaluateProbationBan(99999); verdict.ShouldBan {
		t.Error("该条已关闭，不应封禁")
	}
}

// TestProbationBanRequiresAutoBanEnabled 自动封禁总开关关闭时该条也不生效
func TestProbationBanRequiresAutoBanEnabled(t *testing.T) {
	autoBan := probationAutoBan()
	autoBan.Enabled = false
	setProbationBanRules(t, 20, autoBan)

	if verdict := EvaluateProbationBan(99999); verdict.ShouldBan {
		t.Error("自动封禁已关闭，不应封禁")
	}
}

// TestProbationZeroLimitDoesNotBan 见习额度为 0（未配置）时该条不应生效，
// 否则任何请求都会因「0 × 倍数 = 0」而立即触发封禁
func TestProbationZeroLimitDoesNotBan(t *testing.T) {
	setProbationBanRules(t, 0, probationAutoBan())

	if verdict := EvaluateProbationBan(1); verdict.ShouldBan {
		t.Error("见习额度未配置时该条不应生效")
	}
}

// TestProbationBanIdentsIPv6BansPrefix IPv6 来源封其 /64。
// 段内换址不受任何限制，只封精确地址等于没封。
func TestProbationBanIdentsIPv6BansPrefix(t *testing.T) {
	idents := ProbationBanIdents("2001:db8::1")

	if len(idents) != 1 {
		t.Fatalf("应写入一个标识，实际为 %d 个", len(idents))
	}
	if idents[0].Kind != types.IdentIPNet || idents[0].Value != "2001:db8::/64" {
		t.Errorf("IPv6 应封其 /64，实际为 %s:%s", idents[0].Kind, idents[0].Value)
	}
}

// TestProbationBanIdentsIPv4BansExactIP IPv4 只封精确地址。
// 一个 /24 背后可能是整个校园网出口，自动流程绝不碰它。
func TestProbationBanIdentsIPv4BansExactIP(t *testing.T) {
	for _, ip := range []string{"203.0.113.5", "::ffff:203.0.113.5"} {
		idents := ProbationBanIdents(ip)

		if len(idents) != 1 {
			t.Fatalf("%s 应写入一个标识，实际为 %d 个", ip, len(idents))
		}
		if idents[0].Kind != types.IdentIP {
			t.Errorf("%s 应封精确 IP，实际种类为 %s", ip, idents[0].Kind)
		}
		if idents[0].Value != "203.0.113.5" {
			t.Errorf("%s 应归一化为 203.0.113.5，实际为 %s", ip, idents[0].Value)
		}
	}
}
