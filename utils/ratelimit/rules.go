// Package ratelimit 按访问者令牌限流，并在检出异常时自动封禁来源 IP。
// 限流计数存 Redis、以自然日为单位重置；封禁存 SQLite、永久生效。
package ratelimit

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"

	"bookfinder-backend/types"
)

// maxBurstWindowSeconds 突发窗口上限。
// 超过一小时就该用每日配额表达，窗口过长也会让 Redis 键长期驻留。
const maxBurstWindowSeconds = 3600

// maxProbationDaily 见习每日配额的上限。
// 领取令牌只需一个请求，给到几十已经很宽松；再大就失去了闸门的意义。
const maxProbationDaily = 100

// minPerIPThreshold 按 IP 累计的封禁判据所允许的最小阈值。
//
// 这类判据会连坐：同一出口下的多人共享同一个计数。校园网与图书馆 Wi-Fi
// 恰好是本应用的典型场景，也恰好是出口 IP 高度共享的地方，故阈值过低会
// 造成大面积误封。真正想收紧时应当调低按令牌计的配额，而非这些阈值。
const minPerIPThreshold = 5

// maxNetworkTopVisitors 网段排查时可考察的令牌数上限。
// 一次封禁几十个设备与封整个网段没有区别，就失去了精准处置的意义。
const maxNetworkTopVisitors = 20

// minConcentrationPercent 判定「流量集中」所需的最低占比。
// 低于一半的流量也算集中的话，正常用户会被当成异常设备封掉。
const minConcentrationPercent = 50

var (
	mu    sync.RWMutex
	rules types.RateRules
	// path 规则文件路径，保存时写回此处
	path string
)

// Load 从 JSON 配置文件加载限流规则
func Load(file string) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read rate rules %q: %w", file, err)
	}

	var parsed types.RateRules
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("failed to parse rate rules %q: %w", file, err)
	}

	if err := Validate(&parsed); err != nil {
		return fmt.Errorf("invalid rate rules %q: %w", file, err)
	}

	mu.Lock()
	rules = parsed
	path = file
	mu.Unlock()

	return nil
}

// Validate 校验规则是否自洽可用
func Validate(candidate *types.RateRules) error {
	if candidate.Limits == nil {
		return fmt.Errorf("limits 不能为空")
	}

	for _, category := range types.AllLimitCategories {
		limit, ok := candidate.Limits[category]
		if !ok {
			return fmt.Errorf("缺少 %s 类别的配额", category)
		}
		if limit.Daily < 1 {
			return fmt.Errorf("%s 的每日配额必须为正数，当前为 %d", category, limit.Daily)
		}
		if limit.Burst < 1 {
			return fmt.Errorf("%s 的突发次数必须为正数，当前为 %d", category, limit.Burst)
		}
		// 必须严格小于：相等时每日配额先耗尽，突发规则永远轮不到生效，等于没配
		if limit.Burst >= limit.Daily {
			return fmt.Errorf("%s 的突发次数 %d 必须小于每日配额 %d，否则突发限制不会生效",
				category, limit.Burst, limit.Daily)
		}
		if limit.BurstWindowSeconds < 1 || limit.BurstWindowSeconds > maxBurstWindowSeconds {
			return fmt.Errorf("%s 的突发窗口须在 1~%d 秒之间，当前为 %d",
				category, maxBurstWindowSeconds, limit.BurstWindowSeconds)
		}
	}

	// 未声明的类别会被静默忽略，多半是拼错了键名，直接报错更好排查
	for category := range candidate.Limits {
		if !category.IsValid() {
			return fmt.Errorf("未知的限流类别 %q", category)
		}
	}

	if err := validateProbation(candidate.Probation); err != nil {
		return err
	}

	return validateAutoBan(candidate.AutoBan)
}

// validateProbation 校验见习配额。
//
// 这一项是「无令牌来源」的唯一闸门，配错的后果两头都很糟：
// 配成 0 等于放弃该闸门，不带 cookie 的请求重新变成免费的；
// 配得过大则同样拦不住脚本。故要求为正数且不过大。
func validateProbation(probation types.ProbationRules) error {
	if probation.Daily < 1 {
		return fmt.Errorf("见习每日配额必须为正数，当前为 %d："+
			"该额度是无令牌来源的唯一闸门，置零等于放任不带 Cookie 的请求刷接口",
			probation.Daily)
	}
	// 见习额度只需容纳「正常用户领取令牌」所需的量——通常就是几个请求。
	// 给得过宽，攻击者便可一直停留在见习状态、无需领取令牌。
	if probation.Daily > maxProbationDaily {
		return fmt.Errorf("见习每日配额不应超过 %d，当前为 %d："+
			"正常用户的首个请求即可换到令牌，无需这么多额度",
			maxProbationDaily, probation.Daily)
	}
	if probation.Burst < 1 {
		return fmt.Errorf("见习突发次数必须为正数，当前为 %d", probation.Burst)
	}
	if probation.Burst > probation.Daily {
		return fmt.Errorf("见习突发次数 %d 不应超过每日配额 %d",
			probation.Burst, probation.Daily)
	}
	if probation.BurstWindowSeconds < 1 || probation.BurstWindowSeconds > maxBurstWindowSeconds {
		return fmt.Errorf("见习突发窗口须在 1~%d 秒之间，当前为 %d",
			maxBurstWindowSeconds, probation.BurstWindowSeconds)
	}
	return nil
}

