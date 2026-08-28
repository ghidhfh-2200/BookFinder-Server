import { Typography } from 'antd'

// PageHeader 页面头部：标题、说明与右侧操作区。
// 各管理页共用，保证标题层级与间距一致。
export default function PageHeader({ title, description, extra }) {
  return (
    <div className="page-header">
      <div className="page-header__heading">
        <Typography.Title level={3} style={{ margin: 0, fontWeight: 600 }}>
          {title}
        </Typography.Title>
        {description && (
          <Typography.Text type="secondary" style={{ display: 'block', marginTop: 4 }}>
            {description}
          </Typography.Text>
        )}
      </div>

      {extra && <div className="page-header__actions">{extra}</div>}
    </div>
  )
}
