// 权限位常量，需与后端 utils/permissions.go 保持一致
export const PERMISSION_LIBRARY_READ = 1
export const PERMISSION_LIBRARY_CREATE = 1 << 1
export const PERMISSION_LIBRARY_UPDATE = 1 << 2
export const PERMISSION_LIBRARY_DELETE = 1 << 3
export const PERMISSION_LIBRARY_REPORT_OUTDATED = 1 << 4
export const PERMISSION_IP_BAN_MANAGEMENT = 1 << 5
export const PERMISSION_SYSTEM_MANAGEMENT = 1 << 6

// hasPermission 检查权限位。
// 前端隐藏无权操作只是为了界面清晰，真正的拦截在后端中间件。
export const hasPermission = (permission, target) => (permission & target) !== 0
