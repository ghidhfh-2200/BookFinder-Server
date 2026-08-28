package ratelimit

import (
	"testing"

	"bookfinder-backend/types"
)

// setNetworkRules 直接设置生效中的规则，省去写文件
func setNetworkRules(t *testing.T, autoBan types.AutoBanRules) {
	t.Helper()

	mu.Lock()
	rules = types.RateRules{
		Enabled: true,
		Limits: map[types.LimitCategory]types.CategoryLimit{
			// 预算合计 100，便于心算阈值
			types.CategoryRead:   {Daily: 70, Burst: 20, BurstWindowSeconds: 60},
			types.CategoryReport: {Daily: 30, Burst: 10, BurstWindowSeconds: 60},
		},
		Probation: types.ProbationRules{Daily: 20, Burst: 5, BurstWindowSeconds: 60},
		AutoBan:   autoBan,
	}
	mu.Unlock()
}

// defaultNetworkRules 与默认配置一致的网段规则：
// 预算 100 × 5 = 500 次触发排查，考察 Top 5，占比 80% 视为集中
func defaultNetworkRules() types.AutoBanRules {
	return types.AutoBanRules{
		Enabled:                     true,
		NetworkOverflowMultiplier:   5,
		NetworkTopVisitors:          5,
		NetworkConcentrationPercent: 80,
	}
}

// TestNetworkBudget 网段预算取各类每日配额之和，故阈值随限流规则自动调整
func TestNetworkBudget(t *testing.T) {
	setNetworkRules(t, defaultNetworkRules())

	if budget := NetworkBudget(); budget != 100 {
		t.Errorf("网段预算应为 100，实际为 %d", budget)
	}
}

// TestNetworkUnderThresholdDoesNotBan 未达阈值不应封禁。
// 同一网段有多人是常态，总量略高于单人预算完全正常。
func TestNetworkUnderThresholdDoesNotBan(t *testing.T) {
	setNetworkRules(t, defaultNetworkRules())

	for _, total := range []int{1, 100, 300, 499} {
		verdict := EvaluateNetworkBan(NetworkSignals{
			Profile: NetworkProfile{
				Scope: "2001:db8::/64",
				Total: total,
				Top:   []VisitorLoad{{VisitorKey: "v1", Requests: total}},
			},
			Budget: 100,
			IsIPv6: true,
		})
		if verdict.ShouldBan {
			t.Errorf("网段总量 %d（阈值 500）不应封禁，实际封了: %s", total, verdict.Detail)
		}
	}
}

// TestNetworkConcentratedBansDevicesOnly 流量集中时只封异常设备，不动网段。
//
// 这是本规则的核心：访问者令牌是设备级标识，封它不波及同网段的其他访问者。
func TestNetworkConcentratedBansDevicesOnly(t *testing.T) {
	setNetworkRules(t, defaultNetworkRules())

	verdict := EvaluateNetworkBan(NetworkSignals{
		Profile: NetworkProfile{
			Scope: "2001:db8::/64",
			Total: 600,
			Top: []VisitorLoad{
				{VisitorKey: "abuser-1", Requests: 300},
				{VisitorKey: "abuser-2", Requests: 200},
				// 正常用户：量小，不该被封
				{VisitorKey: "normal-1", Requests: 20},
			},
			DistinctVisitors: 8,
		},
		Budget: 100,
		IsIPv6: true,
	})

	if !verdict.ShouldBan {
		t.Fatal("网段总量 600 且流量集中，应当封禁")
	}
	if verdict.BanNetwork {
		t.Error("流量集中时应精准封设备，不应封整个网段")
	}
	if len(verdict.VisitorKeys) != 2 {
		t.Fatalf("应只封 2 个异常设备，实际为 %d 个: %v",
			len(verdict.VisitorKeys), verdict.VisitorKeys)
	}
	for _, key := range verdict.VisitorKeys {
		if key == "normal-1" {
			t.Error("量未达单人预算的正常用户不应被封")
		}
	}
}

// TestNetworkSmallNetworkNotBanned 小网段里唯一的正常用户占比 100%，
// 但绝对量不高，不应被封。
//
// 这是「占比 + 绝对量」两个条件都要满足的理由：只看占比会把小网段的正常用户
// 当成异常设备——一个 /64 里只有一个人时，他天然占 100%。
func TestNetworkSmallNetworkNotBanned(t *testing.T) {
	setNetworkRules(t, defaultNetworkRules())

	// 总量刚过阈值，但全部来自若干个各自都在配额内的用户
	verdict := EvaluateNetworkBan(NetworkSignals{
		Profile: NetworkProfile{
			Scope: "2001:db8::/64",
			Total: 500,
			Top: []VisitorLoad{
				{VisitorKey: "u1", Requests: 99},
				{VisitorKey: "u2", Requests: 99},
				{VisitorKey: "u3", Requests: 99},
				{VisitorKey: "u4", Requests: 99},
				{VisitorKey: "u5", Requests: 99},
			},
			DistinctVisitors: 6,
		},
		Budget: 100,
		IsIPv6: true,
	})

	// Top 5 占比 99%，但没有任何单个令牌达到 100 的预算线，
	// 故不算「个别设备异常」，应退回网段级处置而非封这 5 个人
	if len(verdict.VisitorKeys) > 0 {
		t.Errorf("没有单个设备超出预算时不应精准封设备，实际封了 %v", verdict.VisitorKeys)
	}
}

