package types

import "time"

// BanSource 封禁来源
type BanSource string

const (
	// BanSourceManual 管理员手动封禁
	BanSourceManual BanSource = "manual"
	// BanSourceAuto 由自动封禁规则触发
	BanSourceAuto BanSource = "auto"
)

// BanIdentKind 封禁标识的种类。
//
// 封禁挂在「主体」上而非单个 IP 上：同一个人可能同时持有来源 IP、访问者令牌、
// 安卓设备标识等多个标识，任一标识命中即视为该主体。这使得封禁一次即可同时
// 挡住浏览器端与安卓端，也使得换 IP 或清 cookie 不足以脱身。
type BanIdentKind string

const (
	// IdentIP 精确来源 IP
	IdentIP BanIdentKind = "ip"
	// IdentIPNet 来源 IP 所属网段（IPv6 /64、IPv4 /24），见 utils/netmask
	IdentIPNet BanIdentKind = "ip_net"
	// IdentVisitor 访问者令牌哈希。浏览器与安卓端共用同一套令牌，故此标识跨端通用。
	IdentVisitor BanIdentKind = "visitor"
	// IdentDevice 安卓设备标识哈希。仅在请求签名校验通过时采信，
	// 否则任何人改一个请求头就能冒充或污染他人的设备标识。
	IdentDevice BanIdentKind = "device"
)

// IsValid 判断标识种类是否受支持
func (k BanIdentKind) IsValid() bool {
	switch k {
	case IdentIP, IdentIPNet, IdentVisitor, IdentDevice:
		return true
	default:
		return false
	}
}

// AllBanIdentKinds 全部标识种类，供管理页展示与校验
var AllBanIdentKinds = []BanIdentKind{IdentIP, IdentIPNet, IdentVisitor, IdentDevice}

// BanSubject 一个被封禁的主体，存储于本地 SQLite。
//
// 封禁是永久的，解封即删除主体及其全部标识。解封只有两条路径：
// 管理员手动解封、申诉受理。不存在自动解封与封禁过期。
type BanSubject struct {
	ID int `json:"id" gorm:"primaryKey;autoIncrement"`
	// Reason 触发的规则或管理员填写的原因
	Reason string `json:"reason" gorm:"size:255"`
	// Detail 自动封禁时触发规则的具体数据，便于复核误判
	Detail string `json:"detail" gorm:"size:512"`
	// Source 封禁来源：管理员手动或规则自动
	Source BanSource `json:"source" gorm:"not null;size:16;index"`
	// Idents 该主体的全部标识，随主体一并删除
	Idents    []BanIdent `json:"idents" gorm:"foreignKey:SubjectID;constraint:OnDelete:CASCADE"`
	CreatedAt time.Time  `json:"created_at"`
}

// TableName 指定表名
func (BanSubject) TableName() string {
	return "ban_subjects"
}

// BanIdent 命中封禁的一个标识。
//
// (Kind, Value) 上有唯一索引：同一个标识不能同时属于两个主体，否则封禁归属
// 无从判定。再次封禁同一标识时更新其归属，而非插入重复行。
type BanIdent struct {
	ID int `json:"id" gorm:"primaryKey;autoIncrement"`
	// SubjectID 所属主体
	SubjectID int `json:"subject_id" gorm:"not null;index"`
	// Kind 标识种类
	Kind BanIdentKind `json:"kind" gorm:"not null;size:16;uniqueIndex:idx_ban_ident,priority:1"`
	// Value 标识取值。IP 与网段为可读文本，令牌与设备标识为 SHA-256 十六进制串。
	// 64 字符可容纳哈希，也够放 IPv6 网段的文本表示。
	Value     string    `json:"value" gorm:"not null;size:64;uniqueIndex:idx_ban_ident,priority:2"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName 指定表名
func (BanIdent) TableName() string {
	return "ban_idents"
}
