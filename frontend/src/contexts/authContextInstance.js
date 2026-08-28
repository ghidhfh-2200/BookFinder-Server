import { createContext } from 'react'

// 认证上下文对象。单独成文件，使 AuthContext.jsx 只导出组件，
// 满足 react-refresh 对「一个文件只导出组件」的要求。
// 文件名不能只与 AuthContext.jsx 大小写不同，否则在 Windows 上会解析到同一路径。
export const AuthContext = createContext(null)
