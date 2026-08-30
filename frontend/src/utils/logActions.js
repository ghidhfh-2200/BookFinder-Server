// 操作类型的中文说明。取值列表由后端接口给出，此处只负责翻译；
// 缺失的取值直接显示原文，故后端新增类型不会导致界面出错。
const ACTION_LABELS = {
  admin_login: '管理员登录',
  admin_login_failed: '登录失败',
  admin_entry_denied: '入口口令错误',
  admin_password_changed: '修改密码',
  library_create: '新增图书馆',
  library_update: '修改图书馆',
  library_delete: '删除图书馆',
  field_report: '报告过时',
  field_report_revoke: '撤销报告',
  field_report_rejected: '报告被判重',
  field_verify: '确认网站可用',
  field_verify_revoke: '撤销确认',
  client_sign_reload: '重载签名密钥',
  schema_update: '变更注册表',
  ip_ban: '封禁 IP',
  ip_ban_auto: '自动封禁',
  ip_ban_advised: '异常待核查',
  ip_ban_skipped: '回环未处置',
  ip_unban: '解封 IP',
  rate_limited: '触发限流',
  rate_rules_update: '变更限流规则',
  appeal_submit: '提交申诉',
  appeal_accepted: '受理申诉',
  appeal_rejected: '驳回申诉',
}

// actionLabel 操作类型的显示名，未收录时返回原值
export const actionLabel = (action) => ACTION_LABELS[action] ?? action

// formatTime 时间显示，去掉时区后缀便于阅读
export const formatTime = (value) => {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value

  const pad = (n) => String(n).padStart(2, '0')
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
    `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  )
}
