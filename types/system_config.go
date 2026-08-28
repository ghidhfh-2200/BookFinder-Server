package types

// SystemConfig 服务自身的运行与存储配置，持久化为外部 JSON 文件。
//
// 与限流规则的分界：限流管「对访问者的约束」，本配置管「服务自身怎么跑」。
// 连接串与密钥不在此列，那些由部署环境经 .env 决定。
type SystemConfig struct {
	// Maintenance 定期清理任务
	Maintenance MaintenanceConfig `json:"maintenance"`
	// Notify 告警外发
	Notify NotifyConfig `json:"notify"`
	// Pagination 列表接口的分页约束
	Pagination PaginationConfig `json:"pagination"`
	// Server 服务器资源上限
	Server ServerLimits `json:"server"`
}

// NotifyConfig 告警外发配置：哪几类事件要通知管理员，以及经哪条通道送出。
//
// 这里没有任何凭据。Telegram 的令牌与接收方 ID、SMTP 的密码都由 .env 注入
// （见 config.TelegramConfig 与 config.SMTPConfig）：凭据可以代管理员发消息，
// 而本配置是管理页可读可写、且以明文 JSON 落盘的，两者不该放在一起。
//
// 三类事件的共同点是「需要人知道，但服务自己已经处理完了」。都有天然的去重：
// 自动封禁只在确有改动入库时发一次、流量异常每网段每天一条、申诉受 3 次上限。
// 故此处不再设节流。
type NotifyConfig struct {
	// AutoBan 规则触发自动封禁时通知。
	// 值得知道是因为它可能误封：判据是当日累计值，正常的重度用户也可能撞上。
	AutoBan bool `json:"auto_ban"`
	// NetworkAnomaly 检出网段流量异常时通知。
	//
	// 这类事件尤其需要人看：IPv4 网段流量异常且认不出具体设备时，服务不做任何
	// 处置（封 /24 或封共享出口都会连坐无关的人），只记一条待人工核查——
	// 没有通知的话，那条记录要等管理员主动翻日志才会被看到。
	NetworkAnomaly bool `json:"network_anomaly"`
	// Appeal 收到封禁申诉时通知。
	// 申诉在管理员处理前一直挂着，而被封者除了等没有别的办法。
	Appeal bool `json:"appeal"`
	// Email 邮件通道。
	//
	// 与 Telegram 并行而非互备：两条都开就都发。Telegram 在国内常需代理才通，
	// 邮件则基本处处可达；同时开启的意义正在于此，不必赌哪一条能出去。
	Email EmailConfig `json:"email"`
}

// Any 是否有任一类事件启用。全部关闭时无需构造发送器。
func (n NotifyConfig) Any() bool {
	return n.AutoBan || n.NetworkAnomaly || n.Appeal
}

// defaultSMTPPort 默认 SMTP 端口，隐式 TLS。
// 取 465 而非 587：前者连接建立即加密，没有「升级失败退回明文」的窗口。
const defaultSMTPPort = 465

// EmailConfig 邮件通道配置。
//
// 除密码之外的各项都在这里，故可在管理页改动并即时生效——换收件地址、
// 换发信服务商都不必重启。密码由 SMTP_PASSWORD 注入（见 config.SMTPConfig）。
type EmailConfig struct {
	// Enabled 是否经邮件外发
	Enabled bool `json:"enabled"`
	// Host SMTP 服务器主机名，如 smtp.qq.com
	Host string `json:"host"`
	// Port SMTP 端口。
	//
	// 465 为隐式 TLS（连接建立即加密），587 为 STARTTLS（先明文握手再升级）。
	// 两者都可用，程序按端口自动选择加密方式——这个区分不能交给用户填错，
	// 填错的后果是凭据以明文过网。
	Port int `json:"port"`
	// Username 发信账号，通常即发件邮箱地址
	Username string `json:"username"`
	// From 发件地址。留空则用 Username。
	//
	// 多数服务商要求它与认证账号一致，不一致会被拒信或判为伪造。
	From string `json:"from"`
	// To 收件地址
	To string `json:"to"`
}

// Usable 邮件通道是否真的可以发信。
//
// password 由调用方从环境变量取入，故本方法不依赖 config 包——types 被
// config 依赖，反向导入会成环。
func (e EmailConfig) Usable(password string) bool {
	return e.Enabled && e.Host != "" && e.Port > 0 &&
		e.Username != "" && e.To != "" && password != ""
}

