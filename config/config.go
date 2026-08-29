package config

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"bookfinder-backend/logger"

	"github.com/joho/godotenv"
)

var (
	config *Config
	once   sync.Once
)

// Config 系统配置结构
type Config struct {
	Server ServerConfig
	// Database 业务数据（图书馆）所在的服务器本地 MySQL
	Database DatabaseConfig
	// AppDatabase 用户与封禁 IP 所在的本地 SQLite
	AppDatabase AppDatabaseConfig
	// Redis 限流计数所在的 Redis
	Redis RedisConfig
	// Library 图书馆 Info 字段注册表
	Library LibraryConfig
	// RateLimit 限流与自动封禁规则
	RateLimit RateLimitConfig
	// System 服务自身的运行与存储配置
	System SystemPathConfig
	// Telegram 告警外发的凭据
	Telegram TelegramConfig
	// SMTP 发信凭据（仅密码，其余在系统配置里）
	SMTP     SMTPConfig
	Security SecurityConfig
	Website  WebsiteConfig
}

// TelegramConfig Telegram 机器人凭据，用于把封禁与申诉告警推到管理员手机上。
//
// 只走环境变量，不进系统配置文件、也不回给前端：这是一对可以代管理员发消息的
// 凭据，而系统配置是管理页可读可写的。哪几类告警要外发由系统配置决定
// （见 types.NotifyConfig），凭据本身不在那里。
type TelegramConfig struct {
	// BotToken 机器人令牌（TELEGRAM_BOT_TOKEN），形如 123456789:AA...
	BotToken string
	// ChatID 接收方（TELEGRAM_CHAT_ID）：用户或群组的数字 ID，群组为负数；
	// 频道可用 @频道名。
	ChatID string
}

// Configured 凭据是否齐备。缺任一项都不外发：
// 只有令牌没有接收方无从发送，只有接收方没有令牌则连接口都调不通。
func (t TelegramConfig) Configured() bool {
	return t.BotToken != "" && t.ChatID != ""
}

// SMTPConfig 发信凭据。
//
// 只有密码在此处。主机、端口、发件账号与收件地址都在系统配置里，可在管理页改动
// 并即时生效——那些是会变的，而密码不是。
//
// 密码不进系统配置的理由与 Telegram 令牌相同，且更硬：系统配置存为明文 JSON
// 文件，而读取接口会把整份配置回给前端。密码放进去等于既落明文盘、又从接口漏出。
//
// QQ 邮箱用的是「授权码」而非登录密码：在邮箱设置里开启 SMTP 服务时一次性获取，
// 之后长期不变，故放在环境变量里并不增加维护负担。
type SMTPConfig struct {
	// Password 发信密码或授权码（SMTP_PASSWORD）
	Password string
}

// Configured 密码是否已配置。
// 其余各项在系统配置里，是否真的可以发信由 types.EmailConfig.Usable 一并判定。
func (s SMTPConfig) Configured() bool {
	return s.Password != ""
}

// SystemPathConfig 系统配置文件的位置
type SystemPathConfig struct {
	// ConfigPath 系统配置的 JSON 文件路径（SYSTEM_CONFIG_PATH）。
	// 文件不存在时会以默认值创建一份。多数项在管理页保存后热生效，
	// HTTP 超时与并发上限须重启（见 types.ServerLimits）。
	ConfigPath string
}

// RedisConfig Redis 配置，存放限流计数。
// 计数以天为单位刷新，丢失只影响当日配额，故不要求持久化。
type RedisConfig struct {
	Addr     string // REDIS_ADDR
	Password string // REDIS_PASSWORD
	DB       int    // REDIS_DB
}

// RateLimitConfig 限流规则配置
type RateLimitConfig struct {
	// RulesPath 限流与自动封禁规则的 JSON 文件路径（RATE_RULES_PATH）。
	// 改这个文件并保存即热生效，无需重启。
	RulesPath string
}

