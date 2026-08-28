package ratelimit

import (
	"fmt"

	"bookfinder-backend/types"
)

// Signals 判定自动封禁所需的当日累计数据
type Signals struct {
	// Category 本次操作的类别
	Category types.LimitCategory
	// DailyUsed 该类别当日尝试次数，含被限流拒绝的那些
	DailyUsed int
	// DailyLimit 该类别的每日配额
	DailyLimit int
	// BurstViolations 当日在「本类别」触发突发限制的累计次数。
	// 各类别分别计数，不跨类别合并。
	BurstViolations int
	// DuplicateReports 当日被判为疑似重复报告的累计次数
	DuplicateReports int
}

// BanVerdict 自动封禁判定结果
type BanVerdict struct {
	// ShouldBan 是否应当封禁
	ShouldBan bool
	// Reason 触发的规则，写入封禁记录的 Reason
	Reason string
	// Detail 触发时的具体数据，写入封禁记录的 Detail，便于复核误判
	Detail string
}

// EvaluateBan 判定是否触发自动封禁。命中任一规则即封。
//
// 有意不封禁的情形：连续多日用满配额。天天用满额度是重度用户的正常特征，
// 这类访问者每天照常被限流拦到次日零点，但不升级为封禁。
// 因此规则一只看当天，不累计跨日的达标天数。
//
// 这里的判据都按访问者令牌累计，处置也落到该令牌。按来源 IP 累计的判据不放在
// 这里：本函数的调用点在限流中间件上，处置对象是「当次请求者」，而共享出口下
// 当次请求者与造成异常的人往往不是同一个——那会封掉无辜者。见习额度超限的
// 判定因此挪到了见习路径上（见 EvaluateProbationBan），那里打中的正是本人。
func EvaluateBan(signals Signals) BanVerdict {
	autoBan := AutoBan()
	if !autoBan.Enabled {
		return BanVerdict{}
	}

	// 一、当日尝试次数远超配额。
	// 计的是尝试量而非成功量：用满配额后的请求虽被拒，仍计入当日计数，
	// 故此条命中意味着「配额用尽后仍在反复叩门」——正常用户看到提示就停了。
	if autoBan.DailyOverflowMultiplier > 0 && signals.DailyLimit > 0 {
		threshold := signals.DailyLimit * autoBan.DailyOverflowMultiplier
		if signals.DailyUsed >= threshold {
			return BanVerdict{
				ShouldBan: true,
				Reason:    "配额用尽后仍反复请求",
				Detail: fmt.Sprintf("%s 类操作当日尝试 %d 次（含被拒），达配额 %d 的 %d 倍阈值",
					signals.Category, signals.DailyUsed, signals.DailyLimit,
					autoBan.DailyOverflowMultiplier),
			}
		}
	}

	// 二、在同一类别上反复撞突发限制：脚本化调用的典型特征。
	// 按类别分别计数，避免多个类别各违规几次就凑够阈值。
	if autoBan.BurstViolations > 0 && signals.BurstViolations >= autoBan.BurstViolations {
		return BanVerdict{
			ShouldBan: true,
			Reason:    "频繁触发突发限制",
			Detail: fmt.Sprintf("%s 类操作当日触发突发限制 %d 次，阈值 %d",
				signals.Category, signals.BurstViolations, autoBan.BurstViolations),
		}
	}

	// 三、大量疑似重复报告：绕过去重、刷报告数的特征
	if autoBan.DuplicateReports > 0 && signals.DuplicateReports >= autoBan.DuplicateReports {
		return BanVerdict{
			ShouldBan: true,
			Reason:    "大量重复报告",
			Detail: fmt.Sprintf("当日疑似重复报告 %d 次，阈值 %d",
				signals.DuplicateReports, autoBan.DuplicateReports),
		}
	}

	return BanVerdict{}
}
