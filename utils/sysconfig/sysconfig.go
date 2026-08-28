// Package sysconfig 持有服务自身的运行与存储配置。
//
// 与 utils/ratelimit 同一套模式：JSON 文件 + 内存副本，管理页保存后热生效。
// 差别在于本配置里有几项技术上无法热生效（HTTP 超时与并发上限在构造时即固定），
// 它们仍然存在此处，只在管理页标注「重启后生效」——配置项散落在代码与文件两处
// 更难维护。
package sysconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"bookfinder-backend/types"
)

// 各项的取值边界。
const (
	// minRetentionDays 保留天数下限。低于一周意味着出问题时日志已经没了。
	minRetentionDays = 7
	// maxRetentionDays 保留天数上限，约十年，防手滑输成一个天文数字
	maxRetentionDays = 3650
	// minBodyBytes 请求体上限的下限。太小连正常的图书馆记录都提交不了。
	minBodyBytes = 1 << 10
	// maxBodyBytes 请求体上限的上限。再大就该考虑是否真的需要。
	maxBodyBytes = 1 << 20
	// maxPageSize 每页条数的上限。过大会让单次响应体积失控。
	maxPageSize = 500
)

// dailyAtPattern 每日执行时刻的格式，24 小时制
var dailyAtPattern = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

var (
	mu     sync.RWMutex
	config = types.DefaultSystemConfig()
	// path 配置文件路径，保存时写回此处
	path string
)

// Load 从 JSON 配置文件加载。
//
// 文件不存在时用默认值并写出一份：这份配置是后来才引入的，已部署的实例不会有它，
// 而缺文件就启动失败太苛刻——各项默认值与此前硬编码的取值一致，可直接上线。
func Load(file string) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read system config %q: %w", file, err)
		}

		defaults := types.DefaultSystemConfig()

		mu.Lock()
		config = defaults
		path = file
		mu.Unlock()

		if err := writeFile(file, &defaults); err != nil {
			return fmt.Errorf("failed to create system config %q: %w", file, err)
		}

		return nil
	}

	// 以默认值为底再反序列化：新增的配置段在已部署实例的文件里并不存在，
	// 直接解析会让它们留在零值上（例如 SMTP 端口为 0），而零值未必可用。
	// JSON 里出现的键照常覆盖，故用户的既有取值不受影响。
	parsed := types.DefaultSystemConfig()
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("failed to parse system config %q: %w", file, err)
	}

	if err := Validate(&parsed); err != nil {
		return fmt.Errorf("invalid system config %q: %w", file, err)
	}

	mu.Lock()
	config = parsed
	path = file
	mu.Unlock()

	return nil
}

// Validate 校验配置是否自洽可用
func Validate(candidate *types.SystemConfig) error {
	if err := validateMaintenance(candidate.Maintenance); err != nil {
		return err
	}
	if err := validateNotify(candidate.Notify); err != nil {
		return err
	}
	if err := validatePagination(candidate.Pagination); err != nil {
		return err
	}
	return validateServer(candidate.Server)
}

// validateMaintenance 校验清理任务配置
func validateMaintenance(m types.MaintenanceConfig) error {
	// 关闭时其余项不生效，无需校验——否则改不回一个合法值就没法关掉任务
	if !m.Enabled {
		return nil
	}

	if !dailyAtPattern.MatchString(m.DailyAt) {
		return fmt.Errorf("执行时刻 %q 格式不正确，应为 HH:MM（24 小时制）", m.DailyAt)
	}

	if err := checkRetention("操作日志", m.OperationLogRetentionDays); err != nil {
		return err
	}
	if err := checkRetention("运行日志", m.AppLogRetentionDays); err != nil {
		return err
	}

	// 操作日志是审计证据，比运行日志更该留久一点。配反了多半是填错行。
	if m.OperationLogRetentionDays < m.AppLogRetentionDays {
		return fmt.Errorf("操作日志保留 %d 天少于运行日志的 %d 天："+
			"操作日志是复核误封与处理申诉的证据，不应比运行日志留得更短",
			m.OperationLogRetentionDays, m.AppLogRetentionDays)
	}

	return nil
}

// checkRetention 校验单个保留天数
func checkRetention(label string, days int) error {
	if days < minRetentionDays {
		return fmt.Errorf("%s保留天数不应少于 %d 天，当前为 %d："+
			"保留期过短意味着出问题时日志已经被清掉了", label, minRetentionDays, days)
	}
	if days > maxRetentionDays {
		return fmt.Errorf("%s保留天数不应超过 %d 天，当前为 %d",
			label, maxRetentionDays, days)
	}
	return nil
}