// LibraryConfig 图书馆配置
type LibraryConfig struct {
	// SchemaPath Info 字段注册表的 JSON 文件路径（LIBRARY_SCHEMA_PATH）。
	// 注册表随业务演进变更，改这个文件并重启即可更新字段，无需迁移数据库。
	SchemaPath string
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host string // SERVER_HOST
	Port int    // SERVER_PORT
	// TrustedProxies 可信反向代理的地址或 CIDR（TRUSTED_PROXIES，逗号分隔）。
	//
	// 只有来自这些地址的请求，其 X-Forwarded-For 才会被采信。留空表示服务直接
	// 对外监听，届时一律以 TCP 对端地址为准、完全无视该头。
	//
	// 这一项决定 ClientIP() 是否可信，而封禁与限流都以它为判据：若采信了任意来源
	// 的 X-Forwarded-For，被封者改一个请求头即可绕过封禁，更能凭伪造的来源
	// 反过来封掉任意 IP。故不允许配成全网段，且解析失败时启动即失败。
	TrustedProxies []string
}

// DatabaseConfig 服务器本地 MySQL 数据库配置，存放图书馆等业务数据
type DatabaseConfig struct {
	Host     string // DB_HOST
	Port     int    // DB_PORT
	User     string // DB_USER
	Password string // DB_PASSWORD
	Database string // DB_NAME
}

// AppDatabaseConfig 本地 SQLite 应用数据库配置，存放用户与封禁 IP
type AppDatabaseConfig struct {
	Path string // APP_DB_PATH，SQLite 文件路径
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	// JWTSecret 管理员令牌与访问者令牌共用的签名密钥（JWT_SECRET）
	JWTSecret string
	// AdminEntryToken 管理员登录入口口令（ADMIN_ENTRY_TOKEN）。
	// 只有访问 /bookfinder/<口令> 才会显示管理员登录界面；留空表示入口关闭。
	AdminEntryToken string
	// AppHMACSecret 安卓客户端请求签名密钥（APP_HMAC_SECRET）。
	//
	// 留空则不校验签名，此时一律不采信客户端上报的设备标识——未验签的设备标识
	// 改一个请求头就能伪造，既能换新身份，也能拿他人的标识去污染其封禁。
	//
	// 侧载分发的现实限制：密钥内置在 APK 里，逆包即可取出。故这一层的作用是
	// 把伪造门槛从「改一个请求头」抬到「逆包取密钥」，而非不可破解。
	AppHMACSecret string
}

// WebsiteConfig 网站配置
type WebsiteConfig struct {
	Name string // WEBSITE_NAME
}

