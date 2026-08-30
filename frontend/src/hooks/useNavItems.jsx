import {
  BookOutlined,
  DashboardOutlined,
  FileTextOutlined,
  ProfileOutlined,
  SettingOutlined,
  StopOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useAuth } from './useAuth'
import { PERMISSION_IP_BAN_MANAGEMENT, PERMISSION_SYSTEM_MANAGEMENT } from '../utils/permissions'

// useNavItems 管理员的导航项。宽屏的侧边栏与窄屏的抽屉共用这一份：
// 两处各写一遍，日后加页面就会漏掉一边。
//
// 只服务管理员——普通访问者只有「图书馆」一个页面，两种形态都不给他导航
// （宽屏无侧边栏，窄屏无抽屉，见 App.jsx）。非管理员时返回空数组而不是
// 那一个入口：给它加回去就会又冒出一个只有一项的菜单。
//
// 按权限过滤只为收拢界面，真正的拦截在后端（/api/admin/* 挂 AdminMiddleware）
// 与 RequireAdmin。
export function useNavItems() {
  const { isAdmin, hasPermission } = useAuth()

  if (!isAdmin) {
    return []
  }

  const items = []

  if (hasPermission(PERMISSION_SYSTEM_MANAGEMENT)) {
    items.push({ key: '/admin/dashboard', icon: <DashboardOutlined />, label: '监控面板' })
  }

  // 管理员的「图书馆管理」已含浏览能力，故不再单列公开浏览入口
  items.push({ key: '/admin/libraries', icon: <BookOutlined />, label: '图书馆管理' })

  if (hasPermission(PERMISSION_SYSTEM_MANAGEMENT)) {
    items.push({ key: '/admin/schema', icon: <ProfileOutlined />, label: '字段注册表' })
  }
  if (hasPermission(PERMISSION_IP_BAN_MANAGEMENT)) {
    items.push({ key: '/admin/bans', icon: <StopOutlined />, label: '封禁管理' })
  }
  if (hasPermission(PERMISSION_SYSTEM_MANAGEMENT)) {
    items.push(
      { key: '/admin/rate-rules', icon: <DashboardOutlined />, label: '限流规则' },
      { key: '/admin/system', icon: <SettingOutlined />, label: '系统管理' },
      { key: '/admin/logs', icon: <FileTextOutlined />, label: '日志' },
    )
  }

  items.push({ key: '/admin/profile', icon: <UserOutlined />, label: '账户' })

  return items
}
