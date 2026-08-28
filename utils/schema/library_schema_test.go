package schema

import (
	"os"
	"path/filepath"
	"testing"

	"bookfinder-backend/types"
)

// searchNameJSON 承担 searchname 角色的字段，每份测试用注册表都要带上。
// 记录名必须声明 summary，否则校验不通过（它是搜索匹配的字段，藏进详情
// 会让表格只剩 ID）。
const searchNameJSON = `{"name": "FullName", "label": "全称", "type": "string", "required": true, "summary": true, "role": "searchname"}`

// writeSchema 把注册表内容写入临时文件并加载
func writeSchema(t *testing.T, content string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "library_schema.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入临时注册表失败: %v", err)
	}
	if err := Load(path); err != nil {
		t.Fatalf("加载注册表失败: %v", err)
	}
}

// str 构造一个字符串值的信息字段
func str(value string, status types.LibraryStatus) types.InfoValue {
	return types.InfoValue{Value: value, Status: status}
}

// TestNormalizeFillsMissingFields 新增字段后，旧记录应自动补出该字段的空值与 good 状态，
// 这是「无需人工迁移即可更新字段」的关键
func TestNormalizeFillsMissingFields(t *testing.T) {
	writeSchema(t, `{"fields": [`+searchNameJSON+`,
		{"name": "ShortName", "label": "简称", "type": "string", "required": false},
		{"name": "Floor", "label": "楼层", "type": "number", "required": false},
		{"name": "Open", "label": "开放", "type": "bool", "required": false},
		{"name": "Extra", "label": "附加", "type": "object", "required": false},
		{"name": "Tags", "label": "标签", "type": "array", "required": false}
	]}`)

	// 模拟库中只有 FullName 的历史记录
	got := Normalize(types.LibraryInfo{
		"FullName": str("国家图书馆", types.StatusOutdated),
	})

	wantEmpty := map[string]any{
		"ShortName": "",
		"Floor":     float64(0),
		"Open":      false,
	}
	for name, want := range wantEmpty {
		if got[name].Value != want {
			t.Errorf("缺失的 %s 应补为 %#v，实际为 %#v", name, want, got[name].Value)
		}
		if got[name].Status != types.StatusGood {
			t.Errorf("补出的 %s 状态应为 good，实际为 %q", name, got[name].Status)
		}
	}

	if m, ok := got["Extra"].Value.(map[string]any); !ok || len(m) != 0 {
		t.Errorf("Extra 应补为空对象，实际为 %#v", got["Extra"].Value)
	}
	if a, ok := got["Tags"].Value.([]any); !ok || len(a) != 0 {
		t.Errorf("Tags 应补为空数组，实际为 %#v", got["Tags"].Value)
	}

	// 已有字段的值与状态都要原样保留
	if got["FullName"].Value != "国家图书馆" {
		t.Errorf("已有值应保留，FullName 实际为 %#v", got["FullName"].Value)
	}
	if got["FullName"].Status != types.StatusOutdated {
		t.Errorf("已有状态应保留，FullName 实际为 %q", got["FullName"].Status)
	}
}

// TestNormalizeDropsUnregisteredFields 未注册的字段应被剔除
func TestNormalizeDropsUnregisteredFields(t *testing.T) {
	writeSchema(t, `{"fields": [`+searchNameJSON+`]}`)

	got := Normalize(types.LibraryInfo{
		"FullName": str("国家图书馆", types.StatusGood),
		"Removed":  str("已删除的字段", types.StatusGood),
		"Injected": str("请求里多传的字段", types.StatusGood),
	})

	if len(got) != 1 {
		t.Errorf("规范化后应只剩 1 个注册字段，实际为 %d 个: %#v", len(got), got)
	}
	for _, name := range []string{"Removed", "Injected"} {
		if _, ok := got[name]; ok {
			t.Errorf("未注册的字段 %s 应被剔除", name)
		}
	}
}

