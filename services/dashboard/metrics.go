// Package dashboard 提供监控面板的数据。
//
// 四项指标分三个来源，各自取自最合适的地方，不另建一套汇总存储：
//
//   - 图书馆数量：查库即得（models.CountLibraries）
//   - 封禁数：内存名单直接给出（banlist.Count）
//   - 今日访问量、当前在线：Redis 计数，本包负责
//
// 只有后两项需要采集，因为它们是流量的函数，无处可查。放在 Redis 而非数据库：
// 这是每个请求都要递增的计数，写库会让它成为请求路径上的瓶颈。
//
// 都按自然日或时间窗分桶，键自身过期，故不需要任何清理任务——与限流计数同一套
// 语义：Redis 计数以天为单位刷新，丢失只影响当日。正因如此，本包不提供「累计
// 访问量」：那种数字要跨重启存活，而这份 Redis 有意不做持久化，拿它装长期数据
// 是错的。需要长期趋势应另存数据库，不该与当日计数混在一处。
package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// keyPrefix Redis 键前缀。与限流的 bf:rl: 分开，便于运维时区分用途。
const keyPrefix = "bf:dash:"

// onlineWindow 判定「在线」的时间窗。
//
// 取 5 分钟这个「刚才还在用」的常识区间。太短（如 1 分钟）会让正常读完一页的人
// 被算作离线，数字剧烈抖动；太长则失去「当前」的含义。
const onlineWindow = 5 * time.Minute

// onlineBucket 在线集合的分桶粒度。
//
// 每分钟一个桶，判定时取最近 onlineWindow 内的若干桶求并集。这样做而非用一个
// ZSET 按时间戳排序：后者更精确，但要定期清理过期成员，而分桶靠键自身过期，
// 无需任何清理逻辑。代价是边界最多一分钟误差，对「当前在线」这个量足够。
const onlineBucket = time.Minute

// 在线人数用 HyperLogLog 而非普通集合（Set）计数。
//
// 关键在于读取方式：PFCOUNT 接受多个键，直接在服务端给出并集基数，一条命令即得。
// 而 Redis 没有 SUNIONCARD——用 Set 就只能 SUNION 把成员全取回来自己数（成员是
// 64 位哈希，活跃时白搬一大坨），或者 SUNIONSTORE 落一个临时键再 SCARD，
// 那又要多一次写和一次清理。
//
// 代价是基数为近似值，且此处是对多个桶求并集，误差略大于单个 HLL 计数——
// 实测 150 人时差 1、5000 人时在 1% 以内。这对「当前在线」完全够用：它本是个
// 参考量，而 5 分钟窗口边界带来的偏差比这个大得多。
//
// 另一个好处是内存有上限：每个桶最多 12KB，而 Set 随人数线性增长。

// requestsKey 当日请求数，按自然日分桶。
//
// 这是个自增计数器，每个 API 请求加一。它不是「访问量」——一个人浏览一次会发出
// 十几个请求，故这个数字反映的是负载而非人数。判断是否被刷时有用：请求数远高于
// 访客数即说明有客户端在密集调用。
func requestsKey(now time.Time) string {
	return fmt.Sprintf("%sreq:%s", keyPrefix, now.Format("20060102"))
}

// visitorsKey 当日访客集合，按自然日分桶。
//
// 「今日访问量」问的是今天有多少人来过，故必须按访问者标识去重，而不能用请求数
// 充当——后者只反映请求次数。与在线人数同一套做法（HyperLogLog），
// 区别只在窗口：这里是一整个自然日，那里是最近 5 分钟。
func visitorsKey(now time.Time) string {
	return fmt.Sprintf("%svis:%s", keyPrefix, now.Format("20060102"))
}

// onlineKey 某一分钟的在线访问者集合
func onlineKey(now time.Time) string {
	return fmt.Sprintf("%sonline:%s", keyPrefix, now.Format("200601021504"))
}

// endOfDay 次日零点，作为当日计数键的过期时刻
func endOfDay(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, 1)
}

