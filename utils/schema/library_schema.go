package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"bookfinder-backend/types"
)

var (
	mu sync.RWMutex
	// fields 当前生效的字段声明，顺序与配置文件一致
	fields []types.InfoField
	// index 字段名到声明的映射，规范化时按此查表
	index map[string]types.InfoField
	// path 注册表文件路径，保存时写回此处
	path string
)

// Load 从 JSON 配置文件加载 Info 字段注册表
func Load(file string) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read library schema %q: %w", file, err)
	}

	var parsed types.LibrarySchema
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("failed to parse library schema %q: %w", file, err)
	}

	if err := Validate(parsed.Fields); err != nil {
		return fmt.Errorf("invalid library schema %q: %w", file, err)
	}

	mu.Lock()
	fields = parsed.Fields
	index = buildIndex(parsed.Fields)
	path = file
	mu.Unlock()

	return nil
}

// Validate 校验一组字段声明是否自洽可用。
// 字段名是标识符，只能增删不能改；显示名与类型可改。
func Validate(candidates []types.InfoField) error {
	if len(candidates) == 0 {
		return fmt.Errorf("注册表至少要有一个字段")
	}

	var (
		seen            = make(map[string]struct{}, len(candidates))
		searchNameCount int
	)

	for _, field := range candidates {
		if strings.TrimSpace(field.Name) == "" {
			return fmt.Errorf("字段名不能为空")
		}
		if field.Name != strings.TrimSpace(field.Name) {
			return fmt.Errorf("字段名 %q 首尾不能有空白", field.Name)
		}
		// 字段名会拼进 JSON 路径 '$."Name".value'，无法参数化，故禁掉能破坏路径的字符
		if strings.ContainsAny(field.Name, `"\`) {
			return fmt.Errorf("字段名 %q 不能包含双引号或反斜杠", field.Name)
		}
		if !field.Type.IsValid() {
			return fmt.Errorf("字段 %s 的类型 %q 不受支持，只允许 string、number、bool、object、array",
				field.Name, field.Type)
		}
		if !field.Role.IsValid() {
			return fmt.Errorf("字段 %s 的角色 %q 不受支持", field.Name, field.Role)
		}
		if _, dup := seen[field.Name]; dup {
			return fmt.Errorf("字段名 %s 重复", field.Name)
		}
		seen[field.Name] = struct{}{}

		if field.Role == types.RoleSearchName {
			if field.Name != types.SearchNameFieldName {
				return fmt.Errorf("searchname 角色只能由字段 %s 承担，不能是 %s",
					types.SearchNameFieldName, field.Name)
			}
			if field.Type != types.InfoTypeString {
				return fmt.Errorf("字段 %s 作为记录名，类型必须为 string，当前为 %q", field.Name, field.Type)
			}
			if !field.Required {
				return fmt.Errorf("字段 %s 作为记录名，必须为必填", field.Name)
			}
			// 记录名是搜索匹配的字段，也是这条记录的身份：不作为列显示，
			// 表格就成了「一列 ID 加一堆看不出是谁的行」
			if !field.Summary {
				return fmt.Errorf("字段 %s 作为记录名，必须作为摘要显示在列表中", field.Name)
			}
			searchNameCount++
		}
	}

	// 记录名是图书馆的身份标识，也是关键字搜索的唯一依据，故必须存在且唯一
	if searchNameCount != 1 {
		return fmt.Errorf("必须有且只有一个字段 %s 承担 searchname 角色，当前有 %d 个",
			types.SearchNameFieldName, searchNameCount)
	}

	return nil
}

// buildIndex 建立字段名到声明的映射
func buildIndex(candidates []types.InfoField) map[string]types.InfoField {
	built := make(map[string]types.InfoField, len(candidates))
	for _, field := range candidates {
		built[field.Name] = field
	}
	return built
}

// Fields 返回当前生效的全部字段声明，顺序与配置文件一致
func Fields() []types.InfoField {
	mu.RLock()
	defer mu.RUnlock()

	out := make([]types.InfoField, len(fields))
	copy(out, fields)
	return out
}

// Field 查询单个字段的声明，未注册时返回 false
func Field(name string) (types.InfoField, bool) {
	mu.RLock()
	defer mu.RUnlock()

	field, ok := index[name]
	return field, ok
}

// SearchNameField 返回承担 searchname 角色的字段名。
// 注册表已校验必有且仅有一个，故此处不会为空。
func SearchNameField() string {
	mu.RLock()
	defer mu.RUnlock()

	for _, field := range fields {
		if field.Role == types.RoleSearchName {
			return field.Name
		}
	}
	return ""
}

// SummaryFieldNames 返回应作为列表表格列的字段名，顺序与注册表一致。
//
// 摘要是显示层的取舍：字段一多，每个都占一列会让表格宽到无法浏览，故只有勾选
// 「摘要」的字段成列，其余收进每行的详情里。存储与读写不受影响。
//
// 一个都没勾选时回落到记录名。Validate 会拦住这种注册表，但配置文件可以手工改，
// 而回落的代价只是少几列，塌成「一列 ID 加一堆看不出是谁的行」则是不可用。
//
// 由后端给出而非前端自己筛：这条回落规则两端各写一遍就会各错一次。
func SummaryFieldNames() []string {
	mu.RLock()
	defer mu.RUnlock()

	return summaryNamesOf(fields)
}

// summaryNamesOf 从给定声明中取摘要字段名。调用方须已持有锁。
func summaryNamesOf(candidates []types.InfoField) []string {
	names := make([]string, 0, len(candidates))
	for _, field := range candidates {
		if field.Summary {
			names = append(names, field.Name)
		}
	}

	if len(names) > 0 {
		return names
	}

	for _, field := range candidates {
		if field.Role == types.RoleSearchName {
			return []string{field.Name}
		}
	}

	return names
}

// Normalize 按当前生效的注册表对齐 Info
func Normalize(info types.LibraryInfo) types.LibraryInfo {
	mu.RLock()
	defer mu.RUnlock()

	return normalizeWith(index, info)
}

// NormalizeWith 按给定的字段声明对齐 Info，供保存注册表时用新声明补全历史数据
func NormalizeWith(candidates []types.InfoField, info types.LibraryInfo) types.LibraryInfo {
	return normalizeWith(buildIndex(candidates), info)
}

// normalizeWith 未声明的字段剔除，缺失的字段补为对应类型的空值，
// 值类型不符的也换成空值（多为注册表改过类型后的历史数据），状态非法则回落到 good。
// 返回新的 map，不改动传入的 info。读取与写入两侧都要经过这里，
// 使数据库中的旧 Info 无需人工迁移即可呈现新字段。
func normalizeWith(byName map[string]types.InfoField, info types.LibraryInfo) types.LibraryInfo {
	normalized := make(types.LibraryInfo, len(byName))

	for name, field := range byName {
		existing, ok := info[name]

		value := existing.Value
		if !ok || !field.Type.Matches(value) {
			value = field.Type.EmptyValue()
		}

		status := existing.Status
		if !status.IsValid() {
			status = types.StatusGood
		}

		normalized[name] = types.InfoValue{Value: value, Status: status}
	}

	return normalized
}

// Commit 用新的字段声明替换当前注册表并写回配置文件。
// 先换内存再落盘：落盘失败时回滚内存，保证内存与文件一致。
func Commit(candidates []types.InfoField) error {
	if err := Validate(candidates); err != nil {
		return err
	}

	mu.Lock()
	prevFields, prevIndex := fields, index
	fields = candidates
	index = buildIndex(candidates)
	file := path
	mu.Unlock()

	if err := writeFile(file, candidates); err != nil {
		mu.Lock()
		fields, index = prevFields, prevIndex
		mu.Unlock()
		return err
	}

	return nil
}

// writeFile 原子写入注册表文件：先写临时文件再改名，避免中途失败留下半个文件
func writeFile(file string, candidates []types.InfoField) error {
	raw, err := json.MarshalIndent(types.LibrarySchema{Fields: candidates}, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode library schema: %w", err)
	}
	raw = append(raw, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(file), ".library_schema-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp schema file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to write temp schema file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to close temp schema file: %w", err)
	}

	if err := os.Rename(tmpName, file); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to replace schema file %q: %w", file, err)
	}

	return nil
}
