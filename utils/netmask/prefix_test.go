package netmask

import "testing"

// TestPrefixOf 各类地址的网段计算
func TestPrefixOf(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want string
	}{
		{"IPv4 取 /24", "203.0.113.5", "203.0.113.0/24"},
		{"IPv4 网段边界", "203.0.113.255", "203.0.113.0/24"},
		{"IPv4 私网", "192.168.1.100", "192.168.1.0/24"},
		{"IPv6 取 /64", "2001:db8::1", "2001:db8::/64"},
		{"IPv6 同段不同地址", "2001:db8::ffff:ffff", "2001:db8::/64"},
		{"IPv6 大写归一化", "2001:DB8::1", "2001:db8::/64"},
		// IPv4-mapped 指向的是同一台主机，按 IPv6 算前缀会得到无关的网段
		{"IPv4-mapped 按 IPv4 处理", "::ffff:203.0.113.5", "203.0.113.0/24"},
		{"回环", "127.0.0.1", "127.0.0.0/24"},
		{"IPv6 回环", "::1", "::/64"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := PrefixOf(tc.ip)
			if !ok {
				t.Fatalf("PrefixOf(%q) 应当成功", tc.ip)
			}
			if got != tc.want {
				t.Errorf("PrefixOf(%q) = %q，期望 %q", tc.ip, got, tc.want)
			}
		})
	}
}

// TestPrefixOfInvalid 非法输入应返回 false 而非 panic
func TestPrefixOfInvalid(t *testing.T) {
	for _, ip := range []string{"", "not-an-ip", "1.2.3", "1.2.3.4.5", "999.1.1.1", "gggg::1"} {
		if _, ok := PrefixOf(ip); ok {
			t.Errorf("PrefixOf(%q) 应当失败", ip)
		}
	}
}

// TestSameNetworkDifferentAddress 同一 /64 内换地址应算作同一网段。
// 这正是只封精确 IPv6 地址拦不住人的原因。
func TestSameNetworkDifferentAddress(t *testing.T) {
	first, ok := PrefixOf("2001:db8:0:1::1")
	if !ok {
		t.Fatal("首个地址解析失败")
	}
	second, ok := PrefixOf("2001:db8:0:1::dead:beef")
	if !ok {
		t.Fatal("次个地址解析失败")
	}

	if first != second {
		t.Errorf("同一 /64 内的地址应得到相同网段：%q 与 %q", first, second)
	}
}

// TestDifferentNetwork 不同 /64 不应混为一谈，否则连坐范围会失控
func TestDifferentNetwork(t *testing.T) {
	first, _ := PrefixOf("2001:db8:0:1::1")
	second, _ := PrefixOf("2001:db8:0:2::1")

	if first == second {
		t.Errorf("不同 /64 应得到不同网段，都是 %q", first)
	}
}

// TestCanonical 同一地址的不同写法应归一化到同一串，否则换个写法即可绕过封禁
func TestCanonical(t *testing.T) {
	cases := []struct {
		ip   string
		want string
	}{
		{"203.0.113.5", "203.0.113.5"},
		{"::ffff:203.0.113.5", "203.0.113.5"},
		{"2001:DB8::1", "2001:db8::1"},
		{"2001:db8:0:0:0:0:0:1", "2001:db8::1"},
	}

	for _, tc := range cases {
		got, ok := Canonical(tc.ip)
		if !ok {
			t.Fatalf("Canonical(%q) 应当成功", tc.ip)
		}
		if got != tc.want {
			t.Errorf("Canonical(%q) = %q，期望 %q", tc.ip, got, tc.want)
		}
	}
}

// TestCanonicalInvalid 非法地址不应通过归一化
func TestCanonicalInvalid(t *testing.T) {
	for _, ip := range []string{"", "nope", "1.2.3"} {
		if _, ok := Canonical(ip); ok {
			t.Errorf("Canonical(%q) 应当失败", ip)
		}
	}
}

// TestContains 网段匹配
func TestContains(t *testing.T) {
	network, err := ParseNetwork("2001:db8::/64")
	if err != nil {
		t.Fatalf("解析网段失败: %v", err)
	}

	if !Contains(network, "2001:db8::5") {
		t.Error("同段地址应命中")
	}
	if Contains(network, "2001:db8:0:1::5") {
		t.Error("邻段地址不应命中")
	}
	if Contains(network, "bad-ip") {
		t.Error("非法地址不应命中")
	}
	if Contains(nil, "2001:db8::5") {
		t.Error("空网段不应命中任何地址")
	}
}

// TestParseNetworkInvalid 非法 CIDR 应报错
func TestParseNetworkInvalid(t *testing.T) {
	for _, cidr := range []string{"", "2001:db8::", "203.0.113.0/99", "junk/24"} {
		if _, err := ParseNetwork(cidr); err == nil {
			t.Errorf("ParseNetwork(%q) 应当报错", cidr)
		}
	}
}

// TestIsIPv6 协议判定。IPv4-mapped 必须按 IPv4 处理，否则它会走到「封 /64」
// 的分支上去，绕过「IPv4 绝不自动封网段」这条保护。
func TestIsIPv6(t *testing.T) {
	cases := map[string]bool{
		"2001:db8::1":        true,
		"::1":                true,
		"fe80::1":            true,
		"203.0.113.5":        false,
		"0.0.0.0":            false,
		"::ffff:203.0.113.5": false,
		"不是地址":               false,
		"":                   false,
	}

	for ip, want := range cases {
		if got := IsIPv6(ip); got != want {
			t.Errorf("IsIPv6(%q) = %v，期望 %v", ip, got, want)
		}
	}
}

// TestIsLoopback 回环地址的识别。
//
// 这个判断决定自动封禁是否处置该地址，故各种写法都要认出来——
// 漏认一种就意味着那种写法下会封掉一个不指向任何访问者的地址。
func TestIsLoopback(t *testing.T) {
	loopback := []string{
		"127.0.0.1",
		"127.0.0.53",       // systemd-resolved 常用
		"127.1.2.3",        // 整个 127/8 都是回环
		"::1",              // IPv6 回环
		"0:0:0:0:0:0:0:1",  // ::1 的完整写法
		"::ffff:127.0.0.1", // IPv4-mapped 的回环
	}
	for _, ip := range loopback {
		if !IsLoopback(ip) {
			t.Errorf("IsLoopback(%q) = false，应识别为回环", ip)
		}
	}

	// 私有地址不算回环：它们是局域网内真实的其他设备，封禁它们是有意义的处置
	notLoopback := []string{
		"10.0.0.1",
		"192.168.1.28",
		"172.16.0.1",
		"203.0.113.9",
		"2001:db8::1",
		"fe80::1", // 链路本地，也不是回环
		"",
		"not-an-ip",
	}
	for _, ip := range notLoopback {
		if IsLoopback(ip) {
			t.Errorf("IsLoopback(%q) = true，不应识别为回环", ip)
		}
	}
}
