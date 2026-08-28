package banlist

import (
	"sync"
	"testing"

	"bookfinder-backend/types"
)

// subject 构造一个带标识的封禁主体
func subject(id int, source types.BanSource, idents ...types.BanIdent) types.BanSubject {
	return types.BanSubject{
		ID:     id,
		Reason: "测试",
		Source: source,
		Idents: idents,
	}
}

// ident 构造一个标识
func ident(kind types.BanIdentKind, value string) types.BanIdent {
	return types.BanIdent{Kind: kind, Value: value}
}

// reset 清空名单，避免用例间互相影响
func reset() {
	Replace(nil)
}

// TestEmptyListMatchesNothing 空名单不应命中任何请求
func TestEmptyListMatchesNothing(t *testing.T) {
	reset()

	if _, hit := Check(Request{IP: "203.0.113.5", VisitorKey: "abc", DeviceKey: "def"}); hit {
		t.Error("空名单不应命中")
	}
}

// TestExactIPMatch 精确 IP 命中
func TestExactIPMatch(t *testing.T) {
	reset()
	Replace([]types.BanSubject{
		subject(1, types.BanSourceManual, ident(types.IdentIP, "203.0.113.5")),
	})

	match, hit := Check(Request{IP: "203.0.113.5"})
	if !hit {
		t.Fatal("应当命中精确 IP")
	}
	if match.Kind != types.IdentIP {
		t.Errorf("命中种类应为 ip，实际为 %s", match.Kind)
	}
	if match.Subject.ID != 1 {
		t.Errorf("应归属主体 1，实际为 %d", match.Subject.ID)
	}

	if _, hit := Check(Request{IP: "203.0.113.6"}); hit {
		t.Error("其他地址不应命中")
	}
}

// TestIPFormMatch 同一地址的不同写法都应命中，否则换个写法即可绕过
func TestIPFormMatch(t *testing.T) {
	reset()
	Replace([]types.BanSubject{
		subject(1, types.BanSourceManual, ident(types.IdentIP, "203.0.113.5")),
	})

	if _, hit := Check(Request{IP: "::ffff:203.0.113.5"}); !hit {
		t.Error("IPv4-mapped 写法应命中同一地址的封禁")
	}
}

// TestVisitorAndDeviceMatch 令牌与设备标识各自都能命中。
// 这是跨端封禁的基础：同一主体挂着浏览器令牌与安卓设备标识，封一次两端都掉。
func TestVisitorAndDeviceMatch(t *testing.T) {
	reset()
	Replace([]types.BanSubject{
		subject(7, types.BanSourceAuto,
			ident(types.IdentVisitor, "visitor-hash"),
			ident(types.IdentDevice, "device-hash"),
		),
	})

	// 换了 IP，但令牌还在
	match, hit := Check(Request{IP: "198.51.100.1", VisitorKey: "visitor-hash"})
	if !hit {
		t.Fatal("令牌应命中")
	}
	if match.Kind != types.IdentVisitor {
		t.Errorf("命中种类应为 visitor，实际为 %s", match.Kind)
	}

	// 安卓端换了网络，但设备标识还在
	match, hit = Check(Request{IP: "198.51.100.2", DeviceKey: "device-hash"})
	if !hit {
		t.Fatal("设备标识应命中")
	}
	if match.Kind != types.IdentDevice {
		t.Errorf("命中种类应为 device，实际为 %s", match.Kind)
	}
	if match.Subject.ID != 7 {
		t.Errorf("两端应归属同一主体 7，实际为 %d", match.Subject.ID)
	}
}

// TestEmptyKeysDoNotMatch 空令牌与空设备标识不应命中，
// 否则未携带标识的请求会误撞到库里的空值记录
func TestEmptyKeysDoNotMatch(t *testing.T) {
	reset()
	Replace([]types.BanSubject{
		subject(1, types.BanSourceAuto,
			ident(types.IdentVisitor, ""),
			ident(types.IdentDevice, ""),
		),
	})

	if _, hit := Check(Request{IP: "203.0.113.9"}); hit {
		t.Error("未携带令牌与设备标识的请求不应命中空值记录")
	}
}

// TestNetworkMatch 网段命中：同段内换地址仍被封
func TestNetworkMatch(t *testing.T) {
	reset()
	Replace([]types.BanSubject{
		subject(2, types.BanSourceAuto, ident(types.IdentIPNet, "2001:db8::/64")),
	})

	match, hit := Check(Request{IP: "2001:db8::dead:beef"})
	if !hit {
		t.Fatal("同段地址应命中网段封禁")
	}
	if match.Kind != types.IdentIPNet {
		t.Errorf("命中种类应为 ip_net，实际为 %s", match.Kind)
	}

	if _, hit := Check(Request{IP: "2001:db8:0:1::1"}); hit {
		t.Error("邻段地址不应命中")
	}
}

// TestReplaceClearsPrevious 重建名单应彻底替换旧数据，
// 否则解封后旧记录仍留在内存里，人还是进不来
func TestReplaceClearsPrevious(t *testing.T) {
	reset()
	Replace([]types.BanSubject{
		subject(1, types.BanSourceManual, ident(types.IdentIP, "203.0.113.5")),
	})

	if _, hit := Check(Request{IP: "203.0.113.5"}); !hit {
		t.Fatal("封禁应生效")
	}

	// 模拟解封：重建为空名单
	Replace(nil)

	if _, hit := Check(Request{IP: "203.0.113.5"}); hit {
		t.Error("解封后不应再命中")
	}
	if stats := Count(); stats.Subjects != 0 || stats.Idents != 0 {
		t.Errorf("重建后规模应归零，实际为 %+v", stats)
	}
}