// validateNotify 校验告警外发配置。
//
// 只在启用邮件时校验其余各项：关掉时那些字段不生效，若一并要求填全，
// 想关掉一个填了一半的配置反而做不到。
func validateNotify(n types.NotifyConfig) error {
	email := n.Email
	if !email.Enabled {
		return nil
	}

	// 逐项检查而非只判一个整体标志：告警的价值在于「没消息就是没事」，
	// 而漏填一项的后果是静默不发——必须在保存时就指出漏的是哪一项
	for label, value := range map[string]string{
		"SMTP 服务器地址": email.Host,
		"发信账号":       email.Username,
		"收件地址":       email.To,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("启用邮件告警时%s不能为空", label)
		}
	}

	if email.Port < 1 || email.Port > 65535 {
		return fmt.Errorf("SMTP 端口须在 1~65535 之间，当前为 %d", email.Port)
	}

	// 地址进邮件头，含换行即可伪造额外的头部。发送前也会再拦一次，
	// 但在保存时就拒绝更好：否则要等到第一条告警发不出去才发现。
	for label, value := range map[string]string{
		"发件地址": email.From,
		"发信账号": email.Username,
		"收件地址": email.To,
	} {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%s不能含换行符", label)
		}
	}

	// 只做最基本的形状检查：真正能不能投递要由服务器判定，
	// 在此处堆正则只会把合法的地址挡在外面
	if !strings.Contains(email.To, "@") {
		return fmt.Errorf("收件地址 %q 不像一个邮箱地址", email.To)
	}

	return nil
}

// validatePagination 校验分页约束
func validatePagination(p types.PaginationConfig) error {
	if p.DefaultSize < 1 || p.DefaultSize > maxPageSize {
		return fmt.Errorf("默认每页条数须在 1~%d 之间，当前为 %d", maxPageSize, p.DefaultSize)
	}
	if p.MaxSize < 1 || p.MaxSize > maxPageSize {
		return fmt.Errorf("每页条数上限须在 1~%d 之间，当前为 %d", maxPageSize, p.MaxSize)
	}
	// 默认值大于上限时，请求不带 size 反而能拿到比上限更多的数据
	if p.DefaultSize > p.MaxSize {
		return fmt.Errorf("默认每页条数 %d 不能大于上限 %d", p.DefaultSize, p.MaxSize)
	}
	return nil
}

// validateServer 校验资源上限
func validateServer(s types.ServerLimits) error {
	if s.MaxRequestBodyBytes < minBodyBytes || s.MaxRequestBodyBytes > maxBodyBytes {
		return fmt.Errorf("请求体上限须在 %d~%d 字节之间，当前为 %d",
			minBodyBytes, maxBodyBytes, s.MaxRequestBodyBytes)
	}
	if s.MaxConcurrentRequests < 1 {
		return fmt.Errorf("并发上限必须为正数，当前为 %d："+
			"这是服务不被打崩的最后一道保险，不能置零", s.MaxConcurrentRequests)
	}

	for _, item := range []struct {
		label   string
		seconds int
	}{
		{"读取请求头超时", s.ReadHeaderTimeoutSeconds},
		{"读取请求超时", s.ReadTimeoutSeconds},
		{"写响应超时", s.WriteTimeoutSeconds},
		{"空闲连接超时", s.IdleTimeoutSeconds},
	} {
		if item.seconds < 1 {
			return fmt.Errorf("%s必须为正数，当前为 %d："+
				"置零等于不限时，只发一半请求头的连接可以无限占用一条连接",
				item.label, item.seconds)
		}
	}

	return nil
}

// Get 返回当前生效的配置副本
func Get() types.SystemConfig {
	mu.RLock()
	defer mu.RUnlock()
	return config
}

// Commit 用新配置替换当前配置并写回文件。
// 先换内存再落盘：落盘失败时回滚内存，保证两者一致。
func Commit(candidate *types.SystemConfig) error {
	if err := Validate(candidate); err != nil {
		return err
	}

	mu.Lock()
	previous := config
	config = *candidate
	file := path
	mu.Unlock()

	if err := writeFile(file, candidate); err != nil {
		mu.Lock()
		config = previous
		mu.Unlock()
		return err
	}

	return nil
}

// DailyAtMinutes 把执行时刻解析为「当日第几分钟」，便于与当前时间比对。
// 取值非法时返回 false，调用方据此跳过本次调度。
func DailyAtMinutes(dailyAt string) (int, bool) {
	if !dailyAtPattern.MatchString(dailyAt) {
		return 0, false
	}

	parts := strings.SplitN(dailyAt, ":", 2)
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, false
	}

	return hour*60 + minute, true
}

// writeFile 原子写入配置文件：先写临时文件再改名，避免中途失败留下半个文件
func writeFile(file string, candidate *types.SystemConfig) error {
	if file == "" {
		return fmt.Errorf("system config path is not set")
	}

	raw, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode system config: %w", err)
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".system_config-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp config file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to write temp config file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to close temp config file: %w", err)
	}

	if err := os.Rename(tmpName, file); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to replace config file %q: %w", file, err)
	}

	return nil
}
