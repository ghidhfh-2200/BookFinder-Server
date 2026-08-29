package types

import "time"

// 日志级别，应用日志与操作日志共用同一套取值
const (
	LevelDebug = "DEBUG"
	LevelInfo  = "INFO"
	LevelWarn  = "WARN"
	LevelError = "ERROR"
)

// LogEntry 应用运行日志，存储于 MySQL 的 logs 表。
// 记录服务自身的运行情况：启动、SQL、HTTP 访问、内部错误。
type LogEntry struct {
	ID        int       `json:"id"        gorm:"primaryKey;autoIncrement"`
	Timestamp time.Time `json:"timestamp" gorm:"index;not null"`
	Level     string    `json:"level"     gorm:"size:16;index;not null"`
	Message   string    `json:"message"   gorm:"type:text"`
}

// TableName 指定表名
func (LogEntry) TableName() string {
	return "logs"
}

// 操作类型，记录「谁对什么做了什么」中的动作部分
const (
	// ActionAdminLogin 管理员登录
	ActionAdminLogin = "admin_login"
	// ActionAdminLoginFailed 管理员登录失败
	ActionAdminLoginFailed = "admin_login_failed"
	// ActionAdminEntryDenied 管理员入口口令校验失败
	ActionAdminEntryDenied = "admin_entry_denied"
	// ActionAdminPasswordChanged 管理员修改密码
	ActionAdminPasswordChanged = "admin_password_changed"

	// ActionLibraryCreate 新增图书馆
	ActionLibraryCreate = "library_create"
	// ActionLibraryUpdate 修改图书馆
	ActionLibraryUpdate = "library_update"
	// ActionLibraryDelete 删除图书馆
	ActionLibraryDelete = "library_delete"

	// ActionFieldReport 报告某字段过时
	ActionFieldReport = "field_report"
	// ActionFieldReportRevoke 撤销自己的过时报告
	ActionFieldReportRevoke = "field_report_revoke"
	// ActionFieldReportRejected 报告被判为疑似重复，未计数
	ActionFieldReportRejected = "field_report_rejected"

	// ActionSchemaUpdate 变更字段注册表
	ActionSchemaUpdate = "schema_update"

	// ActionClientSignReload 重载安卓客户端签名密钥（仅调试模式可触发）
	ActionClientSignReload = "client_sign_reload"

	// ActionIPBan 管理员封禁 IP
	ActionIPBan = "ip_ban"
	// ActionIPBanAuto 规则触发的自动封禁
	ActionIPBanAuto = "ip_ban_auto"
	// ActionIPBanAdvised 判据发现网段流量异常，但没有安全的处置粒度，
	// 未自动封禁、仅记录待人工核查（见 ratelimit.NetworkVerdict.AdviseOnly）
	ActionIPBanAdvised = "ip_ban_advised"
	// ActionIPBanSkipped 判据成立但来源是回环地址，未予处置。
	//
	// 单独一个动作而非复用 ip_ban_auto：那个动作表示「已封」，
	// 混在一起会让审计时数不清到底封了多少。而这条仍要记——它往往说明
	// 本机有脚本在密集调用，或者反代没有透传真实来源。
	ActionIPBanSkipped = "ip_ban_skipped"
	// ActionIPUnban 解封 IP
	ActionIPUnban = "ip_unban"

	// ActionRateLimited 操作被限流拦下
	ActionRateLimited = "rate_limited"
	// ActionRateRulesUpdate 变更限流规则
	ActionRateRulesUpdate = "rate_rules_update"

	// ActionAppealSubmit 提交封禁申诉
	ActionAppealSubmit = "appeal_submit"
	// ActionAppealAccepted 受理申诉并解封
	ActionAppealAccepted = "appeal_accepted"
	// ActionAppealRejected 驳回申诉
	ActionAppealRejected = "appeal_rejected"

	// ActionMaintenanceCleanup 定期清理过期日志。
	// 记一条是为了能确认任务真的在跑——否则它是否执行过无从判断。
	ActionMaintenanceCleanup = "maintenance_cleanup"
	// ActionSystemConfigUpdate 变更系统配置
	ActionSystemConfigUpdate = "system_config_update"
)

// OperationLog 用户操作日志，存储于 MySQL 的 operation_logs 表。
// 与 LogEntry 分表：前者是业务审计记录，后者是服务运行日志，查询方式与保留策略都不同。
type OperationLog struct {
	ID int `json:"id" gorm:"primaryKey;autoIncrement"`
	// User 操作者：管理员为用户名，匿名访问者为来源 IP
	User string `json:"user" gorm:"not null;size:64;index"`
	// Action 操作类型，取值见上方 Action* 常量
	Action string `json:"action" gorm:"not null;size:48;index"`
	// Level 事件等级，取值与应用日志一致
	Level string `json:"level" gorm:"not null;size:16;index"`
	// Detail 操作详情，供人阅读
	Detail string `json:"detail" gorm:"type:text"`
	// IP 来源 IP。管理员的 User 是用户名，故 IP 单列保存以便溯源。
	IP string `json:"ip" gorm:"not null;size:45;index"`
	// VisitorKey 访问者令牌哈希，可把同一人的操作串起来，即便其换了 IP。
	// 管理员操作也带上，便于关联其匿名期间的行为。
	VisitorKey string    `json:"-" gorm:"size:64;index"`
	Timestamp  time.Time `json:"timestamp" gorm:"index;not null"`
}

// TableName 指定表名
func (OperationLog) TableName() string {
	return "operation_logs"
}

// OperationLogQuery 操作日志查询条件
type OperationLogQuery struct {
	User   string // 按操作者精确匹配
	Action string // 按操作类型精确匹配
	Level  string // 按等级精确匹配
	Page   int
	Size   int
}

// LogQuery 应用日志查询条件
type LogQuery struct {
	Level   string // 按等级精确匹配
	Keyword string // 消息内容模糊匹配
	Page    int
	Size    int
}