// TestNormalizeReplacesMismatchedType 值类型不符的应换成空值（注册表改过类型后的历史数据）
func TestNormalizeReplacesMismatchedType(t *testing.T) {
	writeSchema(t, `{"fields": [`+searchNameJSON+`,
		{"name": "Floor", "type": "number", "required": false}
	]}`)

	got := Normalize(types.LibraryInfo{
		"FullName": str("国家图书馆", types.StatusGood),
		"Floor":    str("三层", types.StatusGood), // 曾是 string，现声明为 number
	})

	if got["Floor"].Value != float64(0) {
		t.Errorf("类型不符的 Floor 应换为 0，实际为 %#v", got["Floor"].Value)
	}
}

// TestNormalizeFixesInvalidStatus 状态非法时应回落到 good
func TestNormalizeFixesInvalidStatus(t *testing.T) {
	writeSchema(t, `{"fields": [`+searchNameJSON+`]}`)

	got := Normalize(types.LibraryInfo{
		"FullName": str("国家图书馆", types.LibraryStatus("bogus")),
	})

	if got["FullName"].Status != types.StatusGood {
		t.Errorf("非法状态应回落到 good，实际为 %q", got["FullName"].Status)
	}
}

// TestNormalizeDoesNotMutateInput Normalize 应返回新 map，不改动传入的 Info
func TestNormalizeDoesNotMutateInput(t *testing.T) {
	writeSchema(t, `{"fields": [`+searchNameJSON+`]}`)

	input := types.LibraryInfo{
		"FullName": str("国家图书馆", types.StatusGood),
		"Removed":  str("已删除的字段", types.StatusGood),
	}
	Normalize(input)

	if _, ok := input["Removed"]; !ok {
		t.Error("Normalize 不应改动传入的 Info")
	}
}

// TestLoadRejectsInvalidSchema 注册表本身不合法时应拒绝加载
func TestLoadRejectsInvalidSchema(t *testing.T) {
	tests := map[string]string{
		"没有 searchname 角色": `{"fields": [{"name": "FullName", "type": "string", "required": true}]}`,
		"searchname 落在别的字段": `{"fields": [
			{"name": "Other", "type": "string", "required": true, "role": "searchname"}]}`,
		"searchname 类型不对": `{"fields": [
			{"name": "FullName", "type": "number", "required": true, "role": "searchname"}]}`,
		"searchname 非必填": `{"fields": [
			{"name": "FullName", "type": "string", "required": false, "role": "searchname"}]}`,
		"字段类型不受支持": `{"fields": [` + searchNameJSON + `,
			{"name": "Floor", "type": "int", "required": false}]}`,
		"角色不受支持": `{"fields": [` + searchNameJSON + `,
			{"name": "Floor", "type": "number", "required": false, "role": "bogus"}]}`,
		"字段名重复": `{"fields": [` + searchNameJSON + `,
			{"name": "Rules", "type": "string", "required": false},
			{"name": "Rules", "type": "string", "required": false}]}`,
		"字段名为空": `{"fields": [` + searchNameJSON + `,
			{"name": "", "type": "string", "required": false}]}`,
		"字段名含双引号": `{"fields": [` + searchNameJSON + `,
			{"name": "Bad\"Name", "type": "string", "required": false}]}`,
		"字段列表为空":    `{"fields": []}`,
		"不是合法 JSON": `{"fields": [`,
	}

	for name, content := range tests {
		path := filepath.Join(t.TempDir(), "library_schema.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("写入临时注册表失败: %v", err)
		}
		if err := Load(path); err == nil {
			t.Errorf("注册表「%s」应加载失败，实际通过了", name)
		}
	}
}

// TestLoadRejectsMissingFile 文件不存在时应报错，而非静默使用空注册表
func TestLoadRejectsMissingFile(t *testing.T) {
	if err := Load(filepath.Join(t.TempDir(), "not_exist.json")); err == nil {
		t.Error("注册表文件不存在时应返回错误")
	}
}

