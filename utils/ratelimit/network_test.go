package ratelimit

import (
	"context"
	"testing"

	"bookfinder-backend/utils/netmask"
)

// TestRecordNetworkRequestAggregates 网段总量按网段汇总，各令牌的贡献单独记账。
//
// 「分别记账」是精准处置的前提：判定网段异常之后，还要回答「是哪几个设备造成的」。
func TestRecordNetworkRequestAggregates(t *testing.T) {
	rdb := testRedis(t)
	setNetworkRules(t, defaultNetworkRules())

	ctx := context.Background()

	// 同一 /64 内的不同地址、不同令牌
	for range 5 {
		RecordNetworkRequest(ctx, rdb, "2001:db8:0:1::100", "heavy")
	}
	for range 2 {
		RecordNetworkRequest(ctx, rdb, "2001:db8:0:1::200", "light")
	}

	profile, err := ProfileNetwork(ctx, rdb, "2001:db8:0:1::abcd", 5)
	if err != nil {
		t.Fatalf("读取画像出错: %v", err)
	}

	if profile.Scope != "2001:db8:0:1::/64" {
		t.Errorf("网段应为 2001:db8:0:1::/64，实际为 %q", profile.Scope)
	}
	// 换地址不影响汇总：7 次请求都归到同一个 /64
	if profile.Total != 7 {
		t.Errorf("网段总量应为 7，实际为 %d", profile.Total)
	}
	if profile.DistinctVisitors != 2 {
		t.Errorf("不同令牌数应为 2，实际为 %d", profile.DistinctVisitors)
	}

	if len(profile.Top) != 2 {
		t.Fatalf("Top 应有 2 项，实际为 %d", len(profile.Top))
	}
	// 按请求数降序，故刷量最多的排在最前——这正是要封的对象
	if profile.Top[0].VisitorKey != "heavy" || profile.Top[0].Requests != 5 {
		t.Errorf("Top[0] 应为 heavy/5，实际为 %s/%d",
			profile.Top[0].VisitorKey, profile.Top[0].Requests)
	}
	if profile.Top[1].VisitorKey != "light" || profile.Top[1].Requests != 2 {
		t.Errorf("Top[1] 应为 light/2，实际为 %s/%d",
			profile.Top[1].VisitorKey, profile.Top[1].Requests)
	}
}

// TestRecordNetworkRequestWithoutVisitor 无令牌的请求仍占网段总量，
// 但没有可归属的设备
func TestRecordNetworkRequestWithoutVisitor(t *testing.T) {
	rdb := testRedis(t)
	setNetworkRules(t, defaultNetworkRules())

	ctx := context.Background()

	for range 3 {
		RecordNetworkRequest(ctx, rdb, "2001:db8:0:2::1", "")
	}

	profile, _ := ProfileNetwork(ctx, rdb, "2001:db8:0:2::1", 5)
	if profile.Total != 3 {
		t.Errorf("总量应为 3，实际为 %d", profile.Total)
	}
	if profile.DistinctVisitors != 0 {
		t.Errorf("无令牌请求不该产生可归属的设备，实际为 %d", profile.DistinctVisitors)
	}
}

// TestNetworkScopeSeparation 不同网段的画像互不干扰，
// 否则一个网段的流量会把无关网段拖进封禁
func TestNetworkScopeSeparation(t *testing.T) {
	rdb := testRedis(t)
	setNetworkRules(t, defaultNetworkRules())

	ctx := context.Background()

	for range 10 {
		RecordNetworkRequest(ctx, rdb, "2001:db8:0:1::1", "v1")
	}
	RecordNetworkRequest(ctx, rdb, "2001:db8:0:2::1", "v2")

	first, _ := ProfileNetwork(ctx, rdb, "2001:db8:0:1::1", 5)
	second, _ := ProfileNetwork(ctx, rdb, "2001:db8:0:2::1", 5)

	if first.Total != 10 {
		t.Errorf("第一个网段总量应为 10，实际为 %d", first.Total)
	}
	if second.Total != 1 {
		t.Errorf("第二个网段总量应为 1，实际为 %d", second.Total)
	}
}

