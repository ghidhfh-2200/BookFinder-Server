import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Card,
  Col,
  Form,
  InputNumber,
  Row,
  Space,
  Switch,
  Table,
  Typography,
} from 'antd'
import { ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import PageHeader from '../../components/PageHeader'
import Spinner from '../../components/Spinner'
import LimitCardList from '../../components/rate/LimitCardList'
import { getRateRules, updateRateRules } from '../../api/admin/rateRules'
import { PAGE_STYLE } from '../../hooks/useFillHeight'
import { useIsMobile } from '../../hooks/useIsMobile'
import { SPACE, TOUCH_SIZE } from '../../spacing'

// CATEGORY_LABELS 类别的中文名，取值列表由后端给出
const CATEGORY_LABELS = {
  read: '读取',
  create: '新增',
  update: '修改',
  report: '报告',
  auth: '登录认证',
  appeal: '封禁申诉',
}

export default function RateRules() {
  const { message } = App.useApp()
  const isMobile = useIsMobile()

  const [saved, setSaved] = useState(null)
  const [draft, setDraft] = useState(null)
  const [warnings, setWarnings] = useState([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const fetchRules = useCallback(async () => {
    setLoading(true)
    const resp = await getRateRules()
    setLoading(false)

    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }

    setSaved(resp.data.rules)
    setDraft(resp.data.rules)
    setWarnings(resp.data.warnings ?? [])
  }, [message])

  useEffect(() => {
    const load = async () => {
      await fetchRules()
    }
    load()
  }, [fetchRules])

  const dirty = useMemo(() => JSON.stringify(draft) !== JSON.stringify(saved), [draft, saved])

  // 后端会拒绝 burst >= daily，此处先挡住，免得白跑一次请求
  const invalid = useMemo(
    () => Object.values(draft?.limits ?? {}).some((limit) => limit.burst >= limit.daily),
    [draft],
  )

  const patchLimit = (category, patch) => {
    setDraft({
      ...draft,
      limits: { ...draft.limits, [category]: { ...draft.limits[category], ...patch } },
    })
  }

  const patchAutoBan = (patch) => {
    setDraft({ ...draft, auto_ban: { ...draft.auto_ban, ...patch } })
  }

  const patchProbation = (patch) => {
    setDraft({ ...draft, probation: { ...draft.probation, ...patch } })
  }

  // draft 在首次渲染时为 null（规则尚未拉回），故须用可选链：
  // 下方的 loading 早退发生在这几行之后
  const networkBudget = Object.values(draft?.limits ?? {}).reduce(
    (sum, limit) => sum + (limit.daily ?? 0),
    0,
  )
  const networkThreshold = networkBudget * (draft?.auto_ban?.network_overflow_multiplier ?? 0)
  const culpritThreshold =
    networkBudget * Math.max(draft?.auto_ban?.daily_overflow_multiplier ?? 0, 1)

  const handleSave = async () => {
    setSaving(true)
    const resp = await updateRateRules(draft)
    setSaving(false)

    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }

    message.success('限流规则已保存并生效')
    setSaved(resp.data.rules)
    setDraft(resp.data.rules)
    setWarnings(resp.data.warnings ?? [])
  }

  if (loading || !draft) {
    return <Spinner tip="加载限流规则..." />
  }

  const categories = Object.keys(draft.limits ?? {})

  const columns = [
    {
      title: '操作类别',
      dataIndex: 'category',
      width: 160,
      render: (category) => (
        <Typography.Text strong>{CATEGORY_LABELS[category] ?? category}</Typography.Text>
      ),
    },
    {
      title: '每日配额',
      dataIndex: 'daily',
      width: 160,
      render: (value, record) => (
        <InputNumber
          min={1}
          value={value}
          style={{ width: '100%' }}
          onChange={(next) => patchLimit(record.category, { daily: next ?? 1 })}
        />
      ),
    },
    {
      title: '突发次数',
      dataIndex: 'burst',
      width: 160,
      render: (value, record) => (
        <InputNumber
          min={1}
          max={Math.max(record.daily - 1, 1)}
          value={value}
          status={value >= record.daily ? 'error' : undefined}
          style={{ width: '100%' }}
          onChange={(next) => patchLimit(record.category, { burst: next ?? 1 })}
        />
      ),
    },
    {
      title: '突发窗口（秒）',
      dataIndex: 'burst_window_seconds',
      width: 170,
      render: (value, record) => (
        <InputNumber
          min={1}
          max={3600}
          value={value}
          style={{ width: '100%' }}
          onChange={(next) => patchLimit(record.category, { burst_window_seconds: next ?? 60 })}
        />
      ),
    },
  ]

  const rows = categories.map((category) => ({
    key: category,
    category,
    ...draft.limits[category],
  }))

  return (
    <div style={PAGE_STYLE}>
      <PageHeader
        title="限流规则"
        extra={
          // 窄屏两个按钮等分一行，不用 Space wrap（那会把「保存」甩到单独一行）
          <div style={{ display: 'flex', gap: SPACE.sm, width: isMobile ? '100%' : 'auto' }}>
            <Button
              icon={<ReloadOutlined />}
              disabled={saving}
              onClick={fetchRules}
              style={isMobile ? { flex: 1, height: TOUCH_SIZE } : undefined}
            >
              重置
            </Button>
            <Button
              type="primary"
              icon={<SaveOutlined />}
              loading={saving}
              disabled={!dirty || invalid}
              onClick={handleSave}
              style={isMobile ? { flex: 1, height: TOUCH_SIZE } : undefined}
            >
              保存
            </Button>
          </div>
        }
      />

      {/* 右侧留一点空隙，免得滚动条压在卡片边缘上 */}
      <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', paddingRight: SPACE.xs }}>
        <Space direction="vertical" size={16} style={{ width: '100%', display: 'flex' }}>
          {!draft.enabled && <Alert type="warning" showIcon message="限流已关闭" />}

          {invalid && (
            <Alert type="error" showIcon message="突发次数必须小于每日配额" />
          )}

          {warnings.map((text) => (
            <Alert key={text} type="warning" showIcon message={text} />
          ))}

          <Card
            variant="borderless"
            title="每日与突发配额"
            extra={
              <Space size={8}>
                <Typography.Text type="secondary">启用限流</Typography.Text>
                <Switch
                  checked={draft.enabled}
                  onChange={(checked) => setDraft({ ...draft, enabled: checked })}
                />
              </Space>
            }
          >
            {/* 窄屏改分组表单：四列约 650px 且每格都是输入框，横滚改不了 */}
            {isMobile ? (
              <LimitCardList
                categories={categories}
                limits={draft.limits ?? {}}
                labels={CATEGORY_LABELS}
                onPatch={patchLimit}
              />
            ) : (
              <Table
                size="middle"
                columns={columns}
                dataSource={rows}
                pagination={false}
                scroll={{ x: 'max-content' }}
              />
            )}
          </Card>

          <Card variant="borderless" title="见习配额">
            <Form layout="vertical">
              <Row gutter={16}>
                <Col xs={24} sm={8}>
                  <Form.Item label="每日额度">
                    <InputNumber
                      min={1}
                      max={100}
                      value={draft.probation?.daily}
                      style={{ width: '100%' }}
                      onChange={(next) => patchProbation({ daily: next ?? 1 })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={8}>
                  <Form.Item label="突发次数">
                    <InputNumber
                      min={1}
                      max={Math.max(draft.probation?.daily ?? 1, 1)}
                      value={draft.probation?.burst}
                      style={{ width: '100%' }}
                      onChange={(next) => patchProbation({ burst: next ?? 1 })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={8}>
                  <Form.Item label="突发窗口（秒）">
                    <InputNumber
                      min={1}
                      max={3600}
                      value={draft.probation?.burst_window_seconds}
                      style={{ width: '100%' }}
                      onChange={(next) => patchProbation({ burst_window_seconds: next ?? 60 })}
                    />
                  </Form.Item>
                </Col>
              </Row>
            </Form>
          </Card>

          <Card
            variant="borderless"
            title="自动封禁"
            extra={
              <Space size={8}>
                <Typography.Text type="secondary">启用自动封禁</Typography.Text>
                <Switch
                  checked={draft.auto_ban?.enabled}
                  onChange={(checked) => patchAutoBan({ enabled: checked })}
                />
              </Space>
            }
          >
            <Form layout="vertical" disabled={!draft.auto_ban?.enabled}>
              <Row gutter={16}>
                <Col xs={24} sm={12} lg={6}>
                  <Form.Item label="每日超额倍数">
                    <InputNumber
                      min={0}
                      value={draft.auto_ban?.daily_overflow_multiplier}
                      style={{ width: '100%' }}
                      onChange={(next) => patchAutoBan({ daily_overflow_multiplier: next ?? 0 })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={6}>
                  <Form.Item label="突发违规次数">
                    <InputNumber
                      min={0}
                      value={draft.auto_ban?.burst_violations}
                      style={{ width: '100%' }}
                      onChange={(next) => patchAutoBan({ burst_violations: next ?? 0 })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={6}>
                  <Form.Item label="重复报告次数">
                    <InputNumber
                      min={0}
                      value={draft.auto_ban?.duplicate_reports}
                      style={{ width: '100%' }}
                      onChange={(next) => patchAutoBan({ duplicate_reports: next ?? 0 })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={6}>
                  <Form.Item label="见习超额倍数">
                    <InputNumber
                      min={0}
                      value={draft.auto_ban?.probation_overflow_multiplier}
                      style={{ width: '100%' }}
                      onChange={(next) =>
                        patchAutoBan({ probation_overflow_multiplier: next ?? 0 })
                      }
                    />
                  </Form.Item>
                </Col>
              </Row>

              <Typography.Title level={5} style={{ marginTop: 8 }}>
                网段判定
              </Typography.Title>

              <Row gutter={16}>
                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label="网段超额倍数">
                    <InputNumber
                      min={0}
                      value={draft.auto_ban?.network_overflow_multiplier}
                      style={{ width: '100%' }}
                      onChange={(next) => patchAutoBan({ network_overflow_multiplier: next ?? 0 })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label="排查令牌数">
                    <InputNumber
                      min={1}
                      max={20}
                      value={draft.auto_ban?.network_top_visitors}
                      style={{ width: '100%' }}
                      onChange={(next) => patchAutoBan({ network_top_visitors: next ?? 1 })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label="流量集中度（%）">
                    <InputNumber
                      min={50}
                      max={100}
                      value={draft.auto_ban?.network_concentration_percent}
                      style={{ width: '100%' }}
                      onChange={(next) =>
                        patchAutoBan({ network_concentration_percent: next ?? 80 })
                      }
                    />
                  </Form.Item>
                </Col>
              </Row>

              {(draft.auto_ban?.network_overflow_multiplier ?? 0) > 0 && (
                <Alert
                  type="info"
                  showIcon={false}
                  message={
                    <Space direction="vertical" size={2}>
                      <Typography.Text>
                        网段预算 <Typography.Text strong>{networkBudget}</Typography.Text> 次/天
                      </Typography.Text>
                      <Typography.Text>
                        网段总量超过 <Typography.Text strong>{networkThreshold}</Typography.Text>{' '}
                        次/天启动排查
                      </Typography.Text>
                      <Typography.Text>
                        单个设备超过 <Typography.Text strong>{culpritThreshold}</Typography.Text>{' '}
                        次/天才算异常
                      </Typography.Text>
                    </Space>
                  }
                />
              )}
            </Form>
          </Card>
        </Space>
      </div>
    </div>
  )
}
