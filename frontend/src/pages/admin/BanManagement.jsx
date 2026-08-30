import { useCallback, useEffect, useState } from 'react'
import { App, Button, Card, Popconfirm, Table, Tag, Tooltip, Typography } from 'antd'
import { MailOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import PageHeader from '../../components/PageHeader'
import AppealDrawer from '../../components/AppealDrawer'
import BanModal from '../../components/BanModal'
import BanIdentTags from '../../components/BanIdentTags'
import BanCardList from '../../components/BanCardList'
import { banIP, getBans, unbanSubject } from '../../api/admin/ban'
import { PAGE_STYLE, TABLE_RESERVE, useFillHeight } from '../../hooks/useFillHeight'
import { useIsMobile } from '../../hooks/useIsMobile'
import { SPACE, TOUCH_SIZE } from '../../spacing'

// BanManagement 封禁管理。
//
// 封禁挂在「主体」上而非单个 IP：同一个人可能同时持有来源 IP、所属网段、
// 访问者令牌、安卓设备标识等多个标识，任一命中即视为该主体。因此解封按主体进行，
// 只解其中一项人依然进不来。
export default function BanManagement() {
  const { message } = App.useApp()
  const [fillRef, tableHeight] = useFillHeight(TABLE_RESERVE)
  const isMobile = useIsMobile()
  const [bans, setBans] = useState([])
  // appealIP 非空时打开该 IP 的申诉抽屉
  const [appealIP, setAppealIP] = useState(null)
  const [loading, setLoading] = useState(false)
  const [banOpen, setBanOpen] = useState(false)

  const fetchBans = useCallback(async () => {
    setLoading(true)
    const resp = await getBans()
    setLoading(false)

    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }
    setBans(resp.data || [])
  }, [message])

  useEffect(() => {
    // 包在异步函数里调用，避免在 effect 体内同步 setState
    const load = async () => {
      await fetchBans()
    }
    load()
  }, [fetchBans])

  const handleBan = async ({ ip, reason, banNetwork }) => {
    const resp = await banIP(ip, reason, banNetwork)
    if (resp.code !== 200) {
      message.error(resp.message)
      return false
    }
    message.success(resp.message)
    fetchBans()
    return true
  }

  const handleUnban = async (id) => {
    const resp = await unbanSubject(id)
    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }
    message.success(resp.message)
    fetchBans()
  }

  const banColumns = [
    {
      title: '封禁标识',
      key: 'idents',
      width: 320,
      // 一行列出该主体的全部标识：网段显著标注，免得误以为只封了一个地址
      render: (_, record) => <BanIdentTags idents={record.idents} />,
    },
    {
      title: '来源',
      dataIndex: 'source',
      width: 90,
      render: (source) =>
        source === 'auto' ? (
          <Tag bordered={false} color="volcano">
            自动
          </Tag>
        ) : (
          <Tag bordered={false}>手动</Tag>
        ),
    },
    {
      title: '原因',
      dataIndex: 'reason',
      width: 180,
      ellipsis: true,
      render: (reason) => reason || '-',
    },
    {
      title: '触发详情',
      dataIndex: 'detail',
      ellipsis: true,
      // 自动封禁记下触发时的具体数据，便于复核误判
      render: (detail) =>
        detail ? (
          <Tooltip title={detail}>
            <Typography.Text type="secondary" style={{ fontSize: 13 }}>
              {detail}
            </Typography.Text>
          </Tooltip>
        ) : (
          '-'
        ),
    },
    {
      title: '封禁时间',
      dataIndex: 'created_at',
      width: 180,
      render: (value) => (value ? new Date(value).toLocaleString() : '-'),
    },
    {
      title: '申诉',
      key: 'appeals',
      width: 130,
      render: (_, record) => {
        const total = record.appeals?.total ?? 0
        const pending = record.appeals?.pending ?? 0
        const ip = record.ips?.[0]

        if (total === 0 || !ip) {
          return <Typography.Text type="secondary">无</Typography.Text>
        }

        return (
          <Button
            type="link"
            size="small"
            onClick={() => setAppealIP(ip)}
            icon={<MailOutlined />}
          >
            {pending > 0 ? `${total} 次（${pending} 待处理）` : `${total} 次`}
          </Button>
        )
      },
    },
    {
      title: '操作',
      key: 'action',
      width: 90,
      render: (_, record) => (
        // 解封会一并解除该主体的全部标识，故确认文案把范围说清楚
        <Popconfirm
          title="确认解封？"
          description={`将解除该主体的全部 ${record.idents?.length ?? 0} 个标识`}
          okText="解封"
          cancelText="取消"
          onConfirm={() => handleUnban(record.id)}
        >
          <Button type="link" size="small">
            解封
          </Button>
        </Popconfirm>
      ),
    },
  ]

  return (
    <div style={PAGE_STYLE}>
      <PageHeader
        title="封禁管理"
        extra={
          // 窄屏两个按钮等分一行，不用 Space wrap（那会把第二个甩到下一行）
          <div style={{ display: 'flex', gap: SPACE.sm, width: isMobile ? '100%' : 'auto' }}>
            <Button
              icon={<ReloadOutlined />}
              onClick={fetchBans}
              style={isMobile ? { flex: 1, height: TOUCH_SIZE } : undefined}
            >
              刷新
            </Button>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => setBanOpen(true)}
              style={isMobile ? { flex: 1, height: TOUCH_SIZE } : undefined}
            >
              封禁 IP
            </Button>
          </div>
        }
      />

      <div
        ref={fillRef}
        style={{ flex: 1, minHeight: 0, overflowY: isMobile ? 'auto' : 'visible' }}
      >
        {/* 窄屏改卡片：七列合计约 990px，横滚近三屏没法用。
            卡片列表自己纵向排布，不套外层白底——那是卡片套卡片 */}
        {isMobile ? (
          <BanCardList
            bans={bans}
            loading={loading}
            onUnban={handleUnban}
            onOpenAppeal={setAppealIP}
          />
        ) : (
          <Card variant="borderless" styles={{ body: { padding: 0 } }}>
            <Table
              rowKey="id"
              size="middle"
              loading={loading}
              columns={banColumns}
              dataSource={bans}
              scroll={{ x: 'max-content', y: tableHeight }}
              pagination={{ showTotal: (total) => `共 ${total} 条` }}
              locale={{ emptyText: '暂无封禁记录' }}
            />
          </Card>
        )}
      </div>

      <BanModal open={banOpen} onClose={() => setBanOpen(false)} onSubmit={handleBan} />

      <AppealDrawer
        open={appealIP != null}
        ip={appealIP}
        onClose={() => setAppealIP(null)}
        onReviewed={(status) => {
          // 受理会一并解封，该记录已不在封禁列表中
          if (status === 'accepted') {
            setAppealIP(null)
          }
          fetchBans()
        }}
      />
    </div>
  )
}
