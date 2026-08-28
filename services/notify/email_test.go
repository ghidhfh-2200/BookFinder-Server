package notify

import (
	"strings"
	"testing"

	"bookfinder-backend/types"
)

// testEmailConfig 一份可用的发信配置
func testEmailConfig() types.EmailConfig {
	return types.EmailConfig{
		Enabled:  true,
		Host:     "smtp.qq.com",
		Port:     465,
		Username: "me@qq.com",
		To:       "me@qq.com",
	}
}

// TestComposeRejectsHeaderInjection 地址含换行时必须拒绝发送。
//
// 邮件头与正文以空行分隔，故头部里的换行能让后续内容被当作新的头部——
// 例如追加一个 Bcc 把告警抄送到别处。地址来自管理页配置，保存时已校验，
// 此处是发送前的第二道。
func TestComposeRejectsHeaderInjection(t *testing.T) {
	cases := map[string]types.EmailConfig{
		"收件地址含换行": func() types.EmailConfig {
			c := testEmailConfig()
			c.To = "me@qq.com\r\nBcc: attacker@evil.com"
			return c
		}(),
		"发件地址含换行": func() types.EmailConfig {
			c := testEmailConfig()
			c.From = "me@qq.com\nBcc: attacker@evil.com"
			return c
		}(),
	}

	for name, cfg := range cases {
		_, err := newMailer(cfg, "pw").compose("主题", "正文")
		if err == nil {
			t.Errorf("%s：应拒绝发送", name)
		}
	}
}

// TestComposeEncodesSubject 主题经 RFC 2047 编码。
//
// 这既让中文主题不乱码，也顺带消除了主题里的换行——编码后只剩 ASCII
// 可打印字符。主题可能含申诉正文的首行，那是用户完全控制的内容。
func TestComposeEncodesSubject(t *testing.T) {
	raw, err := newMailer(testEmailConfig(), "pw").compose(
		"🚫 自动封禁\r\nBcc: attacker@evil.com", "正文")
	if err != nil {
		t.Fatalf("拼装失败: %v", err)
	}

	message := string(raw)
	headers, _, found := strings.Cut(message, "\r\n\r\n")
	if !found {
		t.Fatal("报文缺少头部与正文的分隔空行")
	}

	// 关键断言是「头部没有多出一行」，而非「不含 Bcc 这三个字」：
	// 编码后 CRLF 变成字面量 =0D=0A，Bcc 三个字仍在其中却已无害——
	// 它待在 Subject 那一行里，不构成新的头部。
	//
	// 故逐行检查：每一行都必须是我们自己写的那几个头部之一。
	for _, line := range strings.Split(headers, "\r\n") {
		name, _, ok := strings.Cut(line, ":")
		if !ok {
			t.Errorf("头部出现了不含冒号的行，可能是注入的续行: %q", line)
			continue
		}
		switch name {
		case "From", "To", "Subject", "Date", "MIME-Version",
			"Content-Type", "Content-Transfer-Encoding":
		default:
			t.Errorf("头部出现了未预期的字段 %q，整段头部:\n%s", name, headers)
		}
	}

	// 编码后的主题里不该有真正的换行
	subjectLine := ""
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(line, "Subject:") {
			subjectLine = line
		}
	}
	if strings.ContainsAny(subjectLine, "\r\n") {
		t.Errorf("主题行自身含换行: %q", subjectLine)
	}

	// 中文主题必须编码，否则部分客户端显示乱码
	if !strings.Contains(subjectLine, "=?UTF-8?") {
		t.Errorf("主题未经 RFC 2047 编码: %q", subjectLine)
	}
}

// TestComposeIsPlainText 正文必须声明为纯文本。
//
// 正文含用户提供的申诉内容，按 HTML 渲染会让一段构造好的申诉在邮件客户端里
// 变成可点击链接或伪造排版。
func TestComposeIsPlainText(t *testing.T) {
	raw, err := newMailer(testEmailConfig(), "pw").compose("主题", "正文")
	if err != nil {
		t.Fatalf("拼装失败: %v", err)
	}

	message := string(raw)
	if !strings.Contains(message, "Content-Type: text/plain; charset=UTF-8") {
		t.Errorf("未声明为纯文本:\n%s", message)
	}
	if strings.Contains(message, "text/html") {
		t.Error("正文被声明为 HTML")
	}
}

