import { api, callApi } from './config'

// 分页查询图书馆，params: { keyword, page, size }
export const getLibraries = (params) => callApi(() => api.get('/libraries', { params }))

// 获取单个图书馆
export const getLibrary = (id) => callApi(() => api.get(`/libraries/${id}`))

// 创建图书馆。创建者由服务端按访问者令牌记录，前端给了也不采信。
export const createLibrary = (data) => callApi(() => api.post('/libraries', data))

// 更新图书馆
export const updateLibrary = (id, data) => callApi(() => api.put(`/libraries/${id}`, data))

// 删除图书馆。管理员可删任意记录，普通访问者只能删自己创建的
// （能不能删由列表里的 can_delete 给出，真正的拦截在后端）。
export const deleteLibrary = (id) => callApi(() => api.delete(`/libraries/${id}`))

// 将某条记录中指定字段的信息报告为过时。
// 状态属于字段自身，故按字段报告，不影响同一记录的其他字段。
export const reportFieldOutdated = (id, field) =>
  callApi(() => api.post(`/libraries/${id}/fields/${encodeURIComponent(field)}/report-outdated`))

// 撤销某个字段的过时报告，把状态改回有效
export const revokeFieldOutdated = (id, field) =>
  callApi(() => api.delete(`/libraries/${id}/fields/${encodeURIComponent(field)}/report-outdated`))

// 确认某条记录的网站地址可用。新填或改过的网站是「未验证」，
// 攒够阈值（verify_threshold）次独立确认才转为有效。
export const verifyFieldWebsite = (id, field) =>
  callApi(() => api.post(`/libraries/${id}/fields/${encodeURIComponent(field)}/verify`))

// 撤销自己对某个网站的确认。已转正的字段不会因此退回。
export const revokeFieldVerify = (id, field) =>
  callApi(() => api.delete(`/libraries/${id}/fields/${encodeURIComponent(field)}/verify`))