// TestProfileNetworkTopLimit topN 限制返回条数：
// 一次封几十个设备与封整段无异，故考察范围必须有界
func TestProfileNetworkTopLimit(t *testing.T) {
	rdb := testRedis(t)
	setNetworkRules(t, defaultNetworkRules())

	ctx := context.Background()

	for i, key := range []string{"a", "b", "c", "d", "e", "f"} {
		for range i + 1 {
			RecordNetworkRequest(ctx, rdb, "2001:db8:0:3::1", key)
		}
	}

	profile, _ := ProfileNetwork(ctx, rdb, "2001:db8:0:3::1", 3)
	if len(profile.Top) != 3 {
		t.Fatalf("Top 应被限制为 3 项，实际为 %d", len(profile.Top))
	}
	// f 最多（6 次），依次 e（5）、d（4）
	if profile.Top[0].VisitorKey != "f" {
		t.Errorf("Top[0] 应为 f，实际为 %s", profile.Top[0].VisitorKey)
	}
	// 总量仍是全部请求之和，不受 topN 影响
	if profile.Total != 21 {
		t.Errorf("总量应为 21，实际为 %d", profile.Total)
	}
}

// TestProfileNetworkEmpty 尚无请求的网段应返回空画像而非报错
func TestProfileNetworkEmpty(t *testing.T) {
	rdb := testRedis(t)
	setNetworkRules(t, defaultNetworkRules())

	profile, err := ProfileNetwork(context.Background(), rdb, "2001:db8:0:9::1", 5)
	if err != nil {
		t.Fatalf("空网段不应报错: %v", err)
	}
	if profile.Total != 0 || len(profile.Top) != 0 {
		t.Errorf("空网段应返回空画像，实际为 %+v", profile)
	}
}

// TestResetNetworkScope 解封应清掉网段画像。
// 不清的话该网段总量仍然超标，解封后下一个请求就会重新触发网段判定。
func TestResetNetworkScope(t *testing.T) {
	rdb := testRedis(t)
	setNetworkRules(t, defaultNetworkRules())

	ctx := context.Background()

	for range 10 {
		RecordNetworkRequest(ctx, rdb, "2001:db8:0:4::1", "v1")
	}

	if err := ResetNetworkScope(ctx, rdb, mustScope(t, "2001:db8:0:4::1")); err != nil {
		t.Fatalf("重置网段画像失败: %v", err)
	}

	profile, _ := ProfileNetwork(ctx, rdb, "2001:db8:0:4::1", 5)
	if profile.Total != 0 || profile.DistinctVisitors != 0 {
		t.Errorf("重置后画像应清空，实际为 %+v", profile)
	}
}

// TestResetAfterUnbanClearsNetworkProfile 解封的统一入口也应清掉网段画像
func TestResetAfterUnbanClearsNetworkProfile(t *testing.T) {
	rdb := testRedis(t)
	setNetworkRules(t, defaultNetworkRules())

	ctx := context.Background()

	for range 10 {
		RecordNetworkRequest(ctx, rdb, "2001:db8:0:5::1", "v1")
	}

	if err := ResetAfterUnban(ctx, rdb, "2001:db8:0:5::1"); err != nil {
		t.Fatalf("解封重置失败: %v", err)
	}

	profile, _ := ProfileNetwork(ctx, rdb, "2001:db8:0:5::1", 5)
	if profile.Total != 0 {
		t.Errorf("解封后网段总量应清零，实际为 %d", profile.Total)
	}
}

// TestNetworkNilRedisTolerated Redis 未连接时各接口不应崩溃
func TestNetworkNilRedisTolerated(t *testing.T) {
	ctx := context.Background()

	if err := RecordNetworkRequest(ctx, nil, "2001:db8::1", "v1"); err != nil {
		t.Errorf("Redis 未连接时应静默返回，实际报错: %v", err)
	}
	if _, err := ProfileNetwork(ctx, nil, "2001:db8::1", 5); err != nil {
		t.Errorf("Redis 未连接时应静默返回，实际报错: %v", err)
	}
	if err := ResetNetworkScope(ctx, nil, "2001:db8::/64"); err != nil {
		t.Errorf("Redis 未连接时应静默返回，实际报错: %v", err)
	}
}