// Sender 实际使用的发件地址：未单独指定时即认证账号
func (e EmailConfig) Sender() string {
	if e.From != "" {
		return e.From
	}
	return e.Username
}

// MaintenanceConfig 定期清理配置。
//
// 日志表没有清理机制时会一直增长：操作日志不受日志级别过滤（审计必须完整），
// 每次读取、报告、登录失败都写一条，一年后可达百万行以上。而日志查看页要对它
// 做排序分页与计数，表越大越慢。
type MaintenanceConfig struct {
	// Enabled 是否启用定期清理
	Enabled bool `json:"enabled"`
	// DailyAt 每日执行时刻，格式 "HH:MM"（24 小时制，服务器本地时区）。
	// 宜选凌晨低谷：清理要删多行，与正常读写争抢连接。
	DailyAt string `json:"daily_at"`
	// OperationLogRetentionDays 操作日志保留天数。
	// 这是「谁做了什么」的证据，复核误封与处理申诉时要用，故保留得比运行日志久。
	OperationLogRetentionDays int `json:"operation_log_retention_days"`
	// AppLogRetentionDays 运行日志保留天数。
	// 只在排障时有用，过期即可丢。
	AppLogRetentionDays int `json:"app_log_retention_days"`
}

// PaginationConfig 分页约束
type PaginationConfig struct {
	// DefaultSize 未指定 size 时的每页条数
	DefaultSize int `json:"default_size"`
	// MaxSize 每页条数上限。过大会让单次响应体积失控。
	MaxSize int `json:"max_size"`
}

// ServerLimits 服务器资源上限。
//
// 这两项是「服务不被打崩」的最后一道保险，也是唯一不依赖 Redis 的：
// 限流与封禁在 Redis 不可用时都会 fail-open。
type ServerLimits struct {
	// MaxRequestBodyBytes 请求体上限。中间件每个请求读一次，改后即时生效。
	MaxRequestBodyBytes int `json:"max_request_body_bytes"`
	// MaxConcurrentRequests 同时处理的请求数上限。
	//
	// 重启后生效：信号量在中间件构造时创建一次，之后容量不可改。
	MaxConcurrentRequests int `json:"max_concurrent_requests"`
	// ReadHeaderTimeoutSeconds 读完请求头的超时。
	// 不设时，只发一半请求头的连接可以无限占用一个协程与一条连接（Slowloris）。
	//
	// 重启后生效：http.Server 构造时即固定。
	ReadHeaderTimeoutSeconds int `json:"read_header_timeout_seconds"`
	// ReadTimeoutSeconds 读完整个请求的超时。重启后生效。
	ReadTimeoutSeconds int `json:"read_timeout_seconds"`
	// WriteTimeoutSeconds 写响应的超时。须大于最慢的正常响应，过短会截断大列表。
	// 重启后生效。
	WriteTimeoutSeconds int `json:"write_timeout_seconds"`
	// IdleTimeoutSeconds 空闲连接的保持时长。重启后生效。
	IdleTimeoutSeconds int `json:"idle_timeout_seconds"`
}

// DefaultSystemConfig 返回各项的默认值。
//
// 配置文件不存在时用它启动并写出一份，故这些值必须是可直接上线的：
// 取值与此前硬编码在代码里的一致。
func DefaultSystemConfig() SystemConfig {
	return SystemConfig{
		Maintenance: MaintenanceConfig{
			Enabled:                   true,
			DailyAt:                   "03:30",
			OperationLogRetentionDays: 180,
			AppLogRetentionDays:       30,
		},
		// 三类默认开启：凭据未配置时它们不产生任何效果，而配了凭据的部署
		// 显然是想收到通知，不该再去逐个打开
		Notify: NotifyConfig{
			AutoBan:        true,
			NetworkAnomaly: true,
			Appeal:         true,
			// 邮件默认关闭：它需要填主机、账号、收件地址等若干项，
			// 默认开启只会在未填完时反复记「配置不完整」
			Email: EmailConfig{
				Port: defaultSMTPPort,
			},
		},
		Pagination: PaginationConfig{
			DefaultSize: 20,
			MaxSize:     100,
		},
		Server: ServerLimits{
			MaxRequestBodyBytes:      64 << 10,
			MaxConcurrentRequests:    256,
			ReadHeaderTimeoutSeconds: 10,
			ReadTimeoutSeconds:       30,
			WriteTimeoutSeconds:      60,
			IdleTimeoutSeconds:       90,
		},
	}
}
