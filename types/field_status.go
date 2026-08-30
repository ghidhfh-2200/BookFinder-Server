package types

// 字段状态机：哪个动作在哪个状态下允许、会把状态迁到哪里，全部由本文件的表决定。
//
// 为什么要集中：这套规则原先散在各接口的前置检查里，同一件事写在四处，
// 漏一处就是一个 bug——「已转正后撤销确认」曾照样返回成功，就是因为
// 确认那侧加了检查而撤销那侧忘了。表驱动之后，加一个状态或动作只需改这里。

// FieldAction 对某个字段的一次表态。
type FieldAction string

const (
	// ActionReportOutdated 报告信息已过时
	ActionReportOutdated FieldAction = "report_outdated"
	// ActionRevokeOutdated 撤销自己的过时报告
	ActionRevokeOutdated FieldAction = "revoke_outdated"
	// ActionVerify 确认网站可用
	ActionVerify FieldAction = "verify"
	// ActionRevokeVerify 撤销自己的网站确认
	ActionRevokeVerify FieldAction = "revoke_verify"
)

// Kind 该动作作用于哪一种票
func (a FieldAction) Kind() ReportKind {
	switch a {
	case ActionVerify, ActionRevokeVerify:
		return ReportVerify
	default:
		return ReportOutdated
	}
}

// IsRevoke 该动作是否为撤销
func (a FieldAction) IsRevoke() bool {
	return a == ActionRevokeOutdated || a == ActionRevokeVerify
}

// WebsiteOnly 该动作是否只适用于承担 website 角色的字段
func (a FieldAction) WebsiteOnly() bool {
	return a == ActionVerify || a == ActionRevokeVerify
}

// FieldRule 一个「动作 × 状态」格子的规则。
type FieldRule struct {
	// Allowed 该状态下是否受理这个动作。为 false 时 Reason 说明缘由。
	Allowed bool
	// Reason 拒绝的原因，直接呈现给用户。仅 Allowed 为 false 时有意义。
	Reason string
	// Target 计数达到阈值时迁往的状态。留空表示不改变状态。
	//
	// 只有「达到阈值」才迁移；未达阈值只累计票数。撤销使票数跌破阈值时，
	// 状态按 Fallback 回落。
	Target LibraryStatus
	// Fallback 票数低于阈值时该处于的状态。留空表示不回落（状态不动）。
	//
	// 确认票没有 Fallback：转正是既成事实，退回会让「先攒票转正、
	// 再逐个撤票」成为破坏手段。
	Fallback LibraryStatus
}

// fieldRules 状态机全表：动作 → 当前状态 → 规则。
//
// 未列出的组合一律拒绝（见 FieldTransition），故新增状态时不会因为忘了补格子
// 而意外放行——那正是这张表要防的事。
var fieldRules = map[FieldAction]map[LibraryStatus]FieldRule{
	ActionReportOutdated: {
		// 有效 → 攒够票转为过时
		StatusGood: {Allowed: true, Target: StatusOutdated, Fallback: StatusGood},
		// 未验证期间也能报过时：地址本身可能就是错的，不必等它转正。
		// 跌破阈值时回到未验证，而不是被抹成有效——那等于绕过验证白拿一个「有效」
		StatusUnverified: {Allowed: true, Target: StatusOutdated, Fallback: StatusUnverified},
		// 已过时：结论已落定，再多的票都不改变什么
		StatusOutdated: {Reason: "该信息已被标记为过时"},
	},

	// 撤销过时报告：只会减少票数，故各状态都不设 Target——
	// 撤销不该把字段推向「过时」。只有 Fallback 起作用：票数跌破阈值就回落。
	ActionRevokeOutdated: {
		StatusGood:       {Allowed: true, Fallback: StatusGood},
		StatusUnverified: {Allowed: true, Fallback: StatusUnverified},
		// 已过时时允许撤销：撤到票数跌破阈值，状态就该回落，
		// 否则一旦标记过时便再无更正余地。
		//
		// 回落到 good 而非 unverified：过时票能把未验证的字段直接标成过时，
		// 撤回后原本该回到 unverified——但那需要知道它当初是不是未验证，
		// 而状态机只看当前状态。这里取 good 是有意的取舍：
		// 代价是极少数情形下少一道验证，好过让字段卡在过时出不来。
		StatusOutdated: {Allowed: true, Fallback: StatusGood},
	},

	ActionVerify: {
		// 未验证 → 攒够确认票转正。没有 Fallback：见 FieldRule.Fallback
		StatusUnverified: {Allowed: true, Target: StatusGood},
		StatusGood:       {Reason: "该网站已确认有效，无需再次确认"},
		StatusOutdated:   {Reason: "该信息已被标记为过时，无法确认可用"},
	},

	ActionRevokeVerify: {
		// 撤票只会减少票数，不设 Target：撤销不该把字段推向转正。
		// 留着 Target 的话，票数仍 >= 阈值时这一撤会把状态改成 good——
		// 一个「撤销」导致「转正」，没有任何说得通的解释。
		// 也不设 Fallback：未验证撤到 0 票依然是未验证。
		StatusUnverified: {Allowed: true},
		// 转正后不再可撤：撤了也不退回状态，那一票删掉不产生任何可见效果，
		// 回「成功」是在骗调用方；而票被悄悄抹掉后，日后地址变更退回未验证时
		// 票数已经少了一张
		StatusGood:     {Reason: "该网站已确认有效，确认无法再撤销"},
		StatusOutdated: {Reason: "该信息已被标记为过时，确认已无意义"},
	},
}

// FieldTransition 查某个动作在某状态下的规则。
// 未登记的组合返回不允许——新增状态时宁可挡住，也不要意外放行。
func FieldTransition(action FieldAction, status LibraryStatus) FieldRule {
	byStatus, ok := fieldRules[action]
	if !ok {
		return FieldRule{Reason: "不支持的操作"}
	}

	rule, ok := byStatus[status]
	if !ok {
		return FieldRule{Reason: "当前状态下无法执行该操作"}
	}

	return rule
}

// NextStatus 按票数推导该动作作用后字段应处的状态。
//
// 达到阈值迁往 Target，否则回落到 Fallback；两者留空时保持 current 不变。
// 报告与撤销共用这一个推导，故「撤销使票数跌破阈值后状态回落」不需要另写一遍。
func (r FieldRule) NextStatus(current LibraryStatus, count, threshold int) LibraryStatus {
	if count >= threshold {
		if r.Target == "" {
			return current
		}
		return r.Target
	}

	if r.Fallback == "" {
		return current
	}
	return r.Fallback
}
