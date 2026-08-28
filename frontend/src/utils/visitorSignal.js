// 访问者的设备特征信号，仅作后端启发式查重的辅助。
// 身份以服务端下发的 HttpOnly cookie 令牌为准：这里的信号在客户端计算后上报，
// 可被伪造，同型号同系统的设备也常算出相同结果，故不能作为身份凭证。
// 自己算一份轻量信号而不引入 FingerprintJS 之类的库：只用于提示，不值得增加打包体积。
//
// 文件与导出名都避开 fingerprint 一词：内容拦截扩展会按 URL 里的该关键词
// 拦掉整个模块请求，导致开发模式下加载失败。

// collect 采集若干相对稳定的浏览器特征
const collect = () => {
  const nav = window.navigator
  const screen = window.screen

  return [
    nav.userAgent,
    nav.language,
    (nav.languages || []).join(','),
    nav.hardwareConcurrency ?? '',
    nav.maxTouchPoints ?? '',
    nav.platform ?? '',
    screen.colorDepth,
    `${screen.width}x${screen.height}`,
    new Date().getTimezoneOffset(),
    Intl.DateTimeFormat().resolvedOptions().timeZone ?? '',
  ].join('|')
}

// sha256Hex 用 Web Crypto 算哈希
const sha256Hex = async (text) => {
  const data = new TextEncoder().encode(text)
  const digest = await window.crypto.subtle.digest('SHA-256', data)
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

let cached = null

// getVisitorSignal 返回信号哈希。算不出来时返回空串，后端会跳过启发式查重。
export const getVisitorSignal = async () => {
  if (cached !== null) return cached

  try {
    cached = await sha256Hex(collect())
  } catch {
    // crypto.subtle 仅在安全上下文可用（HTTPS 或 localhost）
    cached = ''
  }

  return cached
}
