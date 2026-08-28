import { useEffect, useState } from 'react'

// MOBILE_BREAKPOINT 移动端断点，与 App.css 中的媒体查询保持一致
export const MOBILE_BREAKPOINT = 768

// useIsMobile 视口是否处于移动端宽度。
// 表格的固定列在窄屏会挤掉本就不多的可滚动区域，需据此关掉，
// 而固定列只能在 JS 里配置，无法用 CSS 媒体查询解决。
export function useIsMobile(breakpoint = MOBILE_BREAKPOINT) {
  const query = `(max-width: ${breakpoint}px)`

  // 初始值直接查一次，避免首帧按桌面布局渲染再跳变
  const [isMobile, setIsMobile] = useState(
    () => typeof window !== 'undefined' && window.matchMedia(query).matches,
  )

  useEffect(() => {
    const media = window.matchMedia(query)
    const update = (event) => setIsMobile(event.matches)

    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [query])

  return isMobile
}
