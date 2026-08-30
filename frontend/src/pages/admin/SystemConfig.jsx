import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Card,
  Col,
  Descriptions,
  Form,
  InputNumber,
  Input,
  Row,
  Select,
  Space,
  Switch,
  Typography,
} from 'antd'
import { ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import PageHeader from '../../components/PageHeader'
import Spinner from '../../components/Spinner'
import { getSystemConfig, updateSystemConfig } from '../../api/admin/systemConfig'
import { formatTime } from '../../utils/logActions'
import { PAGE_STYLE } from '../../hooks/useFillHeight'
import { useIsMobile } from '../../hooks/useIsMobile'
import { SPACE, TOUCH_SIZE } from '../../spacing'

// DEFAULT_DAILY_AT 清空时刻输入时的回落值，与后端默认值一致
const DEFAULT_DAILY_AT = '03:30'

// NOTIFY_ITEMS 三类可外发的告警，键名与后端 types.NotifyConfig 对应。
// 这些开关对两条通道同时生效。
const NOTIFY_ITEMS = [
  { key: 'auto_ban', label: '自动封禁' },
  { key: 'network_anomaly', label: '流量异常' },
  { key: 'appeal', label: '申诉请求' },
]

// SMTP_PORTS 端口取固定两项而非自由输入：这个选择决定加密方式，
// 填错的后果是凭据以明文过网，不该交给手输
const SMTP_PORTS = [
  { value: 465, label: '465（SSL/TLS）' },
  { value: 587, label: '587（STARTTLS）' },
]

// RESTART_HINT 标注需重启才生效的项。
// 这些值在服务器构造时即固定：http.Server 的超时字段与并发信号量的容量
// 都不再被重新读取。
const RESTART_HINT = '（重启后生效）'

export default function SystemConfig() {
  const { message } = App.useApp()
  const isMobile = useIsMobile()

  const [saved, setSaved] = useState(null)
  const [draft, setDraft] = useState(null)
  const [logStats, setLogStats] = useState(null)
  // 凭据是否已配置。凭据从 .env 注入，此处只读不改。
  const [telegramReady, setTelegramReady] = useState(false)
  const [smtpReady, setSmtpReady] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const fetchConfig = useCallback(async () => {
    setLoading(true)
    const resp = await getSystemConfig()
    setLoading(false)

    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }

    setSaved(resp.data.config)
    setDraft(resp.data.config)
    setLogStats(resp.data.log_stats ?? null)
    setTelegramReady(resp.data.telegram_configured ?? false)
    setSmtpReady(resp.data.smtp_password_configured ?? false)
  }, [message])

  useEffect(() => {
    const load = async () => {
      await fetchConfig()
    }
    load()
  }, [fetchConfig])

  const dirty = useMemo(() => JSON.stringify(draft) !== JSON.stringify(saved), [draft, saved])

  // 邮件通道是否真的能发信：开关、密码与各项参数都齐备才算。
  // 与后端 types.EmailConfig.Usable 的判断一致。
  const emailUsable = useMemo(() => {
    const email = draft?.notify?.email
    return Boolean(
      smtpReady && email?.enabled && email?.host && email?.port && email?.username && email?.to,
    )
  }, [draft, smtpReady])

  const patchMaintenance = (patch) => {
    setDraft({ ...draft, maintenance: { ...draft.maintenance, ...patch } })
  }

  const patchNotify = (patch) => {
    setDraft({ ...draft, notify: { ...draft.notify, ...patch } })
  }

  const patchEmail = (patch) => {
    // 补上端口默认值：已部署实例的配置文件里没有 email 段，端口会是 0，
    // 而界面上显示的是 465——不补的话保存时会因一个用户没看见的值而被拒
    const email = { port: SMTP_PORTS[0].value, ...draft.notify?.email, ...patch }
    setDraft({ ...draft, notify: { ...draft.notify, email } })
  }

  const patchPagination = (patch) => {
    setDraft({ ...draft, pagination: { ...draft.pagination, ...patch } })
  }

  const patchServer = (patch) => {
    setDraft({ ...draft, server: { ...draft.server, ...patch } })
  }

  const handleSave = async () => {
    setSaving(true)
    const resp = await updateSystemConfig(draft)
    setSaving(false)

    if (resp.code !== 200) {
      message.error(resp.message)
      return
    }

    message.success('系统配置已保存')
    setSaved(resp.data.config)
    setDraft(resp.data.config)
  }

  if (loading || !draft) {
    return <Spinner tip="加载系统配置..." />
  }

  return (
    <div style={PAGE_STYLE}>
      <PageHeader
        title="系统管理"
        extra={
          // 窄屏两个按钮等分一行，不用 Space wrap（那会把「保存」甩到单独一行）
          <div style={{ display: 'flex', gap: SPACE.sm, width: isMobile ? '100%' : 'auto' }}>
            <Button
              icon={<ReloadOutlined />}
              disabled={saving}
              onClick={fetchConfig}
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

      {/* 右侧留一点空隙，免得滚动条压在卡片边缘上 */}
      <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', paddingRight: SPACE.xs }}>
        <Space direction="vertical" size={16} style={{ width: '100%', display: 'flex' }}>
          {!draft.maintenance?.enabled && (
            <Alert type="warning" showIcon message="定期清理已关闭，日志表将持续增长" />
          )}

          <Card
            variant="borderless"
            title="日志清理"
            extra={
              <Space size={8}>
                <Typography.Text type="secondary">启用定期清理</Typography.Text>
                <Switch
                  checked={draft.maintenance?.enabled}
                  onChange={(checked) => patchMaintenance({ enabled: checked })}
                />
              </Space>
            }
          >
            <Form layout="vertical" disabled={!draft.maintenance?.enabled}>
              <Row gutter={16}>
                <Col xs={24} sm={8}>
                  <Form.Item label="每日执行时刻">
                    {/* 用原生 time 输入而非 antd 的 TimePicker：后者要 dayjs，
                        而按格式解析还需额外插件——为一个 HH:MM 字符串不值得 */}
                    <Input
                      type="time"
                      value={draft.maintenance?.daily_at ?? DEFAULT_DAILY_AT}
                      onChange={(e) =>
                        patchMaintenance({ daily_at: e.target.value || DEFAULT_DAILY_AT })
                      }
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={8}>
                  <Form.Item label="操作日志保留（天）">
                    <InputNumber
                      min={7}
                      max={3650}
                      value={draft.maintenance?.operation_log_retention_days}
                      style={{ width: '100%' }}
                      onChange={(next) =>
                        patchMaintenance({ operation_log_retention_days: next ?? 180 })
                      }
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={8}>
                  <Form.Item label="运行日志保留（天）">
                    <InputNumber
                      min={7}
                      max={3650}
                      value={draft.maintenance?.app_log_retention_days}
                      style={{ width: '100%' }}
                      onChange={(next) => patchMaintenance({ app_log_retention_days: next ?? 30 })}
                    />
                  </Form.Item>
                </Col>
              </Row>
            </Form>

            {logStats && (
              <Descriptions
                bordered
                size="small"
                column={{ xs: 1, sm: 2 }}
                items={[
                  {
                    key: 'op',
                    label: '操作日志',
                    children: `${logStats.operation_logs} 行${
                      logStats.oldest_operation_log
                        ? `，最早 ${formatTime(logStats.oldest_operation_log)}`
                        : ''
                    }`,
                  },
                  {
                    key: 'app',
                    label: '运行日志',
                    children: `${logStats.app_logs} 行${
                      logStats.oldest_app_log
                        ? `，最早 ${formatTime(logStats.oldest_app_log)}`
                        : ''
                    }`,
                  },
                ]}
              />
            )}
          </Card>

          <Card variant="borderless" title="告警通知">
            {!telegramReady && !emailUsable && (
              <Alert
                type="warning"
                showIcon
                style={{ marginBottom: 16 }}
                message="两条通道都不可用，通知不会送出"
              />
            )}

            {/* 三个开关排一行，标签在开关右侧。
                原先用 Form.Item + layout="vertical"，标签压在开关上方，
                每个又占一整个 Col——窄屏下三个开关就占了三整行，
                而它们只是三个短词加三个小控件。 */}
            <div
              style={{
                display: 'flex',
                flexWrap: 'wrap',
                // 横向间隙取 lg 而非 xl：三项加起来才刚好放进 375px 的屏，
                // 再宽一点就会折行
                gap: `${SPACE.md}px ${SPACE.lg}px`,
                marginBottom: SPACE.lg,
              }}
            >
              {NOTIFY_ITEMS.map((item) => (
                <div
                  key={item.key}
                  style={{ display: 'flex', alignItems: 'center', gap: SPACE.sm }}
                >
                  <Switch
                    checked={draft.notify?.[item.key] ?? false}
                    onChange={(checked) => patchNotify({ [item.key]: checked })}
                  />
                  <Typography.Text>{item.label}</Typography.Text>
                </div>
              ))}
            </div>

            <Descriptions
              bordered
              size="small"
              column={1}
              style={{ marginBottom: 16 }}
              items={[
                {
                  key: 'tg',
                  label: 'Telegram',
                  children: telegramReady
                    ? '已配置'
                    : '未配置 TELEGRAM_BOT_TOKEN 与 TELEGRAM_CHAT_ID',
                },
              ]}
            />

            <Form layout="vertical">
              <Row gutter={16} align="middle">
                <Col xs={24} sm={8}>
                  <Form.Item label="邮件通知">
                    <Space size={8}>
                      <Switch
                        checked={draft.notify?.email?.enabled ?? false}
                        onChange={(checked) => patchEmail({ enabled: checked })}
                      />
                      {!smtpReady && (
                        <Typography.Text type="warning">未配置 SMTP_PASSWORD</Typography.Text>
                      )}
                    </Space>
                  </Form.Item>
                </Col>
              </Row>

              <Row gutter={16}>
                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label="SMTP 服务器">
                    <Input
                      placeholder="smtp.qq.com"
                      value={draft.notify?.email?.host ?? ''}
                      disabled={!draft.notify?.email?.enabled}
                      onChange={(e) => patchEmail({ host: e.target.value })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label="端口">
                    <Select
                      value={draft.notify?.email?.port ?? 465}
                      disabled={!draft.notify?.email?.enabled}
                      onChange={(next) => patchEmail({ port: next })}
                      options={SMTP_PORTS}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label="发信账号">
                    <Input
                      placeholder="me@qq.com"
                      value={draft.notify?.email?.username ?? ''}
                      disabled={!draft.notify?.email?.enabled}
                      onChange={(e) => patchEmail({ username: e.target.value })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label="收件地址">
                    <Input
                      placeholder="me@qq.com"
                      value={draft.notify?.email?.to ?? ''}
                      disabled={!draft.notify?.email?.enabled}
                      onChange={(e) => patchEmail({ to: e.target.value })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label="发件地址（留空同发信账号）">
                    <Input
                      value={draft.notify?.email?.from ?? ''}
                      disabled={!draft.notify?.email?.enabled}
                      onChange={(e) => patchEmail({ from: e.target.value })}
                    />
                  </Form.Item>
                </Col>
              </Row>
            </Form>
          </Card>

          <Card variant="borderless" title="分页">
            <Form layout="vertical">
              <Row gutter={16}>
                <Col xs={24} sm={12}>
                  <Form.Item label="默认每页条数">
                    <InputNumber
                      min={1}
                      max={draft.pagination?.max_size ?? 500}
                      value={draft.pagination?.default_size}
                      style={{ width: '100%' }}
                      onChange={(next) => patchPagination({ default_size: next ?? 20 })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12}>
                  <Form.Item label="每页条数上限">
                    <InputNumber
                      min={1}
                      max={500}
                      value={draft.pagination?.max_size}
                      style={{ width: '100%' }}
                      onChange={(next) => patchPagination({ max_size: next ?? 100 })}
                    />
                  </Form.Item>
                </Col>
              </Row>
            </Form>
          </Card>

          <Card variant="borderless" title="资源上限">
            <Form layout="vertical">
              <Row gutter={16}>
                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label="请求体上限（字节）">
                    <InputNumber
                      min={1024}
                      max={1048576}
                      step={1024}
                      value={draft.server?.max_request_body_bytes}
                      style={{ width: '100%' }}
                      onChange={(next) => patchServer({ max_request_body_bytes: next ?? 65536 })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label={`并发上限${RESTART_HINT}`}>
                    <InputNumber
                      min={1}
                      value={draft.server?.max_concurrent_requests}
                      style={{ width: '100%' }}
                      onChange={(next) => patchServer({ max_concurrent_requests: next ?? 256 })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label={`读取请求头超时（秒）${RESTART_HINT}`}>
                    <InputNumber
                      min={1}
                      value={draft.server?.read_header_timeout_seconds}
                      style={{ width: '100%' }}
                      onChange={(next) =>
                        patchServer({ read_header_timeout_seconds: next ?? 10 })
                      }
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label={`读取请求超时（秒）${RESTART_HINT}`}>
                    <InputNumber
                      min={1}
                      value={draft.server?.read_timeout_seconds}
                      style={{ width: '100%' }}
                      onChange={(next) => patchServer({ read_timeout_seconds: next ?? 30 })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label={`写响应超时（秒）${RESTART_HINT}`}>
                    <InputNumber
                      min={1}
                      value={draft.server?.write_timeout_seconds}
                      style={{ width: '100%' }}
                      onChange={(next) => patchServer({ write_timeout_seconds: next ?? 60 })}
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} sm={12} lg={8}>
                  <Form.Item label={`空闲连接超时（秒）${RESTART_HINT}`}>
                    <InputNumber
                      min={1}
                      value={draft.server?.idle_timeout_seconds}
                      style={{ width: '100%' }}
                      onChange={(next) => patchServer({ idle_timeout_seconds: next ?? 90 })}
                    />
                  </Form.Item>
                </Col>
              </Row>
            </Form>
          </Card>
        </Space>
      </div>
    </div>
  )
}
