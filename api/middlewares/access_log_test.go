package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestAccessLogReadsStatusFromWriter 中间件应从 c.Writer.Status() 取状态码。
//
// 这是不再解析文本的前提：状态码在处理函数写出响应后才确定，
// 而 Writer 一直持有它，不必等 Gin 格式化成一行日志再解析回来。
func TestAccessLogReadsStatusFromWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, status := range []int{200, 403, 429, 500} {
		recorder := httptest.NewRecorder()
		r := gin.New()
		r.Use(AccessLogMiddleware())
		r.GET("/api/probe", func(c *gin.Context) {
			c.Status(status)
		})

		r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/probe", nil))

		if recorder.Code != status {
			t.Errorf("状态码应为 %d，实际为 %d", status, recorder.Code)
		}
	}
}

// TestAccessLogSkipsNonAPIPaths 静态资源不记：数量大且与业务排障无关
func TestAccessLogSkipsNonAPIPaths(t *testing.T) {
	cases := map[string]bool{
		"/api":             true,
		"/api/libraries":   true,
		"/api/admin/bans":  true,
		"/apidocs":         false,
		"/assets/index.js": false,
		"/":                false,
		"/robots.txt":      false,
	}

	for path, want := range cases {
		if got := isAPIPath(path); got != want {
			t.Errorf("isAPIPath(%q) = %v，期望 %v", path, got, want)
		}
	}
}

// TestRequestPathIncludesQuery 查询串要带上：限流与分页的问题往往出在参数上
func TestRequestPathIncludesQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/logs?page=2&size=50", nil)

	if got := requestPath(c); got != "/api/logs?page=2&size=50" {
		t.Errorf("应带上查询串，实际为 %q", got)
	}

	c.Request = httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	if got := requestPath(c); got != "/api/logs" {
		t.Errorf("无查询串时不应加问号，实际为 %q", got)
	}
}
