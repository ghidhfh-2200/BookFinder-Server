import { Layout, Tag, Tooltip, Typography } from 'antd'
import { BookOutlined } from '@ant-design/icons'
import { useAuth } from '../hooks/useAuth'
import { useIsMobile } from '../hooks/useIsMobile'
import { BORDER_COLOR, PRIMARY_COLOR, SURFACE_BG } from '../theme'

// AppHeader 普通访问者的顶栏。
// 这类访问者只有「图书馆」一个入口，用不着侧边栏导航，
// 故只在顶部放品牌标识与来源 IP。宽窄屏共用，只收窄间距。
export default function AppHeader() {
  const { identity } = useAuth()
  const isMobile = useIsMobile()

  return (
    <Layout.Header
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: isMobile ? 8 : 12,
        height: isMobile ? 56 : 60,
        padding: isMobile ? '0 12px' : '0 24px',
        background: SURFACE_BG,
        borderBottom: `1px solid ${BORDER_COLOR}`,
        flexShrink: 0,
      }}
    >
      <BookOutlined style={{ fontSize: isMobile ? 18 : 20, color: PRIMARY_COLOR, flexShrink: 0 }} />
      <Typography.Text strong style={{ fontSize: 16, letterSpacing: '-0.2px' }}>
        BookFinder
      </Typography.Text>

      {identity?.ip && (
        <Tooltip title="当前来源 IP，未登录访问者以此识别">
          <Tag
            bordered={false}
            style={{
              marginInlineEnd: 0,
              // 窄屏推到右侧并限宽：IPv6 地址很长，会把品牌标识挤出去
              marginLeft: isMobile ? 'auto' : undefined,
              maxWidth: isMobile ? 150 : undefined,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {identity.ip}
          </Tag>
        </Tooltip>
      )}
    </Layout.Header>
  )
}
