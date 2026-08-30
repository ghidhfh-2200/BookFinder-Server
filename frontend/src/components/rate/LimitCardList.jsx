import { InputNumber, Typography } from 'antd'
import { BORDER_COLOR } from '../../theme'
import { SPACE } from '../../spacing'

// LimitCardList 各类别配额的窄屏编辑形态。
//
// 表格四列合计约 650px，且每格都是数字输入框——横滚着改表单没法用。
// 改成按类别分组：类别名作小标题，三个输入项在下方一行排开。
//
// 三项并排而不是各占一行：它们都是短数字，纵向堆叠会让一个类别占掉大半屏，
// 而这一页共六个类别，那样翻起来太长。
export default function LimitCardList({ categories, limits, labels, onPatch }) {
  return (
    <div>
      {categories.map((category, index) => {
        const limit = limits[category] ?? {}
        const burstInvalid = limit.burst >= limit.daily

        return (
          <div
            key={category}
            style={{
              paddingTop: index === 0 ? 0 : SPACE.lg,
              marginTop: index === 0 ? 0 : SPACE.lg,
              borderTop: index === 0 ? undefined : `1px solid ${BORDER_COLOR}`,
            }}
          >
            <Typography.Text strong style={{ display: 'block', marginBottom: SPACE.sm }}>
              {labels[category] ?? category}
            </Typography.Text>

            <div style={{ display: 'flex', gap: SPACE.sm }}>
              <Field label="每日配额">
                <InputNumber
                  min={1}
                  value={limit.daily}
                  style={{ width: '100%' }}
                  onChange={(next) => onPatch(category, { daily: next ?? 1 })}
                />
              </Field>

              <Field label="突发次数">
                <InputNumber
                  min={1}
                  max={Math.max((limit.daily ?? 1) - 1, 1)}
                  value={limit.burst}
                  status={burstInvalid ? 'error' : undefined}
                  style={{ width: '100%' }}
                  onChange={(next) => onPatch(category, { burst: next ?? 1 })}
                />
              </Field>

              <Field label="窗口（秒）">
                <InputNumber
                  min={1}
                  max={3600}
                  value={limit.burst_window_seconds}
                  style={{ width: '100%' }}
                  onChange={(next) => onPatch(category, { burst_window_seconds: next ?? 60 })}
                />
              </Field>
            </div>
          </div>
        )
      })}
    </div>
  )
}

// Field 一个带标签的输入项，三者等宽
function Field({ label, children }) {
  return (
    <div style={{ flex: 1, minWidth: 0 }}>
      <Typography.Text
        type="secondary"
        style={{ fontSize: 12, display: 'block', marginBottom: 2 }}
      >
        {label}
      </Typography.Text>
      {children}
    </div>
  )
}
