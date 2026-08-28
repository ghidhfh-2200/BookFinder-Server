import { useCallback, useEffect, useState } from 'react'
import { Button, Descriptions, Space, Typography } from 'antd'
import { CloseOutlined, MailOutlined } from '@ant-design/icons'
import AppealModal from '../components/AppealModal'
import { getAppealQuota } from '../api/appeal'
import { formatTime } from '../utils/logActions'

// Banned 封禁提示页。
// 被封禁的来源访问任何接口都会收到 403，此页替代正常内容。
// 申诉接口对被封者开放，故此处可提交申诉。
export default function Banned({ ban }) {
  const [quota, setQuota] = useState(null)
  const [modalOpen, setModalOpen] = useState(false)

  const fetchQuota = useCallback(async () => {
    const resp = await getAppealQuota()
    if (resp.code === 200) {
      setQuota(resp.data)
    }
  }, [])

  useEffect(() => {
    const load = async () => {
      await fetchQuota()
    }
    load()
  }, [fetchQuota])

  const exhausted = quota != null && quota.remaining <= 0

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        minHeight: '100vh',
        padding: 24,
        background: '#f6f7f9',
        textAlign: 'center',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: 108,
          height: 108,
          borderRadius: '50%',
          background: '#fff1f0',
          border: '2px solid #ffccc7',
        }}
      >
        <CloseOutlined style={{ fontSize: 56, color: '#cf1322' }} />
      </div>

      <Typography.Title level={2} style={{ marginTop: 28, marginBottom: 8, color: '#cf1322' }}>
        你已被封禁
      </Typography.Title>

      <Typography.Paragraph type="secondary" style={{ maxWidth: 420, marginBottom: 28 }}>
        当前来源已被限制访问本站。如认为是误判，请联系管理员并附上下方信息。
      </Typography.Paragraph>

      <Descriptions
        bordered
        column={1}
        size="small"
        style={{ maxWidth: 560, width: '100%', textAlign: 'left', background: '#ffffff' }}
      >
        <Descriptions.Item label="封禁原因">{ban?.reason || '未说明'}</Descriptions.Item>

        {ban?.detail && <Descriptions.Item label="触发详情">{ban.detail}</Descriptions.Item>}

        <Descriptions.Item label="封禁方式">
          {ban?.source === 'auto' ? '系统自动封禁' : '管理员手动封禁'}
        </Descriptions.Item>

        {ban?.created_at && (
          <Descriptions.Item label="封禁时间">{formatTime(ban.created_at)}</Descriptions.Item>
        )}

        {ban?.ip && (
          <Descriptions.Item label="来源">
            <Typography.Text code>{ban.ip}</Typography.Text>
          </Descriptions.Item>
        )}
      </Descriptions>

      <Space direction="vertical" size={8} align="center" style={{ marginTop: 28 }}>
        <Button
          type="primary"
          danger
          icon={<MailOutlined />}
          disabled={exhausted}
          onClick={() => setModalOpen(true)}
        >
          {exhausted ? '申诉次数已用完' : '提交申诉'}
        </Button>

        {quota != null && (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {exhausted
              ? `已提交 ${quota.used} 次申诉，请等待管理员处理`
              : `已提交 ${quota.used} / ${quota.max} 次，还可申诉 ${quota.remaining} 次`}
          </Typography.Text>
        )}
      </Space>

      <Typography.Text type="secondary" style={{ fontSize: 12, marginTop: 28 }}>
        BookFinder ©{new Date().getFullYear()}
      </Typography.Text>

      <AppealModal
        open={modalOpen}
        quota={quota}
        onCancel={() => setModalOpen(false)}
        onSubmitted={(data) => {
          setModalOpen(false)
          setQuota({ used: data.attempt, max: data.max, remaining: data.remaining })
        }}
      />
    </div>
  )
}