// TestInvalidNetworkIsSkipped 库里的非法网段应被跳过而非导致崩溃
func TestInvalidNetworkIsSkipped(t *testing.T) {
	reset()
	Replace([]types.BanSubject{
		subject(1, types.BanSourceAuto,
			ident(types.IdentIPNet, "这不是网段"),
			ident(types.IdentIP, "203.0.113.5"),
		),
	})

	// 坏数据不应影响同一主体下正常标识的判定
	if _, hit := Check(Request{IP: "203.0.113.5"}); !hit {
		t.Error("非法网段不应妨碍其他标识生效")
	}
}

// TestCount 名单规模统计
func TestCount(t *testing.T) {
	reset()
	Replace([]types.BanSubject{
		subject(1, types.BanSourceAuto,
			ident(types.IdentIP, "203.0.113.5"),
			ident(types.IdentIPNet, "203.0.113.0/24"),
			ident(types.IdentVisitor, "v1"),
		),
		subject(2, types.BanSourceManual, ident(types.IdentIP, "198.51.100.1")),
	})

	stats := Count()
	if stats.Subjects != 2 {
		t.Errorf("主体数应为 2，实际为 %d", stats.Subjects)
	}
	if stats.Idents != 4 {
		t.Errorf("标识数应为 4，实际为 %d", stats.Idents)
	}
	if stats.Networks != 1 {
		t.Errorf("网段数应为 1，实际为 %d", stats.Networks)
	}
}

// TestConcurrentReadWrite 读写并发不应竞争。
// 封禁检查在每个请求上执行，而管理员可能同时在改名单，故必须并发安全。
// 该用例配合 -race 使用。
func TestConcurrentReadWrite(t *testing.T) {
	reset()

	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			for range 200 {
				Check(Request{IP: "203.0.113.5", VisitorKey: "v1"})
				Count()
			}
		})
	}

	for i := range 4 {
		wg.Go(func() {
			for range 50 {
				Replace([]types.BanSubject{
					subject(i+1, types.BanSourceAuto,
						ident(types.IdentIP, "203.0.113.5"),
						ident(types.IdentIPNet, "2001:db8::/64"),
					),
				})
			}
		})
	}

	wg.Wait()
}

// TestHasFindsExactIdents Has 按种类与取值精确判断标识是否已在名单内。
// 自动封禁靠它滤掉已封标识，否则同一判定会在每个后续请求上反复落库。
func TestHasFindsExactIdents(t *testing.T) {
	reset()

	Replace([]types.BanSubject{
		subject(1, types.BanSourceAuto,
			ident(types.IdentIP, "203.0.113.5"),
			ident(types.IdentVisitor, "visitor-hash"),
			ident(types.IdentDevice, "device-hash"),
		),
	})

	for _, tc := range []struct {
		kind  types.BanIdentKind
		value string
	}{
		{types.IdentIP, "203.0.113.5"},
		{types.IdentVisitor, "visitor-hash"},
		{types.IdentDevice, "device-hash"},
	} {
		if !Has(tc.kind, tc.value) {
			t.Errorf("Has(%s, %q) 应为 true", tc.kind, tc.value)
		}
	}

	// 未封的取值、以及种类对不上的取值都不应命中
	if Has(types.IdentIP, "203.0.113.6") {
		t.Error("未封禁的 IP 不应命中")
	}
	if Has(types.IdentVisitor, "203.0.113.5") {
		t.Error("取值虽已封禁但种类不符，不应命中")
	}
}

// TestHasMatchesNetworkByValue 网段按取值比对，而非判断某地址是否落在段内。
//
// 这个区别是有意的：Has 回答「这个网段是否已被封」，供封禁写入前去重；
// 「某个 IP 是否落在已封网段内」是 Check 的职责。
func TestHasMatchesNetworkByValue(t *testing.T) {
	reset()

	Replace([]types.BanSubject{
		subject(1, types.BanSourceAuto, ident(types.IdentIPNet, "2001:db8::/64")),
	})

	if !Has(types.IdentIPNet, "2001:db8::/64") {
		t.Error("已封禁的网段应命中")
	}
	// 段内的某个地址不是「这个网段」，不应由 Has 命中
	if Has(types.IdentIPNet, "2001:db8::1") {
		t.Error("段内地址不应被当作网段取值命中")
	}
	if Has(types.IdentIPNet, "2001:db9::/64") {
		t.Error("未封禁的网段不应命中")
	}
}

// TestHasEmptyValue 空取值一律不命中，避免把「取不到标识」误判为「已被封」
func TestHasEmptyValue(t *testing.T) {
	reset()

	Replace([]types.BanSubject{
		subject(1, types.BanSourceAuto, ident(types.IdentIP, "203.0.113.5")),
	})

	for _, kind := range types.AllBanIdentKinds {
		if Has(kind, "") {
			t.Errorf("Has(%s, \"\") 应为 false", kind)
		}
	}
}

// TestHasAfterUnban 解封后 Has 应随之为假，否则该来源再次异常时无法重新封禁
func TestHasAfterUnban(t *testing.T) {
	reset()

	Replace([]types.BanSubject{
		subject(1, types.BanSourceAuto,
			ident(types.IdentIP, "203.0.113.5"),
			ident(types.IdentIPNet, "203.0.113.0/24"),
		),
	})
	if !Has(types.IdentIP, "203.0.113.5") || !Has(types.IdentIPNet, "203.0.113.0/24") {
		t.Fatal("封禁后应命中")
	}

	// 解封即整体重建名单（见 models.ReloadBanList）
	Replace(nil)

	if Has(types.IdentIP, "203.0.113.5") {
		t.Error("解封后 IP 不应再命中")
	}
	if Has(types.IdentIPNet, "203.0.113.0/24") {
		t.Error("解封后网段不应再命中")
	}
}
