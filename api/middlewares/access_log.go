package middlewares

import (
	"sync"
	"time"

	"bookfinder-backend/database"
	"bookfinder-backend/logger"
	"bookfinder-backend/services/dashboard"
	"bookfinder-backend/utils/netmask"

	"github.com/gin-gonic/gin"
)

// MetricsMiddleware 采集监控面板所需的流量指标。
//
// 挂在 /api 组上而非全局：静态资源与前端入口不算「访问」，一次页面加载会拉走
// 十几个资源文件，计进去的话访问量只反映资源数。
//
// 管理员不计入：面板衡量的是公众使用情况，而管理员是观察者不是受众——
// 否则开着面板每隔几十秒刷一次，访问量里大半是自己。这与「管理员不受限流影响」
// 是同一类取舍。
//
// 放在 BanMiddleware 之后（见 routes.go 的中间件顺序）：被封禁者的请求已被拦下，
// 不该计入访问量。而访问者令牌此时已在上下文里，可用于在线人数去重。
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsAdmin(c) {
			c.Next()
			return
		}

		// 顺带检出反代配置错误。放在这里是因为它需要一个真实请求才能判断——
		// 启动时无从知道前面有没有反代。只告警一次，见 proxyWarned。
		warnIfProxyMisconfigured(c)

		// 先记后放行：此刻请求确实到达了业务处理，而处理结果如何不影响
		// 「有人来访问了」这一事实
		dashboard.RecordVisit(c.Request.Context(), database.GetRedis(),
			contextString(c, VisitorKeyContextKey))

		c.Next()
	}
}

// proxyWarned 保证「反代配置可疑」只告警一次。
//
// 需要节流是因为这个状况对每个请求都成立：一旦反代未透传来源，之后每个请求
// 都会命中，不节流会把日志刷满。
var proxyWarned sync.Once

// warnIfProxyMisconfigured 检出「来源被判定为回环」这一失准状态。
//
// 判据只有一条：ClientIP() 是回环地址。此时全部访问者共用同一个「来源」，
// 按 IP 的限流会把所有人的用量算在一起（一个人用完额度，所有人都被拦），
// 封禁也无法定位到具体访问者。这不是小瑕疵，而是判据整体失准。
//
// 两种成因都要报，且要分开报——它们改的是不同地方的配置：
//
//   - 带 X-Forwarded-For：反代转发了真实来源，但 TRUSTED_PROXIES 没有信任它，
//     于是 Gin 忽略该头、以 TCP 对端（反代自己）为准。改本服务的 .env。
//   - 不带 X-Forwarded-For：反代根本没有转发真实来源。裸写 proxy_pass 就是
//     这个结果，此时无论 TRUSTED_PROXIES 怎么配都拿不到真实客户端——
//     那个信息从未到达本服务。改 Nginx 配置。
//
// 后者更需要提示，恰恰因为它「看起来没问题」：日志里全是回环地址，而 TRUSTED_PROXIES
// 明明配对了，很难想到问题出在上游。
//
// 本机直接访问（无反代）也会命中，但那只在开发时发生，且只报一次，不成负担。
//
// 只告警不阻断：服务照常可用，而此时改配置比停服更要紧。
func warnIfProxyMisconfigured(c *gin.Context) {
	if !netmask.IsLoopback(c.ClientIP()) {
		return
	}

	forwarded := c.GetHeader("X-Forwarded-For")

	proxyWarned.Do(func() {
		if forwarded != "" {
			logger.Errorf("反向代理未被信任：请求带有 X-Forwarded-For，"+
				"但来源仍被判定为回环地址（%s）。这说明 TRUSTED_PROXIES 未包含反代地址，"+
				"于是该请求头被忽略、以反代自身地址为准。"+
				"请在 .env 中把 TRUSTED_PROXIES 设为反代地址"+
				"（同机部署通常是 127.0.0.1）后重启。"+
				"在此之前，按 IP 的限流会让全部访问者互相牵连，封禁也无法定位到具体访问者。",
				c.ClientIP())
			return
		}

		logger.Errorf("来源被判定为回环地址（%s），且请求不带 X-Forwarded-For。"+
			"若本服务在反向代理之后，说明代理未转发真实来源——"+
			"Nginx 需显式配置 proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for; "+
			"（仅写 proxy_pass 不会自动转发）。此时无论 TRUSTED_PROXIES 如何设置都拿不到"+
			"真实客户端，按 IP 的限流会让全部访问者互相牵连。"+
			"若本服务直接对外监听，则此条仅因本机自身访问触发，可忽略。",
			c.ClientIP())
	})
}

// AccessLogMiddleware 记录访问日志，替代 Gin 自带的 Logger()。
//
// 自己写而不用 gin.Logger()：后者把状态码、耗时等格式化成一行文本再交给 Writer，
// 于是「按状态码决定日志级别」只能去解析那行文本——而文本里状态码与耗时形似
// （`| 200 | 505.8µs |`），一不小心就把正常请求判成错误。状态码本就是结构化数据，
// 从 c.Writer.Status() 直接读，不必绕经字符串。
//
// 只落库异常请求：每个请求一条访问日志会让日志表随流量线性膨胀，一次页面加载
// 就有若干请求。正常请求的访问记录价值有限——需要审计「谁做了什么」时看操作日志，
// 那里有结构化的用户、操作与详情。
func AccessLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()

		c.Next()

		// 静态资源与前端入口不记：它们数量大、且与业务排障无关
		if !isAPIPath(c.Request.URL.Path) {
			return
		}

		logger.Access(logger.AccessRecord{
			Status:   c.Writer.Status(),
			Method:   c.Request.Method,
			Path:     requestPath(c),
			ClientIP: c.ClientIP(),
			Latency:  time.Since(started),
			// c.Errors 收集处理链里显式登记的错误（含 panic 恢复后的记录），
			// 它们不体现在状态码上，却往往是排障的关键
			Errors: c.Errors.ByType(gin.ErrorTypePrivate).String(),
		})
	}
}

// requestPath 请求路径，带上查询串。
// 查询串对排障有用：限流与分页的问题往往就出在参数上。
func requestPath(c *gin.Context) string {
	if raw := c.Request.URL.RawQuery; raw != "" {
		return c.Request.URL.Path + "?" + raw
	}
	return c.Request.URL.Path
}

// isAPIPath 判断是否为 API 请求。
// 静态资源走另一条路径（见 routes.go 的 assetsFS 与 NoRoute），不必记。
func isAPIPath(path string) bool {
	const prefix = "/api"

	if len(path) < len(prefix) || path[:len(prefix)] != prefix {
		return false
	}
	// 精确是 /api 或以 /api/ 开头，避免把 /apidocs 之类的路径算进来
	return len(path) == len(prefix) || path[len(prefix)] == '/'
}
