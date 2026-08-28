package middlewares

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// runWarnCheck 在给定的可信代理配置下发一个请求，返回 ClientIP 与是否判定为配错
func runWarnCheck(t *testing.T, trusted []string, remoteAddr, xff string) (string, bool) {
	t.Helper()

	// 每次重置节流，否则只有第一个用例会真正走到判定
	proxyWarned = sync.Once{}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := r.SetTrustedProxies(trusted); err != nil {
		t.Fatalf("设置可信代理失败: %v", err)
	}

	var (
		clientIP string
		warned   bool
	)
	r.GET("/", func(c *gin.Context) {
		clientIP = c.ClientIP()
		warnIfProxyMisconfigured(c)

		// 判断是否告警过：再调一次 Do，若回调没执行说明 Once 已被消耗，
		// 即上面那次确实告警了
		fired := false
		proxyWarned.Do(func() { fired = true })
		warned = !fired
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	r.ServeHTTP(httptest.NewRecorder(), req)

	return clientIP, warned
}

// TestProxyWarnDetectsUntrustedProxy 反代在前但未被信任时应告警。
//
// 这是个真实且危险的配置错误：全部访问者会共用同一个来源，
// 按 IP 的限流互相牵连，封禁也无法定位到具体访问者。
func TestProxyWarnDetectsUntrustedProxy(t *testing.T) {
	clientIP, warned := runWarnCheck(t, nil, "127.0.0.1:54321", "203.0.113.9")

	if clientIP != "127.0.0.1" {
		t.Fatalf("未信任反代时 ClientIP 应为回环，实际 %q", clientIP)
	}
	if !warned {
		t.Error("反代未被信任却没有告警")
	}
}

// TestProxyWarnSilentWhenConfigured 正确配置时不该告警：
// 此时 ClientIP 已是真实客户端，一切正常
func TestProxyWarnSilentWhenConfigured(t *testing.T) {
	clientIP, warned := runWarnCheck(t,
		[]string{"127.0.0.1"}, "127.0.0.1:54321", "203.0.113.9")

	if clientIP != "203.0.113.9" {
		t.Fatalf("正确配置时应取到真实客户端，实际 %q", clientIP)
	}
	if warned {
		t.Error("配置正确却告警了")
	}
}

// TestProxyWarnDetectsMissingForwardedHeader 反代不转发真实来源时也要告警。
//
// 这是最容易犯的错：Nginx 里只写 proxy_pass、不写 proxy_set_header，
// 于是真实客户端地址根本没到达本服务。此时无论 TRUSTED_PROXIES 怎么配都没用，
// 而日志里全是回环地址——看起来「配对了」，实则全部访问者共用一个来源。
//
// 必须报，正因为它看起来没问题：带 XFF 的那种错至少有个头可以看出端倪，
// 这种连线索都没有。
func TestProxyWarnDetectsMissingForwardedHeader(t *testing.T) {
	clientIP, warned := runWarnCheck(t,
		[]string{"127.0.0.1"}, "127.0.0.1:54321", "")

	if clientIP != "127.0.0.1" {
		t.Fatalf("ClientIP = %q，期望回环", clientIP)
	}
	if !warned {
		t.Error("反代未转发真实来源时应告警——否则这个错无从发现")
	}
}

// TestProxyWarnSilentForDirectExternal 直接对外监听时不该告警
func TestProxyWarnSilentForDirectExternal(t *testing.T) {
	clientIP, warned := runWarnCheck(t, nil, "203.0.113.9:54321", "")

	if clientIP != "203.0.113.9" {
		t.Fatalf("ClientIP = %q", clientIP)
	}
	if warned {
		t.Error("直接对外监听不该被判为配置错误")
	}
}
