package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"bookfinder-backend/types"
)

// implicitTLSPort 隐式 TLS 端口：连接建立即加密。
// 其余端口按 STARTTLS 处理——先明文握手，再升级。
const implicitTLSPort = 465

// dialTimeout 建连与握手的超时。
// 必须设：QQ 等服务商在网络不畅时可能长时间不响应，而告警是串行发送的。
const dialTimeout = 15 * time.Second

// mailer 一个 SMTP 发信客户端
type mailer struct {
	cfg      types.EmailConfig
	password string
}

// newMailer 构造发信客户端
func newMailer(cfg types.EmailConfig, password string) *mailer {
	return &mailer{cfg: cfg, password: password}
}

// send 发送一条告警邮件。
//
// text 是已拼好的纯文本消息，首行作为主题、全文作为正文——两处用同一份内容，
// 因为主题栏在手机通知里往往是唯一可见的部分。
func (m *mailer) send(ctx context.Context, text string) error {
	subject, body := splitSubject(text)

	message, err := m.compose(subject, body)
	if err != nil {
		return err
	}

	// smtp 包不接受 context，故用一个带超时的连接把它套住：
	// ctx 取消或超时到达时连接被关闭，阻塞中的读写随即返回错误。
	conn, err := m.dial(ctx)
	if err != nil {
		return m.redact(err)
	}
	defer conn.Close()

	if err := m.deliver(conn, message); err != nil {
		return m.redact(err)
	}

	return nil
}

// dial 建立到 SMTP 服务器的连接。
//
// 465 端口直接在 TLS 上建连；其余端口先明文建连，稍后由 deliver 发 STARTTLS 升级。
// 两种情形都校验证书并要求 TLS 1.2 起——这条链路要送出发信密码。
func (m *mailer) dial(ctx context.Context) (net.Conn, error) {
	address := net.JoinHostPort(m.cfg.Host, fmt.Sprint(m.cfg.Port))

	dialer := &net.Dialer{Timeout: dialTimeout}

	if m.cfg.Port == implicitTLSPort {
		return tls.DialWithDialer(dialer, "tcp", address, m.tlsConfig())
	}

	return dialer.DialContext(ctx, "tcp", address)
}

// tlsConfig 加密参数。
//
// ServerName 取配置里的主机名，证书按它校验。绝不设 InsecureSkipVerify：
// 那会让任何能插到中间的人拿到发信密码。
func (m *mailer) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName: m.cfg.Host,
		MinVersion: tls.VersionTLS12,
	}
}

// deliver 在已建立的连接上完成 SMTP 会话。
//
// 非隐式 TLS 端口必须先 STARTTLS 升级再认证：不升级就认证等于把密码明文发出去。
// 服务器不支持 STARTTLS 时直接失败，而不是退回明文——退回是最坏的选择，
// 它在看起来正常的情况下泄露凭据。
func (m *mailer) deliver(conn net.Conn, message []byte) error {
	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()

	if m.cfg.Port != implicitTLSPort {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return fmt.Errorf("SMTP 服务器 %s:%d 不支持 STARTTLS，"+
				"发信密码会以明文传输，已中止。请改用 465 端口",
				m.cfg.Host, m.cfg.Port)
		}
		if err := client.StartTLS(m.tlsConfig()); err != nil {
			return err
		}
	}

	// 认证放在 TLS 之后，此时链路已加密
	auth := smtp.PlainAuth("", m.cfg.Username, m.password, m.cfg.Host)
	if err := client.Auth(auth); err != nil {
		return err
	}

	if err := client.Mail(m.cfg.Sender()); err != nil {
		return err
	}
	if err := client.Rcpt(m.cfg.To); err != nil {
		return err
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	return client.Quit()
}

// compose 拼出符合 RFC 5322 的邮件报文。
//
// 头部与正文以空行分隔，故头部里绝不能出现换行——那会让后续内容被当作新的头部，
// 或提前结束头部区段把内容挤进正文。主题里含用户提供的文本（申诉正文的首行
// 可能进主题），故必须防这一手。
//
// 做法是两道：地址类头部若含换行直接拒绝发送（那只可能是配置被写坏，
// 不该带着一个畸形报文继续），主题则经 RFC 2047 编码——编码后的结果
// 只含 ASCII 可打印字符，换行天然不可能残留，中文也不会乱码。
func (m *mailer) compose(subject, body string) ([]byte, error) {
	sender := m.cfg.Sender()

	// 地址来自管理页配置，正常不含换行；但配置写坏时宁可不发也不发畸形报文
	for label, value := range map[string]string{
		"发件地址": sender,
		"收件地址": m.cfg.To,
	} {
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("%s %q 含换行符，可用于伪造邮件头，已拒绝发送",
				label, value)
		}
	}

	var builder strings.Builder

	builder.WriteString("From: " + sender + "\r\n")
	builder.WriteString("To: " + m.cfg.To + "\r\n")
	// RFC 2047 编码：中文主题不乱码，且结果只含 ASCII，换行无从残留
	builder.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n")
	builder.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	builder.WriteString("MIME-Version: 1.0\r\n")
	// 纯文本，不是 HTML：正文含用户提供的申诉内容，按 HTML 渲染会让一段
	// 构造好的申诉在邮件客户端里变成可点击的链接或伪造的排版
	builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	builder.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	// 空行分隔头部与正文
	builder.WriteString("\r\n")

	// 正文按 CRLF 换行，并做点填充：行首单独一个点是 SMTP 的数据结束标记
	builder.WriteString(stuffDots(body))

	return []byte(builder.String()), nil
}

// stuffDots 把正文规整为 CRLF 换行，并对行首的点做填充。
//
// SMTP 用单独一行的 "." 表示数据结束，故正文里行首的点必须写成两个，
// 否则一条恰好以点开头的内容会把邮件在那里截断。
func stuffDots(body string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")

	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, ".") {
			lines[i] = "." + line
		}
	}

	return strings.Join(lines, "\r\n") + "\r\n"
}

// redact 抹去错误文本中的发信密码，使其可以安全落日志。
//
// SMTP 的错误多来自服务器应答，正常不含密码；但认证失败时部分实现会回显
// 部分凭据，而这些错误会进 MySQL 日志表，那张表管理页可读。
func (m *mailer) redact(err error) error {
	if err == nil {
		return nil
	}

	text := err.Error()
	if m.password != "" {
		text = strings.ReplaceAll(text, m.password, "<password>")
	}

	return errors.New(text)
}

// splitSubject 把消息拆成主题与正文。
//
// 首行即主题（那是「🚫 自动封禁」这类标题），全文作正文。主题单独取出是因为
// 手机上的邮件通知往往只显示主题，而正文要滑开才看得到。
func splitSubject(text string) (subject, body string) {
	subject, _, found := strings.Cut(text, "\n")
	if !found {
		subject = text
	}

	// 首行可能残留 CR（原文是 CRLF 换行时），一并压掉：
	// 主题里的任何换行都不该带进邮件头
	subject = strings.ReplaceAll(subject, "\r", " ")

	// 截短只为可读：过长的主题会被邮件客户端与服务商截掉，而主题栏在手机
	// 通知里往往是唯一可见的部分，留一个读得完的长度比塞满更有用。
	//
	// 与安全无关——实测 mime.QEncoding.Encode 无论多长都返回单行、不折行，
	// 故长主题不会往头部引入换行。
	const maxSubjectRunes = 80
	subject = truncate(subject, maxSubjectRunes)

	return subject, text
}
