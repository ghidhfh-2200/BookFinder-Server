package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"bookfinder-backend/logger"
	"bookfinder-backend/types"

	"github.com/redis/go-redis/v9"
)

// keyPrefix Redis 键前缀，便于与其他用途的键区分
const keyPrefix = "bf:rl:"

// Decision 一次限流判定的结果
type Decision struct {
	// Allowed 是否放行
	Allowed bool
	// Reason 被拒原因，供响应文案使用
	Reason string
	// DailyUsed 当日尝试次数，含被限流拒绝的那些。
	// 超出配额后的请求仍会累加，故此值可以远大于 DailyLimit——
	// 自动封禁规则一正是据此识别「配额用尽后仍反复叩门」的脚本。
	DailyUsed int
	// DailyLimit 每日配额
	DailyLimit int
	// Remaining 当日剩余次数
	Remaining int
	// RetryAfter 建议的重试等待时长
	RetryAfter time.Duration
}

// allow 构造放行结果
func allow(used, limit int) Decision {
	return Decision{
		Allowed:    true,
		DailyUsed:  used,
		DailyLimit: limit,
		Remaining:  max(limit-used, 0),
	}
}

// endOfDay 返回本地时区下次日零点，作为每日计数键的过期时刻。
// 「以天为单位刷新」指自然日：零点一到，当日计数随键过期一并清零。
func endOfDay(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, 1)
}

// dateStamp 当日日期戳，写入键名使跨日自然分桶
func dateStamp(now time.Time) string {
	return now.Format("20060102")
}

// dailyKey 每日计数键
func dailyKey(now time.Time, category types.LimitCategory, visitorKey string) string {
	return fmt.Sprintf("%sd:%s:%s:%s", keyPrefix, dateStamp(now), category, visitorKey)
}

// burstKey 突发窗口计数键
func burstKey(category types.LimitCategory, visitorKey string) string {
	return fmt.Sprintf("%sb:%s:%s", keyPrefix, category, visitorKey)
}

// violationKey 当日突发违规累计键。
// 按类别分开计数：各类操作的突发阈值不同，合并计数会让「report 违规 3 次 +
// update 违规 3 次」凑成 6 次而触发封禁，尽管单看任一类别都远未越界。
func violationKey(now time.Time, category types.LimitCategory, visitorKey string) string {
	return fmt.Sprintf("%sv:%s:%s:%s", keyPrefix, dateStamp(now), category, visitorKey)
}

// duplicateKey 当日疑似重复报告累计键
func duplicateKey(now time.Time, ip string) string {
	return fmt.Sprintf("%sdup:%s:%s", keyPrefix, dateStamp(now), ip)
}

