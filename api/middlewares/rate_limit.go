package middlewares

import (
	"fmt"
	"net/http"

	"bookfinder-backend/database"
	"bookfinder-backend/logger"
	"bookfinder-backend/services/notify"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/netmask"
	"bookfinder-backend/utils/ratelimit"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RateLimitMiddleware 按访问者令牌限流，并在检出异常时自动封禁。
// 限流拦当日，封禁则永久生效、需管理员解封或申诉受理。
//
// 出错一律放行（fail-open）：Redis 不可用时限流失效胜过整站不可写。
// 此时的兜底是封禁名单与全局并发闸——后者保证服务不会被打崩。
func RateLimitMiddleware(category types.LimitCategory) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 管理员不受限流影响，否则维护操作会被自己的规则挡住
		if IsAdmin(c) {
			c.Next()
			return
		}

		if !ratelimit.Enabled() {
			c.Next()
			return
		}

		rdb := database.GetRedis()
		if rdb == nil {
			c.Next()
			return
		}

		ip := GetClientIPFromContext(c)

		// 计数主体：认证类按 IP（此时还没有令牌），其余按访问者令牌。
		//
		// 取不到令牌的情形已由 VisitorMiddleware 的见习配额兜住：
		// 走到这里若仍无令牌，说明令牌签发失败（极少见），此时按 IP 计数而非放行——
		// 旧实现在此直接放行，等于「不带 cookie 即免限流」。
		subject := ip
		if !category.ByIP() {
			if visitorKey, ok := GetVisitorKeyFromContext(c); ok {
				subject = visitorKey
			}
		}

		// 限流判定、网段流量累计、封禁判据的收集合并为一次 Redis 往返。
		// 三者之间没有数据依赖，分开做只是多花 RTT（见 CheckAndCollect 的说明）。
		visitorKey := contextString(c, VisitorKeyContextKey)
		decision, signals, err := ratelimit.CheckAndCollect(
			c.Request.Context(), rdb, category, subject, ip, visitorKey)
		if err != nil {
			logger.Warnf("限流查询失败，已放行 (%s %s): %v", category, ip, err)
			c.Next()
			return
		}

		// 判定自动封禁：即便本次放行也要查，异常特征不只体现在被拒的那次上
		if banned := evaluateAutoBan(c, rdb, ip, signals); banned {
			return
		}

		if !decision.Allowed {
			c.Header("Retry-After", retryAfterSeconds(decision))
			utils.ResponseError(c, http.StatusTooManyRequests, decision.Reason)
			c.Abort()
			return
		}

		c.Next()
	}
}

// evaluateAutoBan 判定并执行自动封禁，已封禁时写出响应并返回 true。
// 判据由 CheckAndCollect 在限流判定的同一次往返里取回，此处不再查 Redis。
func evaluateAutoBan(c *gin.Context, rdb *redis.Client, ip string,
	signals ratelimit.Signals) bool {

	autoBan := ratelimit.AutoBan()
	if !autoBan.Enabled {
		return false
	}

	// 已被封的来源无需再判：判据是当日累计值，命中后每个后续请求都会重新命中，
	// 而被封者仍能走到这里——申诉接口对其开放（见 BanMiddleware 的放行名单）。
	// 不短路的话，被封者刷申诉接口即可让每个请求都走一遍封禁判定。
	if _, banned := GetBanFromContext(c); banned {
		return false
	}

	verdict := ratelimit.EvaluateBan(signals)
	if !verdict.ShouldBan {
		// 按令牌的各条规则都没命中，再看网段层面：
		// 分散在一个 /64 内的刷量，单看任何一个令牌都可能都在配额之内
		return evaluateNetworkBan(c, rdb, ip)
	}

	// 一次封禁写入本次请求的全部标识：精确 IP 挡住当前来源，所属网段挡住段内换址，
	// 访问者令牌与设备标识则跨 IP、跨端地跟着这个人。
	// 这是「封一次同时挡住浏览器端与安卓端」的落点。
	idents := BanIdentsForRequest(c)

	ban := ApplyAutoBan(c, verdict.Reason, verdict.Detail, idents)
	if ban == nil {
		// 落库失败，或本次判定的标识早已在名单内：两种情形都不该拦下请求，
		// 交由随后的限流判定处理
		return false
	}

	// 与 BanMiddleware 的响应体一致，前端据 data.banned 立即切到封禁页；
	// 若只回一句普通错误，用户要等下次刷新才看得到封禁页
	utils.ResponseBanned(c, ban)

	return true
}

