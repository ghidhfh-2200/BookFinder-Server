import { useMemo, useState } from 'react'
import { Alert, App, Button, Card, Empty, Segmented, Space, Tag } from 'antd'
import { PlusOutlined, ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import PageHeader from '../../components/PageHeader'
import Spinner from '../../components/Spinner'
import FieldTable from '../../components/schema/FieldTable'
import JsonEditor from '../../components/schema/JsonEditor'
import { useLibrarySchema } from '../../hooks/useLibrarySchema'
import { updateLibrarySchema } from '../../api/librarySchema'
import { PAGE_STYLE } from '../../hooks/useFillHeight'

// SchemaEditor 字段注册表编辑器。
// 注册表决定图书馆 Info 里允许出现哪些字段；保存后后端热更新，
// 并自动补全已有记录：新增字段补空值，删除字段的数据一并清除。
export default function SchemaEditor() {
  const { message } = App.useApp()
  const { fields, types, searchNameField, loading, reload } = useLibrarySchema()

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
          <Space wrap>
            <Button
              icon={<ReloadOutlined />}
              disabled={saving}
              onClick={() => {
                setEdited(null)
                reload()
              }}
            >
              重置
            </Button>
            <Button
              type="primary"
              icon={<SaveOutlined />}
              loading={saving}
              disabled={!dirty}
              onClick={handleSave}
            >
              保存
            </Button>
          </Space>
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
            body: {
              paddingTop: 16,
              flex: 1,
              minHeight: 0,
              display: 'flex',
              flexDirection: 'column',
              overflowY: mode === 'table' ? 'auto' : 'hidden',
            },
          }}
          title={
            <Segmented
              value={mode}
              onChange={setMode}
              options={[
                { value: 'table', label: '字段列表' },
                { value: 'json', label: 'JSON 源码' },
              ]}
            />
          }
          extra={
            mode === 'table' && (
              <Button type="dashed" icon={<PlusOutlined />} onClick={addField}>
                新增字段
              </Button>
            )
          }
        >
          {mode === 'table' ? (
            draft.length > 0 ? (
              <FieldTable
                fields={draft}
                types={types}
                searchNameField={searchNameField}
                savedNames={savedNames}
                onChange={setDraft}
                onRemove={removeField}
                onMove={moveField}
              />
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
