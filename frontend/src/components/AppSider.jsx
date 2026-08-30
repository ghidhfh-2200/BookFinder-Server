import { useState } from 'react'
import { Button, Layout, Menu, Tag, Tooltip, Typography } from 'antd'
import {
  BookOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
} from '@ant-design/icons'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { useNavItems } from '../hooks/useNavItems'
import { BORDER_COLOR, PRIMARY_COLOR, SURFACE_BG, TEXT_SECONDARY } from '../theme'

// AppSider 侧边栏导航。全站导航集中在此。
// 管理员的「图书馆管理」已包含浏览能力，故不再显示公开浏览入口。
export default function AppSider() {
  const { isAdmin, identity, logout } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [collapsed, setCollapsed] = useState(false)

  // 导航项与移动端抽屉共用，见 useNavItems
  const items = useNavItems()

  const handleLogout = async () => {
    await logout()
    navigate('/libraries', { replace: true })
  }

  return (
    <Layout.Sider
      breakpoint="lg"
      collapsed={collapsed}
      onCollapse={setCollapsed}
      width={232}
      collapsedWidth={64}
      theme="light"
      // 不用内置的 collapsible：它在底部铺一条整宽的触发条，与整体观感不搭。
      // 改为品牌栏右侧的图标按钮，见下方 trigger。
      trigger={null}
      style={{
        background: SURFACE_BG,
        borderRight: `1px solid ${BORDER_COLOR}`,
        // 滚动交给内部的菜单区，Sider 自身不滚
        overflow: 'hidden',
        height: '100vh',
      }}
    >
      <div className="app-sider__inner">
        <div
          className="app-sider__brand"
          style={collapsed ? { justifyContent: 'center', padding: 0, height: 48 } : undefined}
        >
          <BookOutlined style={{ fontSize: 20, color: PRIMARY_COLOR, flexShrink: 0 }} />

          {!collapsed && (
            <>
              <Typography.Text strong style={{ fontSize: 16, letterSpacing: '-0.2px' }}>
                BookFinder
              </Typography.Text>
              <Tooltip title="收起侧边栏">
                <Button
                  type="text"
                  size="small"
                  icon={<MenuFoldOutlined />}
                  onClick={() => setCollapsed(true)}
                  style={{ marginLeft: 'auto', color: TEXT_SECONDARY }}
                />
              </Tooltip>
            </>
          )}
        </div>

        {/* 收起后品牌栏放不下按钮，单独占一行居中 */}
        {collapsed && (
          <Tooltip title="展开侧边栏" placement="right">
            <Button
              type="text"
              size="small"
              icon={<MenuUnfoldOutlined />}
              onClick={() => setCollapsed(false)}
              style={{ margin: '0 auto 4px', color: TEXT_SECONDARY, flexShrink: 0 }}
            />
          </Tooltip>
        )}

        <div className="app-sider__menu">
          <Menu
            mode="inline"
            selectedKeys={[location.pathname]}
            items={items}
            onClick={({ key }) => navigate(key)}
            style={{ background: 'transparent', borderRight: 0 }}
          />
        </div>

        <div className="app-sider__footer">
          {isAdmin ? (
            <Tooltip title={collapsed ? '退出' : ''} placement="right">
              <Button
                type="text"
                block
                icon={<LogoutOutlined />}
                onClick={handleLogout}
                style={{ textAlign: 'left', color: TEXT_SECONDARY }}
              >
                {collapsed ? '' : '退出'}
              </Button>
            </Tooltip>
          ) : (
            identity?.ip &&
            !collapsed && (
              <Tooltip title="当前来源 IP，未登录访问者以此识别">
                <Tag
                  bordered={false}
                  style={{ maxWidth: '100%', overflow: 'hidden', textOverflow: 'ellipsis' }}
                >
                  {identity.ip}
                </Tag>
              </Tooltip>
            )
          )}
        </div>
      </div>
    </Layout.Sider>
  )
}
