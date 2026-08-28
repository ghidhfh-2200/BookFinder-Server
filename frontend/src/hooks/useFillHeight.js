import { useEffect, useRef, useState } from 'react'

// PAGE_STYLE 页面根节点：纵向撑满内容区，把剩余高度让给表格等可滚动区域
export const PAGE_STYLE = {
  display: 'flex',
  flexDirection: 'column',
  flex: 1,
  minHeight: 0,
}

// TABLE_RESERVE 表格容器内除表体之外的高度：表头 + 分页器 + 卡片留白。
// 从可用高度里扣掉，剩下的才是表体能滚动的区域。
export const TABLE_RESERVE = 120

// useFillHeight 测量容器的可用高度。
// antd 的 Table 需要一个具体像素值才能固定表头、让表体内部滚动，
// 纯 CSS 的 flex 无法喂给它，故用 ResizeObserver 实测。
// reserve 为容器内除滚动区之外要让出的高度（如分页器、工具条）。
export function useFillHeight(reserve = 0) {
  const ref = useRef(null)
  const [height, setHeight] = useState(0)

  useEffect(() => {
    const element = ref.current
    if (!element) return

    const observer = new ResizeObserver(([entry]) => {
      const available = entry.contentRect.height - reserve
      // 低于此值表格就没法用了，留一个下限交给外层滚动
      setHeight(Math.max(available, 120))
    })

    observer.observe(element)
    return () => observer.disconnect()
  }, [reserve])

  return [ref, height]
}
