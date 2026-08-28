package ratelimit

import (
	"context"
	"fmt"
	"time"

	"bookfinder-backend/utils/netmask"

	"github.com/redis/go-redis/v9"
)

// networkTotalKey 当日某网段的请求总量
func networkTotalKey(now time.Time, scope string) string {
	return fmt.Sprintf("%snet:%s:%s", keyPrefix, dateStamp(now), scope)
}

// networkVisitorsKey 当日某网段下各访问者令牌的请求量（ZSET，score 为请求数）。
//
// 用有序集合而非普通计数：判定「网段流量异常」之后还要回答「是哪几个设备造成的」，
// ZREVRANGE 一次即可取出流量最高的若干令牌。这正是「只封异常设备、
// 不连坐同段其他人」所需要的信息。
//
// 成员数即该网段当日出现过的令牌数，与既有的 visitorSetKey 同量级，
// 且同样按自然日过期，不引入新的增长风险。
func networkVisitorsKey(now time.Time, scope string) string {
	return fmt.Sprintf("%snetv:%s:%s", keyPrefix, dateStamp(now), scope)
}

// NetworkScope 网段判据的主体：来源 IP 所属网段。
//
// 与见习计数（probationScope）不同，这里 IPv4 也取 /24：网段判据的目的正是
// 发现「同一出口下的异常总量」，只看单个地址就失去了意义。
//
// 但要注意判据与处置是两件事：判据可以看 /24，处置默认只落到具体设备上，
// 只有在认不出异常设备时才退回网段级封禁，且 IPv4 不自动封 /24
// （见 EvaluateNetworkBan 的说明）。
func NetworkScope(ip string) (string, bool) {
	return netmask.PrefixOf(ip)
}

// RecordNetworkRequest 累计一次网段请求量，并登记该令牌在网段内的贡献。
//
// 请求路径上不走这里——那里由 CheckAndCollect 在限流判定的同一次往返里一并累计，
// 省下一个 RTT。此函数供不做限流判定、只需记一笔流量的场合使用。
//
// visitorKey 为空时只累加总量：无令牌的请求同样占用网段流量，
// 但没有可归属的设备，这一情形由见习额度负责。
func RecordNetworkRequest(ctx context.Context, rdb *redis.Client,
	ip, visitorKey string) error {

	if rdb == nil {
		return nil
	}

	scope, ok := NetworkScope(ip)
	if !ok {
		return nil
	}

	now := time.Now()
	expireAt := endOfDay(now)

	pipe := rdb.Pipeline()

	totalKey := networkTotalKey(now, scope)
	pipe.Incr(ctx, totalKey)
	pipe.ExpireAt(ctx, totalKey, expireAt)

	if visitorKey != "" {
		vKey := networkVisitorsKey(now, scope)
		pipe.ZIncrBy(ctx, vKey, 1, visitorKey)
		pipe.ExpireAt(ctx, vKey, expireAt)
	}

	_, err := pipe.Exec(ctx)

	return err
}

// VisitorLoad 一个访问者令牌在网段内的流量
type VisitorLoad struct {
	// VisitorKey 令牌哈希
	VisitorKey string
	// Requests 当日请求数
	Requests int
}

// NetworkProfile 某网段当日的流量画像，供判定「是否异常、异常出自哪几个设备」
type NetworkProfile struct {
	// Scope 网段
	Scope string
	// Total 当日请求总量
	Total int
	// Top 流量最高的若干令牌，按请求数降序
	Top []VisitorLoad
	// DistinctVisitors 当日出现过的不同令牌数
	DistinctVisitors int
}

// TopRequests Top 令牌的请求量合计
func (p NetworkProfile) TopRequests() int {
	sum := 0
	for _, load := range p.Top {
		sum += load.Requests
	}
	return sum
}

// ProfileNetwork 取回某网段当日的流量画像。
//
// topN 决定考察前几个令牌。取值不宜过大：一次封几十个令牌与封整个网段没有区别，
// 就失去了「精准」的意义。
func ProfileNetwork(ctx context.Context, rdb *redis.Client,
	ip string, topN int) (NetworkProfile, error) {

	profile := NetworkProfile{}

	if rdb == nil || topN < 1 {
		return profile, nil
	}

	scope, ok := NetworkScope(ip)
	if !ok {
		return profile, nil
	}
	profile.Scope = scope

	now := time.Now()
	vKey := networkVisitorsKey(now, scope)

	pipe := rdb.Pipeline()
	total := pipe.Get(ctx, networkTotalKey(now, scope))
	top := pipe.ZRevRangeWithScores(ctx, vKey, 0, int64(topN-1))
	distinct := pipe.ZCard(ctx, vKey)

	// 键不存在会返回 redis.Nil，属正常情况（该网段当日尚无请求），不视为错误
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return profile, err
	}

	profile.Total, _ = total.Int()
	profile.DistinctVisitors = int(distinct.Val())

	for _, member := range top.Val() {
		key, ok := member.Member.(string)
		if !ok || key == "" {
			continue
		}
		profile.Top = append(profile.Top, VisitorLoad{
			VisitorKey: key,
			Requests:   int(member.Score),
		})
	}

	return profile, nil
}

// ResetNetworkScope 清掉某网段当日的流量画像与告警标记，解封时调用。
//
// 不清的话，解封后该网段的总量仍然超标，下一个请求就会重新触发网段判定、
// 立刻再封一次。
//
// 入参是网段本身（如 "2001:db8::/64"），与封禁记录里存的网段标识一致。
// 注意不能传 IP：那会解析失败、静默什么都不做——正是「解封后立刻复发」的
// 一种成因。手上是 IP 时先经 NetworkScope 换算。
func ResetNetworkScope(ctx context.Context, rdb *redis.Client, scope string) error {
	if rdb == nil || scope == "" {
		return nil
	}

	now := time.Now()

	return rdb.Del(ctx,
		networkTotalKey(now, scope),
		networkVisitorsKey(now, scope),
		// 告警去重键一并清掉：解封后若该网段再次异常，应当重新告警
		networkAdviseKey(now, scope),
	).Err()
}

// networkAdviseKey 网段告警的去重键，按自然日分桶
func networkAdviseKey(now time.Time, scope string) string {
	return fmt.Sprintf("%snetadv:%s:%s", keyPrefix, dateStamp(now), scope)
}

// ShouldAdviseNetwork 判断是否该为该网段记一条告警，每个网段每天只记一次。
//
// 需要节流是因为判据是当日累计值：网段总量当天不会下降，故一旦越过阈值，
// 该网段的每个后续请求都会重新命中判定。不节流的话，一个异常网段能把
// 操作日志刷满，真正需要人工核查的那条反而被埋掉。
//
// Redis 不可用时返回 true：宁可重复告警，也不要在限流已失效时连告警一并丢掉。
func ShouldAdviseNetwork(ctx context.Context, rdb *redis.Client, ip string) bool {
	if rdb == nil {
		return true
	}

	scope, ok := NetworkScope(ip)
	if !ok {
		return false
	}

	now := time.Now()

	// SetNX 只在键不存在时写入，返回值即「本次是否是当天第一次告警」
	fresh, err := rdb.SetNX(ctx, networkAdviseKey(now, scope), "1",
		time.Until(endOfDay(now))).Result()
	if err != nil {
		return true
	}

	return fresh
}
