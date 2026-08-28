package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// LibraryStatus 单个信息字段的状态。
// 状态属于字段自身，而非整条记录：一条记录里各字段的状态互不相干。
type LibraryStatus string

const (
	// StatusGood 该字段的信息有效
	StatusGood LibraryStatus = "good"
	// StatusOutdated 该字段的信息已被报告为过时
	StatusOutdated LibraryStatus = "out-dated"
)

// IsValid 判断状态取值是否受支持
func (s LibraryStatus) IsValid() bool {
	switch s {
	case StatusGood, StatusOutdated:
		return true
	default:
		return false
	}
}

// InfoValue 一个信息字段的取值。
// 显示名与类型属于字段声明（存注册表，改一处全表生效），
// 故此处只存随记录变化的两样：值与状态。
type InfoValue struct {
	Value  any           `json:"value"`
	Status LibraryStatus `json:"status"`
}

// LibraryInfo 图书馆信息，键为字段名，整体以 JSON 存储于单列。
// 允许出现哪些字段、各是什么类型，全部由注册表声明。
type LibraryInfo map[string]InfoValue

// Value 实现 driver.Valuer，写库时序列化为 JSON
func (i LibraryInfo) Value() (driver.Value, error) {
	if i == nil {
		return nil, nil
	}
	return json.Marshal(i)
}

// Scan 实现 sql.Scanner，从库中读回 JSON
func (i *LibraryInfo) Scan(value any) error {
	if value == nil {
		*i = nil
		return nil
	}

	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("无法将 %T 解析为 LibraryInfo", value)
	}

	return json.Unmarshal(raw, i)
}

// GormDataType 声明列类型为 JSON
func (LibraryInfo) GormDataType() string {
	return "json"
}

// GetString 读取某字段的字符串值，字段不存在或值不是字符串时返回 false
func (i LibraryInfo) GetString(name string) (string, bool) {
	field, ok := i[name]
	if !ok {
		return "", false
	}
	s, ok := field.Value.(string)
	return s, ok
}

// Library 图书馆，存储于服务器本地 MySQL。
// 业务字段全部收在 Info 里，由字段注册表声明。
type Library struct {
	ID   int         `json:"id"   gorm:"primaryKey;autoIncrement"`
	Info LibraryInfo `json:"info" gorm:"not null;type:json"`
	// SearchName 记录名的副本，由 MySQL 从 Info 里自动抽取（生成列）。
	//
	// 存在的唯一理由是建全文索引：关键字搜索原先直接对 JSON 列做
	// JSON_EXTRACT + LIKE '%kw%'，每行都要解析一遍 JSON 且用不上任何索引，
	// 而这张表是无界增长的。抽成独立列后可挂 ngram 全文索引，
	// 中文任意位置匹配也能走索引（见 models.applyKeyword）。
	//
	// STORED 而非 VIRTUAL：全文索引要求列是 STORED 的。
	// 值由数据库维护，应用侧只读不写，故带 "->" 标签。
	SearchName string `json:"-" gorm:"->;type:varchar(191) GENERATED ALWAYS AS (JSON_UNQUOTE(JSON_EXTRACT(info,'$.\"FullName\".value'))) STORED"`
	// CreatedAt 建索引：列表页每次都按它倒序分页（见 models.GetLibraries），
	// 没有索引时每次分页都要排序整张表。
	CreatedAt time.Time `json:"created_at" gorm:"index"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LibraryQuery 图书馆查询条件
type LibraryQuery struct {
	Keyword string // 匹配承担 searchname 角色的字段的值
	Page    int
	Size    int
}
