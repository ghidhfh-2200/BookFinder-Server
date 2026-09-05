import { Button, Card, Empty, List, Popconfirm, Space, Spin, Typography } from 'antd'
import { DeleteOutlined, EditOutlined, WarningOutlined } from '@ant-design/icons'
import InfoFieldCell from './InfoFieldCell'
import { displayName } from '../hooks/useLibrarySchema'
import { CARD_GAP, CARD_PADDING, SPACE, TOUCH_SIZE } from '../spacing'
import { BORDER_COLOR } from '../theme'

// LibraryCardList 图书馆列表的窄屏形态：一条记录一张卡片。
//
// 手机上不用表格：字段数由注册表决定，每列定宽 150px 的话四五个字段就要
// 横滚六七百像素，而横滚表格在触屏上既难定位也容易与页面滚动打架。
// 卡片纵向排布，一屏之内读完一条记录，不产生横向滚动。
//
// 桌面端仍走 LibraryTable：宽屏下表格的信息密度是它的优势。
export default function LibraryCardList({
  libraries,
  fields,
  summaryFields = [],
  loading,
  pagination,
  onChangePage,
  canUpdate = false,
  canReportOutdated = false,
  onEdit,
  onDelete,
  onReportOutdated,
}) {
  // 与表格同一套判定：管理员恒为真，普通访问者只对自己创建的记录为真
  const rowCanDelete = (record) => Boolean(onDelete) && record.can_delete === true

  // 卡片标题用记录名（summaryFields 的第一项，后端已含回落规则），
  // 其余字段列在卡片内。不像表格那样区分摘要与详情：
  // 卡片是纵向的，多几行不挤，藏起来反而要多点一次
  const titleField = fields.find((field) => field.name === summaryFields[0]) ?? fields[0]
  const bodyFields = fields.filter((field) => field.name !== titleField?.name)

  if (loading) {
    return (
      <div style={{ padding: SPACE.xxl, textAlign: 'center' }}>
        <Spin />
      </div>
    )
  }

  if (libraries.length === 0) {
    return <Empty description="没有记录" style={{ padding: SPACE.xxl }} />
  }

  return (
    <List
      dataSource={libraries}
      // 分页放在列表下方，简单模式：窄屏放不下页码加跳转框加条数选择
      pagination={{
        ...pagination,
        simple: true,
        align: 'center',
        onChange: onChangePage,
      }}
      // 不额外加左右留白：内容区的 padding 已经收好边，
      // 这里再加一层会让卡片的外距大过内距，看着往里缩
      renderItem={(record) => (
        <Card
          size="small"
          style={{ marginBottom: CARD_GAP }}
          // 标题区与内容区取同一个横向留白：antd 两者默认值不同，
          // 不指定的话标题会比下面的字段往外突出一截
          styles={{
            header: { padding: `${SPACE.md}px ${CARD_PADDING}px`, minHeight: 0 },
            body: { padding: CARD_PADDING },
          }}
          title={
            <Space size={6} align="center" wrap={false} style={{ width: '100%' }}>
              <Typography.Text type="secondary" style={{ fontSize: 12, flexShrink: 0 }}>
                #{record.id}
              </Typography.Text>
              {titleField && (
                <div style={{ minWidth: 0, flex: 1 }}>
                  <InfoFieldCell
                    entry={record.info?.[titleField.name]}
                    type={titleField.type}
                    report={record.reports?.[titleField.name]}
                  />
                </div>
              )}
            </Space>
          }
        >
          {/* 字段纵向列出：标签在上、值在下，比左右两列更耐窄屏，
              值长了也不会把标签挤成竖排文字 */}
          {bodyFields.map((field, index) => (
            <div
              key={field.name}
              // 最后一项不留下边距：否则它与操作区的分隔线之间会多出一段空白，
              // 显得分隔线偏下
              style={{ marginBottom: index === bodyFields.length - 1 ? 0 : SPACE.md }}
            >
              <Typography.Text
                type="secondary"
                style={{ fontSize: 12, display: 'block', marginBottom: 2 }}
              >
                {displayName(field)}
              </Typography.Text>
              <InfoFieldCell
                entry={record.info?.[field.name]}
                type={field.type}
                report={record.reports?.[field.name]}
              />
            </div>
          ))}

          {(canUpdate || canReportOutdated || rowCanDelete(record)) && (
            <div
              style={{
                display: 'flex',
                gap: SPACE.sm,
                // 分隔线上下等距，不再 8/4 不对称
                paddingTop: CARD_PADDING,
                marginTop: CARD_PADDING,
                borderTop: `1px solid ${BORDER_COLOR}`,
              }}
            >
              {/* 按钮带文字而非只有图标：手机上纯图标按钮猜不出作用，
                  而这三个动作后果各不相同（尤其删除不可恢复） */}
              {canReportOutdated && (
                <Button
                  icon={<WarningOutlined />}
                  onClick={() => onReportOutdated(record)}
                  style={{ height: TOUCH_SIZE, flex: 1 }}
                >
                  反馈
                </Button>
              )}

              {canUpdate && (
                <Button
                  icon={<EditOutlined />}
                  onClick={() => onEdit(record)}
                  style={{ height: TOUCH_SIZE, flex: 1 }}
                >
                  编辑
                </Button>
              )}

              {rowCanDelete(record) && (
                <Popconfirm
                  title="确认删除这个图书馆？"
                  description="删除后无法恢复。"
                  okText="删除"
                  okButtonProps={{ danger: true }}
                  cancelText="取消"
                  onConfirm={() => onDelete(record.id)}
                >
                  <Button
                    danger
                    icon={<DeleteOutlined />}
                    style={{ height: TOUCH_SIZE, flex: 1 }}
                  >
                    删除
                  </Button>
                </Popconfirm>
              )}
            </div>
          )}
        </Card>
      )}
    />
  )
}
