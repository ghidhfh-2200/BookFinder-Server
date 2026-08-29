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

// ROLE_LABELS 角色的中文名与说明。角色取值来自后端（role_fields / reserved_fields），
// 这里只负责把它显示成人话；未收录的角色回落到显示原始标识符。
const ROLE_LABELS = {
  searchname: { text: '记录名', hint: '图书馆的记录名，关键字搜索匹配的就是它' },
  website: { text: '网站', hint: '图书馆的网站地址，客户端据此提供打开链接的入口' },
}

// FieldTable 注册表的可视化编辑表格。
// 字段名是标识符，只能增删不能改，故已存在的字段名只读；显示名与类型可改。
//
// 内置字段（reservedFields）另有锁定：不可删除、类型不可改，必填与摘要
// 是否锁定各字段不同。锁定项由后端给出，前端不按角色自己推断。
export default function FieldTable({
  fields,
  types,
  reservedFields = [],
  savedNames,
  onChange,
  onRemove,
  onMove,
}) {
  // reservedBy 字段名到内置声明的映射，渲染时按行查表
  const reservedBy = new Map(reservedFields.map((field) => [field.name, field]))
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
  //
  // 拖放事件经 onRow 挂到行上，而不是用 components.body.row 换掉行组件。
  // 那种写法必须在组件内定义一个 row 函数（它要闭包 dragIndex 等状态），
  // 于是每次渲染都是一个新的函数引用——React 按引用判断组件类型，
  // 引用变了就把整行卸载重建，行内输入框每敲一个字符就失去焦点。
  // onRow 返回的是一组 props 而非组件类型，逐次变化只会更新属性。
  const onRow = (_record, index) => {
    const isDropTarget = dropIndex === index && dragIndex !== null && dragIndex !== index
    // 提示线画在上边还是下边，取决于从哪个方向拖来
    const edge = dragIndex !== null && dragIndex < index ? 'borderBottom' : 'borderTop'

    return {
      onDragOver: (e) => {
        if (dragIndex === null) return
        e.preventDefault()
        e.dataTransfer.dropEffect = 'move'
        setDropIndex(index)
      },
      onDrop: (e) => {
        if (dragIndex === null) return
        e.preventDefault()
        if (dragIndex !== index) {
          onMove(dragIndex, index)
        }
        setDragIndex(null)
        setDropIndex(null)
      },
      style: {
        opacity: dragIndex === index ? 0.4 : 1,
        ...(isDropTarget ? { [edge]: '2px solid #2563eb' } : null),
      },
    }
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
          // 内置字段的类型锁定：后端按角色读它的值，换了类型就读不出来
          disabled={reservedBy.has(record.name)}
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
          disabled={reservedBy.get(record.name)?.lock_required ?? false}
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
          // 记录名是搜索匹配的字段，藏进详情会让表格只剩 ID（由后端的锁定项决定）
          disabled={reservedBy.get(record.name)?.lock_summary ?? false}
          onChange={(checked) => update(index, { summary: checked })}
        />
      ),
    },
    {
      title: '角色',
      dataIndex: 'role',
      width: 110,
      // 角色由字段名决定，不是一项可改的设置，故只显示不可编辑
      render: (role) => {
        if (!role) {
          return <span style={{ color: '#a1a1aa' }}>-</span>
        }

        const { text, hint } = ROLE_LABELS[role] ?? { text: role, hint: '' }

        return (
          <Tooltip title={hint ? `${hint}。该字段不可删除，类型不可改。` : ''}>
            <Tag bordered={false} color="blue">
              {text}
            </Tag>
          </Tooltip>
        )
      },
    },
    {
      title: '',
      key: 'action',
      width: 52,
      render: (_, record, index) => {
        // 内置字段承担着按角色定位的职责，删掉会让对应功能失去着落
        if (reservedBy.has(record.name)) {
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
      onRow={onRow}
    />
  )
}
