import { theme } from 'antd'

// 主色。集中在此，改配色只需动这一处。
export const PRIMARY_COLOR = '#2563eb'

// 页面底色，比卡片的白略深，用于拉开层次
export const CONTENT_BG = '#f6f7f9'

// 卡片与侧边栏的表面色
export const SURFACE_BG = '#ffffff'
export const BORDER_COLOR = '#e8eaed'

// 正文与次要文字
export const TEXT_COLOR = '#18181b'
export const TEXT_SECONDARY = '#71717a'

const FONT_FAMILY =
  "-apple-system, BlinkMacSystemFont, 'SF Pro Text', 'Segoe UI Variable Text', 'Segoe UI', " +
  "Roboto, 'PingFang SC', 'Microsoft YaHei', 'Helvetica Neue', Arial, sans-serif"

// 浅色主题。样式一律通过 token 表达，不在 CSS 里覆盖 antd 内部类名：
// 那样会与组件实现耦合，antd 升级即失效。
export const themeConfig = {
  algorithm: theme.defaultAlgorithm,
  token: {
    colorPrimary: PRIMARY_COLOR,
    colorLink: PRIMARY_COLOR,
    colorBgLayout: CONTENT_BG,
    colorText: TEXT_COLOR,
    colorTextSecondary: TEXT_SECONDARY,
    colorBorderSecondary: BORDER_COLOR,
    borderRadius: 10,
    fontFamily: FONT_FAMILY,
    fontSize: 14,
    lineHeight: 1.6,
    controlHeight: 36,
    // 更轻的阴影，避免默认的厚重投影
    boxShadowTertiary:
      '0 1px 2px rgba(16, 24, 40, 0.04), 0 1px 3px rgba(16, 24, 40, 0.06)',
  },
  components: {
    Layout: {
      bodyBg: CONTENT_BG,
      siderBg: SURFACE_BG,
      footerBg: 'transparent',
    },
    Card: {
      // 卡片头不再用灰底，靠字重与分隔线区分
      headerBg: 'transparent',
      headerFontSize: 16,
      paddingLG: 20,
    },
    Menu: {
      itemSelectedColor: PRIMARY_COLOR,
      itemSelectedBg: '#eff4ff',
      itemBg: 'transparent',
      itemBorderRadius: 8,
      itemMarginInline: 8,
      itemHeight: 38,
    },
    Table: {
      headerBg: '#fafafa',
      headerColor: TEXT_SECONDARY,
      headerSplitColor: 'transparent',
      rowHoverBg: '#f8fafc',
      borderColor: BORDER_COLOR,
      cellPaddingBlock: 12,
      // 单元格左右内边距。窄屏多列时这一项能省下可观的横向空间，
      // 故由 ConfigProvider 按视口宽度覆盖（见 main.jsx），而不是写死。
      cellPaddingInline: 16,
    },
    Button: {
      borderRadius: 8,
      fontWeight: 500,
      primaryShadow: 'none',
      defaultShadow: 'none',
    },
    Input: {
      borderRadius: 8,
    },
    Select: {
      borderRadius: 8,
    },
    Modal: {
      borderRadiusLG: 14,
      headerBg: 'transparent',
      titleFontSize: 17,
    },
    Tag: {
      borderRadiusSM: 6,
    },
    Segmented: {
      borderRadius: 8,
      itemSelectedBg: SURFACE_BG,
    },
  },
}

// mobileThemeConfig 窄屏下的主题：压缩各处内边距，把宽度让给内容。
// 表格单元格与卡片是主要受益处——多列表格在手机上每一像素都要紧。
export const mobileThemeConfig = {
  ...themeConfig,
  components: {
    ...themeConfig.components,
    Table: {
      ...themeConfig.components.Table,
      cellPaddingInline: 8,
      cellPaddingBlock: 10,
    },
    Card: {
      ...themeConfig.components.Card,
      paddingLG: 12,
    },
  },
}
