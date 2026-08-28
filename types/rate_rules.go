package types

// LimitCategory 限流类别，按操作性质分组计数
type LimitCategory string

const (
	// CategoryRead 读取：列表、详情、注册表查询。阈值宽松，只防脚本扫库。
	CategoryRead LimitCategory = "read"
	// CategoryCreate 新增图书馆
	CategoryCreate LimitCategory = "create"
	// CategoryUpdate 修改图书馆
	CategoryUpdate LimitCategory = "update"
	// CategoryReport 报告过时与撤销报告
	CategoryReport LimitCategory = "report"
	// CategoryAuth 管理员入口口令校验与登录。
	//
	// 按来源 IP 计而非按令牌：登录时还没有身份，且这一类的目的正是防口令爆破。
	// 不设此类别时，入口口令可被无成本地跑字典——用 404 语义隐藏入口的努力
	// 也就白费了。
	CategoryAuth LimitCategory = "auth"
	// CategoryAppeal 封禁申诉的提交与配额查询。
	//
	// 按来源 IP 计：被封者的令牌未必有效，申诉配额本就按 IP 记。
	// 这是被封者唯一可达的写接口，而应用库只允许一个连接，
	// 不限流的话被封者可以用它占死唯一的写连接。
	CategoryAppeal LimitCategory = "appeal"
)

// AllLimitCategories 全部限流类别，规则校验与前端展示按此顺序
var AllLimitCategories = []LimitCategory{
	CategoryRead,
	CategoryCreate,
	CategoryUpdate,
	CategoryReport,
	CategoryAuth,
	CategoryAppeal,
}

// IsValid 判断类别取值是否受支持
func (c LimitCategory) IsValid() bool {
	switch c {
	case CategoryRead, CategoryCreate, CategoryUpdate, CategoryReport,
		CategoryAuth, CategoryAppeal:
		return true
	default:
		return false
	}
}

// ByIP 该类别是否按来源 IP 计数，而非按访问者令牌。
//
// 认证与申诉都发生在「没有可信令牌」的处境下：登录时还没有身份，
// 被封者的令牌也未必有效，故只能按 IP 计。其余类别按令牌计，
// 使同一出口下的多人互不影响。
func (c LimitCategory) ByIP() bool {
	return c == CategoryAuth || c == CategoryAppeal
}

// CategoryLimit 单个类别的配额
type CategoryLimit struct {
	// Daily 每日配额，自然日零点重置
	Daily int `json:"daily"`
	// Burst 短窗口内允许的次数，用于挡瞄准脚本的突发调用
	Burst int `json:"burst"`
	// BurstWindowSeconds 短窗口长度，单位秒
	BurstWindowSeconds int `json:"burst_window_seconds"`
}

// AutoBanRules 自动封禁规则。各项为 0 表示不启用该条。
// 注意：连续多日用满配额不封禁——天天用满额度是重度用户的正常特征，
// 这类访问者每天照常被限流拦到次日零点，但不升级为封禁。
type AutoBanRules struct {
	// Enabled 是否启用自动封禁
	Enabled bool `json:"enabled"`
	// DailyOverflowMultiplier 当日尝试次数达到「每日配额 × 此倍数」即封禁。
	//
	// 计的是尝试次数而非成功次数：超出配额后的请求虽被拒，仍计入当日计数。
	// 因此触发此条意味着「配额用尽后仍在反复叩门」——正常用户看到提示就停了，
	// 达到配额数倍的尝试量只有脚本才做得出来。
	// 只看当天，不累计跨日。
	DailyOverflowMultiplier int `json:"daily_overflow_multiplier"`
	// BurstViolations 当日触发突发限制的累计次数达此值即封禁
	BurstViolations int `json:"burst_violations"`
	// DuplicateReports 当日被判为疑似重复报告的累计次数达此值即封禁
	DuplicateReports int `json:"duplicate_reports"`
	// ProbationOverflowMultiplier 当日见习请求数达到「见习额度 × 此倍数」即封禁。
	//
	// 针对的是「刻意不保存令牌、每次都以匿名身份请求」的客户端：
	// 正常用户的首个请求就换到了正式令牌，不会反复消耗见习额度。
	//
	// 判定在见习路径上执行（见 ratelimit.EvaluateProbationBan），故打中的正是
	// 那个不保存令牌的客户端；处置粒度为 IPv6 的 /64 或 IPv4 的精确地址。
	ProbationOverflowMultiplier int `json:"probation_overflow_multiplier"`

	// NetworkOverflowMultiplier 网段当日总量达到「网段预算 × 此倍数」时启动设备级排查。
	// 为 0 表示不启用网段级判定。
	//
	// 网段预算是「单个访问者各类每日配额之和」，故阈值随限流规则自动调整。
	// 需要网段级判据的原因：IPv6 终端可在自己的 /64 内随意换址，
	// 只看单个地址时，分散在一个 /64 内的刷量看不出异常。
	NetworkOverflowMultiplier int `json:"network_overflow_multiplier"`
	// NetworkTopVisitors 排查时考察流量最高的前几个访问者令牌。
	// 不宜过大：一次封几十个令牌与封整个网段没有区别，就失去了「精准」的意义。
	NetworkTopVisitors int `json:"network_top_visitors"`
	// NetworkConcentrationPercent Top 令牌合计占网段总量达此百分比即视为「流量集中」，
	// 此时只封这几个设备、不动网段；低于此值说明流量分散，认不出异常设备。
	NetworkConcentrationPercent int `json:"network_concentration_percent"`
}

// ProbationRules 见习配额：尚未持有有效令牌的来源，按其 IP 计的额度。
//
// 这是防盗刷的关键一环。限流按访问者令牌计数，而令牌旧实现是「没带就发一个新的」，
// 于是不带 cookie 的请求每次都是全新访问者、每次都拿满配额——按令牌计数的限流
// 形同不存在。
//
// 改为：无有效令牌的请求先走见习配额（按 IP 计），额度内放行并下发正式令牌，
// 额度耗尽则拒绝且不再下发。正常用户第一个请求就拿到令牌，此后一切照旧，
// 体验没有变化；脚本若坚持不带 cookie，则被按 IP 的小额度拦死。
type ProbationRules struct {
	// Daily 无令牌来源每日可发起的请求数。
	// 只需容纳「正常用户领取令牌」所需的量，故可以很小。
	Daily int `json:"daily"`
	// Burst 短窗口内允许的次数
	Burst int `json:"burst"`
	// BurstWindowSeconds 短窗口长度，单位秒
	BurstWindowSeconds int `json:"burst_window_seconds"`
}

// RateRules 限流与自动封禁规则，持久化为外部 JSON 配置文件。
// 在管理页保存后热生效，无需重启。
type RateRules struct {
	// Enabled 总开关，关闭则不限流也不自动封禁
	Enabled bool `json:"enabled"`
	// Limits 各类别的配额，键为 LimitCategory
	Limits map[LimitCategory]CategoryLimit `json:"limits"`
	// Probation 未持有有效令牌的来源所受的额度
	Probation ProbationRules `json:"probation"`
	// AutoBan 自动封禁规则
	AutoBan AutoBanRules `json:"auto_ban"`
}

// RateStatus 某类别的当前用量，供前端展示剩余配额
type RateStatus struct {
	Category LimitCategory `json:"category"`
	// DailyUsed 当日已用次数
	DailyUsed int `json:"daily_used"`
	// DailyLimit 每日配额
	DailyLimit int `json:"daily_limit"`
	// Remaining 当日剩余次数
	Remaining int `json:"remaining"`
}
