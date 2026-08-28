import { api, callApi } from './config'

// 校验管理员登录入口口令。口令错误时后端返回 404 语义，不暴露入口是否存在。
export const verifyEntry = (entryToken) =>
  callApi(() => api.post('/admin/verify-entry', { entry_token: entryToken }))

// 管理员登录：入口口令需与用户名密码一并提交
export const login = (entryToken, username, password) =>
  callApi(() => api.post('/admin/login', { entry_token: entryToken, username, password }))

// 获取当前访问者身份。未登录来源为 Users 组，以来源 IP 作为标识。
export const getIdentity = () => callApi(() => api.get('/me'))

// 管理员修改自己的密码
export const changePassword = (oldPassword, newPassword) =>
  callApi(() => api.post('/admin/password', { old_password: oldPassword, new_password: newPassword }))