// validateAutoBan 校验自动封禁规则。各项为 0 表示不启用该条。
func validateAutoBan(autoBan types.AutoBanRules) error {
	if !autoBan.Enabled {
		return nil
	}

	if autoBan.DailyOverflowMultiplier < 0 {
		return fmt.Errorf("每日超额倍数不能为负数")
	}
	// 倍数为 1 意味着刚用满配额就封禁，与限流本身重合，且会误伤用满额度的重度用户。
	// 该倍数针对的是「配额用尽后仍反复请求」，故必须留出容忍区间。
	if autoBan.DailyOverflowMultiplier == 1 {
		return fmt.Errorf("每日超额倍数须大于 1，否则用满配额即被封禁")
	}
	if autoBan.BurstViolations < 0 {
		return fmt.Errorf("突发违规次数不能为负数")
	}
	if autoBan.DuplicateReports < 0 {
		return fmt.Errorf("重复报告次数不能为负数")
	}
	// 重复报告同样按 IP 累计，故与同 IP 令牌数一样需要下限：
	// 校园网、图书馆 Wi-Fi 下十个不同的人各报一次重复，就够封掉整个出口。
	if autoBan.DuplicateReports > 0 && autoBan.DuplicateReports < minPerIPThreshold {
		return fmt.Errorf("重复报告阈值不应低于 %d：该项按 IP 累计，"+
			"过低会让共用出口的多人各报一次即触发封禁", minPerIPThreshold)
	}
	if autoBan.ProbationOverflowMultiplier < 0 {
		return fmt.Errorf("见习超额倍数不能为负数")
	}
	// 倍数为 1 意味着刚用完见习额度就封禁。领取令牌本身要消耗额度，
	// 浏览器禁用 Cookie 的正常用户会一直停留在见习状态，故必须留出容忍区间。
	if autoBan.ProbationOverflowMultiplier == 1 {
		return fmt.Errorf("见习超额倍数须大于 1，否则禁用 Cookie 的正常用户会被直接封禁")
	}

	if err := validateNetworkRules(autoBan); err != nil {
		return err
	}

	// 至少要有一条生效，否则等于没开
	if autoBan.DailyOverflowMultiplier == 0 && autoBan.BurstViolations == 0 &&
		autoBan.DuplicateReports == 0 &&
		autoBan.ProbationOverflowMultiplier == 0 &&
		autoBan.NetworkOverflowMultiplier == 0 {
		return fmt.Errorf("已启用自动封禁但没有任何一条规则生效")
	}

	return nil
}

// validateNetworkRules 校验网段级判定的三项参数。
//
// 这条规则的处置默认落到具体设备上（封访问者令牌），故本身不像网段封禁那样容易连坐。
// 但三项参数配错都会让它退化：占比过低会把正常用户当异常设备，
// 考察数过大等于封整段，倍数为 1 则刚超预算就动手。
func validateNetworkRules(autoBan types.AutoBanRules) error {
	if autoBan.NetworkOverflowMultiplier < 0 {
		return fmt.Errorf("网段超额倍数不能为负数")
	}
	// 为 0 表示不启用该条，此时其余两项无需校验
	if autoBan.NetworkOverflowMultiplier == 0 {
		return nil
	}
	// 网段预算按「单个访问者的每日配额合计」算，而同一网段本就可能有多人，
	// 刚超出一倍预算是完全正常的，故必须留出容忍区间
	if autoBan.NetworkOverflowMultiplier == 1 {
		return fmt.Errorf("网段超额倍数须大于 1：网段预算按单个访问者计，" +
			"同网段有多人时轻易超出一倍，倍数为 1 会误封正常网段")
	}

	if autoBan.NetworkTopVisitors < 1 {
		return fmt.Errorf("网段排查的令牌数必须为正数，当前为 %d："+
			"该值决定能否归因到具体设备，置零则只剩下封整段这一种处置",
			autoBan.NetworkTopVisitors)
	}
	if autoBan.NetworkTopVisitors > maxNetworkTopVisitors {
		return fmt.Errorf("网段排查的令牌数不应超过 %d，当前为 %d："+
			"一次封禁这么多设备与封整个网段没有区别，失去了精准处置的意义",
			maxNetworkTopVisitors, autoBan.NetworkTopVisitors)
	}

	if autoBan.NetworkConcentrationPercent < minConcentrationPercent ||
		autoBan.NetworkConcentrationPercent > 100 {
		return fmt.Errorf("网段流量集中度阈值须在 %d~100 之间，当前为 %d："+
			"低于 %d%% 意味着不到一半的流量也算集中，会把正常用户当成异常设备封掉",
			minConcentrationPercent, autoBan.NetworkConcentrationPercent,
			minConcentrationPercent)
	}

	return nil
}

