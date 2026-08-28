package checker

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"bookfinder-backend/types"
)

// SanitizePlainText 把用户输入规整为纯文本。
//
// 做法是白名单式的：只保留可打印字符、换行与制表符，其余一律剥离。
// 这样处理后的内容不含控制字符与零宽字符，无法用于终端转义、日志伪造，
// 也不会因不可见字符干扰管理员阅读。
//
// 注意：此处不做 HTML 转义。内容以纯文本存储，前端用 React 的文本节点渲染，
// 尖括号与引号会被自动转为字面量而非标签，故 HTML 注入在渲染端已经不成立；
// 若在此处转义，管理员看到的反而是 &lt; 这类难读的实体。
func SanitizePlainText(input string) string {
	var builder strings.Builder
	builder.Grow(len(input))

	for _, r := range input {
		switch {
		// 换行与制表符保留，用户会用它们分段
		case r == '\n' || r == '\t':
			builder.WriteRune(r)
		// 回车统一并入换行，避免 Windows 换行留下多余字符
		case r == '\r':
			continue
		// 其余控制字符（含 ESC、退格、NUL）一律剥离
		case unicode.IsControl(r):
			continue
		// 零宽字符与方向覆盖符：不可见，可用于伪造视觉内容
		case isInvisible(r):
			continue
		default:
			builder.WriteRune(r)
		}
	}

	// 折叠连续空行，顺便去掉首尾空白
	return collapseBlankLines(strings.TrimSpace(builder.String()))
}

// isInvisible 判断是否为零宽或方向控制字符。
// 这类字符不可见却会影响文本呈现，常被用来伪装内容。
// 一律写成转义码：直接写字面量会让源文件混入不可见字节，其中 U+FEFF 更会被 Go 当作
// 非法的字节序标记而拒绝编译。
func isInvisible(r rune) bool {
	switch r {
	case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff', // 零宽字符
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e', // 方向覆盖
		'\u2066', '\u2067', '\u2068', '\u2069': // 方向隔离
		return true
	default:
		return false
	}
}

// collapseBlankLines 把三个以上连续换行折叠为两个，防止用大量空行占版面
func collapseBlankLines(text string) string {
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return text
}

// ValidateAppealMessage 校验并清洗申诉内容，返回可入库的纯文本
func ValidateAppealMessage(message string) (string, error) {
	cleaned := SanitizePlainText(message)

	if cleaned == "" {
		return "", errors.New("申诉内容不能为空")
	}
	if length := len([]rune(cleaned)); length > types.MaxAppealMessageLength {
		return "", fmt.Errorf("申诉内容不能超过 %d 个字符，当前 %d 个",
			types.MaxAppealMessageLength, length)
	}

	return cleaned, nil
}

// ValidateAppealReview 校验管理员的处理请求，返回清洗后的备注
func ValidateAppealReview(req *types.AppealReviewRequest) (string, error) {
	// 只能改为终态：受理或驳回。pending 是提交时的初始状态，不作为处理结果。
	if req.Status != types.AppealAccepted && req.Status != types.AppealRejected {
		return "", errors.New("处理结果只能为受理或驳回")
	}

	note := SanitizePlainText(req.AdminNote)
	if length := len([]rune(note)); length > types.MaxAppealMessageLength {
		return "", fmt.Errorf("处理备注不能超过 %d 个字符", types.MaxAppealMessageLength)
	}

	return note, nil
}
