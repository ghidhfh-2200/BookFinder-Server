import { api, callApi } from '../config'

// 分页查询用户操作日志，params: { user, action, level, page, size }
export const getOperationLogs = (params) =>
  callApi(() => api.get('/admin/logs/operations', { params }))

// 分页查询应用运行日志，params: { level, keyword, page, size }
export const getAppLogs = (params) => callApi(() => api.get('/admin/logs/app', { params }))

// 筛选项：已出现的操作类型与可用等级
export const getLogMeta = () => callApi(() => api.get('/admin/logs/meta'))
