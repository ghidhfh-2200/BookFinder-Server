import { useCallback, useEffect, useState } from 'react'
import { App, Button, Card, Input, Segmented, Select, Space, Table, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import PageHeader from '../../components/PageHeader'
import LevelTag from '../../components/LevelTag'
import { getAppLogs, getLogMeta, getOperationLogs } from '../../api/admin/logs'
import { actionLabel, formatTime } from '../../utils/logActions'
import { PAGE_STYLE, useFillHeight } from '../../hooks/useFillHeight'

// LogViewer 管理员日志查看：用户操作日志与应用运行日志。
// 两者分表存储，查询方式不同，故分标签页呈现。
export default function LogViewer() {
  const { message } = App.useApp()

  const [tab, setTab] = useState('operations')
  const [rows, setRows] = useState([])
  const [loading, setLoading] = useState(false)
  const [pagination, setPagination] = useState({ current: 1, pageSize: 20, total: 0 })
  const [meta, setMeta] = useState({ actions: [], levels: [] })

  // 卡片内除表体外还有标签页头、工具条与分页器，故预留更多
  const [fillRef, tableHeight] = useFillHeight(240)

  // 筛选条件。两个标签页的字段不同，切换时一并清空，免得残留值带过去。
  const [level, setLevel] = useState()
  const [action, setAction] = useState()
  const [user, setUser] = useState('')
  const [keyword, setKeyword] = useState('')

  const switchTab = (next) => {
    setLevel(undefined)
    setAction(undefined)
    setUser('')
    setKeyword('')
    setTab(next)
  }

  const fetchLogs = useCallback(
    async (overrides = {}) => {
      const params = {
        page: overrides.page ?? 1,
        size: overrides.size ?? pagination.pageSize,
        level: overrides.level ?? level,
        ...(tab === 'operations'
          ? { user: overrides.user ?? user, action: overrides.action ?? action }
          : { keyword: overrides.keyword ?? keyword }),
      }

      setLoading(true)
      const resp = tab === 'operations' ? await getOperationLogs(params) : await getAppLogs(params)
      setLoading(false)

      if (resp.code !== 200) {
        message.error(resp.message)
        return
      }

      setRows(resp.data || [])
      setPagination({ current: resp.page, pageSize: resp.size, total: resp.total })
    },
    [tab, level, action, user, keyword, pagination.pageSize, message],
  )

  useEffect(() => {
    const load = async () => {
      const resp = await getLogMeta()
      if (resp.code === 200) {
        setMeta(resp.data ?? { actions: [], levels: [] })
      }
    }
    load()
  }, [])

  // 切换标签页或改变筛选条件时回到第一页
  useEffect(() => {
    const load = async () => {
      await fetchLogs({ page: 1 })
    }
    load()
    // fetchLogs 随筛选条件变化而重建，此处只在这些条件变动时重查
  }, [fetchLogs])

  const operationColumns = [
    {
      title: '时间',
      dataIndex: 'timestamp',
      width: 168,
      render: (value) => (
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>
          {formatTime(value)}
        </Typography.Text>
      ),
    },
    { title: '等级', dataIndex: 'level', width: 84, render: (value) => <LevelTag level={value} /> },
    {
      title: '用户',
      dataIndex: 'user',
      width: 148,
      ellipsis: true,
      render: (value, record) => (
        <Space size={4} direction="vertical" style={{ display: 'flex' }}>
          <Typography.Text>{value}</Typography.Text>
          {/* 管理员的用户名不是 IP，单独把来源 IP 显示出来 */}
          {record.ip && record.ip !== value && (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {record.ip}
            </Typography.Text>
          )}
        </Space>
      ),
    },
    {
      title: '操作',
      dataIndex: 'action',
      width: 132,
      render: (value) => actionLabel(value),
    },
    { title: '详情', dataIndex: 'detail', ellipsis: true },
  ]

  const appColumns = [
    {
      title: '时间',
      dataIndex: 'timestamp',
      width: 168,
      render: (value) => (
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>
          {formatTime(value)}
        </Typography.Text>
      ),
    },
    { title: '等级', dataIndex: 'level', width: 84, render: (value) => <LevelTag level={value} /> },
    { title: '消息', dataIndex: 'message', ellipsis: true },
  ]

  const levelOptions = meta.levels.map((value) => ({ value, label: value }))
  const actionOptions = meta.actions.map((value) => ({ value, label: actionLabel(value) }))

  return (
    <div style={PAGE_STYLE}>
      <PageHeader
        title="日志"
        extra={
          <Button icon={<ReloadOutlined />} onClick={() => fetchLogs({ page: pagination.current })}>
            刷新
          </Button>
        }
      />

      <div ref={fillRef} style={{ flex: 1, minHeight: 0 }}>
      <Card
        variant="borderless"
        styles={{ body: { paddingTop: 16 } }}
        title={
          <Segmented
            value={tab}
            onChange={switchTab}
            options={[
              { value: 'operations', label: '操作日志' },
              { value: 'app', label: '运行日志' },
            ]}
          />
        }
      >
        <div className="toolbar">
          <div className="toolbar__group">
            <Select
              allowClear
              placeholder="等级"
              style={{ width: 120 }}
              value={level}
              onChange={setLevel}
              options={levelOptions}
            />

            {tab === 'operations' ? (
              <>
                <Select
                  allowClear
                  showSearch
                  placeholder="操作类型"
                  style={{ width: 180 }}
                  value={action}
                  onChange={setAction}
                  options={actionOptions}
                  optionFilterProp="label"
                />
                <Input.Search
                  allowClear
                  placeholder="按用户或 IP 精确查询"
                  style={{ width: 220 }}
                  onSearch={setUser}
                />
              </>
            ) : (
              <Input.Search
                allowClear
                placeholder="搜索日志内容"
                style={{ width: 260 }}
                onSearch={setKeyword}
              />
            )}
          </div>
        </div>

        <Table
          rowKey="id"
          size="middle"
          loading={loading}
          columns={tab === 'operations' ? operationColumns : appColumns}
          dataSource={rows}
          scroll={{ x: 'max-content', y: tableHeight }}
          pagination={{
            ...pagination,
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 条`,
            onChange: (page, size) => fetchLogs({ page, size }),
          }}
        />
      </Card>
      </div>
    </div>
  )
}
