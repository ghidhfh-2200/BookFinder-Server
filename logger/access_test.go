package logger

import (
	"strings"
	"testing"
	"time"
)

// TestAccessRecordString 记录应含状态码、方法、路径、来源与耗时
func TestAccessRecordString(t *testing.T) {
	got := AccessRecord{
		Status:   200,
		Method:   "GET",
		Path:     "/api/admin/bans",
		ClientIP: "::1",
		Latency:  505800 * time.Nanosecond,
	}.String()

	for _, want := range []string{"200", "GET", "/api/admin/bans", "::1"} {
		if !strings.Contains(got, want) {
			t.Errorf("记录应含 %q，实际为 %q", want, got)
		}
	}
}

// TestAccessRecordIncludesErrors 处理链登记的错误应出现在记录里。
// 它们不体现在状态码上，不记下来这类问题就无从发现。
func TestAccessRecordIncludesErrors(t *testing.T) {
	got := AccessRecord{
		Status: 200,
		Method: "POST",
		Path:   "/api/libraries",
		Errors: "写入索引失败",
	}.String()

	if !strings.Contains(got, "写入索引失败") {
		t.Errorf("记录应含登记的错误，实际为 %q", got)
	}
}

// TestAccessLevelByStatus 日志级别按状态码判定，与耗时无关。
//
// 这是本次修复的要点：旧实现把状态码与耗时格式化成一行文本，再用
// strings.Contains 找 "|5" 来判断 5xx——而 `| 200 | 505.8µs |` 里正好含有它，
// 于是耗时以 5 开头的正常请求全被记成 ERROR（实测 23 页）。
// 现在状态码直接来自 c.Writer.Status()，耗时再怎么变都影响不到级别。
func TestAccessLevelByStatus(t *testing.T) {
	cases := []struct {
		status  int
		latency time.Duration
		want    string
	}{
		// 曾被误判为 5xx 的那几种耗时
		{200, 505800 * time.Nanosecond, LevelDebug},
		{200, 5390 * time.Microsecond, LevelDebug},
		{200, 508500 * time.Nanosecond, LevelDebug},
		// 曾被误判为 4xx 的耗时
		{200, 423100 * time.Nanosecond, LevelDebug},
		{304, time.Millisecond, LevelDebug},
		// 真正的错误
		{400, time.Millisecond, LevelWarn},
		{403, time.Millisecond, LevelWarn},
		{429, time.Millisecond, LevelWarn},
		{500, time.Millisecond, LevelError},
		{503, time.Millisecond, LevelError},
	}

	for _, tc := range cases {
		got := levelFor(AccessRecord{Status: tc.status, Latency: tc.latency})
		if got != tc.want {
			t.Errorf("状态 %d、耗时 %s 应记为 %s，实际为 %s",
				tc.status, tc.latency, tc.want, got)
		}
	}
}

// TestAccessLevelWithErrors 状态码正常但登记了错误时应留痕
func TestAccessLevelWithErrors(t *testing.T) {
	got := levelFor(AccessRecord{Status: 200, Errors: "某处出错"})
	if got != LevelWarn {
		t.Errorf("登记了错误应记为 WARN，实际为 %s", got)
	}
}