// TestStuffDots 行首的点必须加倍。
//
// SMTP 用单独一行的 "." 表示数据结束：正文里一行恰好以点开头时，
// 不加倍会把邮件在那里截断，后面的内容全部丢失。
func TestStuffDots(t *testing.T) {
	got := stuffDots("正常一行\n.开头的一行\n..两个点\n结尾")

	if !strings.Contains(got, "\r\n..开头的一行\r\n") {
		t.Errorf("行首的点未加倍: %q", got)
	}
	if !strings.Contains(got, "\r\n...两个点\r\n") {
		t.Errorf("两个点的行未正确加倍: %q", got)
	}
	// 全文须用 CRLF 换行
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("存在未转为 CRLF 的换行: %q", got)
	}
}

// TestSplitSubjectUsesFirstLine 主题取首行，正文保留全文
func TestSplitSubjectUsesFirstLine(t *testing.T) {
	subject, body := splitSubject("🚫 自动封禁\n处置范围：1.2.3.4\n原因：突发过多")

	if subject != "🚫 自动封禁" {
		t.Errorf("主题 = %q，期望首行", subject)
	}
	if !strings.Contains(body, "处置范围") {
		t.Errorf("正文丢失了后续内容: %q", body)
	}
}

// TestSplitSubjectTruncatesLongLine 过长的首行应被截断，避免被服务商截掉
func TestSplitSubjectTruncatesLongLine(t *testing.T) {
	subject, _ := splitSubject(strings.Repeat("封", 200) + "\n正文")

	if runes := len([]rune(subject)); runes > 80 {
		t.Errorf("主题 %d 个字符，超过上限 80", runes)
	}
}

// TestMailerRedactsPassword 错误文本外泄前必须抹掉发信密码。
// 这些错误会进 MySQL 日志表，而那张表管理页可读。
func TestMailerRedactsPassword(t *testing.T) {
	const password = "qq_auth_code_secret"
	m := newMailer(testEmailConfig(), password)

	err := m.redact(errPlain("535 login fail for " + password))
	if err == nil {
		t.Fatal("应返回错误")
	}
	if strings.Contains(err.Error(), password) {
		t.Errorf("密码未被抹除: %v", err)
	}
	if !strings.Contains(err.Error(), "535") {
		t.Errorf("诊断信息丢失: %v", err)
	}

	if m.redact(nil) != nil {
		t.Error("nil 错误被包装了")
	}
}

// TestEmailUsable 各项齐备才可发信
func TestEmailUsable(t *testing.T) {
	full := testEmailConfig()

	if !full.Usable("pw") {
		t.Error("齐备的配置应可用")
	}
	if full.Usable("") {
		t.Error("缺密码时不应可用")
	}

	for name, mutate := range map[string]func(*types.EmailConfig){
		"未启用":  func(c *types.EmailConfig) { c.Enabled = false },
		"缺主机":  func(c *types.EmailConfig) { c.Host = "" },
		"缺账号":  func(c *types.EmailConfig) { c.Username = "" },
		"缺收件人": func(c *types.EmailConfig) { c.To = "" },
		"端口为零": func(c *types.EmailConfig) { c.Port = 0 },
	} {
		cfg := testEmailConfig()
		mutate(&cfg)
		if cfg.Usable("pw") {
			t.Errorf("%s：不应可用", name)
		}
	}
}

// TestEmailSenderFallsBackToUsername From 留空时用认证账号
func TestEmailSenderFallsBackToUsername(t *testing.T) {
	cfg := testEmailConfig()
	if got := cfg.Sender(); got != cfg.Username {
		t.Errorf("Sender() = %q，期望回落到 %q", got, cfg.Username)
	}

	cfg.From = "alerts@example.com"
	if got := cfg.Sender(); got != "alerts@example.com" {
		t.Errorf("Sender() = %q，期望使用 From", got)
	}
}

// errPlain 构造一个普通错误，避免为测试引入额外依赖
type errPlain string

func (e errPlain) Error() string { return string(e) }
