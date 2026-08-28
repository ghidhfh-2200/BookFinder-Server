// Package dedup 汇集「同一人是否重复提交」的判定逻辑。
// 身份以服务端下发的令牌为准，浏览器指纹与来源 IP 只作辅助信号：
// 指纹由客户端计算后上报，可被伪造，同型号同系统的设备也常算出相同结果。
package dedup

// Signals 一次提交携带的身份信号
type Signals struct {
	// ReporterKey 服务端下发令牌的哈希，唯一可信的身份依据
	ReporterKey string
	// ReporterIP 来源 IP
	ReporterIP string
	// Fingerprint 浏览器指纹哈希，可为空
	Fingerprint string
}

// Verdict 查重结论
type Verdict int

const (
	// VerdictNew 未见过的提交者，应当计数
	VerdictNew Verdict = iota
	// VerdictAlreadyCounted 该令牌已提交过，重复提交不应再计数
	VerdictAlreadyCounted
	// VerdictSuspectedDuplicate 令牌是新的，但 IP 或指纹与既有提交吻合，
	// 疑似同一人换了身份，应拒绝计数
	VerdictSuspectedDuplicate
)

// Lookup 查询既有提交的情况，由调用方注入以避免此包依赖数据层。
// counted 表示该令牌是否已提交过；similar 为同 IP 或同指纹的提交数。
type Lookup struct {
	// AlreadyCounted 该令牌是否已提交过
	AlreadyCounted func() (bool, error)
	// SimilarCount 同 IP 或同指纹的提交数
	SimilarCount func() (int64, error)
}

// Check 判定一次提交该如何处理。
// 先看令牌：已提交过就是重复，无需再查启发式信号。
// 令牌是新的才看 IP 与指纹，避免误伤同一人的正常重试。
func Check(signals Signals, lookup Lookup) (Verdict, error) {
	counted, err := lookup.AlreadyCounted()
	if err != nil {
		return VerdictNew, err
	}
	if counted {
		return VerdictAlreadyCounted, nil
	}

	similar, err := lookup.SimilarCount()
	if err != nil {
		return VerdictNew, err
	}
	if similar > 0 {
		return VerdictSuspectedDuplicate, nil
	}

	return VerdictNew, nil
}

// HasSignals 判断是否具备可用于判定的身份信号。
// 没有令牌就无从去重，调用方应拒绝该次提交。
func (s Signals) HasSignals() bool {
	return s.ReporterKey != ""
}
