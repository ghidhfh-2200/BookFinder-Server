package ratelimit

import (
	"context"
	"testing"
	"time"

	"bookfinder-backend/types"

	"github.com/redis/go-redis/v9"
)

// testRedis 连接本地 Redis，不可用时跳过测试。
// 「解封后立刻复发」这类问题只有连真实 Redis 才验得出，故不打桩。
func testRedis(t *testing.T) *redis.Client {
	t.Helper()

	client := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379",
		// 用高编号库，避免碰到实际数据
		DB: 15,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		t.Skipf("Redis 不可用，跳过: %v", err)
	}

	t.Cleanup(func() {
		client.FlushDB(context.Background())
		client.Close()
	})

	client.FlushDB(ctx)

	return client
}

// setTestRules 设置一组便于测试的规则
func setTestRules(t *testing.T) {
	t.Helper()

	mu.Lock()
	rules = types.RateRules{
		Enabled: true,
		Limits: map[types.LimitCategory]types.CategoryLimit{
			types.CategoryReport: {Daily: 5, Burst: 3, BurstWindowSeconds: 60},
		},
		AutoBan: types.AutoBanRules{
			Enabled:                 true,
			DailyOverflowMultiplier: 3, // 15 次触发封禁
			BurstViolations:         5,
			DuplicateReports:        10,
		},
	}
	mu.Unlock()
}

// TestResetAfterUnbanStopsImmediateReban 解封后不应立刻被重新封禁。
//
// 这是解封的核心语义：封禁判据都是当日累计值，只删封禁记录不清计数，
// 解封后第一个请求就会重新命中规则、立刻再封一次。
func TestResetAfterUnbanStopsImmediateReban(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t)

	ctx := context.Background()
	const visitor = "visitor-reban"
	const ip = "10.0.0.1"

	// 猛捶到触发封禁阈值：配额 5，倍数 3，即 15 次
	for range 20 {
		if _, _, err := CheckAndCollect(ctx, rdb, types.CategoryReport, visitor, ip, visitor); err != nil {
			t.Fatalf("Check 出错: %v", err)
		}
	}

	// 确认此时确实会判定为应封禁
	before := banVerdictFor(t, ctx, rdb, visitor, ip)
	if !before.ShouldBan {
		t.Fatal("捶满 20 次后应判定为封禁，测试前提不成立")
	}

	// 解封：主体名下有 IP 与令牌两类标识，各自重置
	if err := ResetAfterUnban(ctx, rdb, ip); err != nil {
		t.Fatalf("ResetAfterUnban 出错: %v", err)
	}
	if err := ResetVisitorAfterUnban(ctx, rdb, visitor, ""); err != nil {
		t.Fatalf("ResetVisitorAfterUnban 出错: %v", err)
	}

	// 解封后再来一个请求，不应又被判为封禁
	if _, _, err := CheckAndCollect(ctx, rdb, types.CategoryReport, visitor, ip, visitor); err != nil {
		t.Fatalf("解封后 Check 出错: %v", err)
	}

	after := banVerdictFor(t, ctx, rdb, visitor, ip)
	if after.ShouldBan {
		t.Errorf("解封后不应立刻重新封禁，实际仍判定为封禁: %s", after.Detail)
	}
}

// TestResetAfterUnbanCapsToQuota 解封把每日计数恢复到配额值。
// 额度视为已用完（当日仍受限流拦阻，符合已经用过的事实），
// 但不再达到「配额 × 倍数」的封禁阈值。
func TestResetAfterUnbanCapsToQuota(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t)

	ctx := context.Background()
	const visitor = "visitor-cap"
	const ip = "10.0.0.2"

	for range 20 {
		CheckAndCollect(ctx, rdb, types.CategoryReport, visitor, ip, visitor)
	}

	if err := ResetAfterUnban(ctx, rdb, ip); err != nil {
		t.Fatalf("ResetAfterUnban 出错: %v", err)
	}
	if err := ResetVisitorAfterUnban(ctx, rdb, visitor, ""); err != nil {
		t.Fatalf("ResetVisitorAfterUnban 出错: %v", err)
	}

	used, err := getInt(ctx, rdb, dailyKey(time.Now(), types.CategoryReport, visitor))
	if err != nil {
		t.Fatalf("读取每日计数出错: %v", err)
	}

	if used != 5 {
		t.Errorf("每日计数应封顶到配额 5，实际为 %d", used)
	}
}

