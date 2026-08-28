package utils

import (
	"testing"

	"bookfinder-backend/types"
)

// TestUserPermissions 验证 Users 组可查、可增改、可报告过时，但不可删除、不可管 IP 与系统
func TestUserPermissions(t *testing.T) {
	p := UserPermissions()

	allowed := map[string]int{
		"read":            PermissionLibraryRead,
		"create":          PermissionLibraryCreate,
		"update":          PermissionLibraryUpdate,
		"report_outdated": PermissionLibraryReportOutdated,
	}
	for name, perm := range allowed {
		if !HasPermission(p, perm) {
			t.Errorf("Users 组应具备 %s 权限", name)
		}
	}

	denied := map[string]int{
		"delete": PermissionLibraryDelete,
		"ip_ban": PermissionIPBanManagement,
		"system": PermissionSystemManagement,
	}
	for name, perm := range denied {
		if HasPermission(p, perm) {
			t.Errorf("Users 组不应具备 %s 权限", name)
		}
	}
}

// TestAdminPermissionsIncludeAllUserPermissions 验证管理员拥有一切用户级权限，外加删除/封禁/系统管理
func TestAdminPermissionsIncludeAllUserPermissions(t *testing.T) {
	admin := AdminPermissions()

	if admin&UserPermissions() != UserPermissions() {
		t.Error("管理员应拥有一切用户级权限")
	}

	for name, perm := range map[string]int{
		"delete": PermissionLibraryDelete,
		"ip_ban": PermissionIPBanManagement,
		"system": PermissionSystemManagement,
	} {
		if !HasPermission(admin, perm) {
			t.Errorf("管理员应具备 %s 权限", name)
		}
	}
}

// TestPermissionsForRole 验证角色到权限的映射，未知角色回落到 Users 组
func TestPermissionsForRole(t *testing.T) {
	tests := []struct {
		role types.UserRole
		want int
	}{
		{types.RoleAdmin, AdminPermissions()},
		{types.RoleUser, UserPermissions()},
		{types.UserRole("unknown"), UserPermissions()},
		{types.UserRole(""), UserPermissions()},
	}

	for _, tt := range tests {
		if got := PermissionsForRole(tt.role); got != tt.want {
			t.Errorf("PermissionsForRole(%q) = %d, want %d", tt.role, got, tt.want)
		}
	}
}
