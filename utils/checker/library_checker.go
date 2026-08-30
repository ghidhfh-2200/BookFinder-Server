package checker

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"bookfinder-backend/types"
	"bookfinder-backend/utils/schema"
)

// ValidateLibraryCreate 校验图书馆创建请求
func ValidateLibraryCreate(library *types.Library) error {
	return validateLibraryInfo(library.Info)
}

// ValidateLibraryUpdate 校验图书馆更新请求
func ValidateLibraryUpdate(library *types.Library) error {
	return validateLibraryInfo(library.Info)
}

// validateLibraryInfo 按注册表校验 Info。
// 调用前 Info 应已由 schema.Normalize 对齐，故此处只需检查必填字段的值非空、
// 字符串值去除首尾空白，以及每个字段自带的状态取值合法。
func validateLibraryInfo(info types.LibraryInfo) error {
	if info == nil {
		return errors.New("info 不能为空")
	}

	for _, field := range schema.Fields() {
		entry, ok := info[field.Name]
		if !ok {
			return fmt.Errorf("info 缺少 %s 字段", field.Name)
		}

		// 字符串值统一去除首尾空白，空白串等同于未填
		if s, isString := entry.Value.(string); isString {
			entry.Value = strings.TrimSpace(s)
			info[field.Name] = entry
		}

		if field.Required && field.Type.IsEmptyValue(entry.Value) {
			return fmt.Errorf("%s 不能为空", field.DisplayName())
		}

		// 网站地址要能被点开，故形状不对就不收：留到读的人照着点才发现太晚
		if field.Role == types.RoleWebsite {
			if err := ValidateWebsiteURL(entry.Value); err != nil {
				return fmt.Errorf("%s %w", field.DisplayName(), err)
			}
		}

		// 状态属于字段自身，逐个校验
		if err := ValidateLibraryStatus(entry.Status); err != nil {
			return fmt.Errorf("%s 的%w", field.DisplayName(), err)
		}
	}

	return nil
}

// ValidateLibraryStatus 校验状态取值
func ValidateLibraryStatus(status types.LibraryStatus) error {
	if !status.IsValid() {
		return errors.New("状态只能为 good、out-dated 或 unverified")
	}
	return nil
}

// ValidateWebsiteURL 校验网站地址。空值放行——网站不是必填项。
//
// 只认 http 与 https：其余 scheme 要么点不开（ftp、mailto），要么是注入载体
// （javascript: 会在点击时执行，data: 可承载整个页面）。
// 客户端拿到这个值就会直接交给浏览器打开，故过滤在写入时做，不指望每个客户端都记得防。
func ValidateWebsiteURL(value any) error {
	raw, ok := value.(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	raw = strings.TrimSpace(raw)

	parsed, err := url.Parse(raw)
	if err != nil {
		return errors.New("不是合法的网址")
	}

	// 必须带 scheme：没有它 url.Parse 会把 "example.com/a" 整个当成路径，
	// 解析不报错但也不是一个可打开的地址
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	case "":
		return errors.New("必须以 http:// 或 https:// 开头")
	default:
		return fmt.Errorf("不支持 %s 协议，只允许 http 与 https", parsed.Scheme)
	}

	// 主机名不能空：http:///path 这类能通过 Parse，但没有可连接的目标
	if parsed.Host == "" {
		return errors.New("缺少域名")
	}
	// 域名里至少要有一个点或是 localhost，否则多半是漏打了后缀
	host := parsed.Hostname()
	if host != "localhost" && !strings.Contains(host, ".") {
		return errors.New("域名不完整")
	}

	return nil
}
