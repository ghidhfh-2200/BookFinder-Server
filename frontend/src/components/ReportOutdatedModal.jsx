import { Alert, Button, List, Modal, Space, Tag, Typography } from 'antd'
import { CheckOutlined, UndoOutlined, WarningOutlined } from '@ant-design/icons'
import { displayName } from '../hooks/useLibrarySchema'

// renderPreview 字段当前值的简短预览，帮报告者确认自己选的是哪一条
function renderPreview(value, type) {
  if (value === null || value === undefined || value === '') {
    return <Typography.Text type="secondary">-</Typography.Text>
  }

  if (type === 'bool') {
    return <Typography.Text>{value ? '是' : '否'}</Typography.Text>
  }

  const text = type === 'object' || type === 'array' ? JSON.stringify(value) : String(value)
  if (text === '{}' || text === '[]') {
    return <Typography.Text type="secondary">-</Typography.Text>
  }

  return (
    <Typography.Text ellipsis={{ tooltip: text }} style={{ maxWidth: 320 }}>
      {text}
    </Typography.Text>
  )
}

// ReportOutdatedModal 逐字段报告信息过时，并确认未验证的网站。
//
// 报告入口从单元格挪到这里：原先每个单元格都挂一个图标按钮，在移动端与相邻
// 元素挤在一起，很容易误触——而误触的后果是提交一次真实的报告，且报告按人
// 去重，撤销要再点一次。现在每行只有一个入口，字段在弹窗里显式选择，
// 每一项都有独立的确认按钮和足够的点击区域。
//
// 未验证的网站在这里也能确认可用：那同样是「对某个字段的一次表态」，
// 与报告过时是同一类动作，另开一个弹窗只会让人多找一次入口。
export default function ReportOutdatedModal({
  open,
  library,
  fields,
  onReport,
  onRevoke,
  onVerify,
  onRevokeVerify,
  onCancel,
}) {
  // 无记录时不渲染内容：关闭动画期间 library 会先变成 null
  if (!library) {
    return <Modal open={open} onCancel={onCancel} footer={null} />
  }

  return (
    <Modal
      open={open}
      title="报告信息过时"
      onCancel={onCancel}
      footer={<Button onClick={onCancel}>关闭</Button>}
      // 窄屏给足宽度：这里每行都是「字段名 + 当前值 + 操作」，挤在一起反而难点
      width={560}
      styles={{ body: { maxHeight: '60vh', overflowY: 'auto' } }}
    >
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="选择信息已经过时的字段。同一字段被足够多人报告后会自动标记。未验证的网站可在此确认可用。"
      />

      <List
        size="small"
        dataSource={fields}
        renderItem={(field) => {
          const entry = library.info?.[field.name]
          const report = library.reports?.[field.name]

          const outdated = entry?.status === 'out-dated'
          const unverified = entry?.status === 'unverified'
          const count = report?.count ?? 0
          const threshold = report?.threshold ?? 0
          const reported = report?.reported ?? false
          const suspected = report?.suspected ?? false
          const verifyCount = report?.verify_count ?? 0
          const verifyThreshold = report?.verify_threshold ?? 0
          const verified = report?.verified ?? false

          // 未验证的网站多一个「确认可用」入口。报告过时的入口照常保留：
          // 未转正期间地址也可能本就是错的，两条路都得通
          const actions = []

          if (unverified) {
            actions.push(
              verified ? (
                <Button
                  size="small"
                  icon={<UndoOutlined />}
                  onClick={() => onRevokeVerify(field.name)}
                >
                  撤销确认
                </Button>
              ) : (
                <Button size="small" icon={<CheckOutlined />} onClick={() => onVerify(field.name)}>
                  确认可用
                </Button>
              ),
            )
          }

          actions.push(
            outdated ? (
              <Tag bordered={false} color="orange">
                已标记过时
              </Tag>
            ) : reported ? (
              <Button size="small" icon={<UndoOutlined />} onClick={() => onRevoke(field.name)}>
                撤销
              </Button>
            ) : (
              <Button
                size="small"
                danger
                icon={<WarningOutlined />}
                onClick={() => onReport(field.name)}
              >
                报告
              </Button>
            ),
          )

          return (
            <List.Item key={field.name} actions={actions}>
              <List.Item.Meta
                title={
                  <Space size={6} wrap>
                    <Typography.Text>{displayName(field)}</Typography.Text>

                    {unverified && (
                      <Tag bordered={false} color="default">
                        未验证
                      </Tag>
                    )}

                    {/* 转正进度：让确认的人看到自己那一票已计入 */}
                    {unverified && verifyThreshold > 0 && (
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        已确认 {verifyCount}/{verifyThreshold}
                      </Typography.Text>
                    )}

                    {/* 未达阈值时显示进度，让报告的人看到自己那一票已计入 */}
                    {!outdated && count > 0 && (
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        过时 {count}/{threshold}
                      </Typography.Text>
                    )}

                    {/* 同来源已报过但不是自己提交的：提前告知，免得点完才知道没计数 */}
                    {!outdated && !reported && suspected && (
                      <Typography.Text type="warning" style={{ fontSize: 12 }}>
                        疑似重复，可能不计数
                      </Typography.Text>
                    )}
                  </Space>
                }
                description={renderPreview(entry?.value, field.type)}
              />
            </List.Item>
          )
        }}
      />
    </Modal>
  )
}
