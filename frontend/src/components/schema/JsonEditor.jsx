import { useState } from 'react'
import { Alert, Input, Space, Typography } from 'antd'

// serialize 注册表转 JSON 原文
const serialize = (fields) => JSON.stringify({ fields }, null, 2)

// JsonEditor 注册表的 JSON 源码编辑。
// 与可视化表格双向同步：本地文本合法时立即上抛解析结果，非法时只提示不上抛。
// 本地草稿只在文本无法解析成上抛结果时才保留，其余情况以 fields 为准，
// 这样表格那边的改动能直接反映过来。
export default function JsonEditor({ fields, onChange }) {
  const [draft, setDraft] = useState(null)
  const [error, setError] = useState('')

  const text = draft ?? serialize(fields)

  const handleChange = (value) => {
    let parsed
    try {
      parsed = JSON.parse(value)
    } catch (err) {
      setDraft(value)
      setError(err.message)
      return
    }

    if (!parsed || !Array.isArray(parsed.fields)) {
      setDraft(value)
      setError('顶层必须是对象，且含有 fields 数组')
      return
    }

    setError('')
    // 文本与上抛结果一致时丢掉草稿，交回 fields 托管，以便接收表格侧的改动
    setDraft(serialize(parsed.fields) === value ? null : value)
    onChange(parsed.fields)
  }

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Typography.Text type="secondary">
        直接编辑注册表原文。格式合法时会同步到「字段列表」，保存前仍由后端校验。
      </Typography.Text>

      {error && <Alert type="error" showIcon message="JSON 格式有误" description={error} />}

      <Input.TextArea
        value={text}
        onChange={(e) => handleChange(e.target.value)}
        autoSize={{ minRows: 16, maxRows: 32 }}
        spellCheck={false}
        style={{
          fontFamily: "'SF Mono', 'Cascadia Code', Consolas, 'Courier New', monospace",
          fontSize: 13,
          lineHeight: 1.7,
        }}
      />
    </Space>
  )
}
