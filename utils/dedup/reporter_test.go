package dedup

import (
	"errors"
	"testing"
)

// lookup 构造一个固定结论的查询
func lookup(counted bool, similar int64) Lookup {
	return Lookup{
		AlreadyCounted: func() (bool, error) { return counted, nil },
		SimilarCount:   func() (int64, error) { return similar, nil },
	}
}

// TestCheckNewReporter 未见过的提交者应当计数
func TestCheckNewReporter(t *testing.T) {
	got, err := Check(Signals{ReporterKey: "key"}, lookup(false, 0))
	if err != nil {
		t.Fatalf("Check 返回错误: %v", err)
	}
	if got != VerdictNew {
		t.Errorf("Check() = %v, want VerdictNew", got)
	}
}

// TestCheckAlreadyCounted 同一令牌重复提交应判为已计数，且不再查启发式信号
func TestCheckAlreadyCounted(t *testing.T) {
	similarCalled := false

	got, err := Check(Signals{ReporterKey: "key"}, Lookup{
		AlreadyCounted: func() (bool, error) { return true, nil },
		SimilarCount: func() (int64, error) {
			similarCalled = true
			return 0, nil
		},
	})
	if err != nil {
		t.Fatalf("Check 返回错误: %v", err)
	}
	if got != VerdictAlreadyCounted {
		t.Errorf("Check() = %v, want VerdictAlreadyCounted", got)
	}
	if similarCalled {
		t.Error("令牌已计数时不应再查启发式信号")
	}
}

// TestCheckSuspectedDuplicate 令牌是新的但 IP 或指纹吻合，应判为疑似重复
func TestCheckSuspectedDuplicate(t *testing.T) {
	got, err := Check(Signals{ReporterKey: "new", ReporterIP: "1.2.3.4"}, lookup(false, 2))
	if err != nil {
		t.Fatalf("Check 返回错误: %v", err)
	}
	if got != VerdictSuspectedDuplicate {
		t.Errorf("Check() = %v, want VerdictSuspectedDuplicate", got)
	}
}

// TestCheckPropagatesErrors 查询出错时应原样上报，不误判为可计数
func TestCheckPropagatesErrors(t *testing.T) {
	want := errors.New("db down")

	if _, err := Check(Signals{ReporterKey: "key"}, Lookup{
		AlreadyCounted: func() (bool, error) { return false, want },
		SimilarCount:   func() (int64, error) { return 0, nil },
	}); !errors.Is(err, want) {
		t.Errorf("AlreadyCounted 的错误应上报，实际为 %v", err)
	}

	if _, err := Check(Signals{ReporterKey: "key"}, Lookup{
		AlreadyCounted: func() (bool, error) { return false, nil },
		SimilarCount:   func() (int64, error) { return 0, want },
	}); !errors.Is(err, want) {
		t.Errorf("SimilarCount 的错误应上报，实际为 %v", err)
	}
}

// TestHasSignals 没有令牌就无从去重
func TestHasSignals(t *testing.T) {
	if (Signals{}).HasSignals() {
		t.Error("缺少令牌时 HasSignals 应为 false")
	}
	// 仅有指纹不足以识别身份：指纹可伪造
	if (Signals{Fingerprint: "fp", ReporterIP: "1.2.3.4"}).HasSignals() {
		t.Error("只有指纹与 IP 时 HasSignals 应为 false")
	}
	if !(Signals{ReporterKey: "key"}).HasSignals() {
		t.Error("有令牌时 HasSignals 应为 true")
	}
}
