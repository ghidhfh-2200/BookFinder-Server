import { useState } from 'react'
import { Alert, Input, Typography } from 'antd'

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
    // 不用 Space 布局：它把每个子节点包进一层没有 flex 的 div，
    // 高度链在那一层就断了，输入框因此拿不到可用高度。
    // 这里直接用 flex 列，并让输入框独占剩余空间。
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 12,
        width: '100%',
        flex: 1,
        minHeight: 0,
      }}
    >
      <Typography.Text type="secondary" style={{ flexShrink: 0 }}>
        直接编辑注册表原文。格式合法时会同步到「字段列表」，保存前仍由后端校验。
      </Typography.Text>

      {error && (
        <Alert
          type="error"
          showIcon
          message="JSON 格式有误"
          description={error}
          style={{ flexShrink: 0 }}
        />
      )}

      {/* 不用 autoSize：它按内容长高、到 maxRows 就停，超出的部分既不显示
          也不滚动（字段一多就看不到后半截）。改为撑满剩余高度并自己滚动，
          内容再长也能翻到底，且编辑区高度不随输入跳动。

          root 与 textarea 分开指定：antd 可能在两者之间包一层（计数、清除按钮
          等场景），只设外层的话高度传不到真正的 textarea 上。 */}
      <Input.TextArea
        value={text}
        onChange={(e) => handleChange(e.target.value)}
        spellCheck={false}
        styles={{
          root: { flex: 1, minHeight: 240, display: 'flex' },
          textarea: {
            flex: 1,
            resize: 'none',
            overflowY: 'auto',
            fontFamily: "'SF Mono', 'Cascadia Code', Consolas, 'Courier New', monospace",
            fontSize: 13,
            lineHeight: 1.7,
          },
        }}
      />
    </div>
  )
}
