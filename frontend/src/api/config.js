import axios from 'axios'
import { getVisitorSignal } from '../utils/visitorSignal'

// 创建 axios 实例，baseURL 走 vite 代理转发到后端
const createApiInstance = (baseURL = '/api') => {
  const instance = axios.create({
    baseURL,
    timeout: 10000,
    // 带上服务端下发的访问者令牌 cookie，报告过时据此按人去重
    withCredentials: true,
    headers: {
      'Content-Type': 'application/json',
    },
  })

  // 请求拦截器：附加认证 token 与浏览器指纹
  instance.interceptors.request.use(
    async (config) => {
      const token = localStorage.getItem('token')
      if (token && config.headers) {
        config.headers.Authorization = `Bearer ${token}`
      }

      // 设备特征仅作后端启发式查重的辅助信号，身份以服务端下发的 cookie 令牌为准
      if (config.headers) {
        const signal = await getVisitorSignal()
        if (signal) {
          config.headers['X-Visitor-Signal'] = signal
        }
      }

      return config
    },
    (error) => Promise.reject(error),
  )

  // 响应拦截器：把网络/HTTP 错误统一成后端的响应结构，交由调用方按 code 处理
  instance.interceptors.response.use(
    (response) => response,
    (error) => {
      if (error.response) {
        const { status, data } = error.response
        return Promise.resolve({
          ...error.response,
          data: {
            code: status,
            message: data?.message || `请求失败 (${status})`,
            data: null,
          },
        })
      }
      if (error.request) {
        return Promise.resolve({
          data: { code: 0, message: '网络连接失败，请检查网络', data: null },
        })
      }
      return Promise.resolve({
        data: { code: 0, message: error.message || '未知错误', data: null },
      })
    },
  )

  return instance
}

export const api = createApiInstance()

// API 调用工具函数：返回后端的 { code, message, data } 结构
export const callApi = async (apiCall) => {
  const response = await apiCall()
  return response.data
}
