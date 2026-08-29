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
//
// 角色由字段名推导（见 ReservedFields），不由配置文件声明：客户端靠它定位字段，
// 从而不必硬编码键名——Android 端要打开「网站」那一格，不该去猜它叫 WebSite。
type InfoFieldRole string

const (
	// RoleNone 普通字段，后端与客户端都不解释其含义
	RoleNone InfoFieldRole = ""
	// RoleSearchName 图书馆的记录名，列表关键字搜索匹配的就是它
	RoleSearchName InfoFieldRole = "searchname"
	// RoleWebsite 图书馆的网站地址
	RoleWebsite InfoFieldRole = "website"
)

// 内置字段的字段名。名字即身份，钉死在代码里：
// 记录名的名字还写进了 MySQL 生成列的 JSON 路径（见 Library.SearchName），
// 换个名字就要 ALTER TABLE。
const (
	// SearchNameFieldName 承担 searchname 角色的字段名
	SearchNameFieldName = "FullName"
	// WebsiteFieldName 承担 website 角色的字段名
	WebsiteFieldName = "WebSite"
)

// ReservedField 内置字段的声明：它必须存在，且有几项不由管理员定。
//
// 「内置」的判据是后端或客户端要按角色定位它，因而它不能缺席、类型不能变。
// 未被此处固定的项（显示名，以及未强制的必填与摘要）照普通字段处理。
type ReservedField struct {
	// Name 字段名，不可改名也不可删除
	Name string
	// Role 该字段承担的角色，随字段名固定
	Role InfoFieldRole
	// Type 值类型，锁定不可改
	Type InfoFieldType
	// Label 缺省显示名，仅在该字段缺失、需要补齐时用；补齐后管理员可随意改
	Label string
	// ForceRequired 该字段必须必填
	ForceRequired bool
	// ForceSummary 该字段必须作为列显示在列表里
	ForceSummary bool
}

// AsInfoField 按内置声明生成一份字段，供注册表缺它时补齐
func (r ReservedField) AsInfoField() InfoField {
	return InfoField{
		Name:     r.Name,
		Label:    r.Label,
		Type:     r.Type,
		Required: r.ForceRequired,
		Summary:  r.ForceSummary,
		Role:     r.Role,
	}
}

// ReservedFields 内置字段表：既是角色的唯一来源，也是启动时自动补齐的依据。
// 顺序即补齐时的追加顺序。
//
// 角色只放在这里而不由 JSON 说了算：字段名已经是锚（生成列写死了
// $."FullName".value，注册表也只按名字认字段），同一件事存两处，
// 可被手改的那处只会漂移。文件里的 role 是推导结果的副本，每次加载都被重写。
var ReservedFields = []ReservedField{
	{
		Name:  SearchNameFieldName,
		Role:  RoleSearchName,
		Type:  InfoTypeString,
		Label: "全称",
		// 记录是靠它被搜到的，空着就等于搜不着
		ForceRequired: true,
		// 它是这条记录的身份，藏进详情会让表格变成
		// 「一列 ID 加一堆看不出是谁的行」
		ForceSummary: true,
	},
	{
		Name:  WebsiteFieldName,
		Role:  RoleWebsite,
		Type:  InfoTypeString,
		Label: "网站",
		// 必填与摘要都不强制：后端不依赖它的值，而已有记录多半没填，
		// 强制必填会让那些记录连编辑都通不过校验；是否成列由管理员定
	},
}

// Reserved 查内置字段声明，非内置字段返回 false
func Reserved(name string) (ReservedField, bool) {
	for _, field := range ReservedFields {
		if field.Name == name {
			return field, true
		}
	}
	return ReservedField{}, false
}

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
	// 记录名恒为摘要（见 ReservedFields）：它是搜索匹配的字段，也是这条记录的
	// 身份，藏进详情会让表格变成「一列 ID 加一堆看不出是谁的行」。
	Summary bool `json:"summary"`
	// Role 业务角色，供客户端定位字段而不硬编码键名——Android 端要打开
	// 「网站」那一格，不该去猜它叫 WebSite。
	//
	// 它是派生值而非设置项：由字段名经 ReservedFields 推导，加载与保存时都被
	// 重写为推导结果。写进文件只为让人读文件时能看出这字段的来头，
	// 手改它不起作用，因为名字定了角色就定了。
	Role InfoFieldRole `json:"role,omitempty"`
}

// Reconcile 返回按内置声明校正后的副本：角色按字段名推导，
// 内置字段的类型与强制约束一并落定，覆盖调用方传入的值。
func (f InfoField) Reconcile() InfoField {
	reserved, ok := Reserved(f.Name)
	if !ok {
		f.Role = RoleNone
		return f
	}

	f.Role = reserved.Role
	f.Type = reserved.Type
	if reserved.ForceRequired {
		f.Required = true
	}
	if reserved.ForceSummary {
		f.Summary = true
	}
	return f
}

// ReconcileFields 校正一组字段并补齐缺失的内置字段，返回新切片，不改动入参。
// 第二个返回值是被补齐的内置字段名，供调用方留痕。
//
// 加载、保存注册表都经过它，故「内置字段必然存在且形状正确」这条规则只有一处实现，
// 而管理员的自定义字段与顺序原样保留。
func ReconcileFields(candidates []InfoField) ([]InfoField, []string) {
	out := make([]InfoField, 0, len(candidates)+len(ReservedFields))
	present := make(map[string]struct{}, len(candidates))

	for _, field := range candidates {
		out = append(out, field.Reconcile())
		present[field.Name] = struct{}{}
	}

	// 缺失的内置字段追加在末尾：插到原位会打乱管理员排好的列顺序，
	// 而补齐是一次性的，之后顺序照样可拖
	var added []string
	for _, reserved := range ReservedFields {
		if _, ok := present[reserved.Name]; ok {
			continue
		}
		out = append(out, reserved.AsInfoField())
		added = append(added, reserved.Name)
	}

	return out, added
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

// IsValid 判断角色取值是否为已支持的取值。
// 角色不由外部输入（见 InfoField.Role），故这里只用于自检。
func (r InfoFieldRole) IsValid() bool {
	if r == RoleNone {
		return true
	}
	for _, field := range ReservedFields {
		if field.Role == r {
			return true
		}
	}
	return false
}
