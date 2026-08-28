import { api, callApi } from './config'

// 分页查询图书馆，params: { keyword, page, size }
export const getLibraries = (params) => callApi(() => api.get('/libraries', { params }))

// 获取单个图书馆
export const getLibrary = (id) => callApi(() => api.get(`/libraries/${id}`))

// 创建图书馆。created_by 由服务端按来源身份填写，无需前端提供。
export const createLibrary = (data) => callApi(() => api.post('/libraries', data))

// 更新图书馆
export const updateLibrary = (id, data) => callApi(() => api.put(`/libraries/${id}`, data))

// 删除图书馆（仅管理员）
export const deleteLibrary = (id) => callApi(() => api.delete(`/libraries/${id}`))

// 将某条记录中指定字段的信息报告为过时。
// 状态属于字段自身，故按字段报告，不影响同一记录的其他字段。
export const reportFieldOutdated = (id, field) =>
  callApi(() => api.post(`/libraries/${id}/fields/${encodeURIComponent(field)}/report-outdated`))

// 撤销某个字段的过时报告，把状态改回有效
export const revokeFieldOutdated = (id, field) =>
  callApi(() => api.delete(`/libraries/${id}/fields/${encodeURIComponent(field)}/report-outdated`))
