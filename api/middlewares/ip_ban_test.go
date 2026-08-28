package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"bookfinder-backend/types"

	"github.com/gin-gonic/gin"
)

// requestContext 构造一个带指定来源与标识的请求上下文，模拟中间件链执行完毕后的状态
func requestContext(ip, visitorKey, deviceKey string) *gin.Context {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/libraries", nil)

	c.Set(ClientIPKey, ip)
	if visitorKey != "" {
		c.Set(VisitorKeyContextKey, visitorKey)
	}
	if deviceKey != "" {
		c.Set(DeviceKeyContextKey, deviceKey)
	}

	return c
}

// identValues 取出给定种类的全部标识取值
func identValues(idents []types.BanIdent, kind types.BanIdentKind) []string {
	values := make([]string, 0, len(idents))
	for _, ident := range idents {
		if ident.Kind == kind {
			values = append(values, ident.Value)
		}
	}
	return values
}

// TestBanIdentsSkipsIPv4Network IPv4 来源绝不写入网段标识。
//
// 这是本次修复的要点：按访问者的各条规则任一命中都会走到这里，若写入 /24，
// 一次自动封禁就连坐整个校园网出口——而校园网与图书馆 Wi-Fi 恰好是本应用的
// 典型场景。EvaluateNetworkBan 里那套 IPv4 保护，必须在这条路径上同样成立。
func TestBanIdentsSkipsIPv4Network(t *testing.T) {
	c := requestContext("203.0.113.5", "visitor-hash", "")

	idents := BanIdentsForRequest(c)

	if nets := identValues(idents, types.IdentIPNet); len(nets) > 0 {
		t.Errorf("IPv4 来源不应写入网段标识，实际写了 %v", nets)
	}

	// 精确 IP 仍要写入，否则当前来源拦不住
	if ips := identValues(idents, types.IdentIP); len(ips) != 1 || ips[0] != "203.0.113.5" {
		t.Errorf("应写入精确 IP，实际为 %v", ips)
	}
}

// TestBanIdentsIncludesIPv6Network IPv6 来源要写入其 /64。
//
// 与 IPv4 的区别是有意的：IPv6 终端可在自己的 /64 内随意换址、不需要任何权限，
// 只封精确地址等于没封；而一个 /64 通常就是一个宽带用户，封它约等于封一个人。
func TestBanIdentsIncludesIPv6Network(t *testing.T) {
	c := requestContext("2001:db8::1", "visitor-hash", "")

	idents := BanIdentsForRequest(c)

	nets := identValues(idents, types.IdentIPNet)
	if len(nets) != 1 || nets[0] != "2001:db8::/64" {
		t.Errorf("IPv6 来源应写入其 /64，实际为 %v", nets)
	}
}

// TestBanIdentsTreatsMappedIPv4AsIPv4 IPv4-mapped 地址按 IPv4 处理。
//
// ::ffff:203.0.113.5 与 203.0.113.5 指向同一台主机，若按 IPv6 算前缀，
// 会得到一个与实际无关的网段，并绕过上面那条 IPv4 保护。
func TestBanIdentsTreatsMappedIPv4AsIPv4(t *testing.T) {
	c := requestContext("::ffff:203.0.113.5", "visitor-hash", "")

	idents := BanIdentsForRequest(c)

	if nets := identValues(idents, types.IdentIPNet); len(nets) > 0 {
		t.Errorf("IPv4-mapped 来源不应写入网段标识，实际写了 %v", nets)
	}
	if ips := identValues(idents, types.IdentIP); len(ips) != 1 || ips[0] != "203.0.113.5" {
		t.Errorf("IPv4-mapped 来源应归一化为 IPv4 写入，实际为 %v", ips)
	}
}

