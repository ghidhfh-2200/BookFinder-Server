// 间距尺度。所有留白从这里取，不在各组件里零散写死。
//
// 原先 4/8/10/12/16/20/32/48 混用，同一层级两侧还常常不等
// （内容区上 16 下 12、卡片外 4 内 12），看上去就是「边距不对称、忽宽忽窄」。
// 统一到 4 的倍数后，同层级的留白必然一致，改也只改一处。
export const SPACE = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 24,
  xxl: 32,
}

// PAGE_PADDING 内容区四周留白。
//
// 上下左右取同一个值，不再上大下小：页面底部还有页脚文字，
// 底部偏小会让页脚贴着内容。窄屏比宽屏窄一档——手机上横向空间本就紧，
// 32px 会吃掉 64px 宽度，而这个应用的主界面正需要横向空间。
export const PAGE_PADDING = {
  mobile: SPACE.lg,
  desktop: SPACE.xl,
}

// CARD_PADDING 卡片内部留白。与卡片之间的间距（CARD_GAP）配对使用：
// 内边距不小于外边距，否则卡片看起来往里缩。
export const CARD_PADDING = SPACE.lg

// CARD_GAP 卡片之间的纵向间距
export const CARD_GAP = SPACE.md

// TOUCH_SIZE 触摸目标的最小边长。
// 低于这个值在手机上容易误触相邻按钮，而这里的误触会提交真实的报告或删除。
export const TOUCH_SIZE = 44
