package services

import (
	"crypto/subtle"
	"errors"
	"time"

	"bookfinder-backend/config"
	"bookfinder-backend/models"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"

	"github.com/golang-jwt/jwt/v5"
)

// TokenTTL 管理员令牌有效期
const TokenTTL = 7 * 24 * time.Hour

// ErrEntryClosed 入口口令未配置，管理员登录入口关闭
var ErrEntryClosed = errors.New("管理员登录入口未开放")

// ErrInvalidCredentials 用户名或密码错误。
// 不区分「用户不存在」与「密码错误」，避免泄露管理员用户名。
var ErrInvalidCredentials = errors.New("用户名或密码错误")

// JWTClaims JWT 声明
type JWTClaims struct {
	jwt.RegisteredClaims

	UserID     int            `json:"user_id"`
	Username   string         `json:"username"`
	Role       types.UserRole `json:"role"`
	Permission int            `json:"permission"`
}

// VerifyEntryToken 校验管理员登录入口口令。
// 使用恒定时间比较，避免通过响应时间逐字符推测口令。
// 口令未配置时一律拒绝，不能让空口令变成任意口令都放行。
func VerifyEntryToken(token string) error {
	expected := config.Get().Security.AdminEntryToken
	if expected == "" {
		return ErrEntryClosed
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

// GenerateToken 生成管理员 JWT 令牌。
// 权限由角色推导，不从数据库字段读取（见 utils.PermissionsForRole）。
func GenerateToken(user *types.User) (string, error) {
	cfg := config.Get()
	if cfg.Security.JWTSecret == "" {
		return "", errors.New("JWT_SECRET 未配置，无法签发令牌")
	}

	now := time.Now()
	claims := &JWTClaims{
		UserID:     user.ID,
		Username:   user.Username,
		Role:       user.Role,
		Permission: utils.PermissionsForRole(user.Role),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.Security.JWTSecret))
}

// ValidateToken 验证 JWT 令牌
func ValidateToken(tokenString string) (*JWTClaims, error) {
	cfg := config.Get()
	if cfg.Security.JWTSecret == "" {
		return nil, errors.New("JWT_SECRET 未配置，无法校验令牌")
	}

	token, err := jwt.ParseWithClaims(
		tokenString,
		&JWTClaims{},
		func(_ *jwt.Token) (any, error) { return []byte(cfg.Security.JWTSecret), nil },
		// 限定签名算法，避免算法混淆攻击
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, errors.New("无效的令牌")
	}
	return claims, nil
}

// Login 管理员登录：先校验入口口令，再校验用户名与密码。
// 只有 admin 角色可以登录，Users 组不入库也不登录。
func Login(entryToken, username, password string) (string, *types.User, error) {
	if err := VerifyEntryToken(entryToken); err != nil {
		return "", nil, err
	}

	user, err := models.GetUserByUsername(username)
	if err != nil {
		return "", nil, ErrInvalidCredentials
	}
	if user.Role != types.RoleAdmin {
		return "", nil, ErrInvalidCredentials
	}
	if !utils.VerifyPassword(user.Password, password) {
		return "", nil, ErrInvalidCredentials
	}

	token, err := GenerateToken(user)
	if err != nil {
		return "", nil, err
	}

	return token, user, nil
}

// ChangeAdminPassword 修改管理员密码，需先校验原密码
func ChangeAdminPassword(userID int, oldPassword, newPassword string) error {
	admin, err := models.GetAdmin()
	if err != nil {
		return errors.New("管理员账户不存在")
	}
	if admin.ID != userID {
		return ErrInvalidCredentials
	}
	if !utils.VerifyPassword(admin.Password, oldPassword) {
		return errors.New("原密码错误")
	}

	hashed, err := utils.HashPassword(newPassword)
	if err != nil {
		return err
	}

	return models.UpdateAdminPassword(admin.ID, hashed)
}
