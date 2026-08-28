package sysconfig

import (
	"strings"
	"testing"

	"bookfinder-backend/types"
)

// usableEmail 一份填全的邮件配置
func usableEmail() types.EmailConfig {
	return types.EmailConfig{
		Enabled:  true,
		Host:     "smtp.qq.com",
		Port:     465,
		Username: "me@qq.com",
		To:       "me@qq.com",
	}
}

// TestValidateNotifyRejectsIncomplete 启用邮件但填不全时必须报错，
// 且要指出漏的是哪一项——静默不发会让人以为告警在跑
func TestValidateNotifyRejectsIncomplete(t *testing.T) {
	cases := map[string]func(*types.EmailConfig){
		"缺主机":    func(c *types.EmailConfig) { c.Host = "" },
		"缺账号":    func(c *types.EmailConfig) { c.Username = "" },
		"缺收件人":   func(c *types.EmailConfig) { c.To = "" },
		"端口越界":   func(c *types.EmailConfig) { c.Port = 0 },
		"端口过大":   func(c *types.EmailConfig) { c.Port = 70000 },
		"收件人无 @": func(c *types.EmailConfig) { c.To = "notanemail" },
	}

	for name, mutate := range cases {
		email := usableEmail()
		mutate(&email)

		err := validateNotify(types.NotifyConfig{Email: email})
		if err == nil {
			t.Errorf("%s：应被拒绝", name)
		}
	}
}

// TestValidateNotifyRejectsHeaderInjection 地址含换行即可伪造邮件头，
// 保存时就该拦住，而不是等第一条告警发不出去才发现
func TestValidateNotifyRejectsHeaderInjection(t *testing.T) {
	for name, value := range map[string]string{
		"CRLF": "me@qq.com\r\nBcc: attacker@evil.com",
		"LF":   "me@qq.com\nBcc: attacker@evil.com",
	} {
		email := usableEmail()
		email.To = value

		if err := validateNotify(types.NotifyConfig{Email: email}); err == nil {
			t.Errorf("%s：含换行的收件地址应被拒绝", name)
		}
	}
}

// TestValidateNotifySkipsWhenDisabled 关掉邮件时不校验其余项。
// 否则一份填了一半的配置就再也关不掉了。
func TestValidateNotifySkipsWhenDisabled(t *testing.T) {
	if err := validateNotify(types.NotifyConfig{
		Email: types.EmailConfig{Enabled: false},
	}); err != nil {
		t.Errorf("关闭时不应校验其余项: %v", err)
	}
}

// TestValidateNotifyAcceptsComplete 填全时应通过
func TestValidateNotifyAcceptsComplete(t *testing.T) {
	if err := validateNotify(types.NotifyConfig{Email: usableEmail()}); err != nil {
		t.Errorf("填全的配置被拒: %v", err)
	}
}

// TestDefaultConfigPassesValidation 默认配置必须自洽：
// 文件缺失时用它启动并写出一份，若它自身不合法，服务永远起不来
func TestDefaultConfigPassesValidation(t *testing.T) {
	config := types.DefaultSystemConfig()
	if err := Validate(&config); err != nil {
		t.Fatalf("默认配置未通过校验: %v", err)
	}
	// 邮件默认关闭，故不该因缺主机等项而失败
	if config.Notify.Email.Enabled {
		t.Error("邮件通道默认应为关闭")
	}
	if !strings.Contains("465 587", "465") {
		t.Skip()
	}
}
