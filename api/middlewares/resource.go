package middlewares

import (
	"net/http"

	"bookfinder-backend/utils"
	"bookfinder-backend/utils/sysconfig"

	"github.com/gin-gonic/gin"
)

// DefaultMaxConcurrentRequests 并发上限的兜底值，仅在配置非法时使用。
//
// 并发上限是「服务器不被打崩」的最后一道保险，也是唯一一道不依赖 Redis 的：
// 限流与封禁在 Redis 不可用时都会 fail-open，此时若无并发上限，
// 一次并发洪峰就能耗尽连接与内存。
const DefaultMaxConcurrentRequests = 256

// BodyLimitMiddleware 限制请求体大小。
// 须置于 JSON 解析之前，否则超大 body 已经被读进内存了。
//
// 上限每个请求读一次，故在管理页改动后即时生效。不设上限时，一个大 body
// 就能让服务读满内存——这与限流无关，限流按次数计，不约束单次请求的体量。
func BodyLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			limit := int64(sysconfig.Get().Server.MaxRequestBodyBytes)
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

// ConcurrencyLimitMiddleware 限制同时处理的请求数。
//
// 用带缓冲 channel 作信号量：取不到令牌即立刻拒绝，而不是排队等待——
// 排队会让请求堆积在内存里，把「拒绝服务」变成「缓慢死亡」。
//
// 容量在此处一次固定，故配置里的并发上限改动后须重启才生效（管理页已标注）：
// 运行中改 channel 容量做不到，而重建 channel 会让已持有令牌的请求无从归还。
//
// 宁可拒绝一部分请求，也不要整站雪崩——超限时返回 503 并给出 Retry-After。
func ConcurrencyLimitMiddleware(limit int) gin.HandlerFunc {
	if limit < 1 {
		limit = DefaultMaxConcurrentRequests
	}

	slots := make(chan struct{}, limit)

	return func(c *gin.Context) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			c.Next()
		default:
			c.Header("Retry-After", "2")
			utils.ResponseError(c, http.StatusServiceUnavailable,
				"服务器繁忙，请稍后重试")
			c.Abort()
		}
	}
}
