import { useEffect } from 'react'
import { Form, Input, InputNumber, Modal, Select, Space, Switch } from 'antd'
import { displayName, emptyValueFor } from '../hooks/useLibrarySchema'

// STATUS_LABELS 状态的中文名，取值本身来自后端
const STATUS_LABELS = {
  good: '有效',
  'out-dated': '已过时',
  unverified: '未验证',
}

// websiteRule 网站字段的校验规则，与后端 checker.ValidateWebsiteURL 同一套判据。
//
// 前端先校验只为即时反馈，真正的拦截在后端——两处规则若有出入，
// 表现是「这里通过了、提交后被拒」，故此处刻意只做同样的四项检查。
// 空值放行：网站不是必填项。
const websiteRule = {
  validator: (_rule, value) => {
    const text = (value ?? '').trim()
    if (text === '') return Promise.resolve()

    let parsed
    try {
      parsed = new URL(text)
    } catch {
      return Promise.reject(new Error('请输入完整网址，以 http:// 或 https:// 开头'))
    }

    const scheme = parsed.protocol.replace(':', '').toLowerCase()
    if (scheme !== 'http' && scheme !== 'https') {
      return Promise.reject(new Error(`不支持 ${scheme} 协议，只允许 http 与 https`))
    }
    if (!parsed.hostname) {
      return Promise.reject(new Error('缺少域名'))
    }
    if (parsed.hostname !== 'localhost' && !parsed.hostname.includes('.')) {
      return Promise.reject(new Error('域名不完整'))
    }

    return Promise.resolve()
  },
}

// fieldRules 该字段的校验规则。必填与网站格式可以同时生效，故按需拼接。
function fieldRules(field) {
  const rules = []

  if (field.required && field.type === 'string') {
    rules.push({ required: true, whitespace: true, message: `请输入${displayName(field)}` })
  }
  // 按角色识别网站字段，不硬编码字段名——角色由后端下发（见 useLibrarySchema）
  if (field.role === 'website') {
    rules.push(websiteRule)
  }

  return rules.length > 0 ? rules : undefined
}

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

  // 状态取值来自后端注册表接口，这里只把它翻成人话；
  // 未收录的取值直接显示原文，免得新状态被默默标成「有效」
  const statusOptions = statuses.map((value) => ({
    value,
    label: STATUS_LABELS[value] ?? value,
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
                rules={fieldRules(field)}
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
