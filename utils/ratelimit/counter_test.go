package ratelimit

import (
	"strings"
	"testing"
	"time"

	"bookfinder-backend/types"
)

// TestEndOfDayIsNextMidnight 每日计数到自然日零点过期，而非固定 24 小时。
// 「以天为单位刷新」指自然日：零点一到，当日计数随键过期一并清零。
func TestEndOfDayIsNextMidnight(t *testing.T) {
	now := time.Date(2026, 8, 25, 23, 59, 30, 0, time.Local)
	got := endOfDay(now)

	want := time.Date(2026, 8, 26, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("endOfDay(%v) = %v, want %v", now, got, want)
	}

	// 临近零点时剩余时长应很短，说明不是滚动 24 小时
	if remaining := got.Sub(now); remaining > time.Minute {
		t.Errorf("23:59:30 距零点应不足一分钟，实际为 %v", remaining)
	}
}

// TestEndOfDayFromMidnight 零点整时应指向次日零点，留出完整一天
func TestEndOfDayFromMidnight(t *testing.T) {
	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.Local)
	got := endOfDay(now)

	if remaining := got.Sub(now); remaining != 24*time.Hour {
		t.Errorf("零点整的剩余时长应为 24 小时，实际为 %v", remaining)
	}
}

// TestKeysAreScopedByDate 计数键带日期戳，跨日自然分桶
func TestKeysAreScopedByDate(t *testing.T) {
	today := time.Date(2026, 8, 25, 12, 0, 0, 0, time.Local)
	tomorrow := today.AddDate(0, 0, 1)

	todayKey := dailyKey(today, types.CategoryReport, "visitor-a")
	tomorrowKey := dailyKey(tomorrow, types.CategoryReport, "visitor-a")

	if todayKey == tomorrowKey {
		t.Error("不同日期应生成不同的计数键，否则跨日不会清零")
	}
	if !strings.Contains(todayKey, "20260825") {
		t.Errorf("键中应含日期戳，实际为 %q", todayKey)
	}
	if !strings.HasPrefix(todayKey, keyPrefix) {
		t.Errorf("键应带 %q 前缀，实际为 %q", keyPrefix, todayKey)
	}
}

// TestKeysAreScopedByCategoryAndVisitor 不同类别与不同访问者各自计数，互不干扰
func TestKeysAreScopedByCategoryAndVisitor(t *testing.T) {
	now := time.Now()

	if dailyKey(now, types.CategoryRead, "a") == dailyKey(now, types.CategoryReport, "a") {
		t.Error("不同类别应分别计数")
	}
	if dailyKey(now, types.CategoryRead, "a") == dailyKey(now, types.CategoryRead, "b") {
		t.Error("不同访问者应分别计数")
	}
}

// TestBurstKeyHasNoDate 突发窗口键不带日期：它靠自身 TTL 过期，与自然日无关
func TestBurstKeyHasNoDate(t *testing.T) {
	key := burstKey(types.CategoryReport, "visitor-a")

	if strings.Contains(key, time.Now().Format("20060102")) {
		t.Errorf("突发窗口键不应含日期戳，实际为 %q", key)
	}
}

// TestViolationKeyScopedByCategory 违规计数按类别分开。
// 若合并计数，「report 违规 3 次 + update 违规 3 次」会凑成 6 次触发封禁，
// 尽管单看任一类别都远未越界。
func TestViolationKeyScopedByCategory(t *testing.T) {
	now := time.Now()
	visitor := "visitor-a"

	reportKey := violationKey(now, types.CategoryReport, visitor)
	updateKey := violationKey(now, types.CategoryUpdate, visitor)

	if reportKey == updateKey {
		t.Error("不同类别的违规应分别计数，否则会跨类别累加而误封")
	}
	if !strings.Contains(reportKey, string(types.CategoryReport)) {
		t.Errorf("违规键应含类别，实际为 %q", reportKey)
	}
	// 仍需按日分桶，跨日清零
	if violationKey(now, types.CategoryReport, visitor) ==
		violationKey(now.AddDate(0, 0, 1), types.CategoryReport, visitor) {
		t.Error("违规计数应按日分桶")
	}
}

// TestDuplicateKeyScopedByIP 重复报告按 IP 计
func TestDuplicateKeyScopedByIP(t *testing.T) {
	now := time.Now()

	if duplicateKey(now, "1.2.3.4") == duplicateKey(now, "5.6.7.8") {
		t.Error("不同 IP 的重复报告应分别计数")
	}
}

// TestAllowComputesRemaining 放行结果应给出剩余配额，且不为负
func TestAllowComputesRemaining(t *testing.T) {
	if got := allow(10, 50); got.Remaining != 40 {
		t.Errorf("剩余配额应为 40，实际为 %d", got.Remaining)
	}
	// 超出配额时剩余应为 0 而非负数
	if got := allow(60, 50); got.Remaining != 0 {
		t.Errorf("超出配额时剩余应为 0，实际为 %d", got.Remaining)
	}
	if !allow(1, 50).Allowed {
		t.Error("allow 构造的结果应为放行")
	}
}
