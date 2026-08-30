import { useMemo } from 'react'
import { Button, Descriptions, Popconfirm, Space, Table, Tooltip, Typography } from 'antd'
import { DeleteOutlined, EditOutlined, WarningOutlined } from '@ant-design/icons'
import InfoFieldCell from './InfoFieldCell'
import LibraryCardList from './LibraryCardList'
import { displayName } from '../hooks/useLibrarySchema'
import { useIsMobile } from '../hooks/useIsMobile'

// LibraryTable 图书馆列表。
// 列由字段注册表推导，前端不硬编码字段名；各操作按钮按传入的权限开关显示，真正的拦截在后端。
//
// 窄屏改用卡片列表（LibraryCardList）：字段一多，定宽的列会把表格拉到
// 六七百像素宽，横滚表格在触屏上难用。切换在这里做而不在各页面里做，
// 否则两个调用页各写一遍判断，日后必然分叉。
export default function LibraryTable(props) {
  const isMobile = useIsMobile()

  if (isMobile) {
    return <LibraryCardList {...props} />
  }

  return <DesktopTable {...props} />
}

// DesktopTable 宽屏形态：表格。信息密度是它在宽屏下的优势。
function DesktopTable({
  libraries,
  fields,
  summaryFields = [],
  loading,
  pagination,
  scrollY,
  onChangePage,
  canUpdate = false,
  canReportOutdated = false,
  onEdit,
  onDelete,
  onReportOutdated,
}) {

  // 删除按钮按行判定，依据后端逐条下发的 can_delete：
  // 管理员恒为真，普通访问者只对自己创建的记录为真。
  //
  // 不看权限位：删除权不再只由角色决定（Users 组没有 library_delete，
  // 但能删自己创建的），按位判断会让创建者永远看不到按钮。
  // onDelete 未接则不显示——那是页面还没接删除流程。
  const rowCanDelete = (record) => Boolean(onDelete) && record.can_delete === true

  // 摘要字段成列，其余收进展开行。summaryFields 来自后端（含回落规则），
  // 为空时退回全部字段——注册表尚未加载完时不该先渲染一张只有 ID 的表。
  const [columnFields, detailFields] = useMemo(() => {
    if (summaryFields.length === 0) {
      return [fields, []]
    }
    const isSummary = new Set(summaryFields)
    return [
      fields.filter((field) => isSummary.has(field.name)),
      fields.filter((field) => !isSummary.has(field.name)),
    ]
  }, [fields, summaryFields])

  // 单元格的渲染在列与详情里完全一致，故抽出来共用
  const renderCell = (field, record) => (
    <InfoFieldCell
      entry={record.info?.[field.name]}
      type={field.type}
      report={record.reports?.[field.name]}
    />
  )

  const columns = [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 64,
      fixed: 'left',
      render: (id) => <Typography.Text type="secondary">{id}</Typography.Text>,
    },
    ...columnFields.map((field) => ({
      title: displayName(field),
      key: field.name,
      ellipsis: true,
      render: (_, record) => renderCell(field, record),
    })),
  ]

  // 删除按行判定，故只要接了删除流程就留出操作列——
  // 列存在但某些行没有按钮是正常的（那些不是自己创建的）
  if (canUpdate || Boolean(onDelete) || canReportOutdated) {
    columns.push({
      title: '操作',
      key: 'action',
      width: 172,
      fixed: 'right',
      render: (_, record) => (
        <Space size={2}>
          {/* 每行一个报告入口，字段在弹窗里选。原先每个单元格挂一个图标，
              移动端极易误触，而误触会提交一次真实的报告。 */}
          {canReportOutdated && (
            <Tooltip title="报告信息过时">
              <Button
                type="text"
                size="small"
                icon={<WarningOutlined />}
                onClick={() => onReportOutdated(record)}
              />
            </Tooltip>
          )}

          {canUpdate && (
            <Tooltip title="编辑">
              <Button
                type="text"
                size="small"
                icon={<EditOutlined />}
                onClick={() => onEdit(record)}
              />
            </Tooltip>
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
              <Tooltip title="删除">
                <Button type="text" size="small" danger icon={<DeleteOutlined />} />
              </Tooltip>
            </Popconfirm>
          )}
        </Space>
      ),
    })
  }

  return (
    <Table
      rowKey="id"
      size="middle"
      loading={loading}
      columns={columns}
      dataSource={libraries}
      // 没有非摘要字段时不显示展开列：那一列点开是空的，只是白占宽度
      expandable={
        detailFields.length > 0
          ? {
              expandedRowRender: (record) => (
                <Descriptions
                  size="small"
                  column={{ xs: 1, sm: 2, lg: 3 }}
                  items={detailFields.map((field) => ({
                    key: field.name,
                    label: displayName(field),
                    children: renderCell(field, record),
                  }))}
                />
              ),
              // 展开列钉在左侧，与 ID 同侧：它是「这一行的操作」，
              // 跟着内容横向滚走会找不到
              fixed: 'left',
            }
          : undefined
      }
      // 表体内部滚动，表头固定，页面本身不产生滚动条
      scroll={{ x: 'max-content', y: scrollY }}
      pagination={{
        ...pagination,
        showSizeChanger: true,
        showTotal: (total) => `共 ${total} 条`,
        onChange: onChangePage,
      }}
    />
  )
}