// evaluateNetworkBan 判定网段流量是否异常，并尽量只处置真正异常的设备。
//
// 与按令牌的规则分工：那些规则看「一个访问者做了多少」，本函数看「一个网段总共来了多少」。
// 后者是必要的，因为 IPv6 终端可在自己的 /64 内随意换址，把刷量摊到许多「新来源」上，
// 单看任何一个令牌都可能都在配额之内。
//
// 处置优先落到设备而非网段：访问者令牌在同一网段内换址时不变，比 IP 更能标识
// 「一个人」，封它不波及同网段的其他访问者。只有认不出异常设备时才退回网段级封禁，
// 且 IPv4 不自动封 /24（那背后可能是整个校园网出口）。
func evaluateNetworkBan(c *gin.Context, rdb *redis.Client, ip string) bool {
	autoBan := ratelimit.AutoBan()
	if autoBan.NetworkOverflowMultiplier <= 0 {
		return false
	}

	ctx := c.Request.Context()

	profile, err := ratelimit.ProfileNetwork(ctx, rdb, ip, autoBan.NetworkTopVisitors)
	if err != nil {
		// 取不到画像就不判定，宁可漏封也不误封
		logger.Warnf("读取网段流量画像失败 (%s): %v", ip, err)
		return false
	}

	verdict := ratelimit.EvaluateNetworkBan(ratelimit.NetworkSignals{
		Profile: profile,
		Budget:  ratelimit.NetworkBudget(),
		IsIPv6:  netmask.IsIPv6(ip),
	})

	// 判据命中但没有安全的处置粒度（IPv4 且认不出异常设备）：只告警，交管理员判断。
	// 每个网段每天只记一条，否则该网段的每个后续请求都会重复记录。
	if verdict.AdviseOnly {
		if ratelimit.ShouldAdviseNetwork(ctx, rdb, ip) {
			logger.Warnf("网段流量异常未自动处置：%s（%s）", verdict.Reason, verdict.Detail)
			logger.Operation(&types.OperationLog{
				User:       ip,
				Action:     types.ActionIPBanAdvised,
				Level:      types.LevelWarn,
				Detail:     verdict.Reason + "：" + verdict.Detail,
				IP:         ip,
				VisitorKey: contextString(c, VisitorKeyContextKey),
			})

			// 这类事件最需要通知：服务什么都没做，只留了一条待人工核查的记录。
			// 放在节流判定内，故每个网段每天最多一条。
			notify.NetworkAnomaly(verdict.Reason, verdict.Detail)
		}
		return false
	}

	if !verdict.ShouldBan {
		return false
	}

	idents, scope := networkBanIdents(c, ip, verdict)
	if len(idents) == 0 {
		logger.Warnf("网段 %s 流量异常但无可用封禁标识，未处置", profile.Scope)
		return false
	}

	// 精准处置命中的设备可能已在名单内：网段总量当日不会下降，故该网段的每个
	// 后续请求都会重新命中同一判定。ApplyAutoBan 会滤掉已封标识、无新标识则不落库。
	ban := ApplyAutoBan(c, verdict.Reason, verdict.Detail, idents)
	if ban == nil {
		return false
	}

	logger.Warnf("网段级自动封禁处置范围：%s（网段 %s）", scope, profile.Scope)

	// 本次请求的发起者未必在被封之列：精准处置只封那几个异常设备，
	// 而触发判定的可能是同网段里一个正常用户的请求。
	// 故仅当当前来源确实被封时才回封禁响应，否则照常放行。
	if !banlistHitsCurrent(c, ip, verdict) {
		return false
	}

	utils.ResponseBanned(c, ban)

	return true
}

// networkBanIdents 按判定结果给出要写入的封禁标识，以及供日志说明的处置范围。
func networkBanIdents(c *gin.Context, ip string,
	verdict ratelimit.NetworkVerdict) ([]types.BanIdent, string) {

	// 精准处置：只封那几个异常设备的令牌。
	// 令牌是设备级标识，同网段其他访问者不受任何影响。
	if len(verdict.VisitorKeys) > 0 {
		idents := make([]types.BanIdent, 0, len(verdict.VisitorKeys))
		for _, key := range verdict.VisitorKeys {
			idents = append(idents, types.BanIdent{Kind: types.IdentVisitor, Value: key})
		}
		return idents, fmt.Sprintf("%d 个设备", len(idents))
	}

	// 认不出异常设备时的回退，只在 IPv6 下发生：封其 /64。
	//
	// IPv4 走不到这里——EvaluateNetworkBan 对该情形返回 AdviseOnly，
	// 因为封 /24 与封共享出口的精确 IP 都会连坐无关的人。
	if verdict.BanNetwork {
		if prefix, ok := netmask.PrefixOf(ip); ok {
			return []types.BanIdent{{Kind: types.IdentIPNet, Value: prefix}}, prefix
		}
	}

	return nil, ""
}

// banlistHitsCurrent 判断本次请求的发起者是否落在刚写入的封禁范围内。
//
// 精准处置封的是「流量最高的那几个令牌」，而触发判定的请求可能来自同网段的
// 另一个正常用户——那个人不该看到封禁页。
func banlistHitsCurrent(c *gin.Context, ip string, verdict ratelimit.NetworkVerdict) bool {
	if verdict.BanNetwork {
		return true
	}

	if len(verdict.VisitorKeys) == 0 {
		// 回退到精确 IP 封禁：封的就是当前来源
		return true
	}

	current := contextString(c, VisitorKeyContextKey)
	if current == "" {
		return false
	}

	for _, key := range verdict.VisitorKeys {
		if key == current {
			return true
		}
	}

	return false
}
