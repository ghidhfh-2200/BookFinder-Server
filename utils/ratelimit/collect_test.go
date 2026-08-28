package ratelimit

import (
	"context"
	"strings"
	"testing"
	"time"

	"bookfinder-backend/types"

	"github.com/redis/go-redis/v9"
)

// TestCheckAndCollectDecisionSequence 逐次调用应走完「放行 → 撞突发 → 用满配额」。
//
// 合并往返不能改变判定语义，故此处把整个序列的每一步都钉住：
// 配额 5、突发 3/60s，前 3 次放行，第 4~5 次因突发被拒，第 6 次起配额也用尽。
//
// 注意每日计数在被拒时仍然累加（反映尝试量），故第 6 次之后 DailyUsed 会超过 5——
// 那正是封禁规则一识别「配额用尽后仍反复请求」的依据。
func TestCheckAndCollectDecisionSequence(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t) // report: daily=5 burst=3/60s

	ctx := context.Background()
	const visitor = "v-sequence"
	const ip = "10.1.0.1"

	for i := 1; i <= 8; i++ {
		decision, _, err := CheckAndCollect(ctx, rdb, types.CategoryReport, visitor, ip, visitor)
		if err != nil {
			t.Fatalf("第 %d 次出错: %v", i, err)
		}

		if decision.DailyUsed != i {
			t.Errorf("第 %d 次的每日计数应为 %d，实际为 %d", i, i, decision.DailyUsed)
		}

		switch {
		case i <= 3:
			if !decision.Allowed {
				t.Errorf("第 %d 次仍在突发额度内，应放行，实际被拒: %s", i, decision.Reason)
			}
			if decision.Remaining != 5-i {
				t.Errorf("第 %d 次剩余额度应为 %d，实际为 %d", i, 5-i, decision.Remaining)
			}
		case i <= 5:
			// 突发额度耗尽而每日配额尚有余量
			if decision.Allowed {
				t.Errorf("第 %d 次超出突发额度，应被拒", i)
			}
			if !strings.Contains(decision.Reason, "频繁") {
				t.Errorf("第 %d 次应因突发被拒，实际原因: %q", i, decision.Reason)
			}
		default:
			// 每日配额也用尽：此时应报配额而非突发，否则用户会以为等几十秒就能继续
			if decision.Allowed {
				t.Errorf("第 %d 次已超每日配额，应被拒", i)
			}
			if !strings.Contains(decision.Reason, "上限") {
				t.Errorf("第 %d 次应因每日配额被拒，实际原因: %q", i, decision.Reason)
			}
			if decision.Remaining != 0 {
				t.Errorf("第 %d 次剩余额度应为 0，实际为 %d", i, decision.Remaining)
			}
		}
	}
}

// TestCheckAndCollectCountsViolationsSameTurn 违规计数必须在同一次往返里递增。
//
// 若递增晚于读取，「本次撞了突发限制」这一笔要等下一个请求才被看见——
// 于是封禁判定总是慢一步，攒够阈值的时刻也随之推迟。
func TestCheckAndCollectCountsViolationsSameTurn(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t) // burst=3

	ctx := context.Background()
	const visitor = "v-violation"
	const ip = "10.1.0.3"

	var lastSignals Signals
	for i := 1; i <= 5; i++ {
		_, signals, err := CheckAndCollect(ctx, rdb, types.CategoryReport, visitor, ip, visitor)
		if err != nil {
			t.Fatalf("第 %d 次出错: %v", i, err)
		}
		lastSignals = signals

		// 前 3 次在突发额度内，不该记违规
		if i <= 3 && signals.BurstViolations != 0 {
			t.Errorf("第 %d 次仍在突发额度内，不应记违规，实际为 %d",
				i, signals.BurstViolations)
		}
		// 第 4 次起撞突发限制，当次就该看到计数
		if i == 4 && signals.BurstViolations != 1 {
			t.Errorf("第 4 次撞突发限制，当次即应看到违规计数 1，实际为 %d",
				signals.BurstViolations)
		}
	}

	if lastSignals.BurstViolations != 2 {
		t.Errorf("5 次调用应累计 2 次违规，实际为 %d", lastSignals.BurstViolations)
	}

	// 与独立查询的结果一致
	standalone, _ := CountViolations(ctx, rdb, types.CategoryReport, visitor)
	if standalone != lastSignals.BurstViolations {
		t.Errorf("合并路径的违规数 %d 与独立查询的 %d 不一致",
			lastSignals.BurstViolations, standalone)
	}
}

