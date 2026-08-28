import { App as AntdApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { BrowserRouter } from 'react-router-dom'
import { AuthProvider } from './contexts/AuthContext'
import { useIsMobile } from './hooks/useIsMobile'
import { mobileThemeConfig, themeConfig } from './theme'
import App from './App.jsx'

// Root 应用外壳：按视口宽度选主题，窄屏用压缩内边距的那份，
// 把有限的横向空间让给表格内容。
export default function Root() {
  const isMobile = useIsMobile()

  return (
    <ConfigProvider locale={zhCN} theme={isMobile ? mobileThemeConfig : themeConfig}>
      <AntdApp>
        <BrowserRouter>
          <AuthProvider>
            <App />
          </AuthProvider>
        </BrowserRouter>
      </AntdApp>
    </ConfigProvider>
  )
}
