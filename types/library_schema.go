package types

// InfoFieldType Info 中某个字段的值类型
type InfoFieldType string

const (
	// InfoTypeString 字符串，空值为 ""
	InfoTypeString InfoFieldType = "string"
	// InfoTypeNumber 数字，空值为 0
	InfoTypeNumber InfoFieldType = "number"
	// InfoTypeBool 布尔，空值为 false
	InfoTypeBool InfoFieldType = "bool"
	// InfoTypeObject JSON 对象，空值为 {}
	InfoTypeObject InfoFieldType = "object"
	// InfoTypeArray JSON 数组，空值为 []
	InfoTypeArray InfoFieldType = "array"
)

// InfoFieldRole 字段承担的业务角色。
// 角色标识符固定不变，后端靠它定位字段，从而不依赖具体键名。
type InfoFieldRole string

const (
	// RoleNone 普通字段，后端不解释其含义
	RoleNone InfoFieldRole = ""
	// RoleSearchName 图书馆的记录名，列表关键字搜索匹配的就是它。
	// 创建注册表时自带，不可删除，可改显示名。
	RoleSearchName InfoFieldRole = "searchname"
)

// SearchNameFieldName 承担 searchname 角色的字段名，固定为 FullName。
// 它是记录的身份标识，不可删除；显示名可改。
const SearchNameFieldName = "FullName"

// InfoField 注册表中一个字段的声明。
// 字段名是标识符，只能增删不能改；显示名与类型可改。
type InfoField struct {
	// Name 字段名，即 Info 里实际存储的键，也是该字段的标识符
	Name string `json:"name"`
	// Label 显示名，留空则回落到 Name。改它全表立即生效。
	Label string `json:"label,omitempty"`
	// Type 值类型
	Type InfoFieldType `json:"type"`
	// Required 是否必填。必填字段在写入时值不允许为空。
	Required bool `json:"required"`
	// Summary 是否为摘要字段：勾选则作为一列显示在列表表格里，
	// 未勾选的字段收进每行的「详情」中。
	//
	// 纯显示层的取舍，不影响存储与读写：所有字段照常存、照常返回，
	// 只是列表默认不为它们各占一列——字段一多，表格会宽到无法浏览。
	//
	// 记录名恒为摘要（见 Validate）：它是搜索匹配的字段，也是这条记录的身份，
	// 藏进详情会让表格变成「一列 ID 加一堆看不出是谁的行」。
	Summary bool `json:"summary"`
	// Role 业务角色，后端据此定位字段而不依赖键名
	Role InfoFieldRole `json:"role,omitempty"`
}

// DisplayName 返回显示名，Label 留空时回落到字段名
func (f InfoField) DisplayName() string {
	if f.Label != "" {
		return f.Label
	}
	return f.Name
}

// LibrarySchema Info 的字段注册表，持久化为外部 JSON 配置文件。
// 注册表是 Info 的完整定义：读写时未声明的字段一律剔除，缺失的字段补为对应类型的空值。
// 因此增删字段只需改注册表，数据库中已有的 Info 由后端自动补全，无需人工迁移。
type LibrarySchema struct {
	Fields []InfoField `json:"fields"`
}

// SchemaUpdateRequest 更新注册表的请求。
// 字段名只能增删不能改，故无需携带改名映射。
type SchemaUpdateRequest struct {
	Fields []InfoField `json:"fields"`
}

// EmptyValue 返回该类型对应的空值
func (t InfoFieldType) EmptyValue() any {
	switch t {
	case InfoTypeNumber:
		return float64(0)
	case InfoTypeBool:
		return false
	case InfoTypeObject:
		return map[string]any{}
	case InfoTypeArray:
		return []any{}
	default:
		return ""
	}
}

// IsEmptyValue 判断给定值是否为该类型的空值
func (t InfoFieldType) IsEmptyValue(value any) bool {
	switch t {
	case InfoTypeNumber:
		n, ok := value.(float64)
		return !ok || n == 0
	case InfoTypeBool:
		b, ok := value.(bool)
		return !ok || !b
	case InfoTypeObject:
		m, ok := value.(map[string]any)
		return !ok || len(m) == 0
	case InfoTypeArray:
		a, ok := value.([]any)
		return !ok || len(a) == 0
	default:
		s, ok := value.(string)
		return !ok || s == ""
	}
}

// Matches 判断给定值是否符合该类型。
// JSON 解码后数字一律是 float64，故只认这一种数字表示。
func (t InfoFieldType) Matches(value any) bool {
	switch t {
	case InfoTypeNumber:
		_, ok := value.(float64)
		return ok
	case InfoTypeBool:
		_, ok := value.(bool)
		return ok
	case InfoTypeObject:
		_, ok := value.(map[string]any)
		return ok
	case InfoTypeArray:
		_, ok := value.([]any)
		return ok
	default:
		_, ok := value.(string)
		return ok
	}
}

// IsValid 判断类型声明是否为已支持的取值
func (t InfoFieldType) IsValid() bool {
	switch t {
	case InfoTypeString, InfoTypeNumber, InfoTypeBool, InfoTypeObject, InfoTypeArray:
		return true
	default:
		return false
	}
}

// IsValid 判断角色声明是否为已支持的取值
func (r InfoFieldRole) IsValid() bool {
	switch r {
	case RoleNone, RoleSearchName:
		return true
	default:
		return false
	}
}
