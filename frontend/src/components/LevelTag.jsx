import { Tag } from 'antd'

// LEVEL_COLORS 等级配色，取值由后端给出
const LEVEL_COLORS = {
  DEBUG: 'default',
  INFO: 'blue',
  WARN: 'warning',
  ERROR: 'error',
}

// LevelTag 日志等级标签
export default function LevelTag({ level }) {
  return (
    <Tag bordered={false} color={LEVEL_COLORS[level] ?? 'default'} style={{ marginInlineEnd: 0 }}>
      {level}
    </Tag>
  )
}
