import { useMemo, useState } from 'react'
import { Alert, App, Button, Card, Empty, Segmented, Tag } from 'antd'
import { PlusOutlined, ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import PageHeader from '../../components/PageHeader'
import Spinner from '../../components/Spinner'
import FieldTable from '../../components/schema/FieldTable'
import FieldCardList from '../../components/schema/FieldCardList'
import JsonEditor from '../../components/schema/JsonEditor'
import { useLibrarySchema } from '../../hooks/useLibrarySchema'
import { updateLibrarySchema } from '../../api/librarySchema'
import { PAGE_STYLE } from '../../hooks/useFillHeight'
import { useIsMobile } from '../../hooks/useIsMobile'
import { SPACE, TOUCH_SIZE } from '../../spacing'

// SchemaEditor 字段注册表编辑器。
// 注册表决定图书馆 Info 里允许出现哪些字段；保存后后端热更新，
// 并自动补全已有记录：新增字段补空值，删除字段的数据一并清除。
export default function SchemaEditor() {
  const { message } = App.useApp()
  const { fields, types, reservedFields, loading, reload } = useLibrarySchema()
  const isMobile = useIsMobile()

  // 草稿仅在有本地改动时存在，否则直接呈现后端的字段，
  // 这样重新加载注册表后无需 effect 同步即可反映最新值
  const [edited, setEdited] = useState(null)
  const [mode, setMode] = useState('table')
  const [saving, setSaving] = useState(false)

  const draft = edited ?? fields

  // 已保存的字段名：这些是标识符，编辑器里不允许改名
  const savedNames = useMemo(() => fields.map((field) => field.name), [fields])

  const dirty = useMemo(
    () => JSON.stringify(draft) !== JSON.stringify(fields),
    [draft, fields],
  )

  // setDraft 收下改动；与后端一致时丢掉草稿，交回 fields 托管
  const setDraft = (next) => {
    setEdited(JSON.stringify(next) === JSON.stringify(fields) ? null : next)
  }

  // 字段名不可改，故同一次提交里既删又增会被后端拒绝
  const removed = useMemo(
    () => savedNames.filter((name) => !draft.some((field) => field.name === name)),
    [savedNames, draft],
  )
  const added = useMemo(
    () => draft.map((field) => field.name).filter((name) => name && !savedNames.includes(name)),
    [draft, savedNames],
  )

  // 新字段默认不作为摘要：加字段的常见动因正是「信息不够」，
  // 而每个新字段都自动占一列会让表格越用越宽——要显示由管理员显式勾选
  const addField = () => {
    setDraft([...draft, { name: '', label: '', type: 'string', required: false, summary: false }])
  }

  const removeField = (index) => {
    setDraft(draft.filter((_, i) => i !== index))
  }

  const moveField = (from, to) => {
    const next = [...draft]
    const [moved] = next.splice(from, 1)
    next.splice(to, 0, moved)
    setDraft(next)
  }

  const handleSave = async () => {
    setSaving(true)
    const resp = await updateLibrarySchema(draft)
    setSaving(false)

    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }

    const migrated = resp.data?.migrated ?? 0
    message.success(`注册表已保存并生效，补全 ${migrated} 条记录`)
    setEdited(null)
    reload()
  }

  if (loading) {
    return <Spinner tip="加载注册表..." />
  }

  return (
    <div style={PAGE_STYLE}>
      <PageHeader
        title="字段注册表"
        extra={
          // 窄屏两个按钮等分一行，不用 Space wrap（那会把「保存」甩到单独一行）
          <div style={{ display: 'flex', gap: SPACE.sm, width: isMobile ? '100%' : 'auto' }}>
            <Button
              icon={<ReloadOutlined />}
              disabled={saving}
              onClick={() => {
                setEdited(null)
                reload()
              }}
              style={isMobile ? { flex: 1, height: TOUCH_SIZE } : undefined}
            >
              重置
            </Button>
            <Button
              type="primary"
              icon={<SaveOutlined />}
              loading={saving}
              disabled={!dirty}
              onClick={handleSave}
              style={isMobile ? { flex: 1, height: TOUCH_SIZE } : undefined}
            >
              保存
            </Button>
          </div>
        }
      />

      {/* 不用 Space：它把每个子节点包进一层没有 flex 的 div，卡片的 flex: 1
          会被那一层拦住，高度传不到 JSON 编辑器里。直接用 flex 列。 */}
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 16,
          width: '100%',
          flex: 1,
          minHeight: 0,
        }}
      >
        {removed.length > 0 && added.length > 0 && (
          <Alert
            type="error"
            showIcon
            style={{ flexShrink: 0 }}
            message={
              <>
                不能在同一次提交里既删除 <Tag>{removed.join('、')}</Tag> 又新增{' '}
                <Tag>{added.join('、')}</Tag>
              </>
            }
          />
        )}

        {removed.length > 0 && added.length === 0 && (
          <Alert
            type="warning"
            showIcon
            style={{ flexShrink: 0 }}
            message={
              <>
                保存后将清除 <Tag>{removed.join('、')}</Tag> 的已有数据，无法恢复
              </>
            }
          />
        )}

        {/* 表格模式字段多时由卡片内容区滚动；JSON 模式改由输入框自己滚动，
            故此处不设 overflow，交给各自的内容决定。 */}
        <Card
          variant="borderless"
          style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}
          styles={{
            // 窄屏去掉卡片自身的左右留白：里面是一列卡片，
            // 两层留白叠起来会把可用宽度又吃掉一圈
            header: isMobile ? { padding: `${SPACE.md}px ${SPACE.md}px 0` } : undefined,
            body: {
              paddingTop: SPACE.lg,
              paddingLeft: isMobile ? SPACE.md : undefined,
              paddingRight: isMobile ? SPACE.md : undefined,
              flex: 1,
              minHeight: 0,
              display: 'flex',
              flexDirection: 'column',
              overflowY: mode === 'table' ? 'auto' : 'hidden',
            },
          }}
          title={
            // 窄屏让分段控件占满宽度，「新增字段」另起一行铺满：
            // 两者并排时按钮会被挤到只剩图标
            <Segmented
              value={mode}
              onChange={setMode}
              block={isMobile}
              options={[
                { value: 'table', label: '字段列表' },
                { value: 'json', label: 'JSON 源码' },
              ]}
            />
          }
          extra={
            mode === 'table' &&
            !isMobile && (
              <Button type="dashed" icon={<PlusOutlined />} onClick={addField}>
                新增字段
              </Button>
            )
          }
        >
          {mode === 'table' ? (
            draft.length > 0 ? (
              // 窄屏改卡片：这张表每格都是输入控件，八列合计约 830px，
              // 横滚着改表单没法用。排序也从拖拽换成上移/下移按钮。
              isMobile ? (
                <>
                  <FieldCardList
                    fields={draft}
                    types={types}
                    reservedFields={reservedFields}
                    savedNames={savedNames}
                    onChange={setDraft}
                    onRemove={removeField}
                    onMove={moveField}
                  />
                  {/* 新增按钮移到列表末尾：卡片头部并排放不下它，
                      而放在末尾也符合「新字段追加在后面」的实际行为 */}
                  <Button
                    type="dashed"
                    block
                    icon={<PlusOutlined />}
                    onClick={addField}
                    style={{ height: TOUCH_SIZE }}
                  >
                    新增字段
                  </Button>
                </>
              ) : (
                <FieldTable
                  fields={draft}
                  types={types}
                  reservedFields={reservedFields}
                  savedNames={savedNames}
                  onChange={setDraft}
                  onRemove={removeField}
                  onMove={moveField}
                />
              )
            ) : (
              <Empty description="注册表为空，至少需要一个字段" />
            )
          ) : (
            <JsonEditor fields={draft} onChange={setDraft} />
          )}
        </Card>
      </div>
    </div>
  )
}
