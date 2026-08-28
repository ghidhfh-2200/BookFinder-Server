package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"bookfinder-backend/logger"
	"bookfinder-backend/types"
	"bookfinder-backend/utils/netmask"

	"github.com/redis/go-redis/v9"
)

// probationScope 见习计数的主体，按协议区分粒度。
//
// IPv6 取 /64，IPv4 取精确地址。这个区别不是实现细节，而是两种协议下
// 「一个人」对应的东西本就不同：
//
//   - IPv6：终端通常独占一个 /64，段内换址不受任何限制、也不需要任何权限。
//     按单个地址计等于没有额度——实测同一 /64 内换 10 个地址即得到 10 份完整配额。
//     按 /64 计才是「按一个人计」。
//   - IPv4：地址由 ISP 或 NAT 决定，攻击者换不了自己的地址，故本就没有上述漏洞。
//     而一个 /24 背后可能是整个校园网出口，按 /24 计会让几百人共享一份额度——
//     一个人刷爆，同段其他人全都领不到令牌。这是无谓的连坐。
//
// 与封禁粒度的取舍一致（IPv6 封 /64、IPv4 只封精确 IP），同一个道理贯穿计数与处置两处。
func probationScope(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		// IP 不合法时退回原值，宁可粒度偏细也不要漏掉计数
		return ip
	}

	// IPv4 与 IPv4-mapped IPv6 都按精确地址计
	if v4 := parsed.To4(); v4 != nil {
		return v4.String()
	}

	if prefix, ok := netmask.PrefixOf(ip); ok {
		return prefix
	}
	return ip
}

// ProbationScopeOf 返回某个 IP 对应的见习计数主体，供解封与测试换算。
// 粒度见 probationScope：IPv6 取 /64，IPv4 取精确地址。
func ProbationScopeOf(ip string) string {
	return probationScope(ip)
}

// probationKey 见习配额的每日计数键，主体见 probationScope
func probationKey(now time.Time, scope string) string {
	return fmt.Sprintf("%sp:%s:%s", keyPrefix, dateStamp(now), scope)
}

// probationBurstKey 见习配额的突发窗口键，主体见 probationScope
func probationBurstKey(scope string) string {
	return fmt.Sprintf("%spb:%s", keyPrefix, scope)
}

// CheckProbation 判定一个尚未持有有效令牌的来源能否继续。
//
// 计数主体见 probationScope：IPv6 按 /64，IPv4 按精确地址。因此清 cookie 换令牌
// 不再是免费的，IPv6 下换地址也不再能重置额度。额度内放行并允许下发令牌，
// 耗尽则拒绝且不下发——若此时仍补发令牌，攻击者只要每次带一个乱码令牌
// 就能白拿正式配额。
//
// Redis 不可用时放行（fail-open），与其余限流一致：此时整套限流都失效，
// 兜底靠封禁与并发闸。
func CheckProbation(ctx context.Context, rdb *redis.Client, ip string) (Decision, error) {
	probation := Probation()
	if probation.Daily < 1 {
		return Decision{Allowed: true}, nil
	}

	now := time.Now()
	expireAt := endOfDay(now)
	burstWindow := time.Duration(probation.BurstWindowSeconds) * time.Second

	scope := probationScope(ip)
	dKey := probationKey(now, scope)
	bKey := probationBurstKey(scope)

	pipe := rdb.Pipeline()
	daily := pipe.Incr(ctx, dKey)
	pipe.ExpireAt(ctx, dKey, expireAt)
	// 突发窗口用 Lua 完成「累加 + 仅在无过期时设置」：ExpireNX 需要 Redis 7.0，
	// 低版本上会报错，而限流出错即放行——整套闸门会静默失效（见 incrWithTTLScript）
	burst := pipe.Eval(ctx, incrWithTTLScript, []string{bKey}, burstWindow.Milliseconds())

	// 只有取不到计数才真正无法判定；设过期一类的辅助命令失败不影响本次判定
	_, execErr := pipe.Exec(ctx)

	dailyUsed, err := countFrom(daily.Result())
	if err != nil {
		return Decision{Allowed: true}, err
	}
	burstUsed, err := countFrom(burst.Result())
	if err != nil {
		return Decision{Allowed: true}, err
	}

	if execErr != nil && !errors.Is(execErr, redis.Nil) {
		logger.Warnf("见习计数的辅助命令未全部成功，已照常判定 (%s): %v", scope, execErr)
	}

	if dailyUsed > probation.Daily {
		return Decision{
			Allowed:    false,
			Reason:     "请启用 Cookie 后重试；本来源今日的匿名请求次数已达上限",
			DailyUsed:  dailyUsed,
			DailyLimit: probation.Daily,
			RetryAfter: time.Until(expireAt),
		}, nil
	}

	if probation.Burst > 0 && burstUsed > probation.Burst {
		return Decision{
			Allowed: false,
			Reason: fmt.Sprintf("请求过于频繁，请在 %d 秒后重试",
				probation.BurstWindowSeconds),
			DailyUsed:  dailyUsed,
			DailyLimit: probation.Daily,
			Remaining:  max(probation.Daily-dailyUsed, 0),
			RetryAfter: burstWindow,
		}, nil
	}

	return allow(dailyUsed, probation.Daily), nil
}