// CheckAndCollect 判定一次操作是否放行，同时取回自动封禁所需的各项判据。
//
// 一次 Redis 往返完成全部工作。此前这些是分开做的——限流判定、网段流量累计、
// 违规数与重复报告数各一次往返，撞上突发限制时还要再两次递增违规计数，
// 合计 4~6 个 RTT。它们之间没有任何数据依赖（各键都由入参直接算出），
// 故可以合并；Redis 与服务不在同机时，这就是每个请求省下几毫秒。
//
// 违规计数的递增也放在同一批里：它必须与读取同批完成，否则读到的是递增前的值，
// 「本次撞了突发限制」这一笔就要等下一个请求才被看见。
//
// Redis 不可用或取不到计数时一律放行（fail-open）：限流失效胜过整站不可写。
// 判据取不到则按 0 计，宁可漏封也不误封。
func CheckAndCollect(ctx context.Context, rdb *redis.Client,
	category types.LimitCategory, subject, ip, visitorKey string) (Decision, Signals, error) {

	signals := Signals{Category: category}

	limit, ok := LimitFor(category)
	if !ok {
		return Decision{Allowed: true}, signals, nil
	}

	now := time.Now()
	expireAt := endOfDay(now)
	burstWindow := time.Duration(limit.BurstWindowSeconds) * time.Second

	dKey := dailyKey(now, category, subject)
	bKey := burstKey(category, subject)
	vKey := violationKey(now, category, subject)
	dupKey := duplicateKey(now, ip)

	pipe := rdb.Pipeline()

	// 限流计数：无条件累加，包括随后会被拒绝的请求——这样每日计数反映的是尝试量，
	// 配额用尽后继续叩门会把它推高，供自动封禁规则一识别脚本行为
	daily := pipe.Incr(ctx, dKey)
	pipe.ExpireAt(ctx, dKey, expireAt)
	burst := pipe.Eval(ctx, incrWithTTLScript, []string{bKey}, burstWindow.Milliseconds())

	// 封禁判据：违规数与重复报告数。前者用 Lua 做「仅当本次撞了突发限制才递增」，
	// 判断放在服务端是因为此刻还不知道 burstUsed——它就在同一批命令里。
	violations := pipe.Eval(ctx, bumpViolationScript, []string{bKey, vKey},
		limit.Burst, expireAt.Unix())
	duplicates := pipe.Get(ctx, dupKey)

	// 网段流量画像：按网段汇总总量，并记下各令牌各自的贡献。
	// 限流按令牌计数，而 IPv6 终端可在自己的 /64 内随意换址、每换一个地址就是一个
	// 「新来源」——只看单个地址时，分散在一个 /64 内的刷量看不出异常。
	if scope, ok := NetworkScope(ip); ok {
		totalKey := networkTotalKey(now, scope)
		pipe.Incr(ctx, totalKey)
		pipe.ExpireAt(ctx, totalKey, expireAt)

		if visitorKey != "" {
			netvKey := networkVisitorsKey(now, scope)
			pipe.ZIncrBy(ctx, netvKey, 1, visitorKey)
			pipe.ExpireAt(ctx, netvKey, expireAt)
		}
	}

	// 整体报错时不立刻放行：各命令的结果仍可分别读取，只有取不到计数才真正无法判定
	_, execErr := pipe.Exec(ctx)

	dailyUsed, err := countFrom(daily.Result())
	if err != nil {
		return Decision{Allowed: true}, signals, err
	}
	burstUsed, err := countFrom(burst.Result())
	if err != nil {
		return Decision{Allowed: true}, signals, err
	}

	// 判据取不到按 0 计：漏封好过误封
	signals.DailyUsed = dailyUsed
	signals.DailyLimit = limit.Daily
	if n, err := countFrom(violations.Result()); err == nil {
		signals.BurstViolations = n
	}
	if n, err := duplicates.Int(); err == nil {
		signals.DuplicateReports = n
	}

	if execErr != nil && !errors.Is(execErr, redis.Nil) {
		// 计数都拿到了，出错的是设过期一类的辅助命令：照常判定，只记一笔
		logger.Warnf("限流计数的辅助命令未全部成功，已照常判定 (%s %s): %v",
			category, subject, execErr)
	}

	return decide(limit, dailyUsed, burstUsed, expireAt, burstWindow), signals, nil
}

// decide 按两个计数给出判定结果。
//
// 先判每日配额，再判突发窗口。顺序不能反：配额用尽后的请求本就该被拒，
// 若此时还去判突发，这些请求会因同一窗口内计数早已越界而被记作「突发违规」，
// 攒够几次就误触发封禁——那不是高频调用，只是配额用完后的正常重试。
// 用满额度是正常使用，本身不封禁；只有捶到配额数倍才触发封禁规则一。
func decide(limit types.CategoryLimit, dailyUsed, burstUsed int,
	expireAt time.Time, burstWindow time.Duration) Decision {

	if dailyUsed > limit.Daily {
		return Decision{
			Allowed:    false,
			Reason:     "今日操作次数已达上限，请明日再试",
			DailyUsed:  dailyUsed,
			DailyLimit: limit.Daily,
			Remaining:  0,
			RetryAfter: time.Until(expireAt),
		}
	}

	if burstUsed > limit.Burst {
		return Decision{
			Allowed: false,
			Reason: fmt.Sprintf("操作过于频繁，请在 %d 秒后重试",
				limit.BurstWindowSeconds),
			DailyUsed:  dailyUsed,
			DailyLimit: limit.Daily,
			Remaining:  max(limit.Daily-dailyUsed, 0),
			RetryAfter: burstWindow,
		}
	}

	return allow(dailyUsed, limit.Daily)
}

// bumpViolationScript 仅当本次撞了突发限制时才累加违规计数，并返回累计值。
//
// 判断放在 Redis 侧的原因：调用方此刻还不知道本次的突发计数——它与本脚本在同一批
// 命令里。若挪到应用侧判断，就得先取回突发计数、再决定是否递增，那是额外一次往返，
// 也就回到了合并之前的样子。
//
// KEYS[1] 突发窗口键，KEYS[2] 违规累计键；ARGV[1] 突发阈值，ARGV[2] 违规键的过期时刻。
const bumpViolationScript = `
local burst = tonumber(redis.call('GET', KEYS[1]) or '0')
if burst > tonumber(ARGV[1]) then
  local n = redis.call('INCR', KEYS[2])
  redis.call('EXPIREAT', KEYS[2], ARGV[2])
  return n
end
return tonumber(redis.call('GET', KEYS[2]) or '0')
`

