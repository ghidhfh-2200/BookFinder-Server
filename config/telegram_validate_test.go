package config

import (
	"strings"
	"testing"
)

func TestValidateTelegram(t *testing.T) {
	cases := []struct {
		name  string
		token string
		chat  string
		ok    bool
	}{
		{"两项都空即不启用", "", "", true},
		{"只有令牌", "123456789:AAHfake_token_value_01234567890", "", false},
		{"只有会话", "", "12345", false},
		{"合法组合", "123456789:AAHfake_token_value_01234567890", "12345", true},
		{"合法负数群组", "123456789:AAHfake_token_value_01234567890", "-1001234567890", true},
		{"合法频道名", "123456789:AAHfake_token_value_01234567890", "@mychannel", true},
		{"令牌含斜杠可改变请求指向", "123456789:AAH/sendPhoto?x=aaaaaaaaaaaaaaaaaaaaaaa", "12345", false},
		{"令牌含问号", "123456789:AAH?x=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "12345", false},
		{"令牌缺冒号", "123456789AAHfake_token_value_012345678901", "12345", false},
		{"令牌过短", "123:AAH", "12345", false},
		{"会话含注入字符", "123456789:AAHfake_token_value_01234567890", "12345\nx", false},
		{"会话非数字", "123456789:AAHfake_token_value_01234567890", "abc", false},
	}

	for _, tc := range cases {
		err := validateTelegram(TelegramConfig{BotToken: tc.token, ChatID: tc.chat})
		if tc.ok && err != nil {
			t.Errorf("%s：应通过但被拒: %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s：应被拒但通过了", tc.name)
		}
		// 错误文本会进日志表，不该回显令牌
		if err != nil && tc.token != "" && strings.Contains(err.Error(), tc.token) {
			t.Errorf("%s：错误信息里回显了令牌: %v", tc.name, err)
		}
	}
}
