import { Tag } from 'antd'

// STATUS_LABELS 各状态的呈现。取值由后端注册表接口给出（statuses），此处只负责显示。
//
// 未收录的取值回落到「有效」的样式但显示原文：后端加了新状态而前端还没跟上时，
// 界面上会看到那个陌生的标识符——比默默显示成「有效」好，后者会让人以为
// 这条信息已经被确认过。
const STATUS_LABELS = {
  good: { text: '有效', color: 'success' },
  'out-dated': { text: '已过时', color: 'warning' },
  unverified: { text: '未验证', color: 'default' },
}

// StatusTag 单个信息字段的状态。
export default function StatusTag({ status, size = 'default' }) {
  const { text, color } = STATUS_LABELS[status] ?? { text: status, color: 'success' }

  return (
    <Tag
      bordered={false}
      color={color}
      style={
        size === 'small'
          ? { fontSize: 11, lineHeight: '16px', padding: '0 6px', marginInlineEnd: 0 }
          : { marginInlineEnd: 0 }
      }
    >
      {text}
    </Tag>
  )
}