// Load 加载配置：读取 .env 并注入环境变量，缺失项使用默认值。
// 系统环境变量优先级高于 .env（godotenv 不覆盖已存在的变量），便于容器化部署。
func Load() error {
	var err error
	once.Do(func() {
		if loadErr := godotenv.Load(); loadErr != nil {
			logger.Warnf("未找到 .env 文件，改用系统环境变量: %v", loadErr)
		}

		config = &Config{
			Server: ServerConfig{
				Host: getEnv("SERVER_HOST", "localhost"),
				Port: getEnvInt("SERVER_PORT", 8080),
				// 默认只信任本机反代：最常见的部署是 Nginx/Caddy 与本服务同机。
				// 显式设为空串则表示直接对外监听，不信任任何代理。
				TrustedProxies: getEnvList("TRUSTED_PROXIES", []string{"127.0.0.1", "::1"}),
			},
			Database: DatabaseConfig{
				Host:     getEnv("DB_HOST", "127.0.0.1"),
				Port:     getEnvInt("DB_PORT", 3306),
				User:     getEnv("DB_USER", "root"),
				Password: getEnv("DB_PASSWORD", ""),
				Database: getEnv("DB_NAME", "bookfinder"),
			},
			AppDatabase: AppDatabaseConfig{
				Path: getEnv("APP_DB_PATH", "./data/app.db"),
			},
			Redis: RedisConfig{
				Addr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
				Password: getEnv("REDIS_PASSWORD", ""),
				DB:       getEnvInt("REDIS_DB", 0),
			},
			Library: LibraryConfig{
				SchemaPath: getEnv("LIBRARY_SCHEMA_PATH", "./data/library_schema.json"),
			},
			RateLimit: RateLimitConfig{
				RulesPath: getEnv("RATE_RULES_PATH", "./data/rate_rules.json"),
			},
			System: SystemPathConfig{
				ConfigPath: getEnv("SYSTEM_CONFIG_PATH", "./data/system_config.json"),
			},
			Telegram: TelegramConfig{
				BotToken: getEnv("TELEGRAM_BOT_TOKEN", ""),
				ChatID:   getEnv("TELEGRAM_CHAT_ID", ""),
			},
			SMTP: SMTPConfig{
				Password: getEnv("SMTP_PASSWORD", ""),
			},
			Security: SecurityConfig{
				JWTSecret:       getEnv("JWT_SECRET", ""),
				AdminEntryToken: getEnv("ADMIN_ENTRY_TOKEN", ""),
				AppHMACSecret:   getEnv("APP_HMAC_SECRET", ""),
			},
			Website: WebsiteConfig{
				Name: getEnv("WEBSITE_NAME", "BookFinder"),
			},
		}

		if config.Security.JWTSecret == "" {
			logger.Warnf("JWT_SECRET 未配置，请在 .env 中设置后再上线")
		}
		if config.Security.AdminEntryToken == "" {
			logger.Warnf("ADMIN_ENTRY_TOKEN 未配置，管理员登录入口已关闭")
		}

		// 可信代理配错会让封禁形同虚设，故作为加载错误上报，由 main 终止启动
		if vErr := validateTrustedProxies(config.Server.TrustedProxies); vErr != nil {
			err = vErr
			return
		}

		// 令牌会被拼进请求 URL 的路径段，形状不对可能改变请求指向，故配错即启动失败
		if vErr := validateTelegram(config.Telegram); vErr != nil {
			err = vErr
			return
		}

		if len(config.Server.TrustedProxies) == 0 {
			logger.Infof("未配置可信代理，将以 TCP 对端地址为准，忽略 X-Forwarded-For")
		} else {
			logger.Infof("可信代理: %s", strings.Join(config.Server.TrustedProxies, ", "))
		}
	})

	return err
}

// Get 获取配置实例
func Get() *Config {
	if config == nil {
		panic("config not initialized: Load() must be called successfully before Get()")
	}
	return config
}

// ReloadAppHMACSecret 从 .env 重新读取 APP_HMAC_SECRET 并返回新值，
// 供调试模式下的重载接口使用（见 handlers.ReloadClientSignSecret）。
//
// 只返回新值，不改写 config：该结构体启动后由各处并发读取，就地改是数据竞争。
// 密钥的活值在中间件里（那边用原子读写），config 中的只是启动快照。
// 也不重建整个 Config：其余各项已用于建连接、构造 http.Server，改了也不生效。
//
// Overload 而非 Load：后者不覆盖已存在的环境变量，不覆盖就永远读回旧值。
func ReloadAppHMACSecret() (string, error) {
	if err := godotenv.Overload(); err != nil {
		return "", fmt.Errorf("重新读取 .env 失败: %w", err)
	}

	return os.Getenv("APP_HMAC_SECRET"), nil
}

// DSN 生成 MySQL 连接串
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		d.User, d.Password, d.Host, d.Port, d.Database)
}

// ServerDSN 生成不指定库名的 MySQL 连接串，用于建库前连接服务器
func (d DatabaseConfig) ServerDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8mb4&parseTime=True&loc=Local",
		d.User, d.Password, d.Host, d.Port)
}