// TestNetworkDispersedIPv6BansNetwork 流量分散且为 IPv6 时退回封 /64。
// 一个 /64 通常就是一个宽带用户，封它约等于封一个人，不算连坐。
func TestNetworkDispersedIPv6BansNetwork(t *testing.T) {
	setNetworkRules(t, defaultNetworkRules())

	verdict := EvaluateNetworkBan(NetworkSignals{
		Profile: NetworkProfile{
			Scope: "2001:db8::/64",
			Total: 1000,
			// Top 5 只占 250/1000 = 25%，流量摊在很多令牌上
			Top: []VisitorLoad{
				{VisitorKey: "v1", Requests: 50},
				{VisitorKey: "v2", Requests: 50},
				{VisitorKey: "v3", Requests: 50},
				{VisitorKey: "v4", Requests: 50},
				{VisitorKey: "v5", Requests: 50},
			},
			DistinctVisitors: 200,
		},
		Budget: 100,
		IsIPv6: true,
	})

	if !verdict.ShouldBan {
		t.Fatal("网段流量异常应当封禁")
	}
	if !verdict.BanNetwork {
		t.Error("IPv6 下认不出异常设备时应封整个 /64")
	}
	if len(verdict.VisitorKeys) > 0 {
		t.Error("流量分散时不应精准封设备——封 Top 几个解决不了问题")
	}
}

// TestNetworkDispersedIPv4AdvisesOnly 流量分散且为 IPv4 时一律不自动处置。
//
// IPv4 在这一步没有任何安全的处置粒度：/24 背后可能是整个校园网出口，
// 而共享 NAT 出口的精确 IP 就是段内所有人的出口，封它与封整段等价。
// 故只告警，交由管理员判断。
func TestNetworkDispersedIPv4AdvisesOnly(t *testing.T) {
	setNetworkRules(t, defaultNetworkRules())

	verdict := EvaluateNetworkBan(NetworkSignals{
		Profile: NetworkProfile{
			Scope: "203.0.113.0/24",
			Total: 1000,
			Top: []VisitorLoad{
				{VisitorKey: "v1", Requests: 50},
				{VisitorKey: "v2", Requests: 50},
			},
			DistinctVisitors: 200,
		},
		Budget: 100,
		IsIPv6: false,
	})

	if verdict.ShouldBan {
		t.Error("IPv4 认不出异常设备时不应自动封禁：无论封 /24 还是封出口地址都会连坐")
	}
	if !verdict.AdviseOnly {
		t.Error("判据已命中，应标记为待人工核查")
	}
	if verdict.BanNetwork {
		t.Error("IPv4 绝不应自动封 /24：那背后可能是整个校园网出口")
	}
	if len(verdict.VisitorKeys) > 0 {
		t.Error("流量分散时不应精准封设备")
	}
	// 告警要能说明发生了什么，否则人工核查无从下手
	if verdict.Reason == "" || verdict.Detail == "" {
		t.Error("仅告警的判定同样应带上原因与详情")
	}
}

// TestNetworkRuleDisabled 倍数为 0 表示不启用该条
func TestNetworkRuleDisabled(t *testing.T) {
	autoBan := defaultNetworkRules()
	autoBan.NetworkOverflowMultiplier = 0
	setNetworkRules(t, autoBan)

	verdict := EvaluateNetworkBan(NetworkSignals{
		Profile: NetworkProfile{Scope: "2001:db8::/64", Total: 999999},
		Budget:  100,
		IsIPv6:  true,
	})
	if verdict.ShouldBan {
		t.Error("该条已关闭，不应封禁")
	}
}

// TestNetworkAutoBanDisabled 自动封禁总开关关闭时该条也不生效
func TestNetworkAutoBanDisabled(t *testing.T) {
	autoBan := defaultNetworkRules()
	autoBan.Enabled = false
	setNetworkRules(t, autoBan)

	verdict := EvaluateNetworkBan(NetworkSignals{
		Profile: NetworkProfile{Scope: "2001:db8::/64", Total: 999999},
		Budget:  100,
		IsIPv6:  true,
	})
	if verdict.ShouldBan {
		t.Error("自动封禁已关闭，不应封禁")
	}
}

// TestNetworkZeroBudgetDoesNotBan 预算为 0（限流未配置）时该条不应生效，
// 否则任何流量都会因「0 × 倍数 = 0」而立即触发
func TestNetworkZeroBudgetDoesNotBan(t *testing.T) {
	setNetworkRules(t, defaultNetworkRules())

	verdict := EvaluateNetworkBan(NetworkSignals{
		Profile: NetworkProfile{Scope: "2001:db8::/64", Total: 100},
		Budget:  0,
		IsIPv6:  true,
	})
	if verdict.ShouldBan {
		t.Error("预算未配置时该条不应生效")
	}
}

