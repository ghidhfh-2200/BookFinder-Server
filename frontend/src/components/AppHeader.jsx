import { Layout, Tag, Tooltip, Typography } from 'antd'
import { BookOutlined } from '@ant-design/icons'
import { useAuth } from '../hooks/useAuth'
import { BORDER_COLOR, PRIMARY_COLOR, SURFACE_BG } from '../theme'

// AppHeader 普通访问者的顶栏。
// 这类访问者只有「图书馆」一个入口，用不着侧边栏导航，
// 故只在顶部放品牌标识与来源 IP。
export default function AppHeader() {
  const { identity } = useAuth()

  return (
    <Layout.Header
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        height: 60,
        padding: '0 24px',
        background: SURFACE_BG,
        borderBottom: `1px solid ${BORDER_COLOR}`,
      }}
    >
      <BookOutlined style={{ fontSize: 20, color: PRIMARY_COLOR }} />
      <Typography.Text strong style={{ fontSize: 16, letterSpacing: '-0.2px' }}>
        BookFinder
      </Typography.Text>

      {identity?.ip && (
        <Tooltip title="当前来源 IP，未登录访问者以此识别">
          <Tag bordered={false} style={{ marginInlineEnd: 0 }}>
            {identity.ip}
          </Tag>
        </Tooltip>
      )}
    </Layout.Header>
  )
}