// getEnv 读取字符串环境变量，为空时返回默认值
func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getEnvInt 读取整型环境变量，为空或非法时返回默认值
func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		logger.Warnf("环境变量 %s=%q 不是合法整数，使用默认值 %d", key, v, def)
		return def
	}
	return n
}

// getEnvList 读取逗号分隔的列表环境变量。
//
// 与 getEnv 不同，此处区分「未设置」与「显式设为空」：前者取默认值，
// 后者返回空列表。TRUSTED_PROXIES 需要这个区分——显式留空表示
// 「不信任任何代理」，是一个有意义的取值，不该被默认值顶掉。
func getEnvList(key string, def []string) []string {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return def
	}

	items := make([]string, 0, len(def))
	for _, item := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			items = append(items, trimmed)
		}
	}

	return items
}

// telegramTokenPattern 机器人令牌的形状：数字 ID 加冒号，再跟一段 URL 安全的字符。
//
// 必须严格限定字符集，而不能只判非空：令牌会被拼进请求 URL 的路径段
// （Telegram 的接口设计如此），若其中含 "/" 或 "?"，一个配错或被改写的令牌
// 就能把请求改指到另一个方法上。这里只放行 Telegram 实际会签发的字符。
var telegramTokenPattern = regexp.MustCompile(`^\d{5,}:[A-Za-z0-9_-]{30,}$`)

// telegramChatPattern 接收方标识：数字 ID（群组为负数）或 @频道名
var telegramChatPattern = regexp.MustCompile(`^(-?\d{1,20}|@[A-Za-z][A-Za-z0-9_]{4,31})$`)

// validateTelegram 校验 Telegram 凭据。
//
// 两项都留空是合法的，表示不启用外发告警。只填一项则视为配错：那多半是漏了一个，
// 静默不发会让人以为告警在跑——而告警的价值恰在于「没消息就是没事」。
func validateTelegram(t TelegramConfig) error {
	if t.BotToken == "" && t.ChatID == "" {
		return nil
	}

	if t.BotToken == "" || t.ChatID == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN 与 TELEGRAM_CHAT_ID 须同时配置或同时留空：" +
			"只配一项无法发送，而告警静默失效会让人误以为一切正常")
	}

	// 不回显令牌内容：错误会进日志表，而那张表管理页可读
	if !telegramTokenPattern.MatchString(t.BotToken) {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN 格式不正确，应形如 123456789:AA... " +
			"（数字 ID、冒号、再跟字母数字与 -_）")
	}

	if !telegramChatPattern.MatchString(t.ChatID) {
		return fmt.Errorf("TELEGRAM_CHAT_ID %q 格式不正确，应为数字 ID（群组为负数）"+
			"或 @频道名", t.ChatID)
	}

	return nil
}

// validateTrustedProxies 校验可信代理配置。
//
// 采信任意来源的 X-Forwarded-For 会让整套封禁失效，故配错时启动即失败，
// 而不是带着一个可被绕过的封禁系统上线。
func validateTrustedProxies(proxies []string) error {
	for _, proxy := range proxies {
		// 全网段等于信任任何来源，与不设可信代理是两回事：
		// 后者只认 TCP 对端地址，前者会采信伪造的请求头。
		if proxy == "0.0.0.0/0" || proxy == "::/0" {
			return fmt.Errorf("TRUSTED_PROXIES 不能包含 %s：这会采信任意来源的 "+
				"X-Forwarded-For，使封禁可被一个请求头绕过。"+
				"若本服务直接对外监听，请将该项留空", proxy)
		}

		if strings.Contains(proxy, "/") {
			if _, _, err := net.ParseCIDR(proxy); err != nil {
				return fmt.Errorf("TRUSTED_PROXIES 中的 %q 不是合法 CIDR: %w", proxy, err)
			}
			continue
		}

		if net.ParseIP(proxy) == nil {
			return fmt.Errorf("TRUSTED_PROXIES 中的 %q 不是合法 IP 地址", proxy)
		}
	}

	return nil
}
