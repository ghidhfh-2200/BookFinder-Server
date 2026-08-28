package ratelimit

import "fmt"

// NetworkVerdict 网段级判定的结果。判据看网段，处置尽量落到设备。
type NetworkVerdict struct {
	// ShouldBan 是否应当封禁
	ShouldBan bool
	// VisitorKeys 应当封禁的访问者令牌。非空表示精准处置，不产生连坐。
	VisitorKeys []string
	// BanNetwork 是否退回封禁整个网段。仅在认不出异常设备、且来源为 IPv6 时为真。
	BanNetwork bool
	// AdviseOnly 判据命中但没有安全的处置粒度，只告警、不封禁。
	// 仅出现在「IPv4 且认不出异常设备」：/24 与共享出口的精确 IP 都会连坐。
	// 为真时 ShouldBan 为假，Reason 与 Detail 照常填好供日志用。
	AdviseOnly bool
	// Reason 触发的规则，写入封禁记录的 Reason
	Reason string
	// Detail 触发时的具体数据，写入封禁记录的 Detail，便于复核误判
	Detail string
}

// NetworkSignals 网段级判定所需的数据
type NetworkSignals struct {
	// Profile 该网段当日的流量画像
	Profile NetworkProfile
	// Budget 网段预算：单个访问者的每日配额合计
	Budget int
	// IsIPv6 来源是否为 IPv6。决定认不出异常设备时能否退回网段封禁。
	IsIPv6 bool
}

// EvaluateNetworkBan 判定网段流量是否异常，以及该如何处置。
//
// 需要网段级判据是因为 IPv6 终端可在自己的 /64 内随意换址，每换一个地址就是一个
// 「新来源」，按单地址计数的规则看不出异常。
//
// 「流量集中」要求占比与绝对量两个条件同时成立：只看占比会误伤小网段——
// 一个 /64 里只有一个正常用户时，他天然占 100%。
//
// 认不出异常设备时按协议区分：IPv6 封 /64（约等于封一个人）；
// IPv4 不封 /24，那背后可能是整个校园网出口。
func EvaluateNetworkBan(signals NetworkSignals) NetworkVerdict {
	autoBan := AutoBan()
	if !autoBan.Enabled || autoBan.NetworkOverflowMultiplier <= 0 {
		return NetworkVerdict{}
	}

	profile := signals.Profile
	if signals.Budget <= 0 || profile.Total <= 0 {
		return NetworkVerdict{}
	}

	// 一、网段总量是否超出预算的给定倍数
	threshold := signals.Budget * autoBan.NetworkOverflowMultiplier
	if profile.Total < threshold {
		return NetworkVerdict{}
	}

	// 二、流量是否集中在少数令牌上
	topRequests := profile.TopRequests()
	concentration := topRequests * 100 / profile.Total

	// 绝对量下限与规则一同源。不能只取预算本身：画像的计数在限流判定之前就已累加
	// （被拒的请求也算），用满配额后继续点的重度用户很快越过预算线，
	// 那远早于规则一给他的容忍区间。倍数为 0 时退回预算，避免门槛塌成 0。
	culpritThreshold := signals.Budget * max(autoBan.DailyOverflowMultiplier, 1)

	culprits := make([]string, 0, len(profile.Top))
	for _, load := range profile.Top {
		if load.Requests >= culpritThreshold {
			culprits = append(culprits, load.VisitorKey)
		}
	}

	if concentration >= autoBan.NetworkConcentrationPercent && len(culprits) > 0 {
		return NetworkVerdict{
			ShouldBan:   true,
			VisitorKeys: culprits,
			Reason:      "网段内个别设备异常刷量",
			Detail: fmt.Sprintf(
				"网段 %s 当日请求 %d 次，达预算 %d 的 %d 倍阈值；"+
					"其中 %d 个设备贡献 %d 次（占 %d%%），已单独封禁这些设备，"+
					"同网段其他访问者不受影响",
				profile.Scope, profile.Total, signals.Budget,
				autoBan.NetworkOverflowMultiplier,
				len(culprits), topRequests, concentration),
		}
	}

	// 三、认不出异常设备：流量分散，或对方不保存令牌、无稳定令牌可封
	if !signals.IsIPv6 {
		// IPv4 没有安全的处置粒度：/24 可能是整个校园网出口，而共享 NAT 出口的
		// 精确 IP 就是段内所有人的出口，封它与封整段等价。交管理员判断。
		return NetworkVerdict{
			AdviseOnly: true,
			Reason:     "来源网段流量异常且无法归因到具体设备",
			Detail: fmt.Sprintf(
				"网段 %s 当日请求 %d 次，达预算 %d 的 %d 倍阈值；"+
					"流量分散在 %d 个访问者标识上（前 %d 个仅占 %d%%），无法归因到具体设备。"+
					"IPv4 网段可能是共用出口，封整段或封出口地址都会连坐无关的人，"+
					"故未自动处置，请人工核查",
				profile.Scope, profile.Total, signals.Budget,
				autoBan.NetworkOverflowMultiplier,
				profile.DistinctVisitors, len(profile.Top), concentration),
		}
	}

	return NetworkVerdict{
		ShouldBan:  true,
		BanNetwork: true,
		Reason:     "网段流量异常且无法归因到具体设备",
		Detail: fmt.Sprintf(
			"网段 %s 当日请求 %d 次，达预算 %d 的 %d 倍阈值；"+
				"流量分散在 %d 个访问者标识上（前 %d 个仅占 %d%%），无法归因到具体设备。"+
				"IPv6 的 /64 通常归单个用户所有，故封禁整段",
			profile.Scope, profile.Total, signals.Budget,
			autoBan.NetworkOverflowMultiplier,
			profile.DistinctVisitors, len(profile.Top), concentration),
	}
}

// NetworkBudget 网段预算：单个访问者各类操作的每日配额合计。
//
// 以「一个人一天最多能发多少请求」为基准，网段总量达到它的若干倍即视为异常。
// 这样阈值会随限流规则自动调整，不必单独维护一个绝对数字。
func NetworkBudget() int {
	current := Get()

	budget := 0
	for _, limit := range current.Limits {
		budget += limit.Daily
	}

	return budget
}