// TestSearchNameField 记录名字段可查出，且显示名可改
func TestSearchNameField(t *testing.T) {
	writeSchema(t, `{"fields": [`+searchNameJSON+`]}`)

	if got := SearchNameField(); got != types.SearchNameFieldName {
		t.Errorf("SearchNameField() = %q, want %q", got, types.SearchNameFieldName)
	}

	field, ok := Field("FullName")
	if !ok {
		t.Fatal("FullName 应存在于注册表中")
	}
	if field.DisplayName() != "全称" {
		t.Errorf("显示名应为「全称」，实际为 %q", field.DisplayName())
	}
}

// TestDisplayNameFallsBackToName 未设显示名时回落到字段名
func TestDisplayNameFallsBackToName(t *testing.T) {
	field := types.InfoField{Name: "Rules", Type: types.InfoTypeString}
	if field.DisplayName() != "Rules" {
		t.Errorf("未设 Label 时应回落到字段名，实际为 %q", field.DisplayName())
	}
}

// TestCommitWritesFileAndHotReloads 保存注册表应立即生效并写回文件，
// 重新加载同一文件后内容一致
func TestCommitWritesFileAndHotReloads(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "library_schema.json")
	if err := os.WriteFile(file, []byte(`{"fields": [`+searchNameJSON+`]}`), 0o600); err != nil {
		t.Fatalf("写入临时注册表失败: %v", err)
	}
	if err := Load(file); err != nil {
		t.Fatalf("加载注册表失败: %v", err)
	}

	next := []types.InfoField{
		{Name: "FullName", Label: "馆名", Type: types.InfoTypeString, Required: true,
			Summary: true, Role: types.RoleSearchName},
		{Name: "Rules", Label: "规则", Type: types.InfoTypeString},
	}
	if err := Commit(next); err != nil {
		t.Fatalf("保存注册表失败: %v", err)
	}

	// 热更新：无需重启即已生效
	if len(Fields()) != 2 {
		t.Errorf("保存后应有 2 个字段，实际为 %d 个", len(Fields()))
	}
	field, _ := Field("FullName")
	if field.DisplayName() != "馆名" {
		t.Errorf("显示名应已改为「馆名」，实际为 %q", field.DisplayName())
	}

	// 落盘：重新加载同一文件应得到相同内容
	if err := Load(file); err != nil {
		t.Fatalf("重新加载注册表失败: %v", err)
	}
	if len(Fields()) != 2 {
		t.Errorf("重新加载后应有 2 个字段，实际为 %d 个", len(Fields()))
	}
	if _, ok := Field("Rules"); !ok {
		t.Error("重新加载后 Rules 应存在，说明未正确写回文件")
	}
}

// TestCommitRejectsInvalid 非法声明不应生效，也不应写坏当前注册表
func TestCommitRejectsInvalid(t *testing.T) {
	writeSchema(t, `{"fields": [`+searchNameJSON+`]}`)

	// 缺少 searchname 角色
	err := Commit([]types.InfoField{{Name: "Rules", Type: types.InfoTypeString}})
	if err == nil {
		t.Fatal("缺少 searchname 角色的声明应被拒绝")
	}

	if _, ok := Field("FullName"); !ok {
		t.Error("拒绝保存后原注册表应保持不变")
	}
}

// TestNormalizeWith 按给定声明对齐，供保存注册表时补全历史数据
func TestNormalizeWith(t *testing.T) {
	writeSchema(t, `{"fields": [`+searchNameJSON+`]}`)

	next := []types.InfoField{
		{Name: "FullName", Type: types.InfoTypeString, Required: true, Role: types.RoleSearchName},
		{Name: "Rules", Type: types.InfoTypeString},
	}

	got := NormalizeWith(next, types.LibraryInfo{
		"FullName": str("国家图书馆", types.StatusGood),
		"Dropped":  str("将被剔除", types.StatusGood),
	})

	if len(got) != 2 {
		t.Errorf("应按新声明得到 2 个字段，实际为 %d 个: %#v", len(got), got)
	}
	if got["Rules"].Value != "" {
		t.Errorf("新增的 Rules 应补为空串，实际为 %#v", got["Rules"].Value)
	}
	if _, ok := got["Dropped"]; ok {
		t.Error("不在新声明中的字段应被剔除")
	}
}
