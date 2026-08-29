package handlers

import (
	"net/http"

	"bookfinder-backend/api/middlewares"
	"bookfinder-backend/config"
	"bookfinder-backend/logger"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/audit"

	"github.com/gin-gonic/gin"
)

// ReloadClientSignSecret 从 .env 重新读取 APP_HMAC_SECRET 并热替换签名密钥。
// 供开发安卓端时用：改完 .env 调一次即可，不必重启。
//
// 仅调试模式：非 -debug 时路由不注册（见 routes.SetupRouter）。
// 不写回 .env——文件是这一项的唯一来源，流程是先改文件再调它。
func ReloadClientSignSecret(c *gin.Context) {
	// 路由未注册已是主要屏障，此处再查一次，防日后被挪进常规分组
	if !logger.IsDebug() {
		APINotFound(c)
		return
	}

	secret, err := config.ReloadAppHMACSecret()
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	middlewares.SetClientSignSecret([]byte(secret))

	// 只记状态不记密钥：操作日志会在管理页展示
	if secret == "" {
		audit.Warn(c, types.ActionClientSignReload, "签名密钥已重载为空，不再采信设备标识")
		logger.Warnf("签名密钥已重载为空，将不再采信客户端上报的设备标识")
	} else {
		audit.Warn(c, types.ActionClientSignReload, "签名密钥已重载")
		logger.Infof("安卓客户端签名密钥已重载，长度 %d", len(secret))
	}

	// 不回密钥本身，只回是否生效与长度，够确认 .env 的改动被读到了
	utils.ResponseOK(c, gin.H{
		"enabled": secret != "",
		"length":  len(secret),
	})
}
