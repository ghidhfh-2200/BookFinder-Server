import { Tag } from 'antd'

// StatusTag 单个信息字段的状态。取值由后端注册表接口给出，此处只负责呈现。
export default function StatusTag({ status, size = 'default' }) {
  const outdated = status === 'out-dated'

  return (
    <Tag
      bordered={false}
      color={outdated ? 'warning' : 'success'}
      style={
        size === 'small'
          ? { fontSize: 11, lineHeight: '16px', padding: '0 6px', marginInlineEnd: 0 }
          : { marginInlineEnd: 0 }
      }
    >
      {outdated ? '已过时' : '有效'}
    </Tag>
  )
}
