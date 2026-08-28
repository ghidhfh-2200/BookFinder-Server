import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import Spinner from '../../components/Spinner'
import NotFound from '../NotFound'
import Login from './Login'
import { verifyEntry } from '../../api/auth'

// AdminEntry 管理员登录入口 /bookfinder/:entryToken。
// 先由后端校验口令，通过才渲染登录界面；否则与不存在的页面表现一致。
export default function AdminEntry() {
  const { entryToken } = useParams()
  const [state, setState] = useState('checking')

  useEffect(() => {
    let cancelled = false

    // 包在异步函数里调用，避免在 effect 体内同步 setState
    const check = async () => {
      const resp = await verifyEntry(entryToken)
      if (cancelled) return
      setState(resp.code === 200 ? 'valid' : 'invalid')
    }
    check()

    return () => {
      cancelled = true
    }
  }, [entryToken])

  if (state === 'checking') {
    return <Spinner tip="校验中..." />
  }

  // 口令错误时呈现 404，不提示「口令错误」，避免确认入口存在
  if (state === 'invalid') {
    return <NotFound />
  }

  return <Login entryToken={entryToken} />
}
