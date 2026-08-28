import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    sourcemap: false,
    // 产物输出到 dist，由后端 go:embed frontend/dist 打包
    outDir: 'dist',
    emptyOutDir: true,
    // 不配 manualChunks：打包器已按实际引用把 antd 切成了 table、modal 等小块，
    // 手工聚成一个 vendor 块反而让浏览首页也要下载整个 antd（实测 891 KB）。
    // 剩下的体积警告来自 antd 自身的核心，要根治得换更小的组件库，
    // 而按路由分割已经把首屏该下载的部分降下来了。
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        secure: false,
      },
    },
  },
})
