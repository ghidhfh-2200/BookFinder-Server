package types

import "time"

// OutdatedReportThreshold 同一字段被独立报告多少次后置为过时。
// 未达阈值只累计次数，不改动字段状态。
const OutdatedReportThreshold = 5

// VerifyReportThreshold 未验证的网站被独立确认多少次后转正为有效。
const VerifyReportThreshold = 3

// ReportKind 报告的种类。
//
// 两种票分开计：同一人可以先确认某网站可用，日后又报告它失效——那是两件事，
// 不是重复提交。唯一索引因此含 kind（见 FieldReport）。
type ReportKind string

const (
	// ReportOutdated 报告该字段的信息已过时
	ReportOutdated ReportKind = "outdated"
	// ReportVerify 确认该字段的网站可用，累计到阈值则由未验证转为有效
	ReportVerify ReportKind = "verify"
)

// IsValid 判断报告种类是否受支持
func (k ReportKind) IsValid() bool {
	switch k {
	case ReportOutdated, ReportVerify:
		return true
	default:
		return false
	}
}

// Threshold 该种报告触发状态变更所需的独立报告次数
func (k ReportKind) Threshold() int {
	if k == ReportVerify {
		return VerifyReportThreshold
	}
	return OutdatedReportThreshold
}

// FieldReport 一次针对某字段的报告，种类见 Kind。
// 报告按人按种类去重，(LibraryID, FieldName, Kind, ReporterKey) 上有唯一索引；
// 各种报告的次数即此表对应行数，不额外维护计数器，故不存在计数漂移。
type FieldReport struct {
	ID        int `json:"id"         gorm:"primaryKey;autoIncrement"`
	LibraryID int `json:"library_id" gorm:"not null;uniqueIndex:idx_report_unique,priority:1;index"`
	// FieldName 注册表中的字段名，可能含中文。
	// utf8mb4 下 191 字符是联合唯一索引的长度上限。
	FieldName string `json:"field_name" gorm:"not null;size:191;uniqueIndex:idx_report_unique,priority:2"`
	// Kind 报告种类。进唯一索引：同一人对同一字段的两种票各占一行，
	// 既能确认网站可用，日后也能报告它失效。
	Kind ReportKind `json:"kind" gorm:"not null;size:16;uniqueIndex:idx_report_unique,priority:3"`
	// ReporterKey 报告者标识，为服务端下发令牌的哈希，不可反推令牌
	ReporterKey string `json:"-" gorm:"not null;size:64;uniqueIndex:idx_report_unique,priority:4"`
	// ReporterIP 来源 IP，仅用于溯源与封禁，不参与去重
	ReporterIP string `json:"-" gorm:"not null;size:45;index"`
	// Fingerprint 客户端上报的浏览器指纹哈希，仅作启发式判断，可伪造，不作为身份依据
	Fingerprint string    `json:"-" gorm:"size:64;index"`
	CreatedAt   time.Time `json:"created_at"`
}

// FieldReportStat 某个字段的报告情况，随图书馆列表一并返回
type FieldReportStat struct {
	// Count 独立报告次数
	Count int `json:"count"`
	// Threshold 触发过时所需次数
	Threshold int `json:"threshold"`
	// Reported 当前访问者是否已报告过该字段
	Reported bool `json:"reported"`
	// Suspected 该字段已有来自当前 IP 的报告，但不是当前访问者提交的。
	// 前端据此提前提示「疑似重复，可能不计数」，免得点完才知道。
	Suspected bool `json:"suspected,omitempty"`
	// VerifyCount 确认可用的独立报告次数，仅未验证的网站字段有意义
	VerifyCount int `json:"verify_count,omitempty"`
	// VerifyThreshold 转正所需次数
	VerifyThreshold int `json:"verify_threshold,omitempty"`
	// Verified 当前访问者是否已确认过该字段
	Verified bool `json:"verified,omitempty"`
}
