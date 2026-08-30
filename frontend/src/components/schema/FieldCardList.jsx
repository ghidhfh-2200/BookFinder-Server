import { useState } from 'react'
import { Button, List, Popconfirm, Space, Tag, Typography } from 'antd'
import {
  DeleteOutlined,
  DownOutlined,
  EditOutlined,
  LockOutlined,
  UpOutlined,
} from '@ant-design/icons'
import FieldEditModal from './FieldEditModal'
import { SPACE, TOUCH_SIZE } from '../../spacing'

// ROLE_LABELS 角色的中文名
const ROLE_LABELS = {
  searchname: '记录名',
  website: '网站',
}

// FieldCardList 字段注册表的窄屏编辑形态。
//
// 外层每个字段只占一行，显示显示名（连同角色与必填等标记）；
// 六个编辑项收进 FieldEditModal，点「编辑」打开。
//
// 为什么不平铺：注册表每个字段有字段名、显示名、类型、必填、摘要、删除六项，
// 全列在行内会让每一行都很拥挤，而多数时候只是想看一眼有哪些字段、调一下顺序。
//
// 排序用上移/下移而非拖拽：拖拽在触屏上会与页面滚动争抢手势。
export default function FieldCardList({
  fields,
  types,
  reservedFields = [],
  savedNames,
  onChange,
  onRemove,
  onMove,
}) {
  // editing 正在编辑的行号。存序号而非对象：字段内容会随编辑变化，
  // 存对象的话弹窗里显示的是打开那一刻的旧值
  const [editing, setEditing] = useState(null)
  // editingName 打开弹窗那一刻的字段名，用于判定锁定状态。
  //
  // 不实时用 field.name 判：新字段在输入过程中一旦敲成内置字段名
  // （比如打完 "WebSite"），输入框会当场禁用，人就被卡在半截名字上出不来。
  // 锁定该由这一行原本的身份决定，而不是此刻输入到哪个字符。
  const [editingName, setEditingName] = useState('')

  const openEditor = (index) => {
    setEditingName(fields[index]?.name ?? '')
    setEditing(index)
  }

  const reservedBy = new Map(reservedFields.map((field) => [field.name, field]))

  const update = (index, patch) => {
    onChange(fields.map((field, i) => (i === index ? { ...field, ...patch } : field)))
  }

  const current = editing == null ? null : fields[editing]

  return (
    <>
      <List
        size="small"
        dataSource={fields}
        pagination={false}
        // 用行序号作 key：字段名正在被编辑，放进 key 会让每敲一个字符就重建组件
        renderItem={(field, index) => {
          const reserved = reservedBy.get(field.name)
          const saved = savedNames.includes(field.name)

          return (
            <List.Item key={index} style={{ padding: `${SPACE.md}px 0` }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: SPACE.sm, width: '100%' }}>
                {/* 显示名为主，字段名作为副标题——前者是界面上实际看到的 */}
                <div style={{ minWidth: 0, flex: 1 }}>
                  <Space size={6} wrap>
                    <Typography.Text strong>
                      {field.label || field.name || '(未命名)'}
                    </Typography.Text>

                    {reserved && (
                      <Tag bordered={false} color="blue" style={{ marginInlineEnd: 0 }}>
                        {ROLE_LABELS[reserved.role] ?? reserved.role}
                      </Tag>
                    )}
                    {field.required && (
                      <Tag bordered={false} color="orange" style={{ marginInlineEnd: 0 }}>
                        必填
                      </Tag>
                    )}
                    {field.summary && (
                      <Tag bordered={false} style={{ marginInlineEnd: 0 }}>
                        摘要
                      </Tag>
                    )}
                  </Space>

                  <Typography.Text
                    type="secondary"
                    style={{ fontSize: 12, display: 'block', marginTop: 2 }}
                  >
                    {field.name || '未填字段名'}
                    {saved && <LockOutlined style={{ marginLeft: 4 }} />}
                  </Typography.Text>
                </div>

                <Space size={4} style={{ flexShrink: 0 }}>
                  <Button
                    icon={<UpOutlined />}
                    disabled={index === 0}
                    onClick={() => onMove(index, index - 1)}
                    aria-label="上移"
                    style={{ width: TOUCH_SIZE, height: TOUCH_SIZE }}
                  />
                  <Button
                    icon={<DownOutlined />}
                    disabled={index === fields.length - 1}
                    onClick={() => onMove(index, index + 1)}
                    aria-label="下移"
                    style={{ width: TOUCH_SIZE, height: TOUCH_SIZE }}
                  />
                  <Button
                    icon={<EditOutlined />}
                    onClick={() => openEditor(index)}
                    aria-label="编辑"
                    style={{ width: TOUCH_SIZE, height: TOUCH_SIZE }}
                  />

                  {/* 内置字段不可删除，故不给按钮 */}
                  {!reserved && (
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
                      <Button
                        danger
                        icon={<DeleteOutlined />}
                        aria-label="删除"
                        style={{ width: TOUCH_SIZE, height: TOUCH_SIZE }}
                      />
                    </Popconfirm>
                  )}
                </Space>
              </div>
            </List.Item>
          )
        }}
      />

      {/* reserved 与 nameLocked 都按 editingName（打开时的字段名）判，
          见上方 editingName 的说明 */}
      <FieldEditModal
        open={editing != null}
        field={current}
        reserved={reservedBy.get(editingName)}
        nameLocked={savedNames.includes(editingName)}
        types={types}
        onChange={(patch) => update(editing, patch)}
        onClose={() => setEditing(null)}
      />
    </>
  )
}
