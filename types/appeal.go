package types

import "time"

// MaxAppealsPerIP 同一 IP 最多可提交的申诉次数。
// 超出后不再受理，避免申诉入口本身被当作留言通道滥用。
const MaxAppealsPerIP = 3

// MaxAppealMessageLength 申诉内容的最大字符数（按 rune 计，中文同样算一个）
const MaxAppealMessageLength = 500

// AppealStatus 申诉处理状态
type AppealStatus string

const (
	// AppealPending 待管理员处理
	AppealPending AppealStatus = "pending"
	// AppealAccepted 已受理并解封
	AppealAccepted AppealStatus = "accepted"
	// AppealRejected 已驳回
	AppealRejected AppealStatus = "rejected"
)

// IsValid 判断状态取值是否受支持
func (s AppealStatus) IsValid() bool {
	switch s {
	case AppealPending, AppealAccepted, AppealRejected:
		return true
	default:
		return false
	}
}

// BanAppeal 一次封禁申诉，存储于本地 SQLite，与封禁记录同库。
//
// Message 只存纯文本：写入前剥离控制字符，读取端一律按文本渲染，
// 不做任何 HTML/Markdown 解析，故内容无法构成注入。
type BanAppeal struct {
	ID int `json:"id" gorm:"primaryKey;autoIncrement"`
	// IP 提交申诉的来源 IP，与 IPBan.IP 对应
	IP string `json:"ip" gorm:"not null;size:45;index"`
	// Attempt 这是该 IP 的第几次申诉，从 1 起计。
	// 存在每条记录上而非只存计数：解封后再次被封时，历史申诉仍可追溯。
	Attempt int `json:"attempt" gorm:"not null"`
	// Message 申诉内容，纯文本
	Message string `json:"message" gorm:"type:text;not null"`
	// Status 处理状态
	Status AppealStatus `json:"status" gorm:"not null;size:16;index"`
	// AdminNote 管理员的处理备注，纯文本
	AdminNote string `json:"admin_note" gorm:"type:text"`
	// VisitorKey 提交时的访问者令牌哈希，便于与操作日志关联
	VisitorKey string `json:"-" gorm:"size:64;index"`
	// BanReason 提交申诉时的封禁原因快照。
	// 封禁记录可能已被解封删除，快照使申诉在那之后仍可理解。
	BanReason string    `json:"ban_reason" gorm:"size:255"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AppealRequest 提交申诉的请求
type AppealRequest struct {
	Message string `json:"message"`
}

// AppealReviewRequest 管理员处理申诉的请求
type AppealReviewRequest struct {
	Status    AppealStatus `json:"status"`
	AdminNote string       `json:"admin_note"`
}

// AppealSummary 某个 IP 的申诉概况，随封禁列表一并返回
type AppealSummary struct {
	// Total 申诉总数
	Total int `json:"total"`
	// Pending 待处理数
	Pending int `json:"pending"`
}

// AppealQuota 当前来源的申诉配额，供封禁页判断是否还能提交
type AppealQuota struct {
	// Used 已提交次数
	Used int `json:"used"`
	// Max 最多可提交次数
	Max int `json:"max"`
	// Remaining 剩余次数
	Remaining int `json:"remaining"`
}
