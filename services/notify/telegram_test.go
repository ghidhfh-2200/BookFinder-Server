package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// stubTransport 把请求改指到本地测试服务器，同时保留原始 URL 供断言。
//
// 只替换 Transport 而不改 telegramHost：主机名写死是一项安全约束
// （见 telegram.go 的说明），不该为了测试给它开一个可配置的口子。
type stubTransport struct {
	server *httptest.Server
	// lastURL 客户端实际构造出的 URL，用于确认令牌与方法名的位置
	lastURL string
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	s.lastURL = req.URL.String()

	target, err := url.Parse(s.server.URL)
	if err != nil {
		return nil, err
	}
	// 改指本地，路径与请求体原样保留
	rewritten := req.Clone(req.Context())
	rewritten.URL.Scheme = target.Scheme
	rewritten.URL.Host = target.Host

	return s.server.Client().Transport.RoundTrip(rewritten)
}

// captured 测试服务器收到的一次请求
type captured struct {
	method string
	path   string
	body   map[string]any
}

// newStubbedClient 构造一个把请求送往本地服务器的客户端
func newStubbedClient(t *testing.T, status int, response string) (*client, *captured, *stubTransport) {
	t.Helper()

	got := &captured{}

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			got.method = r.Method
			got.path = r.URL.Path

			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &got.body)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(response))
		}))
	t.Cleanup(server.Close)

	transport := &stubTransport{server: server}

	c := newClient(fakeToken, "-1001234567890")
	c.http = &http.Client{Transport: transport}

	return c, got, transport
}

// TestSendRequestShape 确认发出的请求符合 Telegram 的接口约定，
// 且不含 parse_mode——那是防伪造的关键（见下一个测试）
func TestSendRequestShape(t *testing.T) {
	c, got, transport := newStubbedClient(t, http.StatusOK, `{"ok":true}`)

	if err := c.send(context.Background(), "标题\n字段：值"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	if got.method != http.MethodPost {
		t.Errorf("方法应为 POST，实际 %s", got.method)
	}
	if want := "/bot" + fakeToken + "/sendMessage"; got.path != want {
		t.Errorf("路径 = %q，期望 %q", got.path, want)
	}
	// 令牌在路径段里，故它绝不能含 "/" 或 "?"——否则会改变请求指向。
	// config.validateTelegram 负责拦住那种令牌，此处确认位置确实如此。
	if !strings.HasPrefix(transport.lastURL, "https://"+telegramHost+"/bot") {
		t.Errorf("URL 未指向写死的主机: %s", transport.lastURL)
	}

	if got.body["chat_id"] != "-1001234567890" {
		t.Errorf("chat_id = %v", got.body["chat_id"])
	}
	if text, _ := got.body["text"].(string); !strings.Contains(text, "标题") {
		t.Errorf("text 内容不符: %v", got.body["text"])
	}
}

// TestSendOmitsParseMode 请求体绝不能带 parse_mode。
//
// 这是申诉告警防伪造的最后一道：消息里含用户完全控制的申诉正文，
// 一旦启用 Markdown/HTML 解析，一段构造好的申诉就能在管理员手机上
// 插入伪造的格式、链接乃至看似出自服务端的内容。
func TestSendOmitsParseMode(t *testing.T) {
	c, got, _ := newStubbedClient(t, http.StatusOK, `{"ok":true}`)

	if err := c.send(context.Background(), "*粗体* [链接](http://evil)"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	if _, present := got.body["parse_mode"]; present {
		t.Error("请求体带了 parse_mode，用户提供的内容会被当作标记解析")
	}

	// 链接预览要关掉：抓取预览等于替对方发起一次请求
	preview, _ := got.body["link_preview_options"].(map[string]any)
	if preview == nil || preview["is_disabled"] != true {
		t.Errorf("未禁用链接预览: %v", got.body["link_preview_options"])
	}
}

// TestSendRejectsAPIError Telegram 返回失败时应报错，且错误里不带令牌
func TestSendRejectsAPIError(t *testing.T) {
	c, _, _ := newStubbedClient(t, http.StatusBadRequest,
		`{"ok":false,"description":"chat not found"}`)

	err := c.send(context.Background(), "内容")
	if err == nil {
		t.Fatal("接口返回失败时应报错")
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("错误应包含 Telegram 给出的原因: %v", err)
	}
	if strings.Contains(err.Error(), fakeToken) {
		t.Errorf("错误里回显了令牌: %v", err)
	}
}

// TestSendTruncatesLongText 超长消息应被截断而非整条被拒
func TestSendTruncatesLongText(t *testing.T) {
	c, got, _ := newStubbedClient(t, http.StatusOK, `{"ok":true}`)

	if err := c.send(context.Background(), strings.Repeat("封", maxMessageRunes+500)); err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	text, _ := got.body["text"].(string)
	if runes := len([]rune(text)); runes > maxMessageRunes {
		t.Errorf("消息 %d 个字符，超过上限 %d", runes, maxMessageRunes)
	}
}