// TestCheckAndCollectRecordsNetworkProfile 网段画像应在同一次往返里累计
func TestCheckAndCollectRecordsNetworkProfile(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t)

	ctx := context.Background()
	const ip = "2001:db8:cafe::1"
	const visitor = "v-network"

	for range 3 {
		if _, _, err := CheckAndCollect(ctx, rdb, types.CategoryReport, visitor, ip, visitor); err != nil {
			t.Fatalf("CheckAndCollect 出错: %v", err)
		}
	}

	profile, err := ProfileNetwork(ctx, rdb, ip, 5)
	if err != nil {
		t.Fatalf("ProfileNetwork 出错: %v", err)
	}
	if profile.Total != 3 {
		t.Errorf("网段总量应为 3，实际为 %d", profile.Total)
	}
	if len(profile.Top) != 1 || profile.Top[0].VisitorKey != visitor {
		t.Errorf("应记下该令牌的贡献，实际为 %v", profile.Top)
	}
	if profile.Top[0].Requests != 3 {
		t.Errorf("该令牌的贡献应为 3，实际为 %d", profile.Top[0].Requests)
	}
}

// TestCheckAndCollectPicksUpDuplicates 重复报告数应被一并取回
func TestCheckAndCollectPicksUpDuplicates(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t)

	ctx := context.Background()
	const ip = "10.1.0.4"

	for range 4 {
		if _, err := RecordDuplicate(ctx, rdb, ip); err != nil {
			t.Fatalf("RecordDuplicate 出错: %v", err)
		}
	}

	_, signals, err := CheckAndCollect(ctx, rdb, types.CategoryReport, "v-dup", ip, "v-dup")
	if err != nil {
		t.Fatalf("CheckAndCollect 出错: %v", err)
	}
	if signals.DuplicateReports != 4 {
		t.Errorf("重复报告数应为 4，实际为 %d", signals.DuplicateReports)
	}
}

// TestCheckAndCollectFailsOpen Redis 不可用时必须放行。
// 限流失效胜过整站不可写——兜底靠封禁名单与并发闸。
func TestCheckAndCollectFailsOpen(t *testing.T) {
	setTestRules(t)

	dead := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 200 * time.Millisecond,
	})
	defer dead.Close()

	decision, signals, err := CheckAndCollect(context.Background(), dead,
		types.CategoryReport, "v-dead", "10.1.0.5", "v-dead")

	if !decision.Allowed {
		t.Error("Redis 不可用时应放行")
	}
	if err == nil {
		t.Error("应返回错误供调用方记日志")
	}
	// 判据取不到时按 0 计，不能凭空产生封禁依据
	if signals.BurstViolations != 0 || signals.DuplicateReports != 0 {
		t.Errorf("取不到判据时应为 0，实际为 %+v", signals)
	}
}

// TestCheckAndCollectSkipsUnknownCategory 未配置的类别直接放行
func TestCheckAndCollectSkipsUnknownCategory(t *testing.T) {
	rdb := testRedis(t)
	setTestRules(t)

	decision, _, err := CheckAndCollect(context.Background(), rdb,
		types.LimitCategory("nonexistent"), "v", "10.1.0.6", "v")
	if err != nil {
		t.Fatalf("未配置的类别不应报错: %v", err)
	}
	if !decision.Allowed {
		t.Error("未配置配额的类别应放行")
	}
}
