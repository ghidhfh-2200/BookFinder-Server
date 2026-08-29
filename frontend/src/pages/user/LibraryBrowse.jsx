import { useState } from 'react'
import { App, Button, Card, Input, Result, Space } from 'antd'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import PageHeader from '../../components/PageHeader'
import LibraryTable from '../../components/LibraryTable'
import LibraryFormModal from '../../components/LibraryFormModal'
import ReportOutdatedModal from '../../components/ReportOutdatedModal'
import { useLibraries } from '../../hooks/useLibraries'
import { useLibrarySchema } from '../../hooks/useLibrarySchema'
import { useAuth } from '../../hooks/useAuth'
import { PAGE_STYLE, TABLE_RESERVE, useFillHeight } from '../../hooks/useFillHeight'
import {
  createLibrary,
  deleteLibrary,
  reportFieldOutdated,
  revokeFieldOutdated,
  updateLibrary,
} from '../../api/library'
import {
  PERMISSION_LIBRARY_CREATE,
  PERMISSION_LIBRARY_REPORT_OUTDATED,
  PERMISSION_LIBRARY_UPDATE,
} from '../../utils/permissions'

// LibraryBrowse 公开浏览页，Users 组与管理员都可访问。
// Users 组可查、可增改、可按字段报告过时，删除仅限自己创建的记录
// （按行由后端下发的 can_delete 决定）。
export default function LibraryBrowse() {
  const { message } = App.useApp()
  const { hasPermission } = useAuth()
  const { libraries, loading, pagination, search, changePage, reload } = useLibraries()
  const {
    fields,
    summaryFields,
    searchNameField,
    ready: schemaReady,
    error: schemaError,
    loading: schemaLoading,
    reload: reloadSchema,
  } = useLibrarySchema()
  const [fillRef, tableHeight] = useFillHeight(TABLE_RESERVE)

  // 两个请求都算 read 类别，限流下可能只有一个被拒，故刷新时一并重取
  const reloadAll = () => {
    reloadSchema()
    reload()
  }

  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState(null)
  const [submitting, setSubmitting] = useState(false)
  // reporting 正在报告的那条记录。存 ID 而非整个对象：报告后要 reload，
  // 存对象的话弹窗里的次数与状态还是旧的。
  const [reportingId, setReportingId] = useState(null)

  const canCreate = hasPermission(PERMISSION_LIBRARY_CREATE)
  const canUpdate = hasPermission(PERMISSION_LIBRARY_UPDATE)
  const canReport = hasPermission(PERMISSION_LIBRARY_REPORT_OUTDATED)

  // 从当前列表里取那条记录，故报告后 reload 会自然带来最新的次数与状态
  const reporting = libraries.find((item) => item.id === reportingId) ?? null

  const searchLabel = fields.find((field) => field.name === searchNameField)
  const searchPlaceholder = `搜索${searchLabel ? searchLabel.label || searchLabel.name : '记录名'}`

  const handleSubmit = async (values) => {
    setSubmitting(true)
    const resp = editing ? await updateLibrary(editing.id, values) : await createLibrary(values)
    setSubmitting(false)

    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }

    message.success(resp.message)
    setModalOpen(false)
    reload()
  }

  // 只能删自己创建的，按钮也只在那些行上出现（后端逐条给 can_delete）。
  // 真正的拦截在后端：换个浏览器、清了 Cookie 就不再是创建者。
  const handleDelete = async (id) => {
    const resp = await deleteLibrary(id)
    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }
    message.success(resp.message)
    reload()
  }

  // 报告过时与撤销共用一套结果处理
  const handleFieldStatus = async (call) => {
    const resp = await call

    // 疑似重复不是失败，而是判定为同一人已报告过、这次未计数
    if (resp.data?.duplicate) {
      message.warning(resp.message)
      reload()
      return
    }

    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }

    message.success(resp.message)
    reload()
  }

  return (
    <div style={PAGE_STYLE}>
      <PageHeader
        title="图书馆"
        extra={
          <Space wrap>
            <Input.Search
              allowClear
              placeholder={searchPlaceholder}
              style={{ width: 240 }}
              onSearch={search}
            />
            <Button icon={<ReloadOutlined />} onClick={reloadAll}>
              刷新
            </Button>
            {canCreate && (
              <Button
                type="primary"
                icon={<PlusOutlined />}
                onClick={() => {
                  setEditing(null)
                  setModalOpen(true)
                }}
              >
                新增
              </Button>
            )}
          </Space>
        }
      />

      {/* 注册表决定表格有哪些列，取不到时整张表没有内容可显示。
          此时给出提示与重试入口，而不是渲染一张只剩 ID 的空表。 */}
      {!schemaLoading && !schemaReady ? (
        <Card variant="borderless">
          <Result
            status="warning"
            title="字段信息加载失败"
            subTitle={schemaError || '无法获取字段注册表，表格内容暂时无法显示。'}
            extra={
              <Button type="primary" icon={<ReloadOutlined />} onClick={reloadAll}>
                重试
              </Button>
            }
          />
        </Card>
      ) : (
        /* 测量可用高度喂给表格：表体内部滚动，页面本身不产生滚动条 */
        <div ref={fillRef} style={{ flex: 1, minHeight: 0 }}>
          <Card variant="borderless" styles={{ body: { padding: 0 } }}>
            <LibraryTable
              libraries={libraries}
              fields={fields}
              summaryFields={summaryFields}
              loading={loading || schemaLoading}
              pagination={pagination}
              scrollY={tableHeight}
              onChangePage={changePage}
              canUpdate={canUpdate}
              canReportOutdated={canReport}
              onEdit={(record) => {
                setEditing(record)
                setModalOpen(true)
              }}
              onDelete={handleDelete}
              onReportOutdated={(record) => setReportingId(record.id)}
            />
          </Card>
        </div>
      )}

      <LibraryFormModal
        open={modalOpen}
        library={editing}
        fields={fields}
        submitting={submitting}
        onOk={handleSubmit}
        onCancel={() => setModalOpen(false)}
      />

      <ReportOutdatedModal
        open={reportingId != null}
        library={reporting}
        fields={fields}
        onReport={(field) => handleFieldStatus(reportFieldOutdated(reportingId, field))}
        onRevoke={(field) => handleFieldStatus(revokeFieldOutdated(reportingId, field))}
        onCancel={() => setReportingId(null)}
      />
    </div>
  )
}
