import { useCallback, useEffect, useRef, useState } from 'react'
import { App, Button, Card, Col, Row, Space, Statistic, Switch, Tooltip, Typography } from 'antd'
import {
  BookOutlined,
  EyeOutlined,
  ReloadOutlined,
  StopOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import PageHeader from '../../components/PageHeader'
import Spinner from '../../components/Spinner'
import { getDashboard } from '../../api/admin/dashboard'
import { PAGE_STYLE } from '../../hooks/useFillHeight'

// REFRESH_MS 自动刷新间隔。
//
// 取 30 秒：在线人数按 5 分钟窗口统计，刷得比它快得多也看不出新东西，
// 而每次刷新都是一次请求。
const REFRESH_MS = 30000

// NO_DATA 指标不可用时的占位。
// 不显示 0：那会被读成「真的没有访问」，而实际是取不到数。
const NO_DATA = '—'

export default function Dashboard() {
  const { message } = App.useApp()

  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [autoRefresh, setAutoRefresh] = useState(true)
  // 首次加载才显示整页 Spinner；自动刷新时不闪，否则每 30 秒白一次
  const loadedOnce = useRef(false)

  const fetchData = useCallback(
    async ({ quiet = false } = {}) => {
      if (!quiet) setLoading(true)
      const resp = await getDashboard()
      if (!quiet) setLoading(false)

      if (resp.code !== 200) {
        // 静默刷新失败不弹提示：面板长期开着，网络抖一下就弹一次很扰人
        if (!quiet) message.error(resp.message)
        return
      }

      loadedOnce.current = true
      setData(resp.data)
    },
    [message],
  )

  useEffect(() => {
    fetchData()
  }, [fetchData])

  useEffect(() => {
    if (!autoRefresh) return

    const timer = setInterval(() => fetchData({ quiet: true }), REFRESH_MS)
    return () => clearInterval(timer)
  }, [autoRefresh, fetchData])

  if (loading && !loadedOnce.current) {
    return <Spinner tip="加载监控数据..." />
  }

  const traffic = data?.traffic
  const bans = data?.bans

  // 人均请求数。访客数为 0 时不显示比值，避免出现除零得到的 Infinity
  const perVisitor =
    traffic?.available && traffic.visitors_today > 0
      ? (traffic.requests_today / traffic.visitors_today).toFixed(1)
      : NO_DATA

  return (
    <div style={PAGE_STYLE}>
      <PageHeader
        title="监控面板"
        extra={
          <Space wrap>
            <Space size={8}>
              <Typography.Text type="secondary">自动刷新</Typography.Text>
              <Switch checked={autoRefresh} onChange={setAutoRefresh} />
            </Space>
            <Button icon={<ReloadOutlined />} onClick={() => fetchData()}>
              刷新
            </Button>
          </Space>
        }
      />

      <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', paddingRight: 4 }}>
        <Row gutter={[16, 16]}>
          <Col xs={12} lg={6}>
            <Card variant="borderless">
              <Statistic
                title="图书馆"
                value={data?.libraries_available ? data.libraries : NO_DATA}
                prefix={<BookOutlined />}
              />
            </Card>
          </Col>

          <Col xs={12} lg={6}>
            <Card variant="borderless">
              <Tooltip title="当前被封禁的人数。一个人可能同时被封了 IP、网段与设备标识，明细见下方。">
                <Statistic title="封禁" value={bans?.subjects ?? 0} prefix={<StopOutlined />} />
              </Tooltip>
            </Card>
          </Col>

          <Col xs={12} lg={6}>
            <Card variant="borderless">
              <Tooltip title="今天来过多少人，按访问者标识去重。不含管理员，次日零点归零。">
                <Statistic
                  title="今日访问"
                  value={traffic?.available ? traffic.visitors_today : NO_DATA}
                  prefix={<EyeOutlined />}
                />
              </Tooltip>
            </Card>
          </Col>

          <Col xs={12} lg={6}>
            <Card variant="borderless">
              <Tooltip
                title={`最近 ${traffic?.online_window_minutes ?? 5} 分钟内活跃的访问者数，按访问者标识去重`}
              >
                <Statistic
                  title="当前在线"
                  value={traffic?.available ? traffic.online : NO_DATA}
                  prefix={<TeamOutlined />}
                />
              </Tooltip>
            </Card>
          </Col>
        </Row>

        <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
          <Col xs={24} lg={12}>
            <Card variant="borderless" title="封禁明细">
              <Row gutter={16}>
                <Col span={8}>
                  <Tooltip title="被封禁的精确 IP 地址数">
                    <Statistic title="IP" value={bans?.ips ?? 0} />
                  </Tooltip>
                </Col>
                <Col span={8}>
                  <Tooltip title="被封禁的网段数，段内所有地址都进不来">
                    <Statistic title="网段" value={bans?.networks ?? 0} />
                  </Tooltip>
                </Col>
                <Col span={8}>
                  <Tooltip title="标识总数，含 IP、网段、访问者令牌与设备标识">
                    <Statistic title="标识合计" value={bans?.idents ?? 0} />
                  </Tooltip>
                </Col>
              </Row>
            </Card>
          </Col>

          <Col xs={24} lg={12}>
            <Card variant="borderless" title="今日请求">
              <Row gutter={16}>
                <Col span={12}>
                  <Tooltip title="今日 API 请求数，不含静态资源与管理员操作">
                    <Statistic
                      title="请求数"
                      value={traffic?.available ? traffic.requests_today : NO_DATA}
                    />
                  </Tooltip>
                </Col>
                <Col span={12}>
                  <Tooltip title="人均请求数。正常浏览一次会发出十几个请求，这个值异常高说明有客户端在密集调用。">
                    <Statistic title="人均" value={perVisitor} />
                  </Tooltip>
                </Col>
              </Row>
            </Card>
          </Col>
        </Row>

        {!traffic?.available && (
          <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>
            访问量与在线人数存于 Redis，当前不可用，故显示为 {NO_DATA}。
          </Typography.Paragraph>
        )}
      </div>
    </div>
  )
}