// RecordVisit 记一次访问：请求数加一，并把该访问者计入当日访客与当前在线。
//
// visitor 为访问者令牌哈希，为空时只计请求数：没有稳定标识就无从去重——
// 按 IP 计会把同一出口的多人算作一人，按请求计则会把一次页面加载的若干请求
// 算作若干人。这类请求很少，多为尚未领到令牌的首个请求。
//
// 三个计数各自回答不同的问题，故都要记：请求数是负载，访客数是「今天多少人来过」，
// 在线是「此刻多少人在用」。
//
// 全部命令在一个 pipeline 里，且不检查错误：这在每个 API 请求上执行，
// 监控指标的准确性不值得为它在请求路径上增加一个失败点。Redis 真的不可用时，
// 限流那边已经在告警了。
func RecordVisit(ctx context.Context, rdb *redis.Client, visitor string) {
	if rdb == nil {
		return
	}

	now := time.Now()
	expireAt := endOfDay(now)
	pipe := rdb.Pipeline()

	requests := requestsKey(now)
	pipe.Incr(ctx, requests)
	pipe.ExpireAt(ctx, requests, expireAt)

	if visitor != "" {
		visitors := visitorsKey(now)
		pipe.PFAdd(ctx, visitors, visitor)
		pipe.ExpireAt(ctx, visitors, expireAt)

		bucket := onlineKey(now)
		pipe.PFAdd(ctx, bucket, visitor)
		// 存活时间略长于判定窗口，保证窗口内的桶都还在
		pipe.Expire(ctx, bucket, onlineWindow+onlineBucket)
	}

	_, _ = pipe.Exec(ctx)
}

// Traffic 流量指标，取自 Redis。三项都在次日零点自然归零。
type Traffic struct {
	// VisitorsToday 当日访客数，按访问者标识去重。
	// 这是「今天多少人来过」——面板上的「今日访问」指的是它。
	VisitorsToday int64 `json:"visitors_today"`
	// RequestsToday 当日 API 请求数。
	//
	// 与访客数一并给出，因为两者的比值有意义：一个人正常浏览会发出十几个请求，
	// 若请求数远高于这个倍数，说明有客户端在密集调用。
	RequestsToday int64 `json:"requests_today"`
	// Online 最近 OnlineWindowMinutes 分钟内活跃的访问者数
	Online int64 `json:"online"`
	// OnlineWindowMinutes 在线判定所用的时间窗，供前端说明这个数字的含义
	OnlineWindowMinutes int `json:"online_window_minutes"`
	// Available Redis 是否可用。为假时上面各项均为零值，前端应显示「暂无数据」
	// 而非显示 0——后者会被误读为「真的没有访问」。
	Available bool `json:"available"`
}

// ReadTraffic 取回流量指标。
//
// Redis 不可用时返回 Available 为假的零值，不返回错误：面板另两项指标
// （图书馆数、封禁数）不依赖 Redis，不该被这一项拖垮。
func ReadTraffic(ctx context.Context, rdb *redis.Client) Traffic {
	traffic := Traffic{OnlineWindowMinutes: int(onlineWindow / time.Minute)}

	if rdb == nil {
		return traffic
	}

	now := time.Now()

	// 在线人数即最近若干分钟桶的并集基数，PFCOUNT 一条命令在服务端算完。
	// 不存在的桶被当作空集，故窗口跨越服务刚启动的时刻也不会出错。
	buckets := make([]string, 0, int(onlineWindow/onlineBucket))
	for offset := time.Duration(0); offset < onlineWindow; offset += onlineBucket {
		buckets = append(buckets, onlineKey(now.Add(-offset)))
	}

	pipe := rdb.Pipeline()
	requests := pipe.Get(ctx, requestsKey(now))
	visitors := pipe.PFCount(ctx, visitorsKey(now))
	online := pipe.PFCount(ctx, buckets...)

	// redis.Nil 表示键不存在，属正常情况（当日尚无访问）
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return traffic
	}

	traffic.Available = true
	traffic.RequestsToday, _ = requests.Int64()
	traffic.VisitorsToday = visitors.Val()
	traffic.Online = online.Val()

	return traffic
}
