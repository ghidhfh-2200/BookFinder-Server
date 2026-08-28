import { useCallback, useEffect, useState } from 'react'
import { App } from 'antd'
import { getLibrarySchema } from '../api/librarySchema'

// cached 已取到的注册表，在页面间共享。
// 注册表极少变动，而每次请求都消耗一次 read 配额（一次页面加载本就有两个 read 请求），
// 故缓存到内存里，页面切换时直接复用；刷新页面或显式 reload 会重新拉取。
let cached = null

// useLibrarySchema 字段注册表。
// 表格列与表单项都由它推导，字段名一律来自后端，前端不硬编码。
export function useLibrarySchema() {
  const { message } = App.useApp()
  const [schema, setSchema] = useState(cached)
  const [error, setError] = useState(null)
  // 已有缓存时无需等待请求，直接进入就绪状态
  const [loading, setLoading] = useState(cached == null)

  const fetchSchema = useCallback(async () => {
    setLoading(true)
    const resp = await getLibrarySchema()
    setLoading(false)

    if (resp.code !== 200) {
      message.error(resp.message)
      // 记下失败原因：注册表拿不到时表格没有列可渲染，
      // 页面需据此给出提示，而不是显示一张只剩 ID 的空表
      setError(resp.message)
      return
    }

    setError(null)
    cached = resp.data
    setSchema(resp.data)
  }, [message])

  useEffect(() => {
    // 已有缓存就不再请求，省下一次 read 配额
    if (cached != null) return

    const load = async () => {
      await fetchSchema()
    }
    load()
  }, [fetchSchema])

  return {
    fields: schema?.fields ?? [],
    // summaryFields 作为表格列的字段名。由后端给出，含「一个都没勾时回落到
    // 记录名」的规则——前端再筛一遍就会与后端各错一次。
    summaryFields: schema?.summary_fields ?? [],
    searchNameField: schema?.search_name_field ?? '',
    types: schema?.types ?? [],
    statuses: schema?.statuses ?? [],
    // ready 表示注册表已就绪，可以据此渲染表格
    ready: schema != null,
    error,
    loading,
    // reload 会一并更新共享缓存，故注册表编辑器保存后调用它即可让各页面拿到新字段
    reload: fetchSchema,
  }
}

// displayName 字段显示名，未设则回落到字段名
export const displayName = (field) => field.label || field.name

// emptyValueFor 按类型给出空值，与后端的 EmptyValue 保持一致
export const emptyValueFor = (type) => {
  switch (type) {
    case 'number':
      return 0
    case 'bool':
      return false
    case 'object':
      return {}
    case 'array':
      return []
    default:
      return ''
  }
}
