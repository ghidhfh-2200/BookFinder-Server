import { api, callApi } from '../config'

// 读取系统配置，附带两张日志表的当前规模
export const getSystemConfig = () => callApi(() => api.get('/admin/system/config'))

// 保存系统配置。多数项即时生效，HTTP 超时与并发上限须重启
export const updateSystemConfig = (config) =>
  callApi(() => api.put('/admin/system/config', config))
