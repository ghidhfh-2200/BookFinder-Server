// 令牌存储。只有管理员持有 JWT，Users 组没有令牌，
// 身份一律由后端按来源 IP 判定，因此这里为空是正常状态。
const TOKEN_KEY = 'token'

export const getToken = () => localStorage.getItem(TOKEN_KEY)

export const setToken = (token) => localStorage.setItem(TOKEN_KEY, token)

export const clearToken = () => localStorage.removeItem(TOKEN_KEY)