// RecordDuplicate 累计一次疑似重复报告，返回当日累计值。
// 由报告接口在判定为疑似重复时调用，供自动封禁判定。
func RecordDuplicate(ctx context.Context, rdb *redis.Client, ip string) (int, error) {
	now := time.Now()
	key := duplicateKey(now, ip)

	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	rdb.ExpireAt(ctx, key, endOfDay(now))

	return int(count), nil
}

// Status 查询当前访问者各类别的用量，不累加计数。
// 供前端展示剩余配额。
func Status(ctx context.Context, rdb *redis.Client, visitorKey string) ([]types.RateStatus, error) {
	now := time.Now()
	current := Get()

	statuses := make([]types.RateStatus, 0, len(types.AllLimitCategories))

	pipe := rdb.Pipeline()
	gets := make(map[types.LimitCategory]*redis.StringCmd, len(types.AllLimitCategories))
	for _, category := range types.AllLimitCategories {
		if _, ok := current.Limits[category]; !ok {
			continue
		}
		gets[category] = pipe.Get(ctx, dailyKey(now, category, visitorKey))
	}

	// 键不存在会返回 redis.Nil，属正常情况（当日尚未操作），不视为错误
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	for _, category := range types.AllLimitCategories {
		cmd, ok := gets[category]
		if !ok {
			continue
		}

		used, err := cmd.Int()
		if err != nil {
			used = 0
		}

		limit := current.Limits[category]
		statuses = append(statuses, types.RateStatus{
			Category:   category,
			DailyUsed:  used,
			DailyLimit: limit.Daily,
			Remaining:  max(limit.Daily-used, 0),
		})
	}

	return statuses, nil
}

// CountViolations 查询当日某类别的突发违规累计次数
func CountViolations(ctx context.Context, rdb *redis.Client,
	category types.LimitCategory, visitorKey string) (int, error) {
	return getInt(ctx, rdb, violationKey(time.Now(), category, visitorKey))
}

// CountDuplicates 查询当日疑似重复报告累计次数
func CountDuplicates(ctx context.Context, rdb *redis.Client, ip string) (int, error) {
	return getInt(ctx, rdb, duplicateKey(time.Now(), ip))
}

// ResetAfterUnban 解封后重置该 IP 的封禁判据。
//
// 解封只删封禁记录是不够的：各条自动封禁规则的判据都是当日累计值，
// 留着的话解封后第一个请求就会重新命中，立刻再封一次。
//
// 只重置按 IP 计的那些判据：认证与申诉类的每日计数（这两类以 IP 为计数主体）、
// 重复报告计数、见习计数、网段流量画像。按令牌计的判据由 ResetVisitorAfterUnban
// 处理，两者按被解封主体名下的标识分别调用。
//
// 之所以不再按 IP 扫出「当日用过的全部令牌」一并重置：那会连同共用出口的其他人
// 一起重置，等于解封一个人、顺手清掉同出口所有人当天的剩余额度。
func ResetAfterUnban(ctx context.Context, rdb *redis.Client, ip string) error {
	if rdb == nil {
		return nil
	}

	now := time.Now()
	expireAt := endOfDay(now)

	current := Get()
	pipe := rdb.Pipeline()

	// 按 IP 计数的类别（认证、申诉）以 IP 本身为主体，需一并重置，
	// 否则解封后连登录都会被自己遗留的计数挡住
	for category, limit := range current.Limits {
		if !category.ByIP() {
			continue
		}

		dKey := dailyKey(now, category, ip)
		// 仅当已超出配额时才回落到配额值，未超出的保持原样
		pipe.Eval(ctx, capToLimitScript, []string{dKey}, limit.Daily)
		pipe.ExpireAt(ctx, dKey, expireAt)

		// 突发窗口与违规计数是封禁判据，直接清掉
		pipe.Del(ctx, burstKey(category, ip))
		pipe.Del(ctx, violationKey(now, category, ip))
	}

	// 网段流量画像也要清：不清的话该网段总量仍然超标，
	// 解封后下一个请求就会重新触发网段判定、立刻再封一次
	if scope, ok := NetworkScope(ip); ok {
		pipe.Del(ctx, networkTotalKey(now, scope))
		pipe.Del(ctx, networkVisitorsKey(now, scope))
		pipe.Del(ctx, networkAdviseKey(now, scope))
	}

	// 重复报告计数、见习计数按 IP 计，一并清零
	pipe.Del(ctx, duplicateKey(now, ip))
	// 见习计数的主体按协议区分（见 probationScope），故须用同一个主体去清，
	// 否则删的是不存在的键、解封后该来源仍然领不到令牌
	probationScopeKey := probationScope(ip)
	pipe.Del(ctx, probationKey(now, probationScopeKey))
	pipe.Del(ctx, probationBurstKey(probationScopeKey))

	_, err := pipe.Exec(ctx)
	return err
}

