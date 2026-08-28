import { useState } from 'react'
import { Button, Input, Popconfirm, Select, Space, Switch, Table, Tag, Tooltip } from 'antd'
import { DeleteOutlined, HolderOutlined, LockOutlined } from '@ant-design/icons'

// TYPE_LABELS 类型的中文说明，取值本身来自后端
const TYPE_LABELS = {
  string: '文本',
  number: '数字',
  bool: '布尔',
  object: '对象',
  array: '数组',
}

// FieldTable 注册表的可视化编辑表格。
// 字段名是标识符，只能增删不能改，故已存在的字段名只读；显示名与类型可改。
export default function FieldTable({
  fields,
  types,
  searchNameField,
  savedNames,
  onChange,
  onRemove,
  onMove,
}) {
  // dragIndex 正在拖动的行，dropIndex 悬停目标，用于画插入位置提示线
  const [dragIndex, setDragIndex] = useState(null)
  const [dropIndex, setDropIndex] = useState(null)

  const update = (index, patch) => {
    const next = fields.map((field, i) => (i === index ? { ...field, ...patch } : field))
    onChange(next)
  }

  // 用 HTML5 原生拖放实现重排，不引入拖拽库：这里只是单列表纵向换位，原生 API 足够。
  // 只有手柄能发起拖拽（draggable 由 handleProps 挂到手柄上），
  // 整行 draggable 会让行内输入框无法选中文字。
  const row = ({ 'data-row-key': rowKey, ...props }) => {
    // rowKey 即行序号（见下方 rowKey 定义）。取不到序号时按普通行渲染，
    // 不做拖放处理——表头等非数据行也会走到这里。
    const index = Number(rowKey)
    if (!Number.isInteger(index)) {
      return <tr {...props} />
    }

    const isDropTarget = dropIndex === index && dragIndex !== null && dragIndex !== index
    // 提示线画在上边还是下边，取决于从哪个方向拖来
    const edge = dragIndex !== null && dragIndex < index ? 'borderBottom' : 'borderTop'

    return (
      <tr
        {...props}
        onDragOver={(e) => {
          if (dragIndex === null) return
          e.preventDefault()
          e.dataTransfer.dropEffect = 'move'
          setDropIndex(index)
        }}
        onDrop={(e) => {
          if (dragIndex === null) return
          e.preventDefault()
          if (dragIndex !== index) {
            onMove(dragIndex, index)
          }
          setDragIndex(null)
          setDropIndex(null)
        }}
        style={{
          ...props.style,
          opacity: dragIndex === index ? 0.4 : 1,
          ...(isDropTarget ? { [edge]: '2px solid #2563eb' } : null),
        }}
      />
    )
  }

  // handleProps 拖拽手柄的事件，挂在手柄图标上
  const handleProps = (index) => ({
    draggable: true,
    onDragStart: (e) => {
      setDragIndex(index)
      // Firefox 不设置 dataTransfer 就不触发拖动
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', String(index))
    },
    onDragEnd: () => {
      setDragIndex(null)
      setDropIndex(null)
    },
  })

  const columns = [
    {
      title: '',
      key: 'drag',
      width: 44,
      align: 'center',
      render: (_, record, index) => (
        <Tooltip title="按住拖动调整顺序">
          <HolderOutlined
            {...handleProps(index)}
            style={{ color: '#a1a1aa', cursor: 'grab', padding: 4 }}
          />
        </Tooltip>
      ),
    },
    {
      title: '字段名',
      dataIndex: 'name',
      width: 200,
      render: (name, record, index) => {
        // 已保存的字段名是标识符，不可改；新增的行还没落库，允许填写
        const locked = savedNames.includes(name)

        return (
          <Space size={4}>
            <Input
              value={name}
              disabled={locked}
              placeholder="英文标识符"
              onChange={(e) => update(index, { name: e.target.value })}
              style={{ width: locked ? 132 : 176 }}
            />
            {locked && (
              <Tooltip title="字段名是标识符，只能增删不能改。改名请删除后新增，该字段的历史数据会一并清除。">
                <LockOutlined style={{ color: '#a1a1aa' }} />
              </Tooltip>
            )}
          </Space>
        )
      },
    },
    {
      title: '显示名',
      dataIndex: 'label',
      width: 160,
      render: (label, record, index) => (
        <Input
          value={label}
          placeholder={record.name || '留空则用字段名'}
          onChange={(e) => update(index, { label: e.target.value })}
        />
      ),
    },
    {
      title: '类型',
      dataIndex: 'type',
      width: 120,
      render: (type, record, index) => (
        <Select
          value={type}
          // 记录名固定为文本类型
          disabled={record.role === 'searchname'}
          onChange={(value) => update(index, { type: value })}
          style={{ width: '100%' }}
          options={types.map((value) => ({ value, label: TYPE_LABELS[value] ?? value }))}
        />
      ),
    },
    {
      title: '必填',
      dataIndex: 'required',
      width: 72,
      align: 'center',
      render: (required, record, index) => (
        <Switch
          size="small"
          checked={required}
          // 记录名必须必填
          disabled={record.role === 'searchname'}
          onChange={(checked) => update(index, { required: checked })}
        />
      ),
    },
    {
      title: (
        <Tooltip title="勾选的字段作为列显示在图书馆列表中，其余收进每行的「详情」里。只影响显示，不影响数据。">
          摘要
        </Tooltip>
      ),
      dataIndex: 'summary',
      width: 72,
      align: 'center',
      render: (summary, record, index) => (
        <Switch
          size="small"
          checked={summary}
          // 记录名是搜索匹配的字段，藏进详情会让表格只剩 ID
          disabled={record.role === 'searchname'}
          onChange={(checked) => update(index, { summary: checked })}
        />
      ),
    },
    {
      title: '角色',
      dataIndex: 'role',
      width: 110,
      render: (role) =>
        role === 'searchname' ? (
          <Tooltip title="图书馆的记录名，关键字搜索匹配的就是它。不可删除。">
            <Tag bordered={false} color="blue">
              记录名
            </Tag>
          </Tooltip>
        ) : (
          <span style={{ color: '#a1a1aa' }}>-</span>
        ),
    },
    {
      title: '',
      key: 'action',
      width: 52,
      render: (_, record, index) => {
        // 记录名承担搜索职责，不可删除
        if (record.name === searchNameField) {
          return null
        }

        const saved = savedNames.includes(record.name)

        return (
          <Popconfirm
            title="删除这个字段？"
            description={
              saved
                ? '保存后，所有图书馆记录中该字段的数据会被一并清除。'
                : '该字段尚未保存，可直接移除。'
            }
            okText="删除"
            okButtonProps={{ danger: true }}
            cancelText="取消"
            onConfirm={() => onRemove(index)}
          >
            <Tooltip title="删除字段">
              <Button type="text" size="small" danger icon={<DeleteOutlined />} />
            </Tooltip>
          </Popconfirm>
        )
      },
    },
  ]

  return (
    <Table
      // 用行序号作 key，不能带字段名：字段名正在被编辑，把它放进 key 会让
      // 每敲一个字符 key 就变一次，React 据此卸载旧行、挂载新行，
      // 输入框随之失去焦点——新增字段时一次只能输入一个字符。
      //
      // 序号在编辑期间不变，故输入过程稳定。拖拽重排会改变序号，那正是我们
      // 希望 React 重新渲染那些行的时刻。
      rowKey={(record, index) => String(index)}
      size="middle"
      columns={columns}
      dataSource={fields}
      pagination={false}
      scroll={{ x: 'max-content' }}
      components={{ body: { row } }}
    />
  )
}
