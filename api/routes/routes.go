package routes

import (
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"bookfinder-backend/api/handlers"
	"bookfinder-backend/api/handlers/ban"
	"bookfinder-backend/api/handlers/library"
	"bookfinder-backend/api/middlewares"
	"bookfinder-backend/config"
	"bookfinder-backend/logger"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/sysconfig"

	"github.com/gin-gonic/gin"
)

// getStaticFSHandler 从嵌入的文件系统中返回单个静态文件。
// 压缩协商与缓存头见 serveStatic（static.go）。
func getStaticFSHandler(staticFS fs.FS, name string) gin.HandlerFunc {
	// 入口文件每次校验，其余带 hash 或极少变动，可长期缓存
	cacheControl := cacheImmutable
	if name == "index.html" {
		cacheControl = cacheRevalidate
	}

	return func(c *gin.Context) {
		if !serveStatic(c, staticFS, name, cacheControl) {
			c.String(http.StatusNotFound, "Not found")
		}
	}
}

// SetupRouter 设置所有路由。
//
// 可信代理配错会让封禁与限流被一个请求头绕过，故此处返回 error 而非静默继续。
func SetupRouter(staticFS fs.FS) (*gin.Engine, error) {
	if logger.IsDebug() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// 接管 Gin 自身的输出（启动横幅、路由注册、框架告警）
	gin.DefaultWriter = logger.GinWriter()
	gin.DefaultErrorWriter = logger.GinWriter()

	// 不用 gin.Default()：它装的是 Gin 自带的 Logger()，那个把状态码与耗时
	// 格式化成一行文本，逼得日志层去解析字符串才能分辨错误级别。
	// 改用自己的访问日志中间件，直接读 c.Writer.Status()。
	r := gin.New()
	r.Use(middlewares.AccessLogMiddleware())
	r.Use(gin.Recovery())

	// ========== 可信代理 ==========
	// Gin 默认信任所有代理，即无条件采信 X-Forwarded-For。那样一来
	// ClientIP() 可被任意伪造：被封者改一个请求头即可绕过封禁，
	// 更能凭伪造的来源反过来把任意 IP 送进封禁名单。
	// 故此处必须显式设定，且配置非法时启动失败。
	trusted := config.Get().Server.TrustedProxies
	if len(trusted) == 0 {
		// 直接对外监听：只认 TCP 对端地址，完全无视 X-Forwarded-For
		if err := r.SetTrustedProxies(nil); err != nil {
			return nil, fmt.Errorf("failed to disable trusted proxies: %w", err)
		}
	} else if err := r.SetTrustedProxies(trusted); err != nil {
		return nil, fmt.Errorf("failed to set trusted proxies: %w", err)
	}

	// ========== 全局资源封顶 ==========
	// 与限流分工：限流按访问者计次数、可被 Redis 故障旁路；
	// 这两条按资源用量计、不依赖任何外部组件，是服务不被打崩的最后保险。
	r.Use(middlewares.ConcurrencyLimitMiddleware(sysconfig.Get().Server.MaxConcurrentRequests))
	r.Use(middlewares.BodyLimitMiddleware())

	// ========== API 路由 ==========
	// 中间件顺序即判定顺序，不可随意调换：
	//  1. 识别身份——管理员豁免后续限制，故须最先确定；
	//  2. 校验客户端签名——通过后才采信安卓端上报的设备标识；
	//  3. 下发/校验访问者令牌——封禁要用令牌作标识，故须先于封禁判定；
	//  4. 拦截被封禁者——此时 IP、令牌、设备标识三者齐备，可一并比对；
	//  5. 采集流量指标——被封禁者不计入；
	//  6. 限流——只对未被封禁者计数。
	api := r.Group("/api")
	api.Use(middlewares.IdentityMiddleware())
	api.Use(middlewares.ClientSignMiddleware())
	api.Use(middlewares.VisitorMiddleware())
	api.Use(middlewares.BanMiddleware())
	// 6. 采集流量指标——放在封禁之后，被拦下的请求不计入访问量；
	//    此时访问者令牌已在上下文里，可用于在线人数去重。
	api.Use(middlewares.MetricsMiddleware())

	// ========== 认证 API ==========
	// 入口口令校验与登录本身不要求已登录身份。
	// 但必须限流：否则入口口令可被无成本地跑字典，
	// 用 404 语义隐藏入口的做法也就失去了意义。该类按来源 IP 计数。
	api.POST("/admin/verify-entry",
		middlewares.RateLimitMiddleware(types.CategoryAuth), handlers.VerifyEntry)
	api.POST("/admin/login",
		middlewares.RateLimitMiddleware(types.CategoryAuth), handlers.Login)

	// 当前访问者身份（Users 组也可查询，用于前端判断可用操作）
	api.GET("/me", handlers.GetCurrentIdentity)

	// ========== 图书馆 API ==========
	// 读取与报告过时对所有访问者开放；增改需对应权限位；删除仅管理员。
	// 注意：Users 组增改的限流尚未接入，规则待定义。
	// 限流按访问者令牌计数、每日重置；异常特征触发按 IP 的永久封禁。管理员不受限流影响。
	libraries := api.Group("/libraries")
	libraries.GET("", middlewares.PermissionMiddleware(utils.PermissionLibraryRead),
		middlewares.RateLimitMiddleware(types.CategoryRead), library.GetLibraries)
	libraries.GET("/:id", middlewares.PermissionMiddleware(utils.PermissionLibraryRead),
		middlewares.RateLimitMiddleware(types.CategoryRead), library.GetLibrary)
	libraries.POST("", middlewares.PermissionMiddleware(utils.PermissionLibraryCreate),
		middlewares.RateLimitMiddleware(types.CategoryCreate), library.CreateLibrary)
	libraries.PUT("/:id", middlewares.PermissionMiddleware(utils.PermissionLibraryUpdate),
		middlewares.RateLimitMiddleware(types.CategoryUpdate), library.UpdateLibrary)
	// 删除仅管理员可用，不限流
	libraries.DELETE("/:id", middlewares.PermissionMiddleware(utils.PermissionLibraryDelete),
		library.DeleteLibrary)
	// 状态属于每个信息字段自身，故报告过时按字段进行。
	// 撤销与报告是同一件事的两个方向，共用一个权限位与同一份配额。
	libraries.POST("/:id/fields/:field/report-outdated",
		middlewares.PermissionMiddleware(utils.PermissionLibraryReportOutdated),
		middlewares.RateLimitMiddleware(types.CategoryReport), library.ReportFieldOutdated)
	libraries.DELETE("/:id/fields/:field/report-outdated",
		middlewares.PermissionMiddleware(utils.PermissionLibraryReportOutdated),
		middlewares.RateLimitMiddleware(types.CategoryReport), library.RevokeFieldOutdated)

	// ========== 字段注册表 API ==========
	// 读取对所有访问者开放：前端要据此动态渲染表格与表单，不能硬编码字段。
	api.GET("/library-schema", middlewares.PermissionMiddleware(utils.PermissionLibraryRead),
		middlewares.RateLimitMiddleware(types.CategoryRead), library.GetSchema)

	// 当前访问者的剩余配额，供前端提示
	api.GET("/rate-status", handlers.GetRateStatus)

	// ========== 封禁申诉 API ==========
	// 对被封者开放（见 middlewares.BanMiddleware 的放行名单），
	// 接口自身要求来源确实被封。每个 IP 最多提交 3 次，次数由数据库在事务内判定。
	//
	// 必须限流：这是被封者唯一可达的写接口，而提交申诉要在应用库开事务，
	// 应用库又只允许一个连接（见 database/app.go）——不限流的话，
	// 被封者可以用这一个接口占死唯一的写连接。
	// 按 IP 计数：被封者的令牌未必有效，且申诉配额本就是按 IP 记的。
	api.GET("/appeal/quota",
		middlewares.RateLimitMiddleware(types.CategoryAppeal), handlers.GetAppealQuota)
	api.POST("/appeal",
		middlewares.RateLimitMiddleware(types.CategoryAppeal), handlers.SubmitAppeal)

	// ========== 管理员 API ==========
	adminAPI := api.Group("/admin")
	adminAPI.Use(middlewares.AdminMiddleware())

	// 修改自己的密码
	adminAPI.POST("/password", handlers.ChangePassword)

	// 字段注册表编辑：保存后热更新并自动补全库中已有记录
	adminAPI.PUT("/library-schema",
		middlewares.PermissionMiddleware(utils.PermissionSystemManagement), library.UpdateSchema)

	// 限流与自动封禁规则：保存后热生效
	adminAPI.GET("/rate-rules",
		middlewares.PermissionMiddleware(utils.PermissionSystemManagement), handlers.GetRateRules)
	adminAPI.PUT("/rate-rules",
		middlewares.PermissionMiddleware(utils.PermissionSystemManagement), handlers.UpdateRateRules)

	// 监控面板。归入系统管理权限：它汇总的是服务整体状况，
	// 与封禁管理那类具体处置不同。
	adminAPI.GET("/dashboard",
		middlewares.PermissionMiddleware(utils.PermissionSystemManagement),
		handlers.GetDashboard)

	// 系统配置：清理任务、分页与资源上限。多数项保存后热生效，
	// HTTP 超时与并发上限须重启（见 types.ServerLimits）。
	systemMgmt := adminAPI.Group("/system")
	systemMgmt.Use(middlewares.PermissionMiddleware(utils.PermissionSystemManagement))
	systemMgmt.GET("/config", handlers.GetSystemConfig)
	systemMgmt.PUT("/config", handlers.UpdateSystemConfig)

	// 日志查看
	logsMgmt := adminAPI.Group("/logs")
	logsMgmt.Use(middlewares.PermissionMiddleware(utils.PermissionSystemManagement))
	logsMgmt.GET("/operations", handlers.GetOperationLogs)
	logsMgmt.GET("/app", handlers.GetLogs)
	logsMgmt.GET("/meta", handlers.GetLogMeta)

	// 封禁管理。
	// 封禁挂在「主体」上，一个主体可有多个标识（IP、网段、访问者令牌、设备标识），
	// 故解封按主体 ID 而非按 IP——同一主体下的全部标识要一并解除。
	banMgmt := adminAPI.Group("/bans")
	banMgmt.Use(middlewares.PermissionMiddleware(utils.PermissionIPBanManagement))
	banMgmt.GET("", ban.GetBans)
	banMgmt.POST("", ban.BanIP)
	banMgmt.DELETE("/:id", ban.UnbanSubject)
	// 查看某个 IP 的申诉详情。申诉按 IP 记录，故此处仍以 IP 为参数。
	banMgmt.GET("/ip/:ip/appeals", handlers.GetAppealsByIP)

	// 处理申诉：受理则一并解封
	adminAPI.PUT("/appeals/:id",
		middlewares.PermissionMiddleware(utils.PermissionIPBanManagement), handlers.ReviewAppeal)

	// ========== 静态文件服务 ==========
	// 产物根目录下的文件。逐个注册而不是通配整个根路径：通配会与 /api 及
	// 前端路由抢匹配，而这里只需要放出 public/ 下确实存在的那几个。
	//
	// 名单要与 frontend/public/ 的内容一致：列了不存在的文件只是白注册一条
	// 返回 404 的路由，而漏掉真实存在的文件更糟——请求会落到 NoRoute，
	// 拿到一份 index.html。浏览器把 HTML 当图标解析，图标就一直出不来。
	rootStaticFiles := []string{
		"favicon.svg",
	}
	for _, file := range rootStaticFiles {
		r.GET("/"+file, getStaticFSHandler(staticFS, file))
		// HEAD 与 GET 同一处理：serveStatic 会在 HEAD 时只发响应头
		r.HEAD("/"+file, getStaticFSHandler(staticFS, file))
	}

	// 静态资源服务（带 hash，可长期缓存）
	assetsFS, _ := fs.Sub(staticFS, "assets")
	assetHandler := func(c *gin.Context) {
		// 去掉 Gin 通配参数的前导斜杠，并挡掉 ../ 之类的路径穿越
		name := path.Clean(strings.TrimPrefix(c.Param("filepath"), "/"))
		if name == "." || name == ".." || strings.HasPrefix(name, "../") {
			c.String(http.StatusNotFound, "Not found")
			return
		}

		if !serveStatic(c, assetsFS, name, cacheImmutable) {
			c.String(http.StatusNotFound, "Not found")
		}
	}
	r.GET("/assets/*filepath", assetHandler)
	r.HEAD("/assets/*filepath", assetHandler)

	// 其他请求：API 返回 404 JSON，其余交给前端入口
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api") {
			handlers.APINotFound(c)
			return
		}
		getStaticFSHandler(staticFS, "index.html")(c)
	})

	return r, nil
}
