import { Card, Typography } from 'antd'

// AuthLayout 认证类页面的居中容器
export default function AuthLayout({ title, subtitle, children }) {
  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 24,
      }}
    >
      <Card style={{ width: '100%', maxWidth: 380 }}>
        <Typography.Title level={3} style={{ marginTop: 0, marginBottom: subtitle ? 4 : 24 }}>
          {title}
        </Typography.Title>
        {subtitle && (
          <Typography.Paragraph type="secondary" style={{ marginBottom: 24 }}>
            {subtitle}
          </Typography.Paragraph>
        )}
        {children}
      </Card>
    </div>
  )
}
