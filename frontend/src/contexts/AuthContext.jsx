import { useCallback, useEffect, useMemo, useState } from 'react'
import { AuthContext } from './authContextInstance'
import { getIdentity, login as loginApi } from '../api/auth'
import { clearToken, setToken } from '../utils/storage'
import { hasPermission as checkPermission } from '../utils/permissions'

// AuthProvider 维护当前访问者身份。
// 管理员持有 JWT（存 localStorage），Users 组没有令牌，
// 身份由后端按来源 IP 判定，因此无论有无令牌都要拉一次 /api/me。
export function AuthProvider({ children }) {
  const [identity, setIdentity] = useState(null)
  const [ban, setBan] = useState(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    const resp = await getIdentity()

    // 封禁在身份识别之后拦下所有接口，故这里就能拿到封禁详情
    if (resp.data?.banned) {
      setBan(resp.data)
      setIdentity(null)
      setLoading(false)
      return resp
    }

    setBan(null)
    // 取不到身份时按最小权限处理，不擅自假定为 Users 组权限
    setIdentity(resp.code === 200 ? resp.data : null)
    setLoading(false)
    return resp
  }, [])

  useEffect(() => {
    // 包在异步函数里调用，避免在 effect 体内同步 setState
    const load = async () => {
      await refresh()
    }
    load()
  }, [refresh])

  const login = useCallback(
    async (entryToken, username, password) => {
      const resp = await loginApi(entryToken, username, password)
      if (resp.code === 200) {
        setToken(resp.data.token)
        await refresh()
      }
      return resp
    },
    [refresh],
  )

  const logout = useCallback(async () => {
    // 令牌为无状态 JWT，登出即丢弃本地令牌；服务端不保存会话
    clearToken()
    await refresh()
  }, [refresh])

  const value = useMemo(
    () => ({
      identity,
      // ban 非空表示当前来源已被封禁，页面替换为封禁提示
      ban,
      loading,
      isAdmin: identity?.role === 'admin',
      permission: identity?.permission ?? 0,
      hasPermission: (target) => checkPermission(identity?.permission ?? 0, target),
      login,
      logout,
      refresh,
    }),
    [identity, ban, loading, login, logout, refresh],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