// TestNetworkInvalidIPTolerated 非法 IP 不应产生键，也不应报错
func TestNetworkInvalidIPTolerated(t *testing.T) {
	rdb := testRedis(t)
	setNetworkRules(t, defaultNetworkRules())

	ctx := context.Background()

	if err := RecordNetworkRequest(ctx, rdb, "不是地址", "v1"); err != nil {
		t.Errorf("非法 IP 应静默跳过，实际报错: %v", err)
	}

	profile, err := ProfileNetwork(ctx, rdb, "不是地址", 5)
	if err != nil {
		t.Errorf("非法 IP 应静默跳过，实际报错: %v", err)
	}
	if profile.Total != 0 {
		t.Errorf("非法 IP 不应产生计数，实际为 %d", profile.Total)
	}
}

// TestNetworkIPv4UsesSlash24 IPv4 的画像按 /24 汇总。
//
// 判据用 /24 是有意的：网段判据的目的正是发现「同一出口下的异常总量」。
// 但处置不会自动封 /24（见 TestNetworkDispersedIPv4DoesNotBanNetwork）——
// 判据与处置是两件事。
func TestNetworkIPv4UsesSlash24(t *testing.T) {
	rdb := testRedis(t)
	setNetworkRules(t, defaultNetworkRules())

	ctx := context.Background()

	RecordNetworkRequest(ctx, rdb, "203.0.113.10", "v1")
	RecordNetworkRequest(ctx, rdb, "203.0.113.20", "v2")

	profile, _ := ProfileNetwork(ctx, rdb, "203.0.113.30", 5)
	if profile.Scope != "203.0.113.0/24" {
		t.Errorf("IPv4 网段应为 203.0.113.0/24，实际为 %q", profile.Scope)
	}
	if profile.Total != 2 {
		t.Errorf("同 /24 内的请求应汇总，总量应为 2，实际为 %d", profile.Total)
	}
}

// TestNetworkEndToEndConcentrated 端到端：记录真实流量后判定，
// 应只封那个刷量的设备
func TestNetworkEndToEndConcentrated(t *testing.T) {
	rdb := testRedis(t)

	autoBan := defaultNetworkRules()
	setNetworkRules(t, autoBan)

	ctx := context.Background()
	const ip = "2001:db8:0:7::1"

	// 一个设备刷 600 次（预算 100，阈值 500）
	for range 600 {
		RecordNetworkRequest(ctx, rdb, ip, "abuser")
	}
	// 同网段一个正常用户
	for range 10 {
		RecordNetworkRequest(ctx, rdb, "2001:db8:0:7::99", "innocent")
	}

	profile, err := ProfileNetwork(ctx, rdb, ip, autoBan.NetworkTopVisitors)
	if err != nil {
		t.Fatalf("读取画像出错: %v", err)
	}

	verdict := EvaluateNetworkBan(NetworkSignals{
		Profile: profile,
		Budget:  NetworkBudget(),
		IsIPv6:  netmask.IsIPv6(ip),
	})

	if !verdict.ShouldBan {
		t.Fatalf("网段总量 %d 应触发封禁", profile.Total)
	}
	if verdict.BanNetwork {
		t.Error("流量集中时不应封整段，否则同网段的正常用户会被连坐")
	}
	if len(verdict.VisitorKeys) != 1 || verdict.VisitorKeys[0] != "abuser" {
		t.Errorf("应只封 abuser，实际为 %v", verdict.VisitorKeys)
	}
}

// mustScope 算出某 IP 所属网段，算不出即测试前提不成立
func mustScope(t *testing.T, ip string) string {
	t.Helper()

	scope, ok := NetworkScope(ip)
	if !ok {
		t.Fatalf("测试前提不成立：应能算出 %s 所属网段", ip)
	}
	return scope
}
