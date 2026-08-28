import { Space, Tag, Tooltip, Typography } from 'antd'

// 各类封禁标识的展示方式。
//
// 网段用醒目的颜色并注明会连坐：否则管理员容易把 "2001:db8::/64" 看成一个普通地址，
// 意识不到同段的其他人也被挡住了。
const KIND_META = {
  ip: { color: 'red', label: 'IP', hint: '精确来源地址' },
  ip_net: {
    color: 'volcano',
    label: '网段',
    hint: '该网段内的所有地址都会被拦下（同段的其他人也会受影响，可用屏蔽名单单独放行）',
  },
  visitor: {
    color: 'purple',
    label: '访问者',
    hint: '访问者令牌哈希：浏览器与安卓端共用同一套令牌，故换 IP 后仍会命中',
  },
  device: {
    color: 'geekblue',
    label: '设备',
    hint: '安卓设备标识哈希：卸载重装后通常仍在，仅在请求签名校验通过时采信',
  },
}

// shorten 截短哈希类标识：64 个字符全铺出来会挤掉整张表格
const shorten = (value) => (value.length > 20 ? `${value.slice(0, 12)}…` : value)

// BanIdentTags 列出一个封禁主体的全部标识。
// 任一标识命中即视为该主体，故这里展示的是「拦下这个人的全部途径」。
export default function BanIdentTags({ idents }) {
  if (!idents?.length) {
    return <Typography.Text type="secondary">无标识</Typography.Text>
  }

  return (
    <Space size={[4, 4]} wrap>
      {idents.map((ident) => {
        const meta = KIND_META[ident.kind] ?? {
          color: 'default',
          label: ident.kind,
          hint: '未知标识种类',
        }
        const isHash = ident.kind === 'visitor' || ident.kind === 'device'

        return (
          <Tooltip key={ident.id} title={`${meta.hint}${isHash ? `\n完整值：${ident.value}` : ''}`}>
            <Tag bordered={false} color={meta.color} style={{ marginInlineEnd: 0 }}>
              {meta.label} {shorten(ident.value)}
            </Tag>
          </Tooltip>
        )
      })}
    </Space>
  )
}
