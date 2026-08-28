package handlers

import (
	"net/http"

	"bookfinder-backend/models"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"

	"github.com/gin-gonic/gin"
)

// GetOperationLogs 分页查询用户操作日志
func GetOperationLogs(c *gin.Context) {
	page, size := utils.Pagination(c)

	logs, total, err := models.GetOperationLogs(&types.OperationLogQuery{
		User:   c.Query("user"),
		Action: c.Query("action"),
		Level:  normalizeLevelQuery(c.Query("level")),
		Page:   page,
		Size:   size,
	})
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	utils.ResponsePaginated(c, logs, total, page, size)
}

// GetLogs 分页查询应用运行日志
func GetLogs(c *gin.Context) {
	page, size := utils.Pagination(c)

	logs, total, err := models.GetLogs(&types.LogQuery{
		Level:   normalizeLevelQuery(c.Query("level")),
		Keyword: c.Query("keyword"),
		Page:    page,
		Size:    size,
	})
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	utils.ResponsePaginated(c, logs, total, page, size)
}

// GetLogMeta 返回筛选项：已出现的操作类型与可用等级。
// 操作类型从数据里取，新增类型时前端无需同步改动。
func GetLogMeta(c *gin.Context) {
	actions, err := models.ListOperationLogActions()
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	utils.ResponseOK(c, gin.H{
		"actions": actions,
		"levels": []string{
			types.LevelDebug,
			types.LevelInfo,
			types.LevelWarn,
			types.LevelError,
		},
	})
}

// normalizeLevelQuery 等级筛选统一为大写，不识别的取值视为不筛选
func normalizeLevelQuery(level string) string {
	switch level {
	case types.LevelDebug, types.LevelInfo, types.LevelWarn, types.LevelError:
		return level
	default:
		return ""
	}
}
