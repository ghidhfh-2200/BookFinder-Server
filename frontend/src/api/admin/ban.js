import { api, callApi } from '../config'

// 获取所有封禁记录。
// 封禁挂在「主体」上，一个主体可有多个标识（IP、网段、访问者令牌、设备标识），
// 故返回的每条记录带 idents 数组，解封按主体 id 而非按 IP。
export const getBans = () => callApi(() => api.get('/admin/bans'))

// 封禁来源 IP。banNetwork 为真时一并封禁其所属网段（IPv6 /64、IPv4 /24）。
// 网段封禁会连坐同段的其他人，故默认只封精确地址，由管理员明确勾选。
export const banIP = (ip, reason, banNetwork = false) =>
  callApi(() => api.post('/admin/bans', { ip, reason, ban_network: banNetwork }))

// 解封：删除该主体及其全部标识。
// 按主体解封而非按单个标识——主体下还挂着网段与令牌，只解一项人依然进不来。
export const unbanSubject = (id) => callApi(() => api.delete(`/admin/bans/${id}`))
