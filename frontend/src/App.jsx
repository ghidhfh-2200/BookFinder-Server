import { lazy, Suspense } from 'react'
import { Layout, Typography } from 'antd'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import AppHeader from './components/AppHeader'
import AppSider from './components/AppSider'
import RequireAdmin from './components/RequireAdmin'
import Spinner from './components/Spinner'
import LibraryBrowse from './pages/user/LibraryBrowse'
import NotFound from './pages/NotFound'
import Banned from './pages/Banned'
import { useAuth } from './hooks/useAuth'
import { useIsMobile } from './hooks/useIsMobile'
import { CONTENT_BG } from './theme'
import './App.css'

// 管理端页面按需加载：普通访问者只用得到图书馆浏览页，
// 而这几个页面（表格、表单、日志查看器）占了产物的大半。
// RequireAdmin 在非管理员时提前返回、不渲染 children，故这些 chunk
// 不会被无权访问者下载。
const AdminEntry = lazy(() => import('./pages/auth/AdminEntry'))
const BanManagement = lazy(() => import('./pages/admin/BanManagement'))
const Dashboard = lazy(() => import('./pages/admin/Dashboard'))
const LibraryManagement = lazy(() => import('./pages/admin/LibraryManagement'))
const SchemaEditor = lazy(() => import('./pages/admin/SchemaEditor'))
const LogViewer = lazy(() => import('./pages/admin/LogViewer'))
const RateRules = lazy(() => import('./pages/admin/RateRules'))
const SystemConfig = lazy(() => import('./pages/admin/SystemConfig'))
const Profile = lazy(() => import('./pages/admin/Profile'))

export default function App() {
  const { loading, isAdmin, ban } = useAuth()
  const location = useLocation()
  const isMobile = useIsMobile()

  // 管理员入口自带布局，不套用主框架的侧边栏。
  // 放在封禁判断之前：被封时该入口会因接口 403 而呈现 404，
  // 与页面不存在完全一致，被封者因此无法继续探测入口口令。
  if (location.pathname.startsWith('/bookfinder/')) {
    return (
      <Suspense fallback={<Spinner />}>
        <Routes>
          <Route path='/bookfinder/:entryToken' element={<AdminEntry />} />
        </Routes>
      </Suspense>
    )
  }

  // 被封禁时替换整个页面：所有接口都会被拦下，其余界面无从工作。
  // 放在 loading 判断之后，避免身份未定时先闪一下封禁页。
  if (!loading && ban) {
    return <Banned ban={ban} />
  }

  // 管理员的图书馆管理已包含浏览能力，公开浏览页对其不再是独立入口
  const homePath = isAdmin ? '/admin/libraries' : '/libraries'

  return (
    // 整体锁在视窗内：页面不产生滚动条，需要滚动的区域各自在内部滚动
    <Layout style={{ height: '100vh', overflow: 'hidden' }}>
      {/* 普通访问者只有「图书馆」一个入口，侧边栏无意义，改由顶栏承载品牌标识 */}
      {isAdmin && <AppSider />}

      <Layout style={{ minWidth: 0 }}>
        {!isAdmin && <AppHeader />}

        <Layout.Content
          style={{
            flex: 1,
            minHeight: 0,
            display: 'flex',
            flexDirection: 'column',
            // 窄屏收窄左右留白：桌面端的 32px 在手机上会吃掉 64px 宽度，
            // 而表格本就需要尽可能多的横向空间
            padding: isMobile ? '16px 12px 12px' : '24px 32px 16px',
            background: CONTENT_BG,
            maxWidth: 1360,
            width: '100%',
            margin: '0 auto',
          }}
        >
          {/* 页面区可被压缩，页面自身决定内部哪块滚动 */}
          <div
            style={{
              flex: 1,
              minHeight: 0,
              display: 'flex',
              flexDirection: 'column',
            }}
          >
            {loading ? (
              <Spinner />
            ) : (
              <Suspense fallback={<Spinner />}>
                <Routes>
                  <Route
                    path='/'
                    element={<Navigate to={homePath} replace />}
                  />
                  <Route
                    path='/libraries'
                    element={
                      isAdmin ? (
                        <Navigate to='/admin/libraries' replace />
                      ) : (
                        <LibraryBrowse />
                      )
                    }
                  />

                  <Route
                    path='/admin/dashboard'
                    element={
                      <RequireAdmin>
                        <Dashboard />
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path='/admin/libraries'
                    element={
                      <RequireAdmin>
                        <LibraryManagement />
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path='/admin/schema'
                    element={
                      <RequireAdmin>
                        <SchemaEditor />
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path='/admin/bans'
                    element={
                      <RequireAdmin>
                        <BanManagement />
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path='/admin/rate-rules'
                    element={
                      <RequireAdmin>
                        <RateRules />
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path='/admin/system'
                    element={
                      <RequireAdmin>
                        <SystemConfig />
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path='/admin/logs'
                    element={
                      <RequireAdmin>
                        <LogViewer />
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path='/admin/profile'
                    element={
                      <RequireAdmin>
                        <Profile />
                      </RequireAdmin>
                    }
                  />

                  <Route path='*' element={<NotFound />} />
                </Routes>
              </Suspense>
            )}
          </div>

          {/* 页脚并入内容区，省下 Layout.Footer 的固定高度 */}
          <Typography.Text
            type='secondary'
            style={{
              textAlign: 'center',
              fontSize: 12,
              paddingTop: 12,
              flexShrink: 0,
            }}
          >
            BookFinder ©{new Date().getFullYear()}
          </Typography.Text>
        </Layout.Content>
      </Layout>
    </Layout>
  )
}
