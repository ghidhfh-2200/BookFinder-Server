package logger

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// AccessRecord 一次请求的访问记录。
//
// 各字段直接来自 Gin 的结构化接口（c.Writer.Status() 等），不经过文本格式化——
// 那是「按状态码决定日志级别」能可靠工作的前提。
type AccessRecord struct {
	// Status HTTP 状态码
	Status int
	// Method 请求方法
	Method string
	// Path 请求路径，含查询串
	Path string
	// ClientIP 来源 IP，取自可信的 ClientIP()
	ClientIP string
	// Latency 处理耗时
	Latency time.Duration
	// Errors 处理链里显式登记的错误，通常为空。
	// 它们不体现在状态码上，却往往是排障的关键。
	Errors string
}

// Access 记录一次请求。
//
// 只落库异常请求：每个请求一条访问日志会让日志表随流量线性膨胀，
// 一次页面加载就有若干请求。正常请求的访问记录价值有限——需要审计
// 「谁做了什么」时看操作日志，那里有结构化的用户、操作与详情。
func Access(record AccessRecord) {
	msg := record.String()

	switch levelFor(record) {
	case LevelError:
		Errorf("%s", msg)
	case LevelWarn:
		Warnf("%s", msg)
	// 其余（2xx/3xx）只在调试模式下输出到控制台，不落库
	default:
		if IsDebug() {
			printStdout(LevelDebug, msg)
		}
	}
}

// levelFor 按记录判定日志级别。
//
// 只看结构化字段，不碰任何格式化文本——旧实现从格式化后的一行里搜索状态码，
// 而耗时与状态码形似（`| 200 | 505.8µs |` 含有 "|5"），于是耗时以 5 开头的
// 正常请求全被记成 ERROR。
func levelFor(record AccessRecord) string {
	switch {
	// 服务端错误必须留痕
	case record.Status >= 500:
		return LevelError
	// 客户端错误留痕，便于排查限流、封禁与权限问题
	case record.Status >= 400:
		return LevelWarn
	// 状态码正常但处理链登记了错误：同样值得留痕，否则这类问题无从发现
	case record.Errors != "":
		return LevelWarn
	default:
		return LevelDebug
	}
}

// String 拼出可读的一行记录
func (r AccessRecord) String() string {
	msg := fmt.Sprintf("%d %s %s（%s，耗时 %s）",
		r.Status, r.Method, r.Path, r.ClientIP, r.Latency.Round(time.Microsecond))

	if r.Errors != "" {
		msg += "：" + strings.TrimSpace(r.Errors)
	}

	return msg
}

// GinWriter 返回一个 [io.Writer]，接管 Gin 自身的输出。
//
// 这里只剩 Gin 框架自己的消息：启动横幅、路由注册、以及它内部的告警
// （如可信代理配置问题）。它们只在启动时出现一次，故一律记为 INFO。
//
// 访问日志不走这里——那条路径见 middlewares.AccessLogMiddleware，
// 它从结构化接口读状态码，不必解析文本。
func GinWriter() io.Writer {
	return &ginWriter{}
}

type ginWriter struct{}

func (w *ginWriter) Write(p []byte) (int, error) {
	if msg := strings.TrimRight(string(p), "\n"); msg != "" {
		Infof("%s", msg)
	}

	return len(p), nil
}
