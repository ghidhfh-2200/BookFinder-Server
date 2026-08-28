import { useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { Alert, Button, Form, Input, message } from 'antd'
import { LockOutlined, UserOutlined } from '@ant-design/icons'
import AuthLayout from './AuthLayout'
import { useAuth } from '../../hooks/useAuth'

// Login 管理员登录表单。
// entryToken 由 AdminEntry 校验通过后传入，登录时需随用户名密码一并提交。
export default function Login({ entryToken }) {
  const { isAdmin, login } = useAuth()
  const navigate = useNavigate()
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  if (isAdmin) {
    return <Navigate to="/admin/libraries" replace />
  }

  const handleSubmit = async ({ username, password }) => {
    setSubmitting(true)
    setError('')
    const resp = await login(entryToken, username, password)
    setSubmitting(false)

    if (resp.code !== 200) {
      setError(resp.message)
      return
    }

    message.success('登录成功')
    navigate('/admin/libraries', { replace: true })
  }

  return (
    <AuthLayout title="管理员登录">
      {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}

      <Form layout="vertical" onFinish={handleSubmit} requiredMark={false}>
        <Form.Item
          name="username"
          label="用户名"
          rules={[{ required: true, message: '请输入用户名' }]}
        >
          <Input prefix={<UserOutlined />} placeholder="用户名" autoComplete="username" />
        </Form.Item>

        <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
          <Input.Password
            prefix={<LockOutlined />}
            placeholder="密码"
            autoComplete="current-password"
          />
        </Form.Item>

        <Form.Item style={{ marginBottom: 0 }}>
          <Button type="primary" htmlType="submit" block loading={submitting}>
            登录
          </Button>
        </Form.Item>
      </Form>
    </AuthLayout>
  )
}
