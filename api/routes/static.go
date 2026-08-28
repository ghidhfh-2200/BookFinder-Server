package routes

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// 静态资源服务。
//
// 产物在构建期已被压好一份 .br 与一份 .gz（见 frontend/precompress.js），
// 此处只按请求的 Accept-Encoding 挑一份发出。压缩不放在请求路径上：
// 内嵌产物的内容此后再不改变，每个请求现压一遍是把同一份结果算无数次，
// 而离线压缩还能用上 brotli 的最高档。
//
// 找不到预压缩产物时回落到原文件，故 precompress 没跑过也只是慢、不会坏。

// encoding 一种可选的内容编码。
type encoding struct {
	// name 用于 Content-Encoding 响应头与 Accept-Encoding 匹配
	name string
	// ext 预压缩产物相对原文件的扩展名后缀
	ext string
}

// encodings 按优先级排列：brotli 在前，同一份产物它通常比 gzip 再小一成半。
var encodings = []encoding{
	{name: "br", ext: ".br"},
	{name: "gzip", ext: ".gz"},
}

// staticContentTypes 本项目实际会发出的静态类型。
//
// 不直接用 mime.TypeByExtension：它在 Windows 上查注册表，而注册表里的
// .js 常被别的软件改成 text/plain 之类——那会让浏览器拒绝执行脚本，
// 且只在某些开发机上复现。产物类型就这几种，显式列出更可靠。
var staticContentTypes = map[string]string{
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".txt":   "text/plain; charset=utf-8",
	".xml":   "text/xml; charset=utf-8",
	".ico":   "image/x-icon",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".webp":  "image/webp",
	".woff":  "font/woff",
	".woff2": "font/woff2",
}

// acceptsEncoding 判断客户端是否接受某种编码。
//
// 必须看 q 值：`br;q=0` 是明确拒绝，当成接受会发出对方解不开的响应体。
// 只认明确列出的编码，不展开 `*` 通配——浏览器一律逐个列出 gzip 与 br，
// 而对通配的宽松解读会把「没提到」误判成「接受」。
func acceptsEncoding(header, name string) bool {
	for part := range strings.SplitSeq(header, ",") {
		fields := strings.Split(part, ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), name) {
			continue
		}

		for _, param := range fields[1:] {
			key, value, found := strings.Cut(param, "=")
			if !found || !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}
			if q, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				return q > 0
			}
		}

		return true
	}

	return false
}

// negotiate 选出实际要发送的文件，返回其路径与对应的 Content-Encoding。
// 没有可用的预压缩产物时返回原路径与空编码。
func negotiate(fsys fs.FS, name, acceptEncoding string) (string, string) {
	if acceptEncoding == "" {
		return name, ""
	}

	for _, enc := range encodings {
		if !acceptsEncoding(acceptEncoding, enc.name) {
			continue
		}
		// 只在产物确实存在时才选它：precompress 会跳过压不小的文件，
		// 也可能整个没跑过
		candidate := name + enc.ext
		if file, err := fsys.Open(candidate); err == nil {
			file.Close()
			return candidate, enc.name
		}
	}

	return name, ""
}

// isPrecompressed 判断路径是否指向预压缩产物本身。
//
// 这类请求要拒掉：直接发出去的话，响应体是压缩数据却没有 Content-Encoding
// 说明它被压过，浏览器只会拿到一堆乱码。真实资源一律经协商发出。
func isPrecompressed(name string) bool {
	for _, enc := range encodings {
		if strings.HasSuffix(name, enc.ext) {
			return true
		}
	}
	return false
}

// serveStatic 发送一个静态文件，按 Accept-Encoding 选用预压缩产物。
//
// Content-Type 一律按原始文件名判定，与实际发出的是哪份产物无关：
// 发 index.js.br 时类型仍是 text/javascript，压缩这件事只由 Content-Encoding
// 表达。搞反了浏览器会拒绝执行脚本。
//
// 不用 http.ServeContent：它会按发出的内容算 Range 与嗅探类型，
// 而这里发的是压缩产物、字节范围与原文件对不上。压缩响应本就不该支持
// Range（Accept-Ranges 显式置 none），故自己写出更简单也更不易错。
func serveStatic(c *gin.Context, fsys fs.FS, name, cacheControl string) bool {
	// 客户端不能直接点名要压缩产物，那样发出的响应缺少 Content-Encoding
	if isPrecompressed(name) {
		return false
	}

	selected, enc := negotiate(fsys, name, c.GetHeader("Accept-Encoding"))

	file, err := fsys.Open(selected)
	if err != nil {
		return false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}

	header := c.Writer.Header()
	if contentType, ok := staticContentTypes[strings.ToLower(path.Ext(name))]; ok {
		header.Set("Content-Type", contentType)
	}
	header.Set("Cache-Control", cacheControl)
	// 同一 URL 会因请求头不同而返回不同字节，缓存必须按该头分别存储。
	// 漏掉它会让代理把 br 响应发给只支持 gzip 的客户端。
	header.Set("Vary", "Accept-Encoding")
	header.Set("Accept-Ranges", "none")

	if enc != "" {
		header.Set("Content-Encoding", enc)
	}
	header.Set("Content-Length", strconv.FormatInt(info.Size(), 10))

	// HEAD 只要响应头，此时已全部设好
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return true
	}

	c.Status(http.StatusOK)
	// 写出中途出错多半是客户端断开，无从补救也无需记：
	// 响应头已发出，改不了状态码了
	_, _ = io.Copy(c.Writer, file)

	return true
}

// 静态资源的缓存策略。
//
// 带 hash 的资源可长期缓存，入口文件必须每次校验——否则前端更新后，
// 浏览器会拿旧的 index.html 去要早已不存在的 hash 资源。
const (
	cacheImmutable  = "public, max-age=604800, immutable"
	cacheRevalidate = "no-cache"
)
