package dashboard

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// liveRedis 连本机 Redis；连不上则跳过——这些测试验证的是与真实 Redis 的交互
func liveRedis(t *testing.T) *redis.Client {
	t.Helper()

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 15})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("本机 Redis 不可用，跳过: %v", err)
	}

	// 用 DB 15 且每次清空，不碰生产数据
	rdb.FlushDB(context.Background())
	t.Cleanup(func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	})

	return rdb
}

// TestRecordAndReadRoundTrip 记账与读取应当对得上
func TestRecordAndReadRoundTrip(t *testing.T) {
	rdb := liveRedis(t)
	ctx := context.Background()

	// 三个不同访问者，其中一个来两次
	RecordVisit(ctx, rdb, "visitor-a")
	RecordVisit(ctx, rdb, "visitor-b")
	RecordVisit(ctx, rdb, "visitor-a")
	RecordVisit(ctx, rdb, "visitor-c")

	got := ReadTraffic(ctx, rdb)

	if !got.Available {
		t.Fatal("Redis 可用时 Available 应为真")
	}
	// 请求数按请求计，不去重
	if got.RequestsToday != 4 {
		t.Errorf("今日请求数 = %d，期望 4", got.RequestsToday)
	}
	// 访客数按人去重：a 来了两次仍算一人
	if got.VisitorsToday != 3 {
		t.Errorf("今日访客数 = %d，期望 3（a/b/c 去重）", got.VisitorsToday)
	}
	// 在线按访问者去重
	if got.Online != 3 {
		t.Errorf("在线人数 = %d，期望 3（a/b/c 去重）", got.Online)
	}
	if got.OnlineWindowMinutes != 5 {
		t.Errorf("在线窗口 = %d 分钟，期望 5", got.OnlineWindowMinutes)
	}
}

// TestVisitWithoutTokenCountsRequestsOnly 无令牌时只计请求数，
// 不计访客数与在线：没有稳定标识就无从去重
func TestVisitWithoutTokenCountsRequestsOnly(t *testing.T) {
	rdb := liveRedis(t)
	ctx := context.Background()

	RecordVisit(ctx, rdb, "")
	RecordVisit(ctx, rdb, "")

	got := ReadTraffic(ctx, rdb)
	if got.RequestsToday != 2 {
		t.Errorf("请求数 = %d，期望 2", got.RequestsToday)
	}
	if got.VisitorsToday != 0 {
		t.Errorf("无令牌不应计入访客数，实际为 %d", got.VisitorsToday)
	}
	if got.Online != 0 {
		t.Errorf("无令牌不应计入在线，实际为 %d", got.Online)
	}
}

// TestVisitorsDedupeAcrossManyRequests 同一个人发很多请求，访客数仍是 1。
//
// 这是「今日访问量」与「今日请求数」的分界：一次页面加载就有十几个请求，
// 用请求数当访问量会把人数放大一个数量级。
func TestVisitorsDedupeAcrossManyRequests(t *testing.T) {
	rdb := liveRedis(t)
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		RecordVisit(ctx, rdb, "same-visitor")
	}

	got := ReadTraffic(ctx, rdb)
	if got.VisitorsToday != 1 {
		t.Errorf("访客数 = %d，期望 1（同一人的 50 次请求）", got.VisitorsToday)
	}
	if got.RequestsToday != 50 {
		t.Errorf("请求数 = %d，期望 50", got.RequestsToday)
	}
}

// TestOnlineWindowExcludesOldBuckets 超出窗口的桶不该计入。
//
// 直接写一个窗口之外的桶键，确认读取时不会把它算进来——否则「当前在线」
// 会变成「今天来过的所有人」。
func TestOnlineWindowExcludesOldBuckets(t *testing.T) {
	rdb := liveRedis(t)
	ctx := context.Background()

	now := time.Now()
	// 窗口内：应计入
	rdb.PFAdd(ctx, onlineKey(now), "recent")
	// 窗口外（10 分钟前，窗口是 5 分钟）：不该计入
	rdb.PFAdd(ctx, onlineKey(now.Add(-10*time.Minute)), "stale")

	got := ReadTraffic(ctx, rdb)
	if got.Online != 1 {
		t.Errorf("在线 = %d，期望 1（只算窗口内的）", got.Online)
	}
}

