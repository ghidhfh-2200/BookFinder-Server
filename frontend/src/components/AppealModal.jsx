import { useState } from 'react'
import { Alert, App, Input, Modal, Space, Typography } from 'antd'
import { submitAppeal } from '../api/appeal'

// MAX_LENGTH 与后端 types.MaxAppealMessageLength 保持一致
const MAX_LENGTH = 500

// AppealModal 提交封禁申诉。
// 内容为纯文本：后端剥离控制字符后入库，管理端按文本渲染，不做任何解析。
export default function AppealModal({ open, quota, onCancel, onSubmitted }) {
  const { message: toast } = App.useApp()
  const [text, setText] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const remaining = quota?.remaining ?? 0

  const handleOk = async () => {
    const trimmed = text.trim()
    if (!trimmed) {
      toast.warning('请填写申诉内容')
      return
    }

    setSubmitting(true)
    const resp = await submitAppeal(trimmed)
    setSubmitting(false)

    if (resp.code !== 200) {
      toast.error(resp.message)
      return
    }

    toast.success(`申诉已提交（第 ${resp.data.attempt} 次），请等待管理员处理`)
    setText('')
    onSubmitted(resp.data)
  }

  return (
    <Modal
      title="提交申诉"
      open={open}
      onOk={handleOk}
      onCancel={onCancel}
      confirmLoading={submitting}
      okText="提交"
      cancelText="取消"
      width={520}
      destroyOnHidden
    >
      <Space direction="vertical" size={12} style={{ width: '100%', marginTop: 16 }}>
        <Alert
          type="info"
          showIcon
          message={`本次为第 ${(quota?.used ?? 0) + 1} 次申诉，最多可提交 ${quota?.max ?? 3} 次`}
          description="请说明你的使用情况。内容仅管理员可见，提交后无法撤回或修改。"
        />

        <Input.TextArea
          value={text}
          onChange={(e) => setText(e.target.value)}
          maxLength={MAX_LENGTH}
          showCount
          autoSize={{ minRows: 5, maxRows: 10 }}
          placeholder="例如：我是正常浏览的用户，可能因为刷新过快被误判。"
        />

        {remaining <= 1 && (
          <Typography.Text type="warning">
            这是你最后一次申诉机会，提交后将无法再次申诉。
          </Typography.Text>
        )}
      </Space>
    </Modal>
  )
}
