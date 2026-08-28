package checker

import (
	"strings"
	"testing"

	"bookfinder-backend/types"
)

// TestSanitizeStripsControlChars 控制字符应被剥离，换行与制表符保留
func TestSanitizeStripsControlChars(t *testing.T) {
	// 含 ESC（终端转义的起点）、NUL、退格
	input := "正常内容\x1b[31m红色\x00\x08尾部"
	got := SanitizePlainText(input)

	for _, bad := range []string{"\x1b", "\x00", "\x08"} {
		if strings.Contains(got, bad) {
			t.Errorf("清洗后仍含控制字符 %q: %q", bad, got)
		}
	}
	// 可打印部分要留下来，不能连正文一起丢掉
	if !strings.Contains(got, "正常内容") || !strings.Contains(got, "尾部") {
		t.Errorf("正文被误删: %q", got)
	}
}

// TestSanitizeKeepsNewlinesAndTabs 换行与制表符是正常排版手段，应保留
func TestSanitizeKeepsNewlinesAndTabs(t *testing.T) {
	got := SanitizePlainText("第一行\n第二行\t缩进")

	if !strings.Contains(got, "\n") {
		t.Error("换行应保留")
	}
	if !strings.Contains(got, "\t") {
		t.Error("制表符应保留")
	}
}

// TestSanitizeNormalizesCRLF 回车并入换行，避免 Windows 换行留下多余字符
func TestSanitizeNormalizesCRLF(t *testing.T) {
	got := SanitizePlainText("第一行\r\n第二行")

	if strings.Contains(got, "\r") {
		t.Errorf("回车应被剥离: %q", got)
	}
	if got != "第一行\n第二行" {
		t.Errorf("CRLF 应规整为 LF，实际为 %q", got)
	}
}

// TestSanitizeStripsInvisible 零宽与方向控制字符不可见却能伪装内容，应剥离
func TestSanitizeStripsInvisible(t *testing.T) {
	// 零宽空格、零宽连接符、从右到左覆盖、字节序标记
	input := "内\u200b容\u200d有\u202e隐\ufeff藏"
	got := SanitizePlainText(input)

	if got != "内容有隐藏" {
		t.Errorf("零宽与方向字符应被剥离，实际为 %q", got)
	}
}

// TestSanitizeCollapsesBlankLines 大量空行会占满管理员的阅读版面
func TestSanitizeCollapsesBlankLines(t *testing.T) {
	got := SanitizePlainText("上文\n\n\n\n\n\n下文")

	if strings.Contains(got, "\n\n\n") {
		t.Errorf("连续空行应折叠，实际为 %q", got)
	}
}

// TestSanitizeTrimsWhitespace 首尾空白应去掉
func TestSanitizeTrimsWhitespace(t *testing.T) {
	if got := SanitizePlainText("  \n 内容 \n  "); got != "内容" {
		t.Errorf("首尾空白应去掉，实际为 %q", got)
	}
}

// TestSanitizeKeepsMarkupAsLiteral HTML 与脚本片段按字面量保留。
// 内容以纯文本存储、由 React 的文本节点渲染，尖括号会被自动转义，
// 故此处不做 HTML 转义——否则管理员看到的是 &lt; 这类难读的实体。
func TestSanitizeKeepsMarkupAsLiteral(t *testing.T) {
	input := `<script>alert(1)</script> 和 <img src=x onerror=alert(1)>`
	got := SanitizePlainText(input)

	if got != input {
		t.Errorf("标记文本应原样保留为字面量，实际为 %q", got)
	}
}

// TestValidateAppealMessageRejectsEmpty 空内容与纯控制字符都算空
func TestValidateAppealMessageRejectsEmpty(t *testing.T) {
	for _, input := range []string{"", "   ", "\n\n", "\x00\x1b"} {
		if _, err := ValidateAppealMessage(input); err == nil {
			t.Errorf("内容 %q 应被判为空", input)
		}
	}
}

// TestValidateAppealMessageEnforcesLength 超长内容应被拒绝，按 rune 计数
func TestValidateAppealMessageEnforcesLength(t *testing.T) {
	// 中文按一个字符计，不是三个字节
	atLimit := strings.Repeat("字", types.MaxAppealMessageLength)
	if _, err := ValidateAppealMessage(atLimit); err != nil {
		t.Errorf("刚好达到上限应通过，实际报错: %v", err)
	}

	tooLong := strings.Repeat("字", types.MaxAppealMessageLength+1)
	if _, err := ValidateAppealMessage(tooLong); err == nil {
		t.Error("超出上限应被拒绝")
	}
}

// TestValidateAppealMessageReturnsCleaned 返回值应是清洗后的文本。
// 注意 ESC 被剥离后，残留的 "[0m" 只是普通可打印字符，不再构成终端转义，
// 故按字面量保留——方括号是正常写作会用到的符号，不该一并删掉。
func TestValidateAppealMessageReturnsCleaned(t *testing.T) {
	got, err := ValidateAppealMessage("  我\u200b是误封\x1b[0m  ")
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if got != "我是误封[0m" {
		t.Errorf("应返回清洗后的文本，实际为 %q", got)
	}
}

// TestValidateAppealReviewRejectsPending 处理结果只能是终态
func TestValidateAppealReviewRejectsPending(t *testing.T) {
	for _, status := range []types.AppealStatus{types.AppealPending, "", "bogus"} {
		if _, err := ValidateAppealReview(&types.AppealReviewRequest{Status: status}); err == nil {
			t.Errorf("状态 %q 不应作为处理结果", status)
		}
	}

	for _, status := range []types.AppealStatus{types.AppealAccepted, types.AppealRejected} {
		if _, err := ValidateAppealReview(&types.AppealReviewRequest{Status: status}); err != nil {
			t.Errorf("状态 %q 应可作为处理结果，实际报错: %v", status, err)
		}
	}
}

// TestValidateAppealReviewCleansNote 管理员备注同样要清洗
func TestValidateAppealReviewCleansNote(t *testing.T) {
	got, err := ValidateAppealReview(&types.AppealReviewRequest{
		Status:    types.AppealRejected,
		AdminNote: "确认为\x1b[31m脚本\u200b刷量",
	})
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if got != "确认为[31m脚本刷量" {
		t.Errorf("备注应被清洗，实际为 %q", got)
	}
}
