package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// telegramHost API 主机名，写死不可配置。
//
// 若允许配置，一个被改写的配置项就能把消息（连同令牌）送到别处；
// 而这个地址本身不需要变。
const telegramHost = "api.telegram.org"

// maxMessageRunes 单条消息的字符上限。
// Telegram 的硬上限是 4096，留出余量：超限会被整条拒收，而告警宁可截断也不该丢。
const maxMessageRunes = 3500

// sendTimeout 单次发送的超时。
// 必须设：告警在后台协程里串行发送，一次卡死会让后续告警全部积压。
const sendTimeout = 10 * time.Second

// client Telegram 发送客户端
type client struct {
	// token 机器人令牌。会被拼进请求 URL 的路径段，故任何错误文本外泄前
	// 都要经 redact 抹掉它。
	token string
	// chatID 接收方
	chatID string
	http   *http.Client
}

// newClient 构造发送客户端。
//
// 显式设定 TLS 而非用默认值：这条链路要送出可以代管理员发消息的令牌，
// 故最低版本与证书校验都不能依赖外部环境的默认配置。
func newClient(token, chatID string) *client {
	return &client{
		token:  token,
		chatID: chatID,
		http: &http.Client{
			Timeout: sendTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
					// 主机名写死并显式校验证书。绝不设 InsecureSkipVerify：
					// 那会让任何能插到中间的人拿到令牌。
					ServerName: telegramHost,
				},
				DialContext: (&net.Dialer{
					Timeout: 5 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout: 5 * time.Second,
				// 告警是低频事件，不必为它长期占着连接
				MaxIdleConns:    2,
				IdleConnTimeout: 60 * time.Second,
			},
		},
	}
}

// sendPayload sendMessage 的请求体
type sendPayload struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
	// DisableNotification 不设：这几类告警都值得响一声
	//
	// 注意此结构体故意不含 parse_mode。缺省即纯文本，Telegram 不会解析
	// 消息里的任何标记——而消息中含用户提供的内容（申诉正文）。
	// 若开了 Markdown/HTML，一段构造好的申诉就能在告警里插入伪造的格式与链接。
	LinkPreviewOptions struct {
		IsDisabled bool `json:"is_disabled"`
	} `json:"link_preview_options"`
}

// apiResponse sendMessage 的响应
type apiResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

// send 发送一条纯文本消息。
//
// 返回的错误已抹去令牌，可直接落日志。
func (c *client) send(ctx context.Context, text string) error {
	payload := sendPayload{
		ChatID: c.chatID,
		Text:   truncate(text, maxMessageRunes),
	}
	// 告警里会出现用户提供的内容，抓取链接预览等于替对方发起一次请求
	payload.LinkPreviewOptions.IsDisabled = true

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode telegram payload: %w", err)
	}

	// 令牌在路径段里，这是 Telegram 的接口设计。config 已校验其字符集，
	// 不含 "/" 或 "?"，故无法改变请求指向。
	url := fmt.Sprintf("https://%s/bot%s/sendMessage", telegramHost, c.token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return c.redact(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// net/http 的错误带完整 URL，其中就有令牌
		return c.redact(err)
	}
	defer resp.Body.Close()

	var parsed apiResponse
	// 解析失败不当错误处理：状态码才是判据，响应体只用于取错误说明
	_ = json.NewDecoder(resp.Body).Decode(&parsed)

	if resp.StatusCode != http.StatusOK || !parsed.OK {
		description := parsed.Description
		if description == "" {
			description = resp.Status
		}
		// 描述来自 Telegram，正常不含令牌；仍统一过一遍 redact
		return fmt.Errorf("telegram 拒绝了这条消息: %s", redactIn(description, c.token))
	}

	return nil
}

// redact 抹去错误文本中的令牌，使其可以安全落日志。
//
// 这不是多余的谨慎：Telegram 把令牌放在 URL 路径里，而 net/http 的错误
// （超时、DNS 失败、连接被拒）一律附上完整 URL。直接 logger.Errorf("%v", err)
// 会把令牌写进 MySQL 日志表，而那张表在管理页可读。
func (c *client) redact(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", redactIn(err.Error(), c.token))
}

// redactIn 把文本中出现的令牌替换为占位符
func redactIn(text, token string) string {
	if token == "" {
		return text
	}

	text = strings.ReplaceAll(text, token, "<token>")

	// 令牌的两段可能被分开处理（例如某些错误只带路径前缀），故 ID 段单独再抹一次。
	// ID 段本身不足以发消息，但它标识了机器人，没必要留在日志里。
	if id, _, found := strings.Cut(token, ":"); found && id != "" {
		text = strings.ReplaceAll(text, id, "<token>")
	}

	return text
}
