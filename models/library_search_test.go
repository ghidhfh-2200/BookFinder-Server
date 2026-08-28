package models

import "testing"

// TestQuoteFullTextPhrase 关键词须包成引号短语，按字面匹配。
//
// 不包的话，BOOLEAN MODE 会把 + - > < ( ) ~ * @ 当操作符：实测搜「测试-中心」
// 返回空（`-` 意为「排除」），搜「A+B」也是空，尽管库里确有这些记录。
func TestQuoteFullTextPhrase(t *testing.T) {
	cases := map[string]string{
		"大学":    `"大学"`,
		"A+B":   `"A+B"`,
		"测试-中心": `"测试-中心"`,
		"a b":   `"a b"`,
		"(x)":   `"(x)"`,
		"~*@":   `"~*@"`,
	}

	for input, want := range cases {
		if got := quoteFullTextPhrase(input); got != want {
			t.Errorf("quoteFullTextPhrase(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// TestQuoteFullTextPhraseEscapesQuotes 双引号与反斜杠都要转义。
//
// 双引号是必须的：不转义会提前结束短语。反斜杠则是实测确认过安全的选择——
// 用 ngram 解析器时，反斜杠是分词分隔符，转义与不转义都能正确命中
// （中间反斜杠、结尾反斜杠两种位置都验过）。既然两者等效，就一并转义，
// 因为反斜杠是转义符本身，放着不管要依赖「它恰好不参与分词」这个前提。
//
// 注意期望值写在反引号里，其中没有转义：输入一个反斜杠，输出应是
// 「转义符 + 反斜杠」两个字符。
func TestQuoteFullTextPhraseEscapesQuotes(t *testing.T) {
	cases := map[string]string{
		`说"话"`: `"说\"话\""`,
		`a\b`:  `"a\\b"`,
		`"`:    `"\""`,
		`\`:    `"\\"`,
		`x"\y`: `"x\"\\y"`,
	}

	for input, want := range cases {
		if got := quoteFullTextPhrase(input); got != want {
			t.Errorf("quoteFullTextPhrase(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// TestNgramTokenSizeIsRuneBased token 尺寸的判断按字符数而非字节数。
//
// 一个汉字占三字节，按字节算会把「大学」（2 字符 / 6 字节）判成长词——
// 那本身没错，但「大」（1 字符 / 3 字节）会被误判为长词而走 MATCH，
// 结果是空（短于 token 尺寸的词在 ngram 索引里没有条目）。
func TestNgramTokenSizeIsRuneBased(t *testing.T) {
	cases := map[string]bool{
		"大":   true,  // 1 字符，须退回 LIKE
		"a":   true,  // 1 字符
		"大学":  false, // 2 字符，可走 MATCH
		"ab":  false,
		"图书馆": false,
	}

	for keyword, wantFallback := range cases {
		gotFallback := len([]rune(keyword)) < ngramTokenSize
		if gotFallback != wantFallback {
			t.Errorf("关键词 %q：应回退=%v，实际=%v（字符数 %d，字节数 %d）",
				keyword, wantFallback, gotFallback, len([]rune(keyword)), len(keyword))
		}
	}
}
