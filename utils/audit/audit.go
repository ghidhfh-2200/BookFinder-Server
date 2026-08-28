// Package audit 记录用户操作日志。
// 从请求上下文取出操作者身份，调用方只需给出操作类型与详情。
package audit

import (
	"fmt"

	"bookfinder-backend/logger"
	"bookfinder-backend/types"

	"github.com/gin-gonic/gin"
)

// 上下文键，与 middlewares 包保持一致。
// 此处重复定义而不导入 middlewares：后者依赖 models 与 services，
// 导入会形成 handlers → audit → middlewares → models 的绕路依赖。
const (
	usernameKey   = "username"
	roleKey       = "role"
	visitorKeyKey = "visitor_key"
)

// Record 记录一条操作日志。
// 操作者取自上下文：管理员为用户名，匿名访问者为来源 IP。
func Record(c *gin.Context, action, level, detail string) {
	logger.Operation(&types.OperationLog{
		User:       actor(c),
		Action:     action,
		Level:      level,
		Detail:     detail,
		IP:         c.ClientIP(),
		VisitorKey: visitorKey(c),
	})
}

// Info 记录一条常规操作
func Info(c *gin.Context, action, detail string) {
	Record(c, action, types.LevelInfo, detail)
}

// Warn 记录一条需要留意的操作，如登录失败、疑似重复报告
func Warn(c *gin.Context, action, detail string) {
	Record(c, action, types.LevelWarn, detail)
}

// Infof 按格式记录一条常规操作
func Infof(c *gin.Context, action, format string, args ...any) {
	Info(c, action, fmt.Sprintf(format, args...))
}

// Warnf 按格式记录一条需要留意的操作
func Warnf(c *gin.Context, action, format string, args ...any) {
	Warn(c, action, fmt.Sprintf(format, args...))
}

// actor 操作者标识：管理员为用户名，其余为来源 IP
func actor(c *gin.Context) string {
	if role, ok := c.Get(roleKey); ok && role == types.RoleAdmin {
		if username, ok := c.Get(usernameKey); ok {
			if name, ok := username.(string); ok && name != "" {
				return name
			}
		}
		return string(types.RoleAdmin)
	}
	return c.ClientIP()
}

// visitorKey 访问者令牌哈希，用于把同一人的操作串起来
func visitorKey(c *gin.Context) string {
	value, ok := c.Get(visitorKeyKey)
	if !ok {
		return ""
	}
	key, ok := value.(string)
	if !ok {
		return ""
	}
	return key
}
