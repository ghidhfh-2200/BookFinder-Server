import { Spin } from 'antd'

// Spinner 页面级加载占位
export default function Spinner({ tip = '加载中...' }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'center', padding: '80px 0' }}>
      <Spin size="large" tip={tip}>
        <div style={{ width: 120, height: 1 }} />
      </Spin>
    </div>
  )
}