// TestNetworkEmptyProfileDoesNotBan 空画像不应触发封禁
func TestNetworkEmptyProfileDoesNotBan(t *testing.T) {
	setNetworkRules(t, defaultNetworkRules())

	verdict := EvaluateNetworkBan(NetworkSignals{
		Profile: NetworkProfile{},
		Budget:  100,
		IsIPv6:  true,
	})
	if verdict.ShouldBan {
		t.Error("网段当日无请求时不应封禁")
	}
}

// TestNetworkVerdictCarriesDetail 判定结果应带上原因与详情，便于复核误判
func TestNetworkVerdictCarriesDetail(t *testing.T) {
	setNetworkRules(t, defaultNetworkRules())

	verdict := EvaluateNetworkBan(NetworkSignals{
		Profile: NetworkProfile{
			Scope:            "2001:db8::/64",
			Total:            600,
			Top:              []VisitorLoad{{VisitorKey: "abuser", Requests: 550}},
			DistinctVisitors: 3,
		},
		Budget: 100,
		IsIPv6: true,
	})

	if verdict.Reason == "" || verdict.Detail == "" {
		t.Error("封禁判定应带上原因与详情")
	}
}

// TestTopRequests Top 令牌请求量合计
func TestTopRequests(t *testing.T) {
	profile := NetworkProfile{
		Top: []VisitorLoad{
			{VisitorKey: "a", Requests: 10},
			{VisitorKey: "b", Requests: 20},
		},
	}
	if got := profile.TopRequests(); got != 30 {
		t.Errorf("合计应为 30，实际为 %d", got)
	}
	if got := (NetworkProfile{}).TopRequests(); got != 0 {
		t.Errorf("空画像应为 0，实际为 %d", got)
	}
}

// 协议判定（IPv4-mapped 按 IPv4 处理）的用例在 utils/netmask 包内，
// 那里是 IsIPv6 的归属地。

// TestNetworkCulpritThresholdFollowsRuleOne 异常设备的绝对量门槛与规则一同源。
//
// 网段画像的计数在限流判定之前就已累加、被拒的请求也算在内，故一个用满配额后
// 继续点的重度用户很快会越过「预算」这条线。若门槛只取预算本身，他会在远早于
// 规则一容忍区间的位置被判成异常设备——两处对「什么算过量」的判断就不一致了。
func TestNetworkCulpritThresholdFollowsRuleOne(t *testing.T) {
	autoBan := defaultNetworkRules()
	autoBan.DailyOverflowMultiplier = 3
	setNetworkRules(t, autoBan)

	// 预算 100，规则一倍数 3 → 门槛 300。
	// 用满配额后继续点的重度用户停在 299，不该被判为异常设备。
	verdict := EvaluateNetworkBan(NetworkSignals{
		Profile: NetworkProfile{
			Scope:            "2001:db8::/64",
			Total:            600,
			Top:              []VisitorLoad{{VisitorKey: "heavy", Requests: 299}},
			DistinctVisitors: 4,
		},
		Budget: 100,
		IsIPv6: true,
	})
	if len(verdict.VisitorKeys) > 0 {
		t.Errorf("未达规则一门槛的重度用户不应被判为异常设备，实际封了 %v", verdict.VisitorKeys)
	}

	// 达到 300 才算异常
	verdict = EvaluateNetworkBan(NetworkSignals{
		Profile: NetworkProfile{
			Scope:            "2001:db8::/64",
			Total:            600,
			Top:              []VisitorLoad{{VisitorKey: "abuser", Requests: 500}},
			DistinctVisitors: 4,
		},
		Budget: 100,
		IsIPv6: true,
	})
	if len(verdict.VisitorKeys) != 1 || verdict.VisitorKeys[0] != "abuser" {
		t.Errorf("达到门槛的设备应被精准封禁，实际为 %v", verdict.VisitorKeys)
	}
}

// TestNetworkCulpritThresholdWithRuleOneDisabled 规则一关闭（倍数为 0）时，
// 门槛应退回预算本身，而不是塌成 0——否则任何令牌都会被判为异常设备。
func TestNetworkCulpritThresholdWithRuleOneDisabled(t *testing.T) {
	autoBan := defaultNetworkRules()
	autoBan.DailyOverflowMultiplier = 0
	setNetworkRules(t, autoBan)

	verdict := EvaluateNetworkBan(NetworkSignals{
		Profile: NetworkProfile{
			Scope:            "2001:db8::/64",
			Total:            600,
			Top:              []VisitorLoad{{VisitorKey: "tiny", Requests: 1}},
			DistinctVisitors: 4,
		},
		Budget: 100,
		IsIPv6: true,
	})
	for _, key := range verdict.VisitorKeys {
		if key == "tiny" {
			t.Error("门槛不应塌成 0：只发过 1 次请求的令牌被判为异常设备")
		}
	}
}