// TestResetAfterUnbanClearsBanSignals 按 IP 计的封禁判据应清零。
// 这些判据纯粹用于封禁，留着会让解封失效。
// 按令牌计的那些由 ResetVisitorAfterUnban 负责（见下方用例）。
func TestResetAfterUnbanClearsBanSignals(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t)

	ctx := context.Background()
	const visitor = "visitor-signals"
	const ip = "10.0.0.3"

	// 制造突发违规
	for range 10 {
		CheckAndCollect(ctx, rdb, types.CategoryReport, visitor, ip, visitor)
	}
	// 制造重复报告记录
	for range 3 {
		if _, err := RecordDuplicate(ctx, rdb, ip); err != nil {
			t.Fatalf("RecordDuplicate 出错: %v", err)
		}
	}

	if violations, _ := CountViolations(ctx, rdb, types.CategoryReport, visitor); violations == 0 {
		t.Fatal("测试前提不成立：应已产生突发违规")
	}

	if err := ResetAfterUnban(ctx, rdb, ip); err != nil {
		t.Fatalf("ResetAfterUnban 出错: %v", err)
	}
	if err := ResetVisitorAfterUnban(ctx, rdb, visitor, ""); err != nil {
		t.Fatalf("ResetVisitorAfterUnban 出错: %v", err)
	}

	if violations, _ := CountViolations(ctx, rdb, types.CategoryReport, visitor); violations != 0 {
		t.Errorf("突发违规应清零，实际为 %d", violations)
	}
	if duplicates, _ := CountDuplicates(ctx, rdb, ip); duplicates != 0 {
		t.Errorf("重复报告应清零，实际为 %d", duplicates)
	}
}

// TestResetAfterUnbanLeavesNeighborsAlone 解封一个人不应动同出口其他人的计数。
//
// 旧实现按 IP 扫出「当日用过的全部令牌」一并重置，于是解封一个人等于顺手清掉
// 共用出口所有人当天的剩余额度——校园网、图书馆 Wi-Fi 下这就是一片人。
// 现在按被解封主体名下的标识分别重置，邻居不受影响。
func TestResetAfterUnbanLeavesNeighborsAlone(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t)

	ctx := context.Background()
	const ip = "10.0.0.4"
	const culprit = "visitor-culprit"
	const neighbor = "visitor-neighbor"

	// 两个令牌共用同一个出口 IP，各自超出配额（配额 5）
	for _, visitor := range []string{culprit, neighbor} {
		for range 20 {
			CheckAndCollect(ctx, rdb, types.CategoryReport, visitor, ip, visitor)
		}
	}

	// 只解封 culprit：重置其令牌，以及该出口 IP 上按 IP 计的判据
	if err := ResetAfterUnban(ctx, rdb, ip); err != nil {
		t.Fatalf("ResetAfterUnban 出错: %v", err)
	}
	if err := ResetVisitorAfterUnban(ctx, rdb, culprit, ""); err != nil {
		t.Fatalf("ResetVisitorAfterUnban 出错: %v", err)
	}

	if used, _ := getInt(ctx, rdb, dailyKey(time.Now(), types.CategoryReport, culprit)); used != 5 {
		t.Errorf("被解封令牌的计数应封顶到配额 5，实际为 %d", used)
	}

	// 邻居的计数原样保留：他既没被封，也不该因别人解封而白得额度
	if used, _ := getInt(ctx, rdb, dailyKey(time.Now(), types.CategoryReport, neighbor)); used != 20 {
		t.Errorf("同出口其他令牌的计数不应被改动，应为 20，实际为 %d", used)
	}
}

