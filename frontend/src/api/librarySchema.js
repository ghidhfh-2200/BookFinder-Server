import { api, callApi } from './config'

// 读取字段注册表。前端据此动态渲染表格与表单，不硬编码任何字段名。
export const getLibrarySchema = () => callApi(() => api.get('/library-schema'))

// 保存字段注册表（仅管理员）。字段名只能增删不能改，显示名与类型可改。
export const updateLibrarySchema = (fields) =>
  callApi(() => api.put('/admin/library-schema', { fields }))
