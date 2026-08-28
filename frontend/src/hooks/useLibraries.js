import { useCallback, useEffect, useRef, useState } from 'react'
import { App } from 'antd'
import { getLibraries } from '../api/library'

// useLibraries 图书馆列表的分页与搜索状态。
// 管理员页与公开浏览页共用同一份取数逻辑。
export function useLibraries(initialSize = 20) {
  const { message } = App.useApp()
  const [libraries, setLibraries] = useState([])
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)
  const [pagination, setPagination] = useState({ current: 1, pageSize: initialSize, total: 0 })

  // 用 ref 保存当前查询条件，避免 fetch 回调因依赖变化被反复重建
  const queryRef = useRef({ keyword: '', page: 1, size: initialSize })

  const fetchLibraries = useCallback(async (overrides = {}) => {
    const next = { ...queryRef.current, ...overrides }
    queryRef.current = next

    setLoading(true)
    const resp = await getLibraries({
      keyword: next.keyword,
      page: next.page,
      size: next.size,
    })
    setLoading(false)

    if (resp.code !== 200) {
      message.error(resp.message)
      setError(resp.message)
      return
    }

    setError(null)
    setLibraries(resp.data || [])
    setPagination({ current: resp.page, pageSize: resp.size, total: resp.total })
  }, [message])

  useEffect(() => {
    // 包在异步函数里调用，避免在 effect 体内同步 setState
    const load = async () => {
      await fetchLibraries()
    }
    load()
  }, [fetchLibraries])

  const search = useCallback(
    (keyword) => fetchLibraries({ keyword, page: 1 }),
    [fetchLibraries],
  )

  const changePage = useCallback(
    (page, size) => fetchLibraries({ page, size }),
    [fetchLibraries],
  )

  const reload = useCallback(() => fetchLibraries(), [fetchLibraries])

  return { libraries, error, loading, pagination, search, changePage, reload }
}
