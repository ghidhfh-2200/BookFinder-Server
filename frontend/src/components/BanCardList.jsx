import { Button, Card, Empty, List, Popconfirm, Spin, Tag, Typography } from 'antd'
import { MailOutlined } from '@ant-design/icons'
import BanIdentTags from './BanIdentTags'
import { CARD_GAP, CARD_PADDING, SPACE, TOUCH_SIZE } from '../spacing'
import { BORDER_COLOR } from '../theme'

// BanCardList 封禁列表的窄屏形态。
//
// 表格共七列、合计约 990px，在手机上要横滚近三屏。改成卡片后一屏读完一条：
// 标识与来源在标题行，原因与触发详情纵向列出，解封与申诉在底部。
export default function BanCardList({ bans, loading, onUnban, onOpenAppeal }) {
  if (loading) {
    return (
      <div style={{ padding: SPACE.xxl, textAlign: 'center' }}>
        <Spin />
      </div>
    )
  }

  if (bans.length === 0) {
    return <Empty description="没有封禁记录" style={{ padding: SPACE.xxl }} />
  }

  return (
    <List
      dataSource={bans}
      pagination={false}
      renderItem={(record) => {
        const total = record.appeals?.total ?? 0
        const pending = record.appeals?.pending ?? 0
        const ip = record.ips?.[0]
        const hasAppeals = total > 0 && ip

        return (
          <Card
            size="small"
            style={{ marginBottom: CARD_GAP }}
            styles={{
              header: { padding: `${SPACE.md}px ${CARD_PADDING}px`, minHeight: 0 },
              body: { padding: CARD_PADDING },
            }}
            title={
              // 来源在左，封禁时间在右：这两项最短，并排不会挤
              <div style={{ display: 'flex', alignItems: 'center', gap: SPACE.sm }}>
                {record.source === 'auto' ? (
                  <Tag bordered={false} color="volcano" style={{ marginInlineEnd: 0 }}>
                    自动
                  </Tag>
                ) : (
                  <Tag bordered={false} style={{ marginInlineEnd: 0 }}>
                    手动
                  </Tag>
                )}
                <Typography.Text type="secondary" style={{ fontSize: 12, marginLeft: 'auto' }}>
                  {record.created_at ? new Date(record.created_at).toLocaleString() : '-'}
                </Typography.Text>
              </div>
            }
          >
            {/* 标识是这条记录的主体，放在最前。网段会被显著标注，
                免得误以为只封了一个地址 */}
            <div style={{ marginBottom: SPACE.md }}>
              <Typography.Text
                type="secondary"
                style={{ fontSize: 12, display: 'block', marginBottom: 4 }}
              >
                封禁标识
              </Typography.Text>
              <BanIdentTags idents={record.idents} />
            </div>

            <div style={{ marginBottom: record.detail ? SPACE.md : 0 }}>
              <Typography.Text
                type="secondary"
                style={{ fontSize: 12, display: 'block', marginBottom: 2 }}
              >
                原因
              </Typography.Text>
              <Typography.Text>{record.reason || '-'}</Typography.Text>
            </div>

            {/* 触发详情只在有值时出现：自动封禁才有，手动封禁列一个「-」是噪音。
                不截断——窄屏上这是复核误判的主要依据，Tooltip 在触屏上也难用 */}
            {record.detail && (
              <div>
                <Typography.Text
                  type="secondary"
                  style={{ fontSize: 12, display: 'block', marginBottom: 2 }}
                >
                  触发详情
                </Typography.Text>
                <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                  {record.detail}
                </Typography.Text>
              </div>
            )}

            <div
              style={{
                display: 'flex',
                gap: SPACE.sm,
                paddingTop: CARD_PADDING,
                marginTop: CARD_PADDING,
                borderTop: `1px solid ${BORDER_COLOR}`,
              }}
            >
              {hasAppeals && (
                <Button
                  icon={<MailOutlined />}
                  onClick={() => onOpenAppeal(ip)}
                  style={{ height: TOUCH_SIZE, flex: 1 }}
                >
                  {pending > 0 ? `申诉 ${total}（${pending} 待处理）` : `申诉 ${total}`}
                </Button>
              )}

              <Popconfirm
                title="确认解封？"
                description={`将解除该主体的全部 ${record.idents?.length ?? 0} 个标识`}
                okText="解封"
                cancelText="取消"
                onConfirm={() => onUnban(record.id)}
              >
                <Button style={{ height: TOUCH_SIZE, flex: 1 }}>解封</Button>
              </Popconfirm>
            </div>
          </Card>
        )
      }}
    />
  )
}
