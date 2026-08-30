import { Input, Modal, Select, Switch, Tag, Tooltip, Typography } from 'antd'
import { LockOutlined } from '@ant-design/icons'
import { useModalWidth } from '../../hooks/useIsMobile'
import { SPACE } from '../../spacing'

// TYPE_LABELS 类型的中文说明，取值本身来自后端
const TYPE_LABELS = {
  string: '文本',
  number: '数字',
  bool: '布尔',
  object: '对象',
  array: '数组',
}

// ROLE_LABELS 角色的中文名与说明
const ROLE_LABELS = {
  searchname: { text: '记录名', hint: '图书馆的记录名，关键字搜索匹配的就是它' },
  website: { text: '网站', hint: '图书馆的网站地址，客户端据此提供打开链接的入口' },
}

// Row 一个设置项：标签在上、控件在下。
// 纵向排布比左右两列更耐窄屏，值长了也不会把标签挤成竖排文字。
function Row({ label, hint, children }) {
  return (
    <div style={{ marginBottom: SPACE.lg }}>
      <Typography.Text
        type="secondary"
        style={{ fontSize: 12, display: 'block', marginBottom: 4 }}
      >
        {label}
        {hint}
      </Typography.Text>
      {children}
    </div>
  )
}

// FieldEditModal 单个字段的详细编辑。
//
// 注册表的编辑项有六个（字段名、显示名、类型、必填、摘要、删除），
// 平铺在列表里每行都很拥挤——窄屏尤其明显。故外层只列显示名，
// 详细编辑收进这里，一次只专注一个字段。
//
// 改动直接落到草稿（与桌面端表格一致）：整份注册表在按下「保存」前都是草稿，
// 故此处不需要「取消」语义，页面级的「重置」负责回退。
export default function FieldEditModal({
  open,
  field,
  reserved,
  nameLocked,
  types,
  onChange,
  onClose,
}) {
  const modalWidth = useModalWidth(480)

  // 关闭动画期间 field 会先变成 null
  if (!field) {
    return <Modal open={open} onCancel={onClose} footer={null} width={modalWidth} />
  }

  const role = reserved ? (ROLE_LABELS[reserved.role] ?? { text: reserved.role, hint: '' }) : null

  return (
    <Modal
      open={open}
      title={field.label || field.name || '新字段'}
      onCancel={onClose}
      onOk={onClose}
      okText="完成"
      cancelButtonProps={{ style: { display: 'none' } }}
      width={modalWidth}
      destroyOnHidden
    >
      <div style={{ marginTop: SPACE.lg }}>
        {role && (
          <Tooltip title={`${role.hint}。该字段不可删除，类型不可改。`}>
            <Tag bordered={false} color="blue" style={{ marginBottom: SPACE.lg }}>
              {role.text}
            </Tag>
          </Tooltip>
        )}

        <Row
          label="字段名"
          hint={
            nameLocked && (
              <Tooltip title="字段名是标识符，只能增删不能改。改名请删除后新增，该字段的历史数据会一并清除。">
                <LockOutlined style={{ color: '#a1a1aa', marginLeft: 4 }} />
              </Tooltip>
            )
          }
        >
          <Input
            value={field.name}
            disabled={nameLocked}
            placeholder="英文标识符"
            onChange={(e) => onChange({ name: e.target.value })}
          />
        </Row>

        <Row label="显示名">
          <Input
            value={field.label}
            placeholder={field.name || '留空则用字段名'}
            onChange={(e) => onChange({ label: e.target.value })}
          />
        </Row>

        <Row label="类型">
          <Select
            value={field.type}
            // 内置字段的类型锁定：后端按角色读它的值，换了类型就读不出来
            disabled={Boolean(reserved)}
            onChange={(value) => onChange({ type: value })}
            style={{ width: '100%' }}
            options={types.map((value) => ({ value, label: TYPE_LABELS[value] ?? value }))}
          />
        </Row>

        {/* 两个开关并排：它们都短，各占一行反而零散 */}
        <div style={{ display: 'flex', gap: SPACE.xl }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: SPACE.sm }}>
            <Switch
              checked={field.required}
              disabled={reserved?.lock_required ?? false}
              onChange={(checked) => onChange({ required: checked })}
            />
            <Typography.Text>必填</Typography.Text>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: SPACE.sm }}>
            <Switch
              checked={field.summary}
              disabled={reserved?.lock_summary ?? false}
              onChange={(checked) => onChange({ summary: checked })}
            />
            <Tooltip title="勾选的字段作为列显示在图书馆列表中，其余收进每行的「详情」里。只影响显示，不影响数据。">
              <Typography.Text>摘要</Typography.Text>
            </Tooltip>
          </div>
        </div>
      </div>
    </Modal>
  )
}
