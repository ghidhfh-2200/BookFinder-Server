import { useCallback, useEffect, useState } from 'react'
import {
  App,
  Button,
  Card,
  Drawer,
  Empty,
  Input,
  Popconfirm,
  Space,
  Spin,
  Tag,
  Typography,
} from 'antd'
import { CheckOutlined, CloseOutlined } from '@ant-design/icons'
import { getAppealsByIP, reviewAppeal } from '../api/appeal'
import { formatTime } from '../utils/logActions'

// STATUS_META 申诉状态的呈现方式
const STATUS_META = {
  pending: { color: 'processing', label: '待处理' },
  accepted: { color: 'success', label: '已受理' },
  rejected: { color: 'default', label: '已驳回' },
}

// AppealDrawer 查看某个 IP 的申诉详情并处理。
// 申诉内容是纯文本，用 Typography.Paragraph 渲染，不解析 HTML 或 Markdown。
export default function AppealDrawer({ open, ip, onClose, onReviewed }) {
  const { message } = App.useApp()
  const [appeals, setAppeals] = useState([])
  const [maxAppeals, setMaxAppeals] = useState(3)
  const [loading, setLoading] = useState(false)
  const [notes, setNotes] = useState({})
  const [submitting, setSubmitting] = useState(null)

  const fetchAppeals = useCallback(async () => {
    if (!ip) return

    setLoading(true)
    const resp = await getAppealsByIP(ip)
    setLoading(false)

    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }

    setAppeals(resp.data.appeals || [])
    setMaxAppeals(resp.data.max ?? 3)
  }, [ip, message])

  useEffect(() => {
    if (!open) return

    const load = async () => {
      await fetchAppeals()
    }
    load()
  }, [open, fetchAppeals])

  const handleReview = async (id, status) => {
    setSubmitting(id)
    const resp = await reviewAppeal(id, status, notes[id] ?? '')
    setSubmitting(null)

    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }

    message.success(resp.message)
    await fetchAppeals()
    // 受理会一并解封，需刷新封禁列表
    onReviewed?.(status)
  }

  return (
    <Drawer
      title={
        <Space size={8}>
          <span>申诉详情</span>
          <Typography.Text code>{ip}</Typography.Text>
        </Space>
      }
      open={open}
      onClose={onClose}
      width={560}
      destroyOnHidden
    >
      {loading ? (
        <div style={{ textAlign: 'center', padding: '48px 0' }}>
          <Spin />
        </div>
      ) : appeals.length === 0 ? (
        <Empty description="该来源尚未提交申诉" />
      ) : (
        <Space direction="vertical" size={12} style={{ width: '100%', display: 'flex' }}>
          <Typography.Text type="secondary">
            共 {appeals.length} / {maxAppeals} 次申诉
          </Typography.Text>

          {appeals.map((appeal) => {
            const meta = STATUS_META[appeal.status] ?? STATUS_META.pending
            const pending = appeal.status === 'pending'

            return (
              <Card
                key={appeal.id}
                size="small"
                title={
                  <Space size={8}>
                    <Typography.Text strong>第 {appeal.attempt} 次</Typography.Text>
                    <Tag bordered={false} color={meta.color}>
                      {meta.label}
                    </Tag>
                  </Space>
                }
                extra={
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {formatTime(appeal.created_at)}
                  </Typography.Text>
                }
              >
                {/* 纯文本渲染，保留换行；内容不参与任何解析 */}
                <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', marginBottom: 12 }}>
                  {appeal.message}
                </Typography.Paragraph>

                {appeal.ban_reason && (
                  <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 12 }}>
                    申诉时的封禁原因：{appeal.ban_reason}
                  </Typography.Paragraph>
                )}

                {appeal.admin_note && (
                  <Typography.Paragraph
                    type="secondary"
                    style={{ whiteSpace: 'pre-wrap', fontSize: 13, marginBottom: 12 }}
                  >
                    处理备注：{appeal.admin_note}
                  </Typography.Paragraph>
                )}

                {pending && (
                  <Space direction="vertical" size={8} style={{ width: '100%' }}>
                    <Input.TextArea
                      placeholder="处理备注（选填）"
                      maxLength={500}
                      autoSize={{ minRows: 2, maxRows: 4 }}
                      value={notes[appeal.id] ?? ''}
                      onChange={(e) => setNotes({ ...notes, [appeal.id]: e.target.value })}
                    />

                    <Space>
                      <Popconfirm
                        title="受理该申诉？"
                        description="受理后会立即解封该来源。"
                        okText="受理并解封"
                        cancelText="取消"
                        onConfirm={() => handleReview(appeal.id, 'accepted')}
                      >
                        <Button
                          type="primary"
                          size="small"
                          icon={<CheckOutlined />}
                          loading={submitting === appeal.id}
                        >
                          受理并解封
                        </Button>
                      </Popconfirm>

                      <Button
                        size="small"
                        icon={<CloseOutlined />}
                        loading={submitting === appeal.id}
                        onClick={() => handleReview(appeal.id, 'rejected')}
                      >
                        驳回
                      </Button>
                    </Space>
                  </Space>
                )}
              </Card>
            )
          })}
        </Space>
      )}
    </Drawer>
  )
}
