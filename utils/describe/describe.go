// Package describe 把结构化数据概述为供人阅读的文本。
//
// 这些文本进操作日志与告警通知，读者是管理员而非程序，故一律只讲客观事实：
// 改了什么、处置了哪些标识、启用了哪几项。不解释判断依据，那属于代码注释。
//
// 集中在一处而非散落在各 handler 里：同一个概念在不同地方各说一套，
// 日志读起来就要在几种叫法之间换算。
package describe

import (
	"fmt"
	"strings"

	"bookfinder-backend/types"
)

// identShortLen 哈希类标识在文本中保留的长度。
// 令牌与设备标识是 64 位十六进制串，写全对读者没有任何帮助。
const identShortLen = 12

// SystemConfig 概述系统配置变更
func SystemConfig(config *types.SystemConfig) string {
	return fmt.Sprintf("系统配置已更新：定期清理%s，操作日志保留 %d 天、"+
		"运行日志保留 %d 天，分页上限 %d，请求体上限 %d 字节，告警外发 %s",
		enabledText(config.Maintenance.Enabled),
		config.Maintenance.OperationLogRetentionDays,
		config.Maintenance.AppLogRetentionDays,
		config.Pagination.MaxSize,
		config.Server.MaxRequestBodyBytes,
		Notify(config.Notify))
}

// NotifyChannels 概述哪几条外发通道可用，供启动日志说明。
//
// 两条通道并行，故要分别列出：只说「已启用」看不出实际走的是哪条，
// 而这恰是排查「为什么没收到通知」时第一个要确认的。
func NotifyChannels(telegram, email bool) string {
	channels := make([]string, 0, 2)
	if telegram {
		channels = append(channels, "Telegram")
	}
	if email {
		channels = append(channels, "邮件")
	}

	if len(channels) == 0 {
		return "无可用通道"
	}

	return strings.Join(channels, " + ")
}

// Notify 概述启用了哪几类告警、经哪条通道送出
func Notify(cfg types.NotifyConfig) string {
	enabled := make([]string, 0, 3)
	if cfg.AutoBan {
		enabled = append(enabled, "自动封禁")
	}
	if cfg.NetworkAnomaly {
		enabled = append(enabled, "流量异常")
	}
	if cfg.Appeal {
		enabled = append(enabled, "申诉")
	}

	if len(enabled) == 0 {
		return "全部关闭"
	}

	summary := strings.Join(enabled, "、")

	// 邮件通道的收件地址值得写进审计日志：改了往哪发是一次实质变更
	if cfg.Email.Enabled {
		summary += fmt.Sprintf("（邮件发往 %s）", cfg.Email.To)
	}

	return summary
}

// RateRules 概述限流规则变更
func RateRules(rules *types.RateRules) string {
	return fmt.Sprintf("限流规则已更新：限流%s，自动封禁%s",
		enabledText(rules.Enabled), enabledText(rules.AutoBan.Enabled))
}

// Idents 把标识列表逐条列出，供审计日志追溯。
//
// 与 BanScope 的分工：本函数面向「事后要查是哪几个标识」，故每条都列、
// 哈希截短；BanScope 面向「这次封了谁」，只讲范围。
func Idents(idents []types.BanIdent) string {
	if len(idents) == 0 {
		return "无标识"
	}

	parts := make([]string, 0, len(idents))
	for _, ident := range idents {
		parts = append(parts, string(ident.Kind)+":"+Shorten(ident.Value))
	}

	return strings.Join(parts, ", ")
}

// BanScope 概述一次封禁处置了什么范围。
//
// 不逐条列出标识：令牌与设备标识是哈希，列出来对人没有意义，
// 而 IP 与网段才是管理员据以判断「这封得对不对」的东西。
func BanScope(idents []types.BanIdent) string {
	var (
		addresses []string
		devices   int
	)

	for _, ident := range idents {
		switch ident.Kind {
		case types.IdentIP, types.IdentIPNet:
			addresses = append(addresses, ident.Value)
		default:
			devices++
		}
	}

	parts := make([]string, 0, 2)
	if len(addresses) > 0 {
		parts = append(parts, strings.Join(addresses, "、"))
	}
	if devices > 0 {
		parts = append(parts, fmt.Sprintf("%d 个设备标识", devices))
	}

	if len(parts) == 0 {
		return "无标识"
	}

	return strings.Join(parts, " 及 ")
}

// Shorten 截短哈希类标识，日志里没必要写全 64 个字符
func Shorten(value string) string {
	if len(value) <= identShortLen {
		return value
	}
	return value[:identShortLen] + "…"
}

// Fallback 空串时返回占位文本，避免日志里出现「原因: 」这样的空栏
func Fallback(value, placeholder string) string {
	if value == "" {
		return placeholder
	}
	return value
}

// enabledText 开关的中文表述
func enabledText(enabled bool) string {
	if enabled {
		return "已启用"
	}
	return "已关闭"
}
