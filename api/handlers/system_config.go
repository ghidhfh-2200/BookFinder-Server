package handlers

import (
	"net/http"

	"bookfinder-backend/config"
	"bookfinder-backend/logger"
	"bookfinder-backend/models"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/audit"
	"bookfinder-backend/utils/describe"
	"bookfinder-backend/utils/sysconfig"

	"github.com/gin-gonic/gin"
)

// GetSystemConfig 读取系统配置，并附上两张日志表的当前规模。
//
// 一并返回规模是因为只看保留天数看不出策略的实际效果：表里到底还有多少行、
// 最早一条是什么时候，才说明清理有没有在跑。
func GetSystemConfig(c *gin.Context) {
	payload := gin.H{"config": sysconfig.Get()}

	// 只回「凭据是否齐备」这两个布尔值，绝不回令牌、接收方 ID 或发信密码。
	//
	// 需要它们是因为告警开关与凭据分处两地：开关在这份配置里，凭据在 .env 里。
	// 没有它们，管理页上开关是开着的，而通知可能根本发不出去，从界面上看不出来。
	payload["telegram_configured"] = config.Get().Telegram.Configured()
	payload["smtp_password_configured"] = config.Get().SMTP.Configured()

	stats, err := models.GetLogStats()
	if err != nil {
		// 取不到规模不影响配置本身的读取与保存，故只记一笔、照常返回
		logger.Warnf("读取日志表规模失败: %v", err)
	} else {
		payload["log_stats"] = stats
	}

	utils.ResponseOK(c, payload)
}

// UpdateSystemConfig 保存系统配置，保存后即时热生效。
//
// 例外是 HTTP 超时与并发上限：它们在服务器构造时即固定，改动需重启
// （管理页已标注）。此处照常保存，重启后自然生效。
func UpdateSystemConfig(c *gin.Context) {
	var req types.SystemConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := sysconfig.Commit(&req); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 配置决定日志留多久、请求体多大，变更记为 WARN 以便筛查
	audit.Warn(c, types.ActionSystemConfigUpdate, describe.SystemConfig(&req))

	utils.ResponseOK(c, gin.H{"config": sysconfig.Get()})
}
