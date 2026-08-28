package types

import "time"

// OutdatedReportThreshold 同一字段被独立报告多少次后置为过时。
// 未达阈值只累计次数，不改动字段状态。
const OutdatedReportThreshold = 5

// FieldReport 一次「某字段信息已过时」的报告。
// 报告按人去重，(LibraryID, FieldName, ReporterKey) 上有唯一索引；
// 字段的报告次数即此表的行数，不额外维护计数器，故不存在计数漂移。
type FieldReport struct {
	ID        int `json:"id"         gorm:"primaryKey;autoIncrement"`
	LibraryID int `json:"library_id" gorm:"not null;uniqueIndex:idx_report_unique,priority:1;index"`
	// FieldName 注册表中的字段名，可能含中文。
	// utf8mb4 下 191 字符是联合唯一索引的长度上限。
	FieldName string `json:"field_name" gorm:"not null;size:191;uniqueIndex:idx_report_unique,priority:2"`
	// ReporterKey 报告者标识，为服务端下发令牌的哈希，不可反推令牌
	ReporterKey string `json:"-" gorm:"not null;size:64;uniqueIndex:idx_report_unique,priority:3"`
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
}
