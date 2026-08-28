package handlers

import (
	"bookfinder-backend/database"
	"bookfinder-backend/services/dashboard"
	"bookfinder-backend/utils"

	"github.com/gin-gonic/gin"
)

// GetDashboard 读取监控面板数据。
//
// 不写操作日志：这是个纯读接口，而管理员开着面板会周期性刷新，记进去只会把
// 操作日志刷满，真正需要审计的条目反被埋掉。
func GetDashboard(c *gin.Context) {
	utils.ResponseOK(c, dashboard.Read(c.Request.Context(), database.GetRedis()))
}
