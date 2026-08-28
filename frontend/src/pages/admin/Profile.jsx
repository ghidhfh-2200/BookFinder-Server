import { useState } from 'react'
import { Button, Card, Descriptions, Form, Input, Space, Tag, Typography, message } from 'antd'
import { useAuth } from '../../hooks/useAuth'
import { changePassword } from '../../api/auth'

// Profile 管理员账户信息与修改密码。
// 用户名与角色固定不可改，管理员身份不可转让。
export default function Profile() {
  const { identity } = useAuth()
  const [submitting, setSubmitting] = useState(false)
  const [form] = Form.useForm()

  const handleSubmit = async ({ old_password, new_password }) => {
    setSubmitting(true)
    const resp = await changePassword(old_password, new_password)
    setSubmitting(false)

    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }

    message.success(resp.message)
    form.resetFields()
  }

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Card
        title={
          <Typography.Title level={4} style={{ margin: 0 }}>
            账户信息
          </Typography.Title>
        }
      >
        <Descriptions column={1} size="small">
          <Descriptions.Item label="用户名">{identity?.username ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="角色">
            <Tag color="blue">管理员</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="权限">
            <Space size={4} wrap>
              {(identity?.permissions ?? []).map((name) => (
                <Tag key={name}>{name}</Tag>
              ))}
            </Space>
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Card
        title={
          <Typography.Title level={4} style={{ margin: 0 }}>
            修改密码
          </Typography.Title>
        }
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
          requiredMark={false}
          style={{ maxWidth: 360 }}
        >
          <Form.Item
            name="old_password"
            label="原密码"
            rules={[{ required: true, message: '请输入原密码' }]}
          >
            <Input.Password placeholder="原密码" autoComplete="current-password" />
          </Form.Item>

          <Form.Item
            name="new_password"
            label="新密码"
            rules={[
              { required: true, message: '请输入新密码' },
              { min: 8, message: '新密码长度不能少于 8 位' },
            ]}
          >
            <Input.Password placeholder="至少 8 位" autoComplete="new-password" />
          </Form.Item>

          <Form.Item
            name="confirm_password"
            label="确认新密码"
            dependencies={['new_password']}
            rules={[
              { required: true, message: '请再次输入新密码' },
              ({ getFieldValue }) => ({
                validator: (_, value) =>
                  !value || getFieldValue('new_password') === value
                    ? Promise.resolve()
                    : Promise.reject(new Error('两次输入的密码不一致')),
              }),
            ]}
          >
            <Input.Password placeholder="再次输入新密码" autoComplete="new-password" />
          </Form.Item>

          <Form.Item style={{ marginBottom: 0 }}>
            <Button type="primary" htmlType="submit" loading={submitting}>
              保存
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </Space>
  )
}