// Warnings 检查规则之间是否存在「配得上但实际不生效」的组合，返回提示文本。
//
// 与 Validate 分开：这些组合是自洽的、能正常保存，只是某条规则在算术上永远
// 轮不到触发。拒绝保存太过，静默接受又会让人以为规则在生效。
func Warnings(candidate *types.RateRules) []string {
	if candidate == nil || !candidate.AutoBan.Enabled {
		return nil
	}

	autoBan := candidate.AutoBan
	if autoBan.NetworkOverflowMultiplier <= 0 || autoBan.DailyOverflowMultiplier <= 0 {
		return nil
	}

	budget := 0
	for _, limit := range candidate.Limits {
		budget += limit.Daily
	}
	if budget <= 0 {
		return nil
	}

	// 网段判定要认出「异常设备」，需某个令牌的量达到 预算 × 每日超额倍数；
	// 而规则一在「该类别配额 × 同一倍数」就已经把这个人封掉了。
	// 前者必然大于后者（预算是各类配额之和），故 IPv4 下网段判定几乎轮不到——
	// 除非对方不断换令牌，那时单令牌量上不去、流量算分散，而 IPv4 分散只告警不封。
	//
	// IPv6 不受影响：终端可在自己的 /64 内随意换址，按单地址计数的规则一二看不出
	// 异常，网段判定正是为这种情形存在的。
	culprit := budget * autoBan.DailyOverflowMultiplier

	earliest, category := 0, types.LimitCategory("")
	for name, limit := range candidate.Limits {
		threshold := limit.Daily * autoBan.DailyOverflowMultiplier
		if earliest == 0 || threshold < earliest {
			earliest, category = threshold, name
		}
	}

	if earliest > 0 && earliest < culprit {
		return []string{fmt.Sprintf(
			"IPv4 来源的网段判定可能难以生效：认出异常设备需单个访问者当日达 %d 次"+
				"（网段预算 %d × 每日超额倍数 %d），而每日超额那条规则在 %s 类 %d 次时"+
				"就已封禁该来源。IPv6 不受影响。",
			culprit, budget, autoBan.DailyOverflowMultiplier, category, earliest)}
	}

	return nil
}

// Get 返回当前生效的规则副本
func Get() types.RateRules {
	mu.RLock()
	defer mu.RUnlock()

	// map 是引用类型，需复制后返回，避免调用方改动生效中的规则
	copied := rules
	copied.Limits = maps.Clone(rules.Limits)

	return copied
}

// LimitFor 查询某类别的配额，未配置时返回 false
func LimitFor(category types.LimitCategory) (types.CategoryLimit, bool) {
	mu.RLock()
	defer mu.RUnlock()

	limit, ok := rules.Limits[category]
	return limit, ok
}

// Enabled 限流总开关是否打开
func Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return rules.Enabled
}

// AutoBan 返回自动封禁规则
func AutoBan() types.AutoBanRules {
	mu.RLock()
	defer mu.RUnlock()
	return rules.AutoBan
}

// Probation 返回见习配额规则
func Probation() types.ProbationRules {
	mu.RLock()
	defer mu.RUnlock()
	return rules.Probation
}

// Commit 用新规则替换当前规则并写回配置文件。
// 先换内存再落盘：落盘失败时回滚内存，保证内存与文件一致。
func Commit(candidate *types.RateRules) error {
	if err := Validate(candidate); err != nil {
		return err
	}

	mu.Lock()
	previous := rules
	rules = *candidate
	file := path
	mu.Unlock()

	if err := writeFile(file, candidate); err != nil {
		mu.Lock()
		rules = previous
		mu.Unlock()
		return err
	}

	return nil
}

// writeFile 原子写入规则文件：先写临时文件再改名，避免中途失败留下半个文件
func writeFile(file string, candidate *types.RateRules) error {
	raw, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode rate rules: %w", err)
	}
	raw = append(raw, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(file), ".rate_rules-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp rules file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to write temp rules file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to close temp rules file: %w", err)
	}

	if err := os.Rename(tmpName, file); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to replace rules file %q: %w", file, err)
	}

	return nil
}