// TestDailyKeysExpireNextDay 两个当日计数键都须设了次日过期，
// 否则每天各留一个键、永不回收
func TestDailyKeysExpireNextDay(t *testing.T) {
	rdb := liveRedis(t)
	ctx := context.Background()

	RecordVisit(ctx, rdb, "visitor")

	now := time.Now()
	for label, key := range map[string]string{
		"请求数": requestsKey(now),
		"访客数": visitorsKey(now),
	} {
		ttl, err := rdb.TTL(ctx, key).Result()
		if err != nil {
			t.Fatalf("%s：取 TTL 失败: %v", label, err)
		}
		if ttl <= 0 {
			t.Errorf("%s键没有过期时间，TTL = %v", label, ttl)
		}
		if ttl > 24*time.Hour+time.Minute {
			t.Errorf("%s键过期时间 %v 超过一天，不该跨日存活", label, ttl)
		}
	}
}

// TestOnlineBucketExpires 在线桶须过期，且存活时间要覆盖判定窗口
func TestOnlineBucketExpires(t *testing.T) {
	rdb := liveRedis(t)
	ctx := context.Background()

	RecordVisit(ctx, rdb, "visitor")

	ttl, err := rdb.TTL(ctx, onlineKey(time.Now())).Result()
	if err != nil {
		t.Fatalf("取 TTL 失败: %v", err)
	}
	if ttl < onlineWindow {
		t.Errorf("桶存活 %v 短于判定窗口 %v，窗口内的桶会提前消失", ttl, onlineWindow)
	}
}

// TestReadWithoutRedis Redis 不可用时应返回 Available 为假，而不是 panic 或报错
func TestReadWithoutRedis(t *testing.T) {
	got := ReadTraffic(context.Background(), nil)

	if got.Available {
		t.Error("Redis 为 nil 时 Available 应为假")
	}
	// 窗口说明仍要给出：前端据它渲染文案
	if got.OnlineWindowMinutes != 5 {
		t.Errorf("窗口说明丢失: %d", got.OnlineWindowMinutes)
	}
}

// TestRecordWithoutRedisIsSafe Redis 为 nil 时记账不该 panic：
// 这在每个请求上执行
func TestRecordWithoutRedisIsSafe(t *testing.T) {
	RecordVisit(context.Background(), nil, "visitor")
}

// TestOnlineUnionsAcrossBuckets 跨分钟桶的访问者应当合并去重。
//
// 这是本包真正要保证的事：桶按分钟切分，而「当前在线」是窗口内各桶的并集。
// 若并集逻辑错了，一个持续浏览的人会在跨过整分时被算作两个人。
//
// 直接写入指定的桶键，不依赖 time.Now()：此前用「循环记 5000 次」来测，
// 结果在跨越整分或跨日的那一刻会失败——那测的是 Redis 的 HLL 精度与运气，
// 不是本包的逻辑。
func TestOnlineUnionsAcrossBuckets(t *testing.T) {
	rdb := liveRedis(t)
	ctx := context.Background()

	now := time.Now()

	// 三个相邻分钟桶，其中 shared 出现在两个桶里，应当只算一次
	rdb.PFAdd(ctx, onlineKey(now), "alice", "shared")
	rdb.PFAdd(ctx, onlineKey(now.Add(-time.Minute)), "bob", "shared")
	rdb.PFAdd(ctx, onlineKey(now.Add(-2*time.Minute)), "carol")

	got := ReadTraffic(ctx, rdb)

	// alice + bob + carol + shared = 4
	if got.Online != 4 {
		t.Errorf("在线 = %d，期望 4（三桶并集去重后）", got.Online)
	}
}

// TestOnlineCoversWholeWindow 窗口内每一个分钟桶都要被读到。
//
// 逐个桶单独放一个访问者，故漏读任何一个桶都会让结果偏小——若只读当前桶，
// 「当前在线」就退化成「这一分钟内在线」。
func TestOnlineCoversWholeWindow(t *testing.T) {
	rdb := liveRedis(t)
	ctx := context.Background()

	now := time.Now()
	buckets := int(onlineWindow / onlineBucket)

	for i := 0; i < buckets; i++ {
		rdb.PFAdd(ctx, onlineKey(now.Add(-time.Duration(i)*onlineBucket)),
			fmt.Sprintf("visitor-%d", i))
	}

	got := ReadTraffic(ctx, rdb)
	if got.Online != int64(buckets) {
		t.Errorf("在线 = %d，期望 %d（窗口内每个桶各一人）", got.Online, buckets)
	}
}