// TestResetVisitorRemovesNetworkContribution 解封令牌时应把它从网段画像里移除。
//
// 网段判定靠 ZSET 里的分数认出「异常设备」。不移除的话，解封后第一个请求
// 就会重新把它排进 Top N、立刻再封一次——而这类主体名下只有令牌标识，
// 管理员除了反复解封别无办法。
func TestResetVisitorRemovesNetworkContribution(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t)

	ctx := context.Background()
	const ip = "2001:db8:aa::1"
	const visitor = "visitor-in-network"

	for range 30 {
		if err := RecordNetworkRequest(ctx, rdb, ip, visitor); err != nil {
			t.Fatalf("RecordNetworkRequest 出错: %v", err)
		}
	}

	scope, ok := NetworkScope(ip)
	if !ok {
		t.Fatal("测试前提不成立：应能算出网段")
	}

	profile, err := ProfileNetwork(ctx, rdb, ip, 5)
	if err != nil {
		t.Fatalf("ProfileNetwork 出错: %v", err)
	}
	if len(profile.Top) == 0 {
		t.Fatal("测试前提不成立：该令牌应已计入网段画像")
	}

	if err := ResetVisitorAfterUnban(ctx, rdb, visitor, scope); err != nil {
		t.Fatalf("ResetVisitorAfterUnban 出错: %v", err)
	}

	profile, err = ProfileNetwork(ctx, rdb, ip, 5)
	if err != nil {
		t.Fatalf("ProfileNetwork 出错: %v", err)
	}
	for _, load := range profile.Top {
		if load.VisitorKey == visitor {
			t.Errorf("解封后该令牌仍留在网段画像里，贡献 %d 次", load.Requests)
		}
	}
}

// TestResetVisitorTolerateNilRedis Redis 不可用与空令牌都应静默返回
func TestResetVisitorTolerateNilRedis(t *testing.T) {
	if err := ResetVisitorAfterUnban(context.Background(), nil, "v", ""); err != nil {
		t.Errorf("Redis 为 nil 时应静默返回，实际报错: %v", err)
	}
}

// TestResetAfterUnbanKeepsUnderQuotaCount 未超配额的计数不应被抬高
func TestResetAfterUnbanKeepsUnderQuotaCount(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t)

	ctx := context.Background()
	const visitor = "visitor-light"
	const ip = "10.0.0.5"

	// 只用 2 次，远未超配额 5
	for range 2 {
		CheckAndCollect(ctx, rdb, types.CategoryReport, visitor, ip, visitor)
	}

	if err := ResetAfterUnban(ctx, rdb, ip); err != nil {
		t.Fatalf("ResetAfterUnban 出错: %v", err)
	}

	used, _ := getInt(ctx, rdb, dailyKey(time.Now(), types.CategoryReport, visitor))
	if used != 2 {
		t.Errorf("未超配额的计数应保持原样（2），实际为 %d", used)
	}
}

// TestResetAfterUnbanTolerateNilRedis Redis 不可用时不应报错
func TestResetAfterUnbanTolerateNilRedis(t *testing.T) {
	if err := ResetAfterUnban(context.Background(), nil, "10.0.0.6"); err != nil {
		t.Errorf("Redis 为 nil 时应静默返回，实际报错: %v", err)
	}
}

// banVerdictFor 按当前计数算出封禁判定，模拟中间件的判定过程
func banVerdictFor(t *testing.T, ctx context.Context, rdb *redis.Client,
	visitorKey, ip string) BanVerdict {
	t.Helper()

	used, _ := getInt(ctx, rdb, dailyKey(time.Now(), types.CategoryReport, visitorKey))
	violations, _ := CountViolations(ctx, rdb, types.CategoryReport, visitorKey)
	duplicates, _ := CountDuplicates(ctx, rdb, ip)

	limit, _ := LimitFor(types.CategoryReport)

	return EvaluateBan(Signals{
		Category:         types.CategoryReport,
		DailyUsed:        used,
		DailyLimit:       limit.Daily,
		BurstViolations:  violations,
		DuplicateReports: duplicates,
	})
}
