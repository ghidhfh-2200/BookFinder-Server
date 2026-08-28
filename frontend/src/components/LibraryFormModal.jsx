import { useEffect } from 'react'
import { Form, Input, InputNumber, Modal, Select, Space, Switch } from 'antd'
import { displayName, emptyValueFor } from '../hooks/useLibrarySchema'

// fieldInput 按声明类型给出对应的输入控件
function fieldInput(field) {
  switch (field.type) {
    case 'number':
      return <InputNumber style={{ width: '100%' }} placeholder={`请输入${displayName(field)}`} />
    case 'bool':
      return <Switch />
    case 'object':
    case 'array':
      return (
        <Input.TextArea
          autoSize={{ minRows: 2, maxRows: 6 }}
          placeholder={field.type === 'array' ? '如 ["a", "b"]' : '如 {"key": "value"}'}
        />
      )
    default:
      return <Input placeholder={`请输入${displayName(field)}`} />
  }
}

// parseValue 把表单值还原成后端期望的类型
function parseValue(field, raw) {
  if (raw === undefined || raw === null) {
    return emptyValueFor(field.type)
  }

  if (field.type === 'object' || field.type === 'array') {
    if (typeof raw !== 'string') return raw
    const text = raw.trim()
    if (text === '') return emptyValueFor(field.type)
    return JSON.parse(text)
  }

  if (field.type === 'number') {
    return Number(raw)
  }

  if (field.type === 'bool') {
    return Boolean(raw)
  }

  return raw
}

// formatValue 把后端值填回表单控件
function formatValue(field, value) {
  if (field.type === 'object' || field.type === 'array') {
    if (value === undefined || value === null) return ''
    const text = JSON.stringify(value)
    return text === '{}' || text === '[]' ? '' : JSON.stringify(value, null, 2)
  }
  return value
}

// LibraryFormModal 图书馆新增/编辑表单。
// 表单项由字段注册表推导；showStatus 控制是否允许直接改每个字段的状态，
// Users 组只能通过单元格上的「报告过时」变更状态。
export default function LibraryFormModal({
  open,
  library,
  fields,
  statuses = [],
  showStatus = false,
  submitting = false,
  onOk,
  onCancel,
}) {
  const [form] = Form.useForm()

  useEffect(() => {
    if (!open) return

    if (!library) {
      form.resetFields()
      return
    }

    // Info 是 { 字段名: { value, status } }，摊平成表单项
    const values = {}
    fields.forEach((field) => {
      const entry = library.info?.[field.name]
      values[field.name] = formatValue(field, entry?.value)
      values[`${field.name}__status`] = entry?.status ?? 'good'
    })
    form.setFieldsValue(values)
  }, [open, library, fields, form])

  const handleOk = async () => {
    let values
    try {
      values = await form.validateFields()
    } catch {
      return
    }

    // 组装回 { 字段名: { value, status } }
    const info = {}
    for (const field of fields) {
      let value
      try {
        value = parseValue(field, values[field.name])
      } catch {
        form.setFields([
          { name: field.name, errors: [`${displayName(field)}不是合法的 JSON`] },
        ])
        return
      }

      info[field.name] = showStatus
        ? { value, status: values[`${field.name}__status`] ?? 'good' }
        : { value }
    }

    onOk({ info })
  }

  const statusOptions = statuses.map((value) => ({
    value,
    label: value === 'out-dated' ? '已过时' : '有效',
  }))

  return (
    <Modal
      title={library ? '编辑图书馆' : '新增图书馆'}
      open={open}
      onOk={handleOk}
      onCancel={onCancel}
      confirmLoading={submitting}
      okText="保存"
      cancelText="取消"
      width={560}
      destroyOnHidden
    >
      <Form form={form} layout="vertical" style={{ marginTop: 20 }}>
        {fields.map((field) => (
          <Form.Item key={field.name} label={displayName(field)} required={field.required}>
            <Space.Compact block>
              <Form.Item
                name={field.name}
                noStyle
                valuePropName={field.type === 'bool' ? 'checked' : 'value'}
                rules={
                  field.required && field.type === 'string'
                    ? [{ required: true, whitespace: true, message: `请输入${displayName(field)}` }]
                    : undefined
                }
              >
                {fieldInput(field)}
              </Form.Item>

              {showStatus && (
                <Form.Item name={`${field.name}__status`} noStyle initialValue="good">
                  <Select options={statusOptions} style={{ width: 104 }} />
                </Form.Item>
              )}
            </Space.Compact>
          </Form.Item>
        ))}
      </Form>
    </Modal>
  )
}
