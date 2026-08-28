import { api, callApi } from '../config'

// 读取限流与自动封禁规则
export const getRateRules = () => callApi(() => api.get('/admin/rate-rules'))

// 保存限流规则，保存后立即热生效
export const updateRateRules = (rules) => callApi(() => api.put('/admin/rate-rules', rules))
