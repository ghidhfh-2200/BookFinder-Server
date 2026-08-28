import { api, callApi } from './config'

// 当前来源的申诉配额，供封禁页判断是否还能提交
export const getAppealQuota = () => callApi(() => api.get('/appeal/quota'))

// 提交申诉。内容为纯文本，后端会剥离控制字符后入库。
export const submitAppeal = (message) => callApi(() => api.post('/appeal', { message }))

// 查看某个 IP 的申诉详情（管理员）。申诉按 IP 记录，故仍以 IP 为参数。
export const getAppealsByIP = (ip) =>
  callApi(() => api.get(`/admin/bans/ip/${encodeURIComponent(ip)}/appeals`))

// 处理申诉（管理员）：受理会一并解封
export const reviewAppeal = (id, status, adminNote) =>
  callApi(() => api.put(`/admin/appeals/${id}`, { status, admin_note: adminNote }))
