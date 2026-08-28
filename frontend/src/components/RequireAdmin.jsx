import { Button, Result } from 'antd'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import Spinner from './Spinner'

// RequireAdmin 管理员路由守卫。
// 不重定向到登录页：登录入口需要口令，未持有口令者无从跳转。
export default function RequireAdmin({ children }) {
  const { isAdmin, loading } = useAuth()
  const navigate = useNavigate()

  if (loading) {
    return <Spinner />
  }

  if (!isAdmin) {
    return (
      <Result
        status="403"
        title="403"
        subTitle="需要管理员身份，请通过管理员入口登录。"
        extra={
          <Button type="primary" onClick={() => navigate('/libraries')}>
            返回图书馆
          </Button>
        }
      />
    )
  }

  return children
}
