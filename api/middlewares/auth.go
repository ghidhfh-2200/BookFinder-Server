package middlewares

import (
	"net/http"
	"strings"

	"bookfinder-backend/models"
	"bookfinder-backend/services"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"

	"github.com/gin-gonic/gin"
)

// 上下文键常量
const (
	UserIDKey      = "user_id"
	UsernameKey    = "username"
	RoleKey        = "role"
	PermissionsKey = "permission"
	ClientIPKey    = "client_ip"
)

// IdentityMiddleware 识别来源身份，不拦截请求。
// 携带有效令牌的按 admin 处理，其余（含无令牌、令牌失效）一律按 Users 组处理，
// Users 组以来源 IP 作为识别标识，不入库。
func IdentityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ClientIPKey, c.ClientIP())

		// 默认身份：Users 组
		c.Set(RoleKey, types.RoleUser)
		c.Set(PermissionsKey, utils.UserPermissions())

		claims, err := parseBearerToken(c)
		if err != nil || claims.Role != types.RoleAdmin {
			c.Next()
			return
		}

		// 令牌有效，确认管理员仍存在于本地库
		admin, err := models.GetAdmin()
		if err != nil || admin.ID != claims.UserID {
			c.Next()
			return
		}

		c.Set(UserIDKey, admin.ID)
		c.Set(UsernameKey, admin.Username)
		c.Set(RoleKey, types.RoleAdmin)
		c.Set(PermissionsKey, utils.PermissionsForRole(types.RoleAdmin))

		c.Next()
	}
}

// AdminMiddleware 要求当前身份为管理员
func AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if role, ok := GetRoleFromContext(c); !ok || role != types.RoleAdmin {
			utils.ResponseError(c, http.StatusUnauthorized, "需要管理员身份")
			c.Abort()
			return
		}
		c.Next()
	}
}

// PermissionMiddleware 校验具体权限位
func PermissionMiddleware(required int) gin.HandlerFunc {
	return func(c *gin.Context) {
		permission, ok := GetPermissionsFromContext(c)
		if !ok || !utils.HasPermission(permission, required) {
			utils.ResponseError(c, http.StatusForbidden, "权限不足")
			c.Abort()
			return
		}
		c.Next()
	}
}

// parseBearerToken 从 Authorization 头解析并校验令牌
func parseBearerToken(c *gin.Context) (*services.JWTClaims, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return nil, errNoToken
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return nil, errNoToken
	}

	return services.ValidateToken(parts[1])
}

// errNoToken 请求未携带可解析的令牌
var errNoToken = errNoTokenType{}

type errNoTokenType struct{}

func (errNoTokenType) Error() string { return "缺少令牌" }

// ========== 上下文工具函数 ==========

// GetRoleFromContext 从上下文获取角色
func GetRoleFromContext(c *gin.Context) (types.UserRole, bool) {
	role, ok := c.Get(RoleKey)
	if !ok {
		return "", false
	}
	r, ok := role.(types.UserRole)
	return r, ok
}

// GetPermissionsFromContext 从上下文获取权限位
func GetPermissionsFromContext(c *gin.Context) (int, bool) {
	permission, ok := c.Get(PermissionsKey)
	if !ok {
		return 0, false
	}
	p, ok := permission.(int)
	return p, ok
}

// GetUserIDFromContext 从上下文获取用户 ID（仅管理员存在）
func GetUserIDFromContext(c *gin.Context) (int, bool) {
	userID, ok := c.Get(UserIDKey)
	if !ok {
		return 0, false
	}
	id, ok := userID.(int)
	return id, ok
}

// GetClientIPFromContext 从上下文获取来源 IP
func GetClientIPFromContext(c *gin.Context) string {
	if ip, ok := c.Get(ClientIPKey); ok {
		if s, ok := ip.(string); ok {
			return s
		}
	}
	return c.ClientIP()
}

// IsAdmin 判断当前身份是否为管理员
func IsAdmin(c *gin.Context) bool {
	role, ok := GetRoleFromContext(c)
	return ok && role == types.RoleAdmin
}
