package utils

import "bookfinder-backend/types"

// 权限位常量（位操作模型）
const (
	PermissionLibraryRead           = 1 << iota // 1   - 查询图书馆
	PermissionLibraryCreate                     // 2   - 新增图书馆
	PermissionLibraryUpdate                     // 4   - 修改图书馆
	PermissionLibraryDelete                     // 8   - 删除图书馆
	PermissionLibraryReportOutdated             // 16  - 报告某个信息字段为过时，以及撤销该报告
	PermissionIPBanManagement                   // 32  - 封禁/解封来源 IP、配置自动封禁规则
	PermissionSystemManagement                  // 64  - 系统全部关键配置，含服务启停
)

// UserPermissions 返回 Users 组（未登录来源 IP）的权限：
// 可查、可增改、可报告过时，不可删除。增改的限流由中间件层实施，不体现为权限位。
func UserPermissions() int {
	return PermissionLibraryRead |
		PermissionLibraryCreate |
		PermissionLibraryUpdate |
		PermissionLibraryReportOutdated
}

// AdminPermissions 返回 admin 组的权限：包含全部用户级权限，外加删除、IP 封禁与系统管理
func AdminPermissions() int {
	return UserPermissions() |
		PermissionLibraryDelete |
		PermissionIPBanManagement |
		PermissionSystemManagement
}

// PermissionsForRole 按角色推导权限。
// 权限不落库，只由角色推导，因此管理员身份无法通过改写数据库字段转让。
func PermissionsForRole(role types.UserRole) int {
	switch role {
	case types.RoleAdmin:
		return AdminPermissions()
	default:
		// 未登录来源 IP 一律按 Users 组处理
		return UserPermissions()
	}
}

// HasPermission 检查是否有指定权限
func HasPermission(userPermission, targetPermission int) bool {
	return userPermission&targetPermission != 0
}

// GetPermissionNames 获取权限名称列表
func GetPermissionNames(permission int) []string {
	var permissions []string

	if HasPermission(permission, PermissionLibraryRead) {
		permissions = append(permissions, "library_read")
	}
	if HasPermission(permission, PermissionLibraryCreate) {
		permissions = append(permissions, "library_create")
	}
	if HasPermission(permission, PermissionLibraryUpdate) {
		permissions = append(permissions, "library_update")
	}
	if HasPermission(permission, PermissionLibraryDelete) {
		permissions = append(permissions, "library_delete")
	}
	if HasPermission(permission, PermissionLibraryReportOutdated) {
		permissions = append(permissions, "library_report_outdated")
	}
	if HasPermission(permission, PermissionIPBanManagement) {
		permissions = append(permissions, "ip_ban_management")
	}
	if HasPermission(permission, PermissionSystemManagement) {
		permissions = append(permissions, "system_management")
	}

	return permissions
}
