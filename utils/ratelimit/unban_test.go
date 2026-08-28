package ratelimit

import (
	"context"
	"testing"
	"time"

	"bookfinder-backend/types"
)

// TestResetForSubjectVisitorOnly 只含令牌标识的主体也要被重置。
//
// 这是修复的要点：网段级的精准处置只写令牌标识，那类主体名下没有任何 IP 标识。
// 旧实现只遍历 IP 标识，于是一次都不执行——解封后第一个请求就重新命中、立刻再封，
// 管理员除了反复解封别无办法。
func TestResetForSubjectVisitorOnly(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t)

	ctx := context.Background()
	const ip = "2001:db8:bb::1"
	const visitor = "visitor-only-subject"

	// 制造计数：既超出配额，也在网段画像里留下贡献
	for range 20 {
		CheckAndCollect(ctx, rdb, types.CategoryReport, visitor, ip, visitor)
	}
	for range 30 {
		if err := RecordNetworkRequest(ctx, rdb, ip, visitor); err != nil {
			t.Fatalf("RecordNetworkRequest 出错: %v", err)
		}
	}

	scope, ok := NetworkScope(ip)
	if !ok {
		t.Fatal("测试前提不成立：应能算出网段")
	}

	// 主体只有令牌标识，且网段推不出来——退而只清该令牌的各类计数
	err := ResetForSubject(ctx, rdb, []types.BanIdent{
		{Kind: types.IdentVisitor, Value: visitor},
	})
	if err != nil {
		t.Fatalf("ResetForSubject 出错: %v", err)
	}

	if used, _ := getInt(ctx, rdb, dailyKey(time.Now(), types.CategoryReport, visitor)); used != 5 {
		t.Errorf("令牌的每日计数应封顶到配额 5，实际为 %d", used)
	}

	// 带上网段标识时，网段贡献也应被清掉
	for range 30 {
		RecordNetworkRequest(ctx, rdb, ip, visitor)
	}
	err = ResetForSubject(ctx, rdb, []types.BanIdent{
		{Kind: types.IdentVisitor, Value: visitor},
		{Kind: types.IdentIPNet, Value: scope},
	})
	if err != nil {
		t.Fatalf("ResetForSubject 出错: %v", err)
	}

	profile, err := ProfileNetwork(ctx, rdb, ip, 5)
	if err != nil {
		t.Fatalf("ProfileNetwork 出错: %v", err)
	}
	if profile.Total != 0 {
		t.Errorf("网段总量应清零，实际为 %d", profile.Total)
	}
	for _, load := range profile.Top {
		if load.VisitorKey == visitor {
			t.Errorf("该令牌应已从网段画像移除，实际仍贡献 %d 次", load.Requests)
		}
	}
}

// TestResetForSubjectNetworkIdentUsesScope 网段标识存的是 CIDR 串，
// 必须按网段清理，不能当成 IP。
//
// 把 "2001:db8::/64" 当 IP 传给按 IP 的清理函数会解析失败、静默什么都不做，
// 于是该网段的画像与见习计数原样保留——看起来解封成功，实际下一个请求就复发。
func TestResetForSubjectNetworkIdentUsesScope(t *testing.T) {
	rdb := testRedis(t)
	// 需要见习额度为正，否则 CheckProbation 直接放行、不产生计数
	setProbationBanRules(t, 20, probationAutoBan())

	ctx := context.Background()
	const ip = "2001:db8:cc::1"

	for range 30 {
		if err := RecordNetworkRequest(ctx, rdb, ip, "someone"); err != nil {
			t.Fatalf("RecordNetworkRequest 出错: %v", err)
		}
	}
	// 见习计数：IPv6 的主体就是其 /64
	for range 5 {
		if _, err := CheckProbation(ctx, rdb, ip); err != nil {
			t.Fatalf("CheckProbation 出错: %v", err)
		}
	}

	scope, ok := NetworkScope(ip)
	if !ok {
		t.Fatal("测试前提不成立：应能算出网段")
	}

	if used, _ := CountProbation(ctx, rdb, ip); used == 0 {
		t.Fatal("测试前提不成立：应已产生见习计数")
	}

	err := ResetForSubject(ctx, rdb, []types.BanIdent{
		{Kind: types.IdentIPNet, Value: scope},
	})
	if err != nil {
		t.Fatalf("ResetForSubject 出错: %v", err)
	}

	profile, err := ProfileNetwork(ctx, rdb, ip, 5)
	if err != nil {
		t.Fatalf("ProfileNetwork 出错: %v", err)
	}
	if profile.Total != 0 {
		t.Errorf("网段画像应清零，实际总量为 %d", profile.Total)
	}
	if used, _ := CountProbation(ctx, rdb, ip); used != 0 {
		t.Errorf("该网段的见习计数应清零，实际为 %d", used)
	}
}

// TestResetForSubjectMixedIdents 混合标识的主体：各种类都应被处理
func TestResetForSubjectMixedIdents(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t)

	ctx := context.Background()
	const ip = "10.0.1.9"
	const visitor = "visitor-mixed"

	for range 20 {
		CheckAndCollect(ctx, rdb, types.CategoryReport, visitor, ip, visitor)
	}
	for range 3 {
		RecordDuplicate(ctx, rdb, ip)
	}

	err := ResetForSubject(ctx, rdb, []types.BanIdent{
		{Kind: types.IdentIP, Value: ip},
		{Kind: types.IdentVisitor, Value: visitor},
		// 设备标识不参与限流计数，应被安静跳过
		{Kind: types.IdentDevice, Value: "device-hash"},
	})
	if err != nil {
		t.Fatalf("ResetForSubject 出错: %v", err)
	}

	if used, _ := getInt(ctx, rdb, dailyKey(time.Now(), types.CategoryReport, visitor)); used != 5 {
		t.Errorf("令牌的每日计数应封顶到配额 5，实际为 %d", used)
	}
	if duplicates, _ := CountDuplicates(ctx, rdb, ip); duplicates != 0 {
		t.Errorf("重复报告应清零，实际为 %d", duplicates)
	}
}

// TestNetworkScopeOfPrefersExplicitNetwork 推算网段时优先用主体自己的网段标识，
// 没有才从精确 IP 算
func TestNetworkScopeOfPrefersExplicitNetwork(t *testing.T) {
	got := networkScopeOf([]types.BanIdent{
		{Kind: types.IdentIP, Value: "203.0.113.5"},
		{Kind: types.IdentIPNet, Value: "2001:db8::/64"},
	})
	if got != "2001:db8::/64" {
		t.Errorf("应优先取网段标识，实际为 %q", got)
	}

	got = networkScopeOf([]types.BanIdent{{Kind: types.IdentIP, Value: "203.0.113.5"}})
	if got != "203.0.113.0/24" {
		t.Errorf("应从精确 IP 推算网段，实际为 %q", got)
	}

	// 只有令牌标识时推不出网段
	if got := networkScopeOf([]types.BanIdent{
		{Kind: types.IdentVisitor, Value: "v"},
	}); got != "" {
		t.Errorf("只含令牌标识时应推不出网段，实际为 %q", got)
	}
}

// TestResetForSubjectTolerateNilRedis Redis 不可用时应静默返回
func TestResetForSubjectTolerateNilRedis(t *testing.T) {
	err := ResetForSubject(context.Background(), nil, []types.BanIdent{
		{Kind: types.IdentIP, Value: "203.0.113.5"},
	})
	if err != nil {
		t.Errorf("Redis 为 nil 时应静默返回，实际报错: %v", err)
	}
}
