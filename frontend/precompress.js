import { readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { extname, join, resolve } from 'node:path'
import { brotliCompressSync, constants, gzipSync } from 'node:zlib'

// 构建期预压缩：为每个可压缩产物额外生成一份 .br 与一份 .gz，
// 由后端按请求的 Accept-Encoding 挑一份发出（见 api/routes/static.go）。
//
// 压缩放在构建期而不是请求时，因为产物要内嵌进二进制、内容此后再不改变：
// 每个请求现压一遍等于把同一份结果算无数次。这样还能用上 brotli 的最高档
// q11——那一档压首屏最大的那个 chunk 要一秒多，不可能放进请求路径。
//
// 用 node:zlib 而不引第三方插件：brotli 与 gzip 都在标准库里，
// 这点功能不值得多一个依赖。

// 只压文本类。png/woff/ico 这些本身已是压缩格式，再压一遍通常还会变大。
const COMPRESSIBLE = new Set([
  '.js', '.mjs', '.css', '.html', '.svg', '.json', '.txt', '.xml',
])

// 小于这个尺寸就不压：省下的字节数与一个响应头的体量相当，
// 却要多一份文件占二进制体积。
const MIN_BYTES = 1024

// walk 递归列出目录下的全部文件
const walk = (dir) => {
  const files = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) {
      files.push(...walk(full))
    } else if (entry.isFile()) {
      files.push(full)
    }
  }
  return files
}

// compressed 生成两份压缩产物。
// 压完反而更大的就不写——那种情况下后端找不到产物，自然回落到原文件。
const compressed = (source) => [
  {
    suffix: '.br',
    data: brotliCompressSync(source, {
      params: {
        [constants.BROTLI_PARAM_QUALITY]: constants.BROTLI_MAX_QUALITY,
        // 给出大小提示，brotli 据此选窗口，比让它自己猜压得更小
        [constants.BROTLI_PARAM_SIZE_HINT]: source.length,
      },
    }),
  },
  { suffix: '.gz', data: gzipSync(source, { level: 9 }) },
]

// kb 便于阅读的体积
const kb = (bytes) => `${(bytes / 1024).toFixed(1)} KB`

export default function precompress() {
  let outDir = ''

  return {
    name: 'bookfinder-precompress',
    // 只在构建时跑：dev 模式没有产物文件，也没有内嵌这回事
    apply: 'build',

    configResolved(config) {
      outDir = resolve(config.root, config.build.outDir)
    },

    // 用 closeBundle 而非 writeBundle：public/ 下的文件（favicon.svg 等）
    // 由 Vite 另行拷贝、不在 bundle 对象里，得等全部写完再扫一遍目录。
    closeBundle() {
      let count = 0
      let raw = 0
      let br = 0
      let gz = 0

      for (const file of walk(outDir)) {
        if (!COMPRESSIBLE.has(extname(file))) continue
        if (statSync(file).size < MIN_BYTES) continue

        const source = readFileSync(file)
        let wrote = false

        for (const { suffix, data } of compressed(source)) {
          if (data.length >= source.length) continue
          writeFileSync(file + suffix, data)
          wrote = true
          if (suffix === '.br') br += data.length
          else gz += data.length
        }

        if (wrote) {
          count += 1
          raw += source.length
        }
      }

      console.log(
        `预压缩 ${count} 个文件：原始 ${kb(raw)}，` +
          `brotli ${kb(br)}，gzip ${kb(gz)}`,
      )
    },
  }
}
