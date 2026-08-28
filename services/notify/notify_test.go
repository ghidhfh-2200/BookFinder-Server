package notify

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// 一个形状合法的假令牌，仅用于测试
const fakeToken = "123456789:AAHfake_test_token_value_0123456789"

// TestRedactRemovesToken 确认错误文本外泄前抹掉了令牌。
//
// 这是本包最重要的一条保证：Telegram 把令牌放在 URL 路径里，而 net/http 的错误
// 一律附上完整 URL。不抹的话，一次网络故障就会把令牌写进 MySQL 日志表，
// 而那张表在管理页可读。
func TestRedactRemovesToken(t *testing.T) {
	c := newClient(fakeToken, "12345")

	// 形状取自 net/http 真实的错误文本
	original := fmt.Errorf(`Post "https://api.telegram.org/bot%s/sendMessage": `+
		`dial tcp: i/o timeout`, fakeToken)

	got := c.redact(original).Error()

	if strings.Contains(got, fakeToken) {
		t.Fatalf("令牌未被抹除: %s", got)
	}
	// ID 段单独也不该留下
	if strings.Contains(got, "123456789") {
		t.Errorf("令牌的 ID 段未被抹除: %s", got)
	}
	// 抹除不该把有用的诊断信息一并吃掉
	if !strings.Contains(got, "i/o timeout") {
		t.Errorf("错误原因丢失了: %s", got)
	}
}

// TestRedactNilError nil 错误应原样返回，不该被包成一个非 nil 的 error
func TestRedactNilError(t *testing.T) {
	if err := newClient(fakeToken, "1").redact(nil); err != nil {
		t.Errorf("nil 错误被包装成了 %v", err)
	}
}

// TestRedactHandlesWrappedError 确认 redact 对任意错误都成立，包括包装过的
func TestRedactHandlesWrappedError(t *testing.T) {
	c := newClient(fakeToken, "1")
	wrapped := fmt.Errorf("发送失败: %w", errors.New("bot"+fakeToken+" rejected"))

	if got := c.redact(wrapped).Error(); strings.Contains(got, fakeToken) {
		t.Errorf("包装错误中的令牌未被抹除: %s", got)
	}
}

// TestSanitizeValueFlattensNewlines 确认换行被压平。
//
// 这是防伪造的关键：告警是「标签：取值」的逐行结构，申诉正文由用户完全控制。
// 若换行原样透传，一段构造好的申诉可以在管理员手机上伪造出额外的告警行。
func TestSanitizeValueFlattensNewlines(t *testing.T) {
	forged := "我要申诉\n来源：127.0.0.1\n原因：管理员手动解封"

	got := sanitizeValue(forged)

	if strings.Contains(got, "\n") {
		t.Fatalf("换行未被压平: %q", got)
	}
	// 内容本身要保留，压平不等于丢弃
	if !strings.Contains(got, "我要申诉") {
		t.Errorf("正文内容丢失: %q", got)
	}
}

// TestSanitizeValueStripsInvisible 确认零宽与双向控制符被剥除。
// 双向控制符能让显示顺序与实际内容不一致，足以把一个 IP 显示成另一个。
//
// 一律用转义码写这些字符：直接写字面量会让源文件混入不可见字节，
// 其中 U+FEFF 更会被 Go 当作非法的字节序标记而拒绝编译。
func TestSanitizeValueStripsInvisible(t *testing.T) {
	cases := map[string]string{
		"零宽空格":     "1.2\u200b.3.4",
		"零宽不连字":    "1.2\u200c.3.4",
		"零宽连字":     "1.2\u200d.3.4",
		"单词连接符":    "1.2\u2060.3.4",
		"BOM":      "1.2\ufeff.3.4",
		"方向覆盖 RLO": "1.2\u202e.3.4",
		"方向隔离 LRI": "1.2\u2066.3.4",
	}

	for name, input := range cases {
		if got := sanitizeValue(input); got != "1.2.3.4" {
			t.Errorf("%s：sanitizeValue(%q) = %q，期望 %q", name, input, got, "1.2.3.4")
		}
	}
}

// TestBuildMessageSkipsEmptyFields 空取值的行应整行省略，
// 否则告警里会出现「原因：」这样的空栏
func TestBuildMessageSkipsEmptyFields(t *testing.T) {
	got := buildMessage("标题", []field{
		{label: "有值", value: "x"},
		{label: "空值", value: ""},
		{label: "全是空白", value: "  \n\t "},
	})

	if strings.Contains(got, "空值") || strings.Contains(got, "全是空白") {
		t.Errorf("空取值的行未被省略: %q", got)
	}
	if !strings.Contains(got, "有值：x") {
		t.Errorf("有值的行丢失: %q", got)
	}
}

// TestBuildMessageStructure 确认消息为「标题 + 每字段一行」
func TestBuildMessageStructure(t *testing.T) {
	got := buildMessage("🚫 自动封禁", []field{
		{label: "处置范围", value: "1.2.3.4"},
		{label: "原因", value: "突发请求过多"},
	})

	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("期望 3 行，得到 %d 行: %q", len(lines), got)
	}
	if lines[0] != "🚫 自动封禁" {
		t.Errorf("首行应为标题，得到 %q", lines[0])
	}
	if lines[1] != "处置范围：1.2.3.4" {
		t.Errorf("字段行格式不符: %q", lines[1])
	}
}

// TestTruncateByRunes 确认按字符而非字节截断。
// 告警里大量是中文，按字节算会既提前截断、又可能切坏一个字符。
func TestTruncateByRunes(t *testing.T) {
	// 30 个汉字，90 字节
	text := strings.Repeat("封", 30)

	if got := truncate(text, 30); got != text {
		t.Errorf("未超限的文本被改动了: %q", got)
	}

	got := truncate(text, 20)
	if runes := len([]rune(got)); runes > 20 {
		t.Errorf("截断后有 %d 个字符，超过上限 20: %q", runes, got)
	}
	if !strings.HasSuffix(got, "（已截断）") {
		t.Errorf("截断后未标注: %q", got)
	}
	// 切坏字符会产生替换符
	if strings.ContainsRune(got, '�') {
		t.Errorf("截断切坏了一个字符: %q", got)
	}
}

// TestDispatchWithoutStartIsSafe 未启动时投递不应 panic。
// 凭据未配置是最常见的情形，三个触发点都在请求路径上。
func TestDispatchWithoutStartIsSafe(t *testing.T) {
	mu.Lock()
	queue = nil
	mu.Unlock()

	// 不 panic 即通过
	AutoBan(1, "1.2.3.4", "原因", "详情")
	NetworkAnomaly("原因", "详情")
	Appeal("1.2.3.4", 1, 3, "封禁原因", "申诉内容")
}
