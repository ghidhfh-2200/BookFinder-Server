package ratelimit

import (
	"context"
	"testing"

	"bookfinder-backend/types"
)

// setProbationRules 设置一组便于测试的见习配额
func setProbationRules(t *testing.T, probation types.ProbationRules) {
	t.Helper()

	mu.Lock()
	rules = types.RateRules{
		Enabled: true,
		Limits: map[types.LimitCategory]types.CategoryLimit{
			types.CategoryReport: {Daily: 5, Burst: 3, BurstWindowSeconds: 60},
		},
		Probation: probation,
		AutoBan:   types.AutoBanRules{Enabled: false},
	}
	mu.Unlock()
}

// TestProbationAllowsThenBlocks 见习额度内放行，耗尽后拒绝。
//
// 这是「不带 Cookie 也要付代价」的实现：额度按来源计（IPv6 为其 /64，
// IPv4 为其地址），故清 cookie 换令牌不再免费——领取新令牌本身要消耗额度。
func TestProbationAllowsThenBlocks(t *testing.T) {
	rdb := testRedis(t)
	setProbationRules(t, types.ProbationRules{Daily: 3, Burst: 3, BurstWindowSeconds: 60})

	ctx := context.Background()
	const ip = "203.0.113.50"

	for i := range 3 {
		decision, err := CheckProbation(ctx, rdb, ip)
		if err != nil {
			t.Fatalf("第 %d 次见习判定出错: %v", i+1, err)
		}
		if !decision.Allowed {
			t.Fatalf("第 %d 次应放行（额度 3），实际被拒: %s", i+1, decision.Reason)
		}
	}

	decision, err := CheckProbation(ctx, rdb, ip)
	if err != nil {
		t.Fatalf("见习判定出错: %v", err)
	}
	if decision.Allowed {
		t.Error("额度耗尽后应拒绝")
	}
	if decision.Reason == "" {
		t.Error("被拒时应给出原因，供前端提示用户启用 Cookie")
	}
}

// TestProbationIsPerIPv4Address IPv4 下额度按精确地址隔离。
//
// 不按 /24 计是有意的：IPv4 地址由 ISP 或 NAT 决定，攻击者换不了自己的地址，
// 故没有「换址重置额度」的漏洞；而一个 /24 背后可能是整个校园网出口，
// 按 /24 计会让一个人刷爆额度、同段几百人都领不到令牌。
func TestProbationIsPerIPv4Address(t *testing.T) {
	rdb := testRedis(t)
	setProbationRules(t, types.ProbationRules{Daily: 2, Burst: 2, BurstWindowSeconds: 60})

	ctx := context.Background()

	// 同一 /24 内的另一台机器，现实中很可能是另一个人
	for range 3 {
		CheckProbation(ctx, rdb, "203.0.113.51")
	}

	decision, err := CheckProbation(ctx, rdb, "203.0.113.52")
	if err != nil {
		t.Fatalf("见习判定出错: %v", err)
	}
	if !decision.Allowed {
		t.Error("同 /24 内另一个 IPv4 地址的额度不应受影响，否则共用出口的人会被连坐")
	}
}

// TestProbationIPv6SameNetworkShared 同一 /64 内换地址不应重置见习额度。
//
// 这是一个回归测试，对应一个实测确认过的漏洞：IPv6 终端通常独占一个 /64，
// 段内换址不受任何限制，此前按单个地址计数时，换 10 个地址即得到 10 份完整配额——
// 按令牌计数的限流在 IPv6 下形同不存在。
func TestProbationIPv6SameNetworkShared(t *testing.T) {
	rdb := testRedis(t)
	setProbationRules(t, types.ProbationRules{Daily: 3, Burst: 3, BurstWindowSeconds: 60})

	ctx := context.Background()

	// 同一 /64 内换 3 个地址，共用同一份额度，故第 4 次起应被拒
	for i, ip := range []string{
		"2001:db8:0:1::100",
		"2001:db8:0:1::200",
		"2001:db8:0:1::dead:beef",
	} {
		decision, err := CheckProbation(ctx, rdb, ip)
		if err != nil {
			t.Fatalf("第 %d 次见习判定出错: %v", i+1, err)
		}
		if !decision.Allowed {
			t.Fatalf("第 %d 次应放行（额度 3）", i+1)
		}
	}

	decision, err := CheckProbation(ctx, rdb, "2001:db8:0:1::ffff")
	if err != nil {
		t.Fatalf("见习判定出错: %v", err)
	}
	if decision.Allowed {
		t.Error("同一 /64 内换地址不应重置额度，否则 IPv6 下的见习闸门等于不存在")
	}
}

// TestProbationIPv6DifferentNetworks 不同 /64 之间互不影响，
// 否则会连坐到无关的用户
func TestProbationIPv6DifferentNetworks(t *testing.T) {
	rdb := testRedis(t)
	setProbationRules(t, types.ProbationRules{Daily: 2, Burst: 2, BurstWindowSeconds: 60})

	ctx := context.Background()

	for range 3 {
		CheckProbation(ctx, rdb, "2001:db8:0:1::1")
	}

	decision, err := CheckProbation(ctx, rdb, "2001:db8:0:2::1")
	if err != nil {
		t.Fatalf("见习判定出错: %v", err)
	}
	if !decision.Allowed {
		t.Error("另一个 /64 的额度不应受影响")
	}
}

