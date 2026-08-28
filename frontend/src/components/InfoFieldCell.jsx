import { Space, Tooltip, Typography } from 'antd'
import { CheckOutlined, CloseOutlined } from '@ant-design/icons'
import StatusTag from './StatusTag'

// renderValue 按声明类型呈现字段值
function renderValue(value, type) {
  if (value === null || value === undefined || value === '') {
    return <Typography.Text type="secondary">-</Typography.Text>
  }

  if (type === 'bool') {
    return value ? <CheckOutlined /> : <CloseOutlined style={{ color: '#a1a1aa' }} />
  }

  if (type === 'object' || type === 'array') {
    const text = JSON.stringify(value)
    if (text === '{}' || text === '[]') {
      return <Typography.Text type="secondary">-</Typography.Text>
    }
    return (
      <Typography.Text code copyable={{ text }} style={{ fontSize: 12 }}>
        {text}
      </Typography.Text>
    )
  }

  return <Typography.Text>{String(value)}</Typography.Text>
}

// InfoFieldCell 一个信息字段的单元格：值 + 该字段自己的状态。
//
// 只负责呈现，不含任何操作入口。报告过时曾经挂在这里，但每个单元格一个图标按钮
// 在移动端与相邻元素挤在一起、极易误触，而误触会提交一次真实的报告。
// 现在改为每行一个入口，字段在弹窗里显式选择（见 ReportOutdatedModal）。
export default function InfoFieldCell({ entry, type, report }) {
  const status = entry?.status
  const outdated = status === 'out-dated'

  const count = report?.count ?? 0
  const threshold = report?.threshold ?? 0

  return (
    <Space size={6} align="center" wrap={false}>
      {renderValue(entry?.value, type)}

      {outdated && <StatusTag status={status} size="small" />}

      {/* 未达阈值时显示进度：这条信息已有人质疑，读到它的人应当知道。
          纯展示，不可点击。 */}
      {!outdated && count > 0 && (
        <Tooltip title={`已有 ${count} 人报告过时，满 ${threshold} 次将标为过时`}>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {count}/{threshold}
          </Typography.Text>
        </Tooltip>
      )}
    </Space>
  )
}
