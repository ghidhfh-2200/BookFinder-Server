package handlers

import (
	"errors"
	"net/http"

	"bookfinder-backend/api/middlewares"
	"bookfinder-backend/services"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/audit"

	"github.com/gin-gonic/gin"
)

// entryTokenRequest 入口口令校验请求
type entryTokenRequest struct {
	EntryToken string `json:"entry_token"`
}

// loginRequest 管理员登录请求
type loginRequest struct {
	EntryToken string `json:"entry_token"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

// changePasswordRequest 修改密码请求
type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// VerifyEntry 校验管理员登录入口口令。
// 前端访问 /bookfinder/<口令> 时先调用此接口，通过后才渲染登录界面。
func VerifyEntry(c *gin.Context) {
	var req entryTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := services.VerifyEntryToken(req.EntryToken); err != nil {
		// 入口口令错误一律回 404 语义，不暴露入口是否存在
		audit.Warn(c, types.ActionAdminEntryDenied, "管理员入口口令校验失败")
		utils.ResponseError(c, http.StatusNotFound, "页面不存在")
		return
	}

	utils.ResponseOK(c, gin.H{"valid": true})
}

// Login 管理员登录，需同时提供入口口令、用户名和密码
func Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	token, user, err := services.Login(req.EntryToken, req.Username, req.Password)
	if err != nil {
		// 登录失败时上下文里还没有管理员身份，操作者会记为来源 IP
		audit.Warnf(c, types.ActionAdminLoginFailed, "管理员登录失败，用户名 %q，原因: %v",
			req.Username, err)

		if errors.Is(err, services.ErrEntryClosed) {
			utils.ResponseError(c, http.StatusNotFound, "页面不存在")
			return
		}
		utils.ResponseError(c, http.StatusUnauthorized, err.Error())
		return
	}

	audit.Infof(c, types.ActionAdminLogin, "管理员 %s 登录成功", user.Username)

	utils.ResponseOK(c, gin.H{
		"token":       token,
		"username":    user.Username,
		"role":        user.Role,
		"permission":  utils.PermissionsForRole(user.Role),
		"permissions": utils.GetPermissionNames(utils.PermissionsForRole(user.Role)),
	})
}

// GetCurrentIdentity 返回当前访问者的身份与权限。
// 未登录来源一律为 Users 组，以来源 IP 作为标识。
func GetCurrentIdentity(c *gin.Context) {
	role, _ := middlewares.GetRoleFromContext(c)
	permission, _ := middlewares.GetPermissionsFromContext(c)

	data := gin.H{
		"role":        role,
		"permission":  permission,
		"permissions": utils.GetPermissionNames(permission),
	}

	if role == types.RoleAdmin {
		if username, ok := c.Get(middlewares.UsernameKey); ok {
			data["username"] = username
		}
	} else {
		// Users 组的识别标识就是来源 IP
		data["ip"] = middlewares.GetClientIPFromContext(c)
	}

	utils.ResponseOK(c, data)
}

// ChangePassword 管理员修改自己的密码
func ChangePassword(c *gin.Context) {
	userID, ok := middlewares.GetUserIDFromContext(c)
	if !ok {
		utils.ResponseError(c, http.StatusUnauthorized, "需要管理员身份")
		return
	}

	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	if len(req.NewPassword) < 8 {
		utils.ResponseError(c, http.StatusBadRequest, "新密码长度不能少于 8 位")
		return
	}

	if err := services.ChangeAdminPassword(userID, req.OldPassword, req.NewPassword); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	audit.Info(c, types.ActionAdminPasswordChanged, "管理员修改了自己的密码")

	utils.ResponseSuccessWithCustomMessage(c, "密码修改成功")
}