// TestProbationScope 计数主体的换算：IPv6 取 /64，IPv4 取精确地址
func TestProbationScope(t *testing.T) {
	cases := map[string]string{
		"203.0.113.5":        "203.0.113.5",
		"::ffff:203.0.113.5": "203.0.113.5",
		"2001:db8:0:1::1":    "2001:db8:0:1::/64",
		"2001:db8:0:1::dead": "2001:db8:0:1::/64",
		"2001:DB8:0:1::1":    "2001:db8:0:1::/64",
		"不是一个地址":             "不是一个地址",
	}

	for ip, want := range cases {
		if got := probationScope(ip); got != want {
			t.Errorf("probationScope(%q) = %q，期望 %q", ip, got, want)
		}
	}
}

// TestProbationBurst 短窗口内超限应被拦下，即便每日额度尚有余量
func TestProbationBurst(t *testing.T) {
	rdb := testRedis(t)
	setProbationRules(t, types.ProbationRules{Daily: 50, Burst: 2, BurstWindowSeconds: 60})

	ctx := context.Background()
	const ip = "203.0.113.53"

	for i := range 2 {
		if decision, _ := CheckProbation(ctx, rdb, ip); !decision.Allowed {
			t.Fatalf("第 %d 次应放行", i+1)
		}
	}

	decision, _ := CheckProbation(ctx, rdb, ip)
	if decision.Allowed {
		t.Error("突发超限应被拒绝")
	}
}

// TestProbationDisabled 额度为 0 表示不启用该闸门，一律放行
func TestProbationDisabled(t *testing.T) {
	rdb := testRedis(t)
	setProbationRules(t, types.ProbationRules{Daily: 0})

	ctx := context.Background()

	for range 10 {
		if decision, _ := CheckProbation(ctx, rdb, "203.0.113.54"); !decision.Allowed {
			t.Fatal("未启用见习闸门时应一律放行")
		}
	}
}

// TestCountProbation 计数应含被拒的请求。
// 自动封禁规则五据此识别「额度耗尽后仍反复请求」，若只计放行的那些，
// 计数会停在额度上限、永远达不到封禁阈值。
func TestCountProbation(t *testing.T) {
	rdb := testRedis(t)
	setProbationRules(t, types.ProbationRules{Daily: 2, Burst: 2, BurstWindowSeconds: 60})

	ctx := context.Background()
	const ip = "203.0.113.55"

	for range 5 {
		CheckProbation(ctx, rdb, ip)
	}

	count, err := CountProbation(ctx, rdb, ip)
	if err != nil {
		t.Fatalf("查询见习计数出错: %v", err)
	}
	if count != 5 {
		t.Errorf("见习计数应为 5（含被拒的 3 次），实际为 %d", count)
	}
}

// TestResetProbationScope 解封后见习计数应清零。
// 不清的话，解封后连领取新令牌都做不到——人还是进不来。
func TestResetProbationScope(t *testing.T) {
	rdb := testRedis(t)
	setProbationRules(t, types.ProbationRules{Daily: 2, Burst: 2, BurstWindowSeconds: 60})

	ctx := context.Background()
	const ip = "203.0.113.56"

	for range 5 {
		CheckProbation(ctx, rdb, ip)
	}

	if err := ResetProbationScope(ctx, rdb, ProbationScopeOf(ip)); err != nil {
		t.Fatalf("重置见习计数失败: %v", err)
	}

	count, _ := CountProbation(ctx, rdb, ip)
	if count != 0 {
		t.Errorf("重置后计数应为 0，实际为 %d", count)
	}

	// 突发窗口也应清掉，否则解封后立刻又撞上突发限制
	if decision, _ := CheckProbation(ctx, rdb, ip); !decision.Allowed {
		t.Error("重置后应可以重新领取令牌")
	}
}

// TestResetAfterUnbanClearsProbation 解封应一并清掉见习计数，
// 否则被封者解封后仍然领不到令牌
func TestResetAfterUnbanClearsProbation(t *testing.T) {
	rdb := testRedis(t)
	setProbationRules(t, types.ProbationRules{Daily: 2, Burst: 2, BurstWindowSeconds: 60})

	ctx := context.Background()
	const ip = "203.0.113.57"

	for range 5 {
		CheckProbation(ctx, rdb, ip)
	}

	if err := ResetAfterUnban(ctx, rdb, ip); err != nil {
		t.Fatalf("解封重置失败: %v", err)
	}

	if count, _ := CountProbation(ctx, rdb, ip); count != 0 {
		t.Errorf("解封后见习计数应为 0，实际为 %d", count)
	}
}

// TestResetProbationScopeWithNilClient Redis 未连接时不应崩溃
func TestResetProbationScopeWithNilClient(t *testing.T) {
	if err := ResetProbationScope(context.Background(), nil, ProbationScopeOf("203.0.113.58")); err != nil {
		t.Errorf("Redis 未连接时应静默返回，实际报错: %v", err)
	}
}