// TestBanIdentsCarriesVisitorAndDevice 令牌与设备标识都要写入：
// 前者跨 IP 跟着这个人，后者跨端（浏览器 ↔ 安卓）跟着这台设备。
func TestBanIdentsCarriesVisitorAndDevice(t *testing.T) {
	c := requestContext("203.0.113.5", "visitor-hash", "device-hash")

	idents := BanIdentsForRequest(c)

	if got := identValues(idents, types.IdentVisitor); len(got) != 1 || got[0] != "visitor-hash" {
		t.Errorf("应写入访问者令牌标识，实际为 %v", got)
	}
	if got := identValues(idents, types.IdentDevice); len(got) != 1 || got[0] != "device-hash" {
		t.Errorf("应写入设备标识，实际为 %v", got)
	}
}

// TestBanIdentsWithoutOptionalKeys 取不到令牌与设备标识时只写 IP，不产生空标识。
// 空取值的标识永远不会被命中，只是一条垃圾记录。
func TestBanIdentsWithoutOptionalKeys(t *testing.T) {
	c := requestContext("203.0.113.5", "", "")

	idents := BanIdentsForRequest(c)

	if len(idents) != 1 {
		t.Fatalf("应只写入精确 IP 一个标识，实际为 %d 个: %v", len(idents), idents)
	}
	for _, ident := range idents {
		if ident.Value == "" {
			t.Error("不应写入空取值的标识")
		}
	}
}

// TestBanIdentsWithInvalidIP 来源 IP 不合法时不写 IP 类标识，但令牌仍要保留。
func TestBanIdentsWithInvalidIP(t *testing.T) {
	c := requestContext("这不是地址", "visitor-hash", "")

	idents := BanIdentsForRequest(c)

	if got := identValues(idents, types.IdentIP); len(got) > 0 {
		t.Errorf("非法 IP 不应写入标识，实际写了 %v", got)
	}
	if got := identValues(idents, types.IdentIPNet); len(got) > 0 {
		t.Errorf("非法 IP 不应写入网段标识，实际写了 %v", got)
	}
	if got := identValues(idents, types.IdentVisitor); len(got) != 1 {
		t.Errorf("令牌标识不应受 IP 合法性影响，实际为 %v", got)
	}
}

// TestIdentsExcludingAddressKeepsDeviceIdents 回环来源应丢掉地址类标识、
// 保留令牌与设备标识。
//
// 这是回环处置的核心：地址不指向特定访问者（反代未透传时全部访问者共用它），
// 而令牌与设备标识跨 IP 跟人，依然准确。若把它们一并丢掉，
// 反代配错时刷接口的客户端就完全无法处置了。
func TestIdentsExcludingAddressKeepsDeviceIdents(t *testing.T) {
	kept := identsExcludingAddress([]types.BanIdent{
		{Kind: types.IdentIP, Value: "127.0.0.1"},
		{Kind: types.IdentIPNet, Value: "127.0.0.0/24"},
		{Kind: types.IdentVisitor, Value: "visitor-hash"},
		{Kind: types.IdentDevice, Value: "device-hash"},
	})

	if len(kept) != 2 {
		t.Fatalf("保留了 %d 个标识，期望 2 个（令牌与设备）: %+v", len(kept), kept)
	}
	for _, ident := range kept {
		if ident.Kind == types.IdentIP || ident.Kind == types.IdentIPNet {
			t.Errorf("地址类标识 %s 未被滤除", ident.Kind)
		}
	}
}

// TestIdentsExcludingAddressEmptyWhenOnlyAddresses 只有地址标识时应返回空。
// 调用方据此判断「无从处置」，转为只告警——那正是本机脚本不带 cookie 猛刷的形态。
func TestIdentsExcludingAddressEmptyWhenOnlyAddresses(t *testing.T) {
	kept := identsExcludingAddress([]types.BanIdent{
		{Kind: types.IdentIP, Value: "127.0.0.1"},
		{Kind: types.IdentIPNet, Value: "127.0.0.0/24"},
	})

	if len(kept) != 0 {
		t.Errorf("期望空，实际保留了 %+v", kept)
	}
}
