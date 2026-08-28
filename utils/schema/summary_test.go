package schema

import (
	"strings"
	"testing"

	"bookfinder-backend/types"
)

// TestSummaryFieldNamesKeepsSchemaOrder 摘要字段的顺序须与注册表一致：
// 管理员拖拽调整过的顺序，表格列要照此呈现
func TestSummaryFieldNamesKeepsSchemaOrder(t *testing.T) {
	writeSchema(t, `{"fields": [`+searchNameJSON+`,
		{"name": "ShortName", "type": "string", "summary": true},
		{"name": "WebSite", "type": "string", "summary": false},
		{"name": "Floor", "type": "number", "summary": true}
	]}`)

	got := SummaryFieldNames()
	want := []string{"FullName", "ShortName", "Floor"}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("摘要字段 = %v，期望 %v", got, want)
	}
}

// TestSummaryFieldNamesExcludesUnchecked 未勾选的字段不该成为表格列——
// 那正是这个开关的用途：字段一多，每个都占一列会让表格宽到无法浏览
func TestSummaryFieldNamesExcludesUnchecked(t *testing.T) {
	writeSchema(t, `{"fields": [`+searchNameJSON+`,
		{"name": "ShortName", "type": "string", "summary": false},
		{"name": "WebSite", "type": "string", "summary": false}
	]}`)

	got := SummaryFieldNames()
	if len(got) != 1 || got[0] != "FullName" {
		t.Errorf("只勾了记录名时摘要应只有它，实际为 %v", got)
	}
}

// TestValidateRejectsSearchNameNotSummary 记录名必须是摘要。
//
// 它是搜索匹配的字段，也是这条记录的身份：藏进详情后表格就成了
// 「一列 ID 加一堆看不出是谁的行」。
func TestValidateRejectsSearchNameNotSummary(t *testing.T) {
	err := Validate([]types.InfoField{
		{Name: "FullName", Type: types.InfoTypeString, Required: true,
			Summary: false, Role: types.RoleSearchName},
	})

	if err == nil {
		t.Fatal("记录名未勾选摘要时应被拒绝")
	}
	if !strings.Contains(err.Error(), "摘要") {
		t.Errorf("错误信息应指出摘要，实际为: %v", err)
	}
}

// TestValidateAcceptsAllSummary 全部勾选是合法的：字段少时每列都显示很合理
func TestValidateAcceptsAllSummary(t *testing.T) {
	err := Validate([]types.InfoField{
		{Name: "FullName", Type: types.InfoTypeString, Required: true,
			Summary: true, Role: types.RoleSearchName},
		{Name: "ShortName", Type: types.InfoTypeString, Summary: true},
	})

	if err != nil {
		t.Errorf("全部勾选摘要应通过校验: %v", err)
	}
}

// TestSummaryDoesNotAffectNormalize 摘要只是显示层的取舍，不该影响存储：
// 未勾选的字段照常存、照常返回，否则一次显示设置的改动会丢数据
func TestSummaryDoesNotAffectNormalize(t *testing.T) {
	writeSchema(t, `{"fields": [`+searchNameJSON+`,
		{"name": "WebSite", "type": "string", "summary": false}
	]}`)

	normalized := Normalize(types.LibraryInfo{
		"FullName": str("上海图书馆", types.StatusGood),
		"WebSite":  str("https://library.sh.cn", types.StatusGood),
	})

	entry, ok := normalized["WebSite"]
	if !ok {
		t.Fatal("未勾选摘要的字段被剔除了，摘要不该影响存储")
	}
	if entry.Value != "https://library.sh.cn" {
		t.Errorf("未勾选摘要的字段值被改动了: %v", entry.Value)
	}
}
