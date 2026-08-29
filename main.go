package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"bookfinder-backend/api/middlewares"
	"bookfinder-backend/api/routes"
	"bookfinder-backend/config"
	"bookfinder-backend/database"
	"bookfinder-backend/logger"
	"bookfinder-backend/models"
	"bookfinder-backend/services"
	"bookfinder-backend/services/notify"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/banlist"
	"bookfinder-backend/utils/describe"
	"bookfinder-backend/utils/ratelimit"
	"bookfinder-backend/utils/schema"
	"bookfinder-backend/utils/sysconfig"
)

// 嵌入式前端文件
//
//go:embed all:frontend/dist
var staticFiles embed.FS

func main() {
	debugMode := flag.Bool("debug", false, "启用调试模式 Enable debug mode")
	flag.Parse()

	// 加载配置（从 .env 注入）。日志库路径来自配置，故配置先行加载，
	// 此阶段的日志尚无处落库，会回落到 stderr。
	if err := config.Load(); err != nil {
		fmt.Printf("系统配置加载失败: %v\n", err)
		os.Exit(1)
	}
	cfg := config.Get()

	// 加载图书馆 Info 字段注册表。读写 Info 都要按它对齐，故须在数据库初始化前就位。
	// 日志库也在 MySQL 里，此前的日志都回落到 stderr。
	//
	// 加载会顺带补齐缺失的内置字段并回写文件，故版本升级新增内置字段时，
	// 停服换二进制重启即可，不必手改 JSON。补齐了哪些留到日志就绪后再报。
	restoredFields, err := schema.Load(cfg.Library.SchemaPath)
	if err != nil {
		fmt.Printf("图书馆字段注册表加载失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化图书馆数据库（服务器本地 MySQL）。
	// 它会在库不存在时建库，故必须先于日志系统：日志表也建在同一个库里。
	if err := database.Initialize(); err != nil {
		fmt.Printf("图书馆数据库初始化失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志系统（MySQL，独立连接）
	logCfg := &logger.Config{
		DSN:          cfg.Database.DSN(),
		AlsoToStdout: false,
		Level:        "info",
	}
	if *debugMode {
		logCfg.AlsoToStdout = true
		logCfg.Level = "debug"
	}
	if err := logger.Initialize(logCfg); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := logger.Close(); err != nil {
			fmt.Printf("Failed to close logger: %v\n", err)
		}
	}()

	logger.Infof("系统配置加载完成")
	logger.Infof("图书馆字段注册表加载完成，共 %d 个字段: %s",
		len(schema.Fields()), cfg.Library.SchemaPath)
	// 补齐记为 WARN：它意味着配置文件缺了内置字段（升级带来的新字段，
	// 或被手工删掉），已自动补回并回写，但值得管理员知道注册表变过
	if len(restoredFields) > 0 {
		logger.Warnf("注册表缺少内置字段 %s，已按内置声明补齐并回写配置文件；"+
			"已有记录的该字段将补为空值", strings.Join(restoredFields, "、"))
	}
	logger.Infof("图书馆数据库初始化完成，日志系统已就绪")

	// 加载限流与自动封禁规则
	if err := ratelimit.Load(cfg.RateLimit.RulesPath); err != nil {
		logger.Fatalf("限流规则加载失败: %v", err)
	}
	logger.Infof("限流规则加载完成: %s", cfg.RateLimit.RulesPath)

	// 加载系统配置。文件不存在时以默认值创建一份，故已部署的实例无需先手工建文件。
	if err := sysconfig.Load(cfg.System.ConfigPath); err != nil {
		logger.Fatalf("系统配置加载失败: %v", err)
	}
	logger.Infof("系统配置加载完成: %s", cfg.System.ConfigPath)

	// 连接 Redis（限流计数）。连不上只告警不中断：限流是 fail-open 的。
	database.InitializeRedis()

	// 初始化应用数据库（本地 SQLite：用户、封禁主体与屏蔽名单）
	logger.Infof("正在初始化应用数据库...")
	if err := database.InitializeApp(); err != nil {
		logger.Fatalf("应用数据库初始化失败: %v", err)
	}
	logger.Infof("应用数据库初始化完成，应用库: %s", cfg.AppDatabase.Path)

	// 注入访问者令牌的签名密钥。
	//
	// 令牌自证签发方，服务端因此可以拒绝一切非自己签发的令牌——这是限流的根基：
	// 若令牌无从验真，就只能「没带就发一个新的」，于是不带 Cookie 的请求每次都是
	// 全新访问者、每次都拿满配额，按令牌计数的限流形同不存在。
	//
	// 复用 JWT 密钥而不另设一个：两者的安全要求相同，多一个必填项只会增加漏配的机会。
	// 密钥变更会使已签发的令牌全部失效，访问者需重新领取，仅此而已。
	if cfg.Security.JWTSecret == "" {
		logger.Fatalf("JWT_SECRET 未配置：访问者令牌与管理员登录都依赖它，不能留空")
	}
	utils.SetVisitorTokenSecret([]byte(cfg.Security.JWTSecret))

	// 注入安卓客户端的请求签名密钥。留空则不校验签名，
	// 此时一律不采信客户端上报的设备标识（见 middlewares.ClientSignMiddleware）。
	if cfg.Security.AppHMACSecret == "" {
		logger.Warnf("APP_HMAC_SECRET 未配置，将不采信安卓端上报的设备标识")
	} else {
		middlewares.SetClientSignSecret([]byte(cfg.Security.AppHMACSecret))
		logger.Infof("安卓客户端请求签名校验已启用")
	}

	// 载入内存封禁名单。
	//
	// 封禁判定在每个 API 请求上执行，而封禁数据在单连接的 SQLite 里，
	// 若每个请求都查库，封禁检查本身就成了最容易被打崩的瓶颈。故全量驻留内存，
	// 写操作时再重建（见 models.ReloadBanList）。
	if err := models.ReloadBanList(); err != nil {
		logger.Fatalf("封禁名单载入失败: %v", err)
	}
	stats := banlist.Count()
	logger.Infof("封禁名单载入完成：%d 个主体、%d 个标识（含 %d 个网段）",
		stats.Subjects, stats.Idents, stats.Networks)

	// 获取嵌入的静态文件
	staticFS, err := fs.Sub(staticFiles, "frontend/dist")
	if err != nil {
		logger.Fatalf("嵌入静态文件加载失败: %v", err)
	}

	// 设置路由
	logger.Infof("正在初始化路由...")
	router, err := routes.SetupRouter(staticFS)
	if err != nil {
		// 可信代理配错会让封禁与限流被一个请求头绕过，故不带着这种配置上线
		logger.Fatalf("路由初始化失败: %v", err)
	}
	logger.Infof("路由初始化完成")

	// 启动定期清理任务。
	//
	// 放在 HTTP 服务器之前启动、之后停止：它只碰日志表，与请求路径无关，
	// 但停止要等它把当前这批删完（见下方关闭顺序的说明）。
	maintenanceCtx, stopMaintenance := context.WithCancel(context.Background())
	waitMaintenance := services.StartMaintenance(maintenanceCtx)

	// 启动 Telegram 告警外发。
	//
	// 凭据只从环境变量取，不进系统配置文件、也不回给前端：这是一对可以代管理员
	// 发消息的凭据，而系统配置在管理页可读可写。哪几类告警要发由系统配置决定。
	//
	// 未配置时静默跳过：这是一项可选能力，不配就是不用。
	notifyCtx, stopNotify := context.WithCancel(context.Background())
	notifyStarted, waitNotify := notify.Start(notifyCtx, notify.Credentials{
		TelegramToken:  cfg.Telegram.BotToken,
		TelegramChatID: cfg.Telegram.ChatID,
		SMTPPassword:   cfg.SMTP.Password,
	})
	if notifyStarted {
		logger.Infof("告警外发已启用：%s", describe.NotifyChannels(
			cfg.Telegram.Configured(),
			sysconfig.Get().Notify.Email.Usable(cfg.SMTP.Password)))
	} else if sysconfig.Get().Notify.Any() {
		// 开关开着却没有凭据：告警不会送出，而这一点从管理页上看不出来
		logger.Warnf("系统配置中启用了告警，但两条通道都不可用：" +
			"Telegram 需 TELEGRAM_BOT_TOKEN 与 TELEGRAM_CHAT_ID，" +
			"邮件需 SMTP_PASSWORD 并在管理页填好发信参数。通知不会送出")
	}

	// 创建 HTTP 服务器。
	//
	// 各项超时不是可选的调优项，而是「服务器不被打崩」的一部分：
	// 不设 ReadHeaderTimeout 时，只发一半请求头的连接可以无限占用一个 goroutine
	// 与一条连接（Slowloris）；限流按请求次数计，拦不住这种「一次请求打一整天」。
	//
	// 取值来自系统配置，但在此处一次读定：http.Server 构造后这些字段不再被读取，
	// 故它们改动后必须重启才生效（管理页已标注）。
	limits := sysconfig.Get().Server
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           router,
		ReadHeaderTimeout: time.Duration(limits.ReadHeaderTimeoutSeconds) * time.Second,
		ReadTimeout:       time.Duration(limits.ReadTimeoutSeconds) * time.Second,
		WriteTimeout:      time.Duration(limits.WriteTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(limits.IdleTimeoutSeconds) * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	// 启动服务器（后台 goroutine）
	go func() {
		defer logger.RecoverPanic("HTTP Server Goroutine")
		mode := "PRODUCTION"
		if *debugMode {
			mode = "DEBUG"
		}
		fmt.Printf("%s 已启动 [%s]\n", cfg.Website.Name, mode)
		fmt.Printf("地址: http://%s:%d\n", cfg.Server.Host, cfg.Server.Port)

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("HTTP 服务器启动失败: %v", err)
		}
	}()

	// 优雅关闭处理
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Infof("正在关闭服务器...")

	// 关闭顺序不可调换，各步都依赖前一步已完成：
	//
	//  1. 先停 HTTP 服务器并等在途请求处理完——它们还要读写数据库；
	//  2. 再停后台任务并等它们退出——清理任务正在删日志表里的行，
	//     告警协程可能正在发一条已出队的消息；
	//  3. 最后关连接。
	//
	// 反过来做（先关库）会让在途请求打到已关闭的连接上。
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Errorf("HTTP 服务器关闭超时，仍继续退出流程: %v", err)
	}

	// 通知清理任务停止并等它退出
	stopMaintenance()
	waitMaintenance()

	// 停止告警外发。顺序不可调换：先取消 ctx 让协程退出，再关通道阻止新的投递。
	// 反过来做的话，协程可能在收到取消信号前把积压的告警逐条发完，
	// 每条最多等一个发送超时。队列中未发出的告警被丢弃——事件本身已在操作日志里。
	stopNotify()
	notify.Stop()
	waitNotify()

	// 关闭应用数据库
	if err := database.CloseApp(); err != nil {
		logger.Errorf("应用数据库关闭失败: %v", err)
	}

	// 关闭图书数据库
	if err := database.Close(); err != nil {
		logger.Errorf("图书数据库关闭失败: %v", err)
	}

	// 关闭 Redis
	if err := database.CloseRedis(); err != nil {
		logger.Errorf("Redis 关闭失败: %v", err)
	}

	logger.Infof("服务器已退出")
}
