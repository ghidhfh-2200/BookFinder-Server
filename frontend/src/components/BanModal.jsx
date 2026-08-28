import { useState } from 'react'
import { Alert, Checkbox, Form, Input, Modal } from 'antd'
import { StopOutlined } from '@ant-design/icons'

// isIPv6 粗略判断是否为 IPv6 字面量。
// 仅用于决定提示文案，真正的校验在后端；IPv4-mapped 按 IPv4 处理。
const isIPv6 = (ip) => ip?.includes(':') && !/^::ffff:\d+\.\d+\.\d+\.\d+$/i.test(ip.trim())

// BanModal 手动封禁来源 IP。
//
// 网段封禁默认关闭，且两种协议的风险差别很大，故提示按协议区分：
// 一个 IPv6 的 /64 通常就是一个宽带用户，封它约等于封一个人；
// 而一个 IPv4 的 /24 背后可能是整个校园网出口，会波及大量无关的人。
//
// 自动封禁不会碰 IPv4 的 /24（网段流量异常时它只封具体设备或当前地址），
// 这里允许勾选，是因为手动封禁属于明确的人工决定。
export default function BanModal({ open, onClose, onSubmit }) {
  const [form] = Form.useForm()
  const [submitting, setSubmitting] = useState(false)
  const [banNetwork, setBanNetwork] = useState(false)
  const [ip, setIP] = useState('')

  const handleOk = async () => {
    let values
    try {
      values = await form.validateFields()
    } catch {
      return
    }

    setSubmitting(true)
    const ok = await onSubmit({
      ip: values.ip,
      reason: values.reason,
      banNetwork: values.ban_network ?? false,
    })
    setSubmitting(false)

    if (ok) {
      form.resetFields()
      setBanNetwork(false)
      setIP('')
      onClose()
    }
  }

  const handleCancel = () => {
    form.resetFields()
    setBanNetwork(false)
    setIP('')
    onClose()
  }

  const v6 = isIPv6(ip)

  return (
    <Modal
      title="封禁来源 IP"
      open={open}
      onOk={handleOk}
      onCancel={handleCancel}
      confirmLoading={submitting}
      okText="封禁"
      okButtonProps={{ danger: true, icon: <StopOutlined /> }}
      cancelText="取消"
      destroyOnHidden
    >
      <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
        <Form.Item name="ip" label="IP 地址" rules={[{ required: true, message: '请输入 IP 地址' }]}>
          <Input placeholder="支持 IPv4 与 IPv6" onChange={(e) => setIP(e.target.value)} />
        </Form.Item>

        <Form.Item
          name="reason"
          label="封禁原因"
          rules={[{ max: 255, message: '原因长度不能超过 255 个字符' }]}
        >
          <Input.TextArea rows={3} placeholder="选填" />
        </Form.Item>

        <Form.Item name="ban_network" valuePropName="checked" style={{ marginBottom: 8 }}>
          <Checkbox onChange={(e) => setBanNetwork(e.target.checked)}>
            一并封禁所属网段{v6 ? '（/64）' : '（/24）'}
          </Checkbox>
        </Form.Item>

        {banNetwork && !v6 && (
          <Alert type="warning" showIcon message="该 /24 可能是共用出口，会波及同段的其他访问者" />
        )}
      </Form>
    </Modal>
  )
}
