package middlewares

import (
	"net/http"
	"strconv"

	"bookfinder-backend/database"
	"bookfinder-backend/logger"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/ratelimit"

	"github.com/gin-gonic/gin"
)

// sameSiteLax cookie 的 SameSite 策略
const sameSiteLax = http.SameSiteLaxMode

// 访问者令牌相关常量
const (
	// VisitorCookieName 存放访问者令牌的 cookie 名
	VisitorCookieName = "bf_visitor"
	// VisitorHeaderName 安卓端携带访问者令牌的请求头。
	// 原生客户端不走 cookie，故用请求头承载同一套令牌，两端因此共用同一份配额，
	// 封禁也随之可以跨端命中。
	VisitorHeaderName = "X-BF-Visitor"
	// VisitorKeyContextKey 上下文中访问者标识（令牌哈希）的键
	VisitorKeyContextKey = "visitor_key"
	// visitorCookieMaxAge cookie 有效期，一年。
	// 报告记录存在 MySQL 里且永不过期，故 cookie 失效只影响「能否撤销自己的报告」，
	// 不会让已有的报告次数丢失。
	visitorCookieMaxAge = 365 * 24 * 60 * 60
)

// VisitorMiddleware 确保每个访问者持有一个服务端签发的有效令牌。
//
// 令牌是限流与封禁的身份基础：限流按令牌计数，封禁把令牌作为跨 IP、跨端的标识。
// 因此令牌不能是「没带就免费发一个」——那会让按令牌计数的限流形同不存在：
// 不带 cookie 的请求每次都是全新访问者、每次都拿满配额，这正是 API 被盗刷的
// 主路径。
//
// 现在的处理是：
//   - 携带本服务签发且未过期的令牌 → 照常放行，体验与从前一致；
//   - 未携带或令牌无效 → 先扣「见习配额」（按 IP 计）。额度内放行并签发正式令牌，
//     额度耗尽则拒绝，且不再签发。
//
// 于是正常用户的第一个请求就换到令牌，此后不受见习额度影响；坚持不带 cookie 的
// 脚本则被按 IP 的小额度拦死，清 cookie 换配额也不再免费。
func VisitorMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 管理员已凭令牌证明身份，不受见习额度约束，
		// 否则维护操作会被自己的规则挡住
		if IsAdmin(c) {
			if token, ok := presentedToken(c); ok {
				if _, err := utils.ParseVisitorToken(token); err == nil {
					c.Set(VisitorKeyContextKey, utils.HashVisitorToken(token))
				}
			}
			c.Next()
			return
		}

		if token, ok := presentedToken(c); ok {
			if _, err := utils.ParseVisitorToken(token); err == nil {
				c.Set(VisitorKeyContextKey, utils.HashVisitorToken(token))
				c.Next()
				return
			}
			// 令牌无效（伪造、被截断、或已过期）：按无令牌处理。
			// 不在此处补发，否则每次带一个乱码令牌就能白拿一份正式配额。
		}

		if !passProbation(c) {
			return
		}

		issued, err := utils.GenerateVisitorToken()
		if err != nil {
			// 签发失败不阻断请求：读取类接口与令牌无关，
			// 报告类接口会在取不到访问者标识时自行拒绝。
			logger.Errorf("签发访问者令牌失败: %v", err)
			c.Next()
			return
		}

		setVisitorCookie(c, issued)
		// 安卓端不走 cookie，同时经响应头下发，由客户端自行保存
		c.Header(VisitorHeaderName, issued)
		c.Set(VisitorKeyContextKey, utils.HashVisitorToken(issued))

		c.Next()
	}
}

// passProbation 扣一次见习配额，额度耗尽时写出响应并返回 false。
//
// Redis 不可用时放行（fail-open），与其余限流一致：此时整套限流都已失效，
// 让整站不可用换不来任何安全收益，兜底靠封禁与并发闸。
func passProbation(c *gin.Context) bool {
	if !ratelimit.Enabled() {
		return true
	}

	rdb := database.GetRedis()
	if rdb == nil {
		return true
	}

	ip := GetClientIPFromContext(c)

	decision, err := ratelimit.CheckProbation(c.Request.Context(), rdb, ip)
	if err != nil {
		logger.Warnf("见习配额查询失败，已放行 (%s): %v", ip, err)
		return true
	}
	if decision.Allowed {
		return true
	}

	// 额度已耗尽却还在请求：判定是否升级为封禁。
	//
	// 这一判定必须在这里做，不能挪到限流中间件上——额度耗尽的请求会被下面几行
	// 直接拦下，根本走不到那里。放在那里的话，真正刷接口的客户端永远判不到，
	// 反而是同一出口下持有效令牌的正常用户会被自己邻居的用量封掉。
	if verdict := ratelimit.EvaluateProbationBan(decision.DailyUsed); verdict.ShouldBan {
		if banProbationSource(c, ip, verdict) {
			return false
		}
	}

	c.Header("Retry-After", retryAfterSeconds(decision))
	utils.ResponseError(c, http.StatusTooManyRequests, decision.Reason)
	c.Abort()

	return false
}

// banProbationSource 封禁一个反复消耗见习额度的来源，已写出封禁响应时返回 true。
//
// 处置粒度与见习计数的主体一致：IPv6 封其 /64，IPv4 只封精确地址。
// 此时对方没有可信令牌（正是因此才走见习路径），故无令牌标识可写。
func banProbationSource(c *gin.Context, ip string,
	verdict ratelimit.ProbationVerdict) bool {

	idents := ratelimit.ProbationBanIdents(ip)
	if len(idents) == 0 {
		logger.Warnf("见习额度超限但无可用封禁标识，未处置 (%s)", ip)
		return false
	}

	ban := ApplyAutoBan(c, verdict.Reason, verdict.Detail, idents)
	if ban == nil {
		// 落库失败，或该来源早已在名单内：照常回限流响应即可
		return false
	}

	// 与 BanMiddleware 的响应体一致，前端据 data.banned 立即切到封禁页
	utils.ResponseBanned(c, ban)

	return true
}

// presentedToken 取出请求携带的访问者令牌：优先 cookie（浏览器），
// 其次请求头（安卓端）。两端共用同一套令牌体系。
func presentedToken(c *gin.Context) (string, bool) {
	if token, err := c.Cookie(VisitorCookieName); err == nil && token != "" {
		return token, true
	}
	if token := c.GetHeader(VisitorHeaderName); token != "" {
		return token, true
	}
	return "", false
}

// setVisitorCookie 下发访问者令牌。
// HttpOnly 阻止脚本读取，SameSite=Lax 兼顾跨站导航与 CSRF 防护。
func setVisitorCookie(c *gin.Context, token string) {
	c.SetSameSite(sameSiteLax)
	c.SetCookie(
		VisitorCookieName,
		token,
		visitorCookieMaxAge,
		"/",
		"",
		// 生产环境应经 HTTPS 提供服务；此处按请求协议决定是否加 Secure，
		// 以免本地 HTTP 调试时 cookie 被浏览器丢弃。
		c.Request.TLS != nil,
		true,
	)
}

// GetVisitorKeyFromContext 从上下文获取访问者标识，缺失时返回 false
func GetVisitorKeyFromContext(c *gin.Context) (string, bool) {
	value, ok := c.Get(VisitorKeyContextKey)
	if !ok {
		return "", false
	}
	key, ok := value.(string)
	return key, ok && key != ""
}

// retryAfterSeconds Retry-After 头的秒数
func retryAfterSeconds(decision ratelimit.Decision) string {
	return strconv.Itoa(max(int(decision.RetryAfter.Seconds()), 1))
}
