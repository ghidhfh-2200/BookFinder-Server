import { api, callApi } from '../config'

// 读取监控面板数据：图书馆数、封禁规模、今日访问量与当前在线
export const getDashboard = () => callApi(() => api.get('/admin/dashboard'))
