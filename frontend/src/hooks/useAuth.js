import { useContext } from 'react'
import { AuthContext } from '../contexts/authContextInstance'

// useAuth 读取当前访问者身份与权限
export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth 必须在 AuthProvider 内使用')
  }
  return ctx
}
