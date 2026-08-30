import { Button, Drawer, Menu, Tag, Typography } from 'antd'
import { BookOutlined, LogoutOutlined, MenuOutlined } from '@ant-design/icons'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { useNavItems } from '../hooks/useNavItems'
import { BORDER_COLOR, PRIMARY_COLOR, SURFACE_BG, TEXT_SECONDARY } from '../theme'

// MobileNav 管理员在窄屏下的顶栏：品牌标识 + 汉堡按钮，导航收进抽屉。
//
// 侧边栏在手机上即便收起也占 64px，等于永久吃掉内容区一角，而主界面正需要
// 横向空间。抽屉平时不占位置。
//
// 只给管理员用：普通访问者只有一个页面，走 AppHeader 即可（见 App.jsx）。
// 导航项来自 useNavItems，与侧边栏同一份。
export default function MobileNav({ open, onOpen, onClose }) {
  const { identity, logout } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const items = useNavItems()

  const go = (key) => {
    navigate(key)
    // 导航后关掉抽屉：留着它盖住刚打开的页面，还得再点一次
    onClose()
  }

  const handleLogout = async () => {
    onClose()
    await logout()
    navigate('/libraries', { replace: true })
  }

  return (
    <>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          height: 56,
          padding: '0 12px',
          background: SURFACE_BG,
          borderBottom: `1px solid ${BORDER_COLOR}`,
          flexShrink: 0,
        }}
      >
        {/* 汉堡按钮放在最左：单手持握时拇指最容易够到的一侧 */}
        <Button
          type="text"
          aria-label="打开导航菜单"
          icon={<MenuOutlined style={{ fontSize: 18 }} />}
          onClick={onOpen}
          // 44px 是可靠的触摸目标下限，默认按钮高度不足
          style={{ width: 44, height: 44 }}
        />

        <BookOutlined style={{ fontSize: 18, color: PRIMARY_COLOR, flexShrink: 0 }} />
        <Typography.Text strong style={{ fontSize: 16, letterSpacing: '-0.2px' }}>
          BookFinder
        </Typography.Text>

        {/* 来源 IP 靠右，窄屏下超长则省略 */}
        {identity?.ip && (
          <Tag
            bordered={false}
            style={{
              marginInlineEnd: 0,
              marginLeft: 'auto',
              maxWidth: 140,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {identity.ip}
          </Tag>
        )}
      </div>

      <Drawer
        open={open}
        onClose={onClose}
        placement="left"
        width={260}
        closable={false}
        styles={{ body: { padding: 0, display: 'flex', flexDirection: 'column' } }}
        title={
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <BookOutlined style={{ fontSize: 18, color: PRIMARY_COLOR }} />
            <Typography.Text strong style={{ fontSize: 16 }}>
              BookFinder
            </Typography.Text>
          </div>
        }
      >
        <Menu
          mode="inline"
          selectedKeys={[location.pathname]}
          items={items}
          onClick={({ key }) => go(key)}
          style={{ borderRight: 0, flex: 1 }}
        />

        <div style={{ padding: 12, borderTop: `1px solid ${BORDER_COLOR}`, flexShrink: 0 }}>
          <Button
            type="text"
            block
            icon={<LogoutOutlined />}
            onClick={handleLogout}
            style={{ textAlign: 'left', color: TEXT_SECONDARY, height: 44 }}
          >
            退出
          </Button>
        </div>
      </Drawer>
    </>
  )
}
