package notify

import (
	"strings"

	"bookfinder-backend/utils/checker"
)

// field 消息里的一行「标签：取值」
type field struct {
	label string
	value string
}

// buildMessage 拼出一条告警消息。
//
// 结构固定为标题加若干「标签：取值」行。取值一律经 sanitizeValue 压成单行，
// 这是防伪造的关键：告警里含用户提供的内容（申诉正文），而这套逐行结构
// 恰好可以被换行伪造——一段形如「...\n来源：127.0.0.1」的申诉，
// 会在管理员手机上多出一行看起来出自服务端的事实。
func buildMessage(title string, fields []field) string {
	var builder strings.Builder

	builder.WriteString(sanitizeValue(title))

	for _, f := range fields {
		value := sanitizeValue(f.value)
		if value == "" {
			continue
		}
		builder.WriteString("\n")
		builder.WriteString(sanitizeValue(f.label))
		builder.WriteString("：")
		builder.WriteString(value)
	}

	return builder.String()
}

// sanitizeValue 把一个取值压成安全的单行文本。
//
// 先交给 checker.SanitizePlainText 剥掉控制字符、零宽字符与双向排版控制符
// （那里已经处理了这些，不必再写一遍），再把剩余的换行与制表符压成空格。
//
// 压成单行是防伪造的关键，见 buildMessage 的说明：换行是这套逐行结构里
// 唯一能被用户内容利用的东西，而 SanitizePlainText 有意保留了它——
// 申诉正文入库时需要分段，只有外发到告警里才必须压平。
func sanitizeValue(value string) string {
	// strings.Fields 按任意空白切分，故换行与制表符在此一并压掉
	return strings.Join(strings.Fields(checker.SanitizePlainText(value)), " ")
}

// truncate 按字符数截断，超长时以省略号收尾。
//
// 按 rune 计而非字节：Telegram 的长度上限是按字符算的，
// 而告警里大量是中文，按字节截断会既提前截断、又可能切坏一个字符。
func truncate(text string, limit int) string {
	if limit < 1 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}

	const ellipsis = "…（已截断）"
	keep := limit - len([]rune(ellipsis))
	if keep < 1 {
		return string(runes[:limit])
	}

	return string(runes[:keep]) + ellipsis
}
