package logger

import (
	"testing"
	"time"

	"bookfinder-backend/types"
)

// TestNormalizeLevel 级别解析，无法识别时回落到 INFO
func TestNormalizeLevel(t *testing.T) {
	tests := map[string]string{
		"debug":   LevelDebug,
		"DEBUG":   LevelDebug,
		"warn":    LevelWarn,
		"error":   LevelError,
		"info":    LevelInfo,
		"":        LevelInfo,
		"unknown": LevelInfo,
	}

	for input, want := range tests {
		if got := normalizeLevel(input); got != want {
			t.Errorf("normalizeLevel(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestLevelOrder 级别顺序决定过滤行为，须严格递增
func TestLevelOrder(t *testing.T) {
	ordered := []string{LevelDebug, LevelInfo, LevelWarn, LevelError}

	for i := 1; i < len(ordered); i++ {
		if levelOrder[ordered[i]] <= levelOrder[ordered[i-1]] {
			t.Errorf("级别 %s 应高于 %s", ordered[i], ordered[i-1])
		}
	}
}

// TestEnqueueWithoutInitialize 未初始化时投递不应 panic，而是回落到 stderr。
// 配置加载阶段就会打日志，那时日志库还没就绪。
func TestEnqueueWithoutInitialize(t *testing.T) {
	mu.Lock()
	queue, db = nil, nil
	mu.Unlock()

	// 不应 panic
	Infof("日志系统未就绪时的应用日志")
	Operation(&types.OperationLog{
		User:   "1.2.3.4",
		Action: types.ActionFieldReport,
		Detail: "日志系统未就绪时的操作日志",
	})
}

// TestOperationFillsDefaults 操作日志未给时间与等级时应自动补全
func TestOperationFillsDefaults(t *testing.T) {
	captured := make(chan any, 1)

	mu.Lock()
	queue = make(chan any, 1)
	db = nil
	ch := queue
	mu.Unlock()

	go func() {
		captured <- <-ch
	}()

	before := time.Now()
	Operation(&types.OperationLog{
		User:   "admin",
		Action: types.ActionAdminLogin,
		Detail: "登录成功",
	})

	select {
	case record := <-captured:
		entry, ok := record.(*types.OperationLog)
		if !ok {
			t.Fatalf("入队记录类型为 %T，want *types.OperationLog", record)
		}
		if entry.Level != LevelInfo {
			t.Errorf("未指定等级时应补为 INFO，实际为 %q", entry.Level)
		}
		if entry.Timestamp.Before(before) {
			t.Error("未指定时间时应补为当前时间")
		}
	case <-time.After(time.Second):
		t.Fatal("操作日志未入队")
	}

	mu.Lock()
	queue = nil
	mu.Unlock()
}

// TestEnqueueDropsWhenFull 队列满时应丢弃而非阻塞业务
func TestEnqueueDropsWhenFull(t *testing.T) {
	mu.Lock()
	queue = make(chan any, 1)
	db = nil
	mu.Unlock()

	t.Cleanup(func() {
		mu.Lock()
		queue = nil
		mu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		// 队列容量 1，投三条：不应阻塞
		for range 3 {
			enqueue(&types.LogEntry{Message: "x"}, "x")
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("队列满时 enqueue 阻塞了")
	}
}
