package handlers

import (
	"net/http"

	"bookfinder-backend/api/middlewares"
	"bookfinder-backend/database"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/audit"
	"bookfinder-backend/utils/describe"
	"bookfinder-backend/utils/ratelimit"

	"github.com/gin-gonic/gin"
)

// GetRateRules 读取限流与自动封禁规则
func GetRateRules(c *gin.Context) {
	rules := ratelimit.Get()

	utils.ResponseOK(c, gin.H{
		"rules":      rules,
		"categories": types.AllLimitCategories,
		"warnings":   ratelimit.Warnings(&rules),
	})
}

// UpdateRateRules 保存限流规则，保存后立即热生效
func UpdateRateRules(c *gin.Context) {
	var req types.RateRules
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := ratelimit.Commit(&req); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 规则决定谁被拦、谁被封，变更记为 WARN 以便筛查
	audit.Warn(c, types.ActionRateRulesUpdate, describe.RateRules(&req))

	rules := ratelimit.Get()

	utils.ResponseOK(c, gin.H{
		"rules": rules,
		// 规则自洽但某条可能永远轮不到触发时在此提示，不阻断保存
		"warnings": ratelimit.Warnings(&rules),
	})
}

// GetRateStatus 返回当前访问者各类别的剩余配额。
// 对所有访问者开放，供前端提示还能操作多少次。
func GetRateStatus(c *gin.Context) {
	if !ratelimit.Enabled() {
		utils.ResponseOK(c, gin.H{"enabled": false, "statuses": []types.RateStatus{}})
		return
	}

	rdb := database.GetRedis()
	visitorKey, ok := middlewares.GetVisitorKeyFromContext(c)

	// Redis 不可用或无令牌时限流本身也不生效，如实告知前端
	if rdb == nil || !ok {
		utils.ResponseOK(c, gin.H{"enabled": false, "statuses": []types.RateStatus{}})
		return
	}

	statuses, err := ratelimit.Status(c.Request.Context(), rdb, visitorKey)
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	utils.ResponseOK(c, gin.H{"enabled": true, "statuses": statuses})
}