// ResetVisitorAfterUnban 解封后重置某个访问者令牌的封禁判据。
//
// 网段级的精准处置只封令牌（见 middlewares 的 networkBanIdents），这类主体名下
// 没有 IP 标识，故必须能按令牌单独重置——否则解封后该令牌的计数与网段贡献原样
// 保留，下一个请求立刻重新命中、立刻再封。
//
// scope 为该令牌所属网段，用于把它从网段画像的 ZSET 里移除；为空则跳过那一步
// （主体只含令牌标识时无从推断网段）。
func ResetVisitorAfterUnban(ctx context.Context, rdb *redis.Client,
	visitorKey, scope string) error {

	if rdb == nil || visitorKey == "" {
		return nil
	}

	now := time.Now()
	expireAt := endOfDay(now)

	current := Get()
	pipe := rdb.Pipeline()

	for category, limit := range current.Limits {
		// 按 IP 计数的类别与令牌无关，其键的主体是 IP
		if category.ByIP() {
			continue
		}

		dKey := dailyKey(now, category, visitorKey)
		pipe.Eval(ctx, capToLimitScript, []string{dKey}, limit.Daily)
		pipe.ExpireAt(ctx, dKey, expireAt)

		pipe.Del(ctx, burstKey(category, visitorKey))
		pipe.Del(ctx, violationKey(now, category, visitorKey))
	}

	// 从网段画像里移除该令牌的贡献：它是网段判定认出「异常设备」的依据，
	// 留着的话解封后第一个请求就会重新把它排进 Top N
	if scope != "" {
		pipe.ZRem(ctx, networkVisitorsKey(now, scope), visitorKey)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// incrWithTTLScript 累加计数，并仅在该键尚无过期时间时设置过期。
//
// 等价于 INCR + ExpireNX，但不依赖 Redis 7.0——ExpireNX 是 7.0 才有的选项，
// 在更低的版本上它会直接报错，而限流出错即放行，于是整套限流会静默失效，
// 日志里只留一行警告。用 Lua 表达同一语义，任何版本都能跑。
//
// 「仅在无过期时设置」是必要的：每次调用都设一遍会把突发窗口不断往后推，
// 于是持续请求的客户端永远等不到窗口结束。
const incrWithTTLScript = `
local n = redis.call('INCR', KEYS[1])
if redis.call('PTTL', KEYS[1]) < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return n
`

// errCountUnavailable 取不到限流计数，无法据此判定。
// 调用方据此 fail-open：限流失效胜过整站不可写。
var errCountUnavailable = errors.New("取不到限流计数")

// countFrom 从命令结果里取出计数值。
// Eval 返回的是 any（Lua 数字经协议转为 int64），Incr 返回 int64，故统一处理。
//
// 注意不能只看命令自己的 err：pipeline 拨号失败时 go-redis 会让 Incr 之类的命令
// 返回「0, nil」——实测确认。若把那个 0 当成真计数，就会误判为「未超限」而放行，
// 那与有意的 fail-open 效果相同但成因不同：Redis 只是部分失败时会真的漏判。
// 故要求值本身必须是个数字，拿不到数字即视为计数不可用。
func countFrom(value any, err error) (int, error) {
	if err != nil {
		// redis.Nil 表示键不存在，那是正常情况；其余都是取数失败
		if errors.Is(err, redis.Nil) {
			return 0, nil
		}
		return 0, fmt.Errorf("%w: %v", errCountUnavailable, err)
	}

	switch n := value.(type) {
	case int64:
		return int(n), nil
	case int:
		return n, nil
	case nil:
		// 命令没有真正执行（多半是连不上 Redis），此时没有任何计数可用
		return 0, errCountUnavailable
	default:
		return 0, fmt.Errorf("%w：类型出乎预料 %T", errCountUnavailable, value)
	}
}

// capToLimitScript 把计数封顶到给定值：超出则设为该值，未超出则不动。
// 用 Lua 保证「读取—比较—写入」三步原子完成，避免并发请求期间被覆盖。
const capToLimitScript = `
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
local limit = tonumber(ARGV[1])
if current > limit then
  redis.call('SET', KEYS[1], limit)
  return limit
end
return current
`

// getInt 读取整型计数，键不存在时返回 0
func getInt(ctx context.Context, rdb *redis.Client, key string) (int, error) {
	value, err := rdb.Get(ctx, key).Int()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return value, nil
}
