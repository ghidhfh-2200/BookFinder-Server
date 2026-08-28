package types

import "time"

// UserRole 用户角色（权限组）
type UserRole string

const (
	// RoleAdmin 管理员，全站唯一且不可转让，存储于本地 SQLite
	RoleAdmin UserRole = "admin"
	// RoleUser Users 组，即任何未登录的来源 IP，不入库
	RoleUser UserRole = "user"
)

// AdminUsername 唯一管理员的用户名，固定不变
const AdminUsername = "admin"

// User 用户模型，仅存储管理员；Users 组按来源 IP 识别，不落库。
// 权限不作为字段存储，由角色推导（见 utils.PermissionsForRole）。
type User struct {
	ID        int       `json:"id"         gorm:"primaryKey;autoIncrement"`
	Username  string    `json:"username"   gorm:"uniqueIndex;not null;size:50"`
	Password  string    `json:"-"          gorm:"not null"`
	Role      UserRole  `json:"role"       gorm:"uniqueIndex;not null;size:20"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
