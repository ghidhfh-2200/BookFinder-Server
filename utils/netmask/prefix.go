// Package netmask 计算来源 IP 所属的封禁网段。
//
// 只封精确地址在现实中拦不住人：IPv6 下终端通常独占一个 /64，在段内换地址不受
// 任何限制；IPv4 家宽与移动网络的动态地址也常在同一 /24 内浮动。故自动封禁在
// 记录精确地址之外，一并记录其所属网段。
//
// 网段也是流量判据的汇总单位：IPv6 终端可在自己的 /64 内随意换址，
// 只看单个地址时，分散在一个 /64 内的刷量看不出异常。
//
// 但判据与处置是两件事：判定网段异常后会尽量只封那几个异常设备（按访问者令牌），
// 只有认不出时才退回网段级封禁，且 IPv4 不自动封 /24——那背后可能是整个校园网出口。
// 详见 utils/ratelimit 的 EvaluateNetworkBan。
package netmask

import (
	"fmt"
	"net"
)

// 封禁网段的前缀长度。
//
// IPv6 取 /64：这是 IPv6 地址分配的惯例边界，一个终端用户通常独占一个 /64，
// 段内地址可随意更换，故 /64 才是「一个用户」的合理粒度。
// IPv4 取 /24：再宽会把整个 ISP 的用户卷进来，再窄则挡不住动态地址浮动。
const (
	IPv6PrefixBits = 64
	IPv4PrefixBits = 24
)

// PrefixOf 返回该 IP 所属的封禁网段，形如 "2001:db8::/64" 或 "203.0.113.0/24"。
// 传入的地址不合法时返回 false。
func PrefixOf(ip string) (string, bool) {
	network, ok := NetworkOf(ip)
	if !ok {
		return "", false
	}
	return network.String(), true
}

// NetworkOf 返回该 IP 所属的封禁网段。
// 供匹配时直接使用，免去「转成字符串再解析回来」的往返。
func NetworkOf(ip string) (*net.IPNet, bool) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil, false
	}

	// IPv4 与 IPv4-mapped IPv6（::ffff:1.2.3.4）都按 IPv4 处理：
	// 二者指向同一台主机，若按 IPv6 算前缀会得到一个与实际无关的网段。
	if v4 := parsed.To4(); v4 != nil {
		mask := net.CIDRMask(IPv4PrefixBits, 32)
		return &net.IPNet{IP: v4.Mask(mask), Mask: mask}, true
	}

	v6 := parsed.To16()
	if v6 == nil {
		return nil, false
	}

	mask := net.CIDRMask(IPv6PrefixBits, 128)
	return &net.IPNet{IP: v6.Mask(mask), Mask: mask}, true
}

// ParseNetwork 解析封禁记录里存的网段字符串
func ParseNetwork(cidr string) (*net.IPNet, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ban network %q: %w", cidr, err)
	}
	return network, nil
}

// Canonical 归一化 IP 的字符串表示。
//
// 同一个地址可以有多种写法：::ffff:203.0.113.5 与 203.0.113.5、
// 2001:DB8::1 与 2001:db8::1。封禁按字符串精确匹配，若不归一化，
// 换一种写法就能绕过；两条本应相同的封禁记录也会各占一行。
func Canonical(ip string) (string, bool) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", false
	}

	if v4 := parsed.To4(); v4 != nil {
		return v4.String(), true
	}

	return parsed.String(), true
}

// IsIPv6 判断来源是否为 IPv6 地址（IPv4-mapped 按 IPv4 处理）。
//
// 封禁粒度按协议区分，故这个判断散落在多处：一个 /64 通常就是一个宽带用户，
// 封它约等于封一个人；而一个 /24 背后可能是整个校园网出口，自动流程绝不碰它。
//
// IPv4-mapped（::ffff:1.2.3.4）必须按 IPv4 处理：它与 1.2.3.4 指向同一台主机，
// 若按 IPv6 判定，就会走到「封 /64」的分支上去，绕过上面那条 IPv4 保护。
func IsIPv6(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.To4() == nil
}

// IsLoopback 判断来源是否为回环地址（127.0.0.0/8 或 ::1）。
//
// 自动封禁不处置回环地址，理由是它不指向任何外部访问者：
//
//   - 来自本机的请求就是本机自己发的——健康检查、监控探针、cron 里的脚本。
//     它们频率高又通常不保存令牌，恰好符合自动封禁要抓的特征，
//     于是服务会把自己的运维工具封掉。
//   - 反向代理配置正确时（TRUSTED_PROXIES 指向本机反代），若哪天配漏了
//     X-Forwarded-For，所有请求的来源都会被算作回环地址。此时一次自动封禁
//     就会连坐全部访问者，整站被自己封死。
//
// 判据照常累计、告警照常发出，只是不落封禁——与「IPv4 网段异常但无安全处置
// 粒度」是同一类取舍：判据成立不等于有一个安全的处置对象。
//
// 只排除回环，不排除私有地址（10/8、192.168/16 等）：那些是局域网内的真实
// 其他设备，封禁它们是有意义的处置。
func IsLoopback(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

// Contains 判断 IP 是否落在给定网段内
func Contains(network *net.IPNet, ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil || network == nil {
		return false
	}
	return network.Contains(parsed)
}