// CountProbation 查询当日该来源（IPv6 为其 /64，IPv4 为其地址）的见习用量，供自动封禁判定。
//
// 见习额度耗尽后仍反复请求，是「不带 cookie 刷接口」的典型特征：
// 正常用户的第一个请求就换到了正式令牌，根本走不到这一步。
func CountProbation(ctx context.Context, rdb *redis.Client, ip string) (int, error) {
	return getInt(ctx, rdb, probationKey(time.Now(), probationScope(ip)))
}

// ResetProbationScope 清掉某个见习计数主体的计数，解封时调用。
//
// 入参是计数主体（见 probationScope：IPv6 为其 /64、IPv4 为精确地址），
// 而非任意 IP——封禁记录里存的网段标识本就是这个粒度，可直接传入；
// 若手上是一个 IP，先经 ProbationScopeOf 换算。
//
// 主体须与计数时一致，否则删的是不存在的键、解封后该来源仍然领不到令牌。
func ResetProbationScope(ctx context.Context, rdb *redis.Client, scope string) error {
	if rdb == nil || scope == "" {
		return nil
	}
	now := time.Now()
	return rdb.Del(ctx, probationKey(now, scope), probationBurstKey(scope)).Err()
}

// ProbationVerdict 见习额度超限的封禁判定结果
type ProbationVerdict struct {
	// ShouldBan 是否应当封禁
	ShouldBan bool
	// Reason 触发的规则，写入封禁记录的 Reason
	Reason string
	// Detail 触发时的具体数据，写入封禁记录的 Detail，便于复核误判
	Detail string
}

// EvaluateProbationBan 判定「见习额度用尽后仍反复请求」是否该封禁。
//
// 这是「不带 Cookie 刷接口」的典型特征：正常用户的第一个请求就换到了正式令牌，
// 根本走不到反复消耗见习额度这一步；反复消耗意味着客户端刻意不保存令牌，
// 以求每次都拿一份新配额。
//
// 判定放在见习路径上而非限流中间件上，是这条规则能成立的前提。放在后者时：
//
//   - 见习额度耗尽的请求会被 passProbation 直接拦下，走不到限流中间件，
//     于是真正刷接口的那个客户端永远不会被这条规则判到——它是死代码；
//   - 而每个持有效令牌的请求都会读一次该来源的见习用量，于是同一出口下
//     累计够数之后，任何一个正常用户的下一个请求都会把自己封掉。
//
// 放在这里，打中的就是那个不保存令牌的客户端本身。处置粒度同样按 probationScope
// 走（IPv6 的 /64、IPv4 的精确地址），与计数主体一致。
func EvaluateProbationBan(used int) ProbationVerdict {
	autoBan := AutoBan()
	if !autoBan.Enabled || autoBan.ProbationOverflowMultiplier <= 0 {
		return ProbationVerdict{}
	}

	probation := Probation()
	if probation.Daily < 1 {
		return ProbationVerdict{}
	}

	threshold := probation.Daily * autoBan.ProbationOverflowMultiplier
	if used < threshold {
		return ProbationVerdict{}
	}

	return ProbationVerdict{
		ShouldBan: true,
		Reason:    "持续以匿名身份反复请求",
		Detail: fmt.Sprintf("当日见习请求 %d 次（含被拒），达额度 %d 的 %d 倍阈值，"+
			"疑似刻意不保存访问者令牌以重置配额",
			used, probation.Daily, autoBan.ProbationOverflowMultiplier),
	}
}

// ProbationBanIdents 见习超限时应写入的封禁标识。
//
// 粒度与计数主体一致（见 probationScope）：IPv6 封其 /64——段内换址不受任何限制，
// 只封精确地址等于没封；IPv4 只封精确地址——一个 /24 背后可能是整个校园网出口。
//
// 此时对方没有可信令牌（正是因此才走见习路径），故没有令牌标识可写。
func ProbationBanIdents(ip string) []types.BanIdent {
	scope := probationScope(ip)
	if scope == "" {
		return nil
	}

	if netmask.IsIPv6(ip) {
		// probationScope 对 IPv6 返回的就是 /64
		return []types.BanIdent{{Kind: types.IdentIPNet, Value: scope}}
	}

	return []types.BanIdent{{Kind: types.IdentIP, Value: scope}}
}
