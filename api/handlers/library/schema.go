package library

import (
	"fmt"
	"net/http"
	"strings"

	"bookfinder-backend/logger"
	"bookfinder-backend/models"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/audit"
	"bookfinder-backend/utils/schema"

	"github.com/gin-gonic/gin"
)

// schemaResponse 注册表响应。
// 一并给出受支持的类型与角色，前端据此渲染下拉项，不必自己维护一份枚举。
type schemaResponse struct {
	Fields []types.InfoField `json:"fields"`
	// SummaryFields 应作为表格列显示的字段名，顺序与 Fields 一致。
	//
	// 由后端算而非前端自己筛：一个都没勾时要回落到记录名，这条规则
	// 两端各写一遍就会各错一次（见 types.SummaryFields）。
	SummaryFields []string `json:"summary_fields"`
	// SearchNameField 承担 searchname 角色的字段名，该字段不可删除
	SearchNameField string `json:"search_name_field"`
	// Types 受支持的值类型
	Types []string `json:"types"`
	// Statuses 受支持的字段状态取值
	Statuses []string `json:"statuses"`
}

// GetSchema 读取字段注册表。
// 前端据此动态渲染图书馆表格与表单，故字段名一律从这里取，不在前端硬编码。
func GetSchema(c *gin.Context) {
	utils.ResponseOK(c, schemaResponse{
		Fields:          schema.Fields(),
		SummaryFields:   schema.SummaryFieldNames(),
		SearchNameField: schema.SearchNameField(),
		Types: []string{
			string(types.InfoTypeString),
			string(types.InfoTypeNumber),
			string(types.InfoTypeBool),
			string(types.InfoTypeObject),
			string(types.InfoTypeArray),
		},
		Statuses: []string{
			string(types.StatusGood),
			string(types.StatusOutdated),
		},
	})
}

// UpdateSchema 保存字段注册表。
// 字段名是标识符，只能增删不能改；显示名与类型可改。
// 保存成功后立即热更新，并按新声明补全库中已有记录：新增字段补空值，删除字段剔除。
func UpdateSchema(c *gin.Context) {
	var req types.SchemaUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := schema.Validate(req.Fields); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 字段名不可改，改名只能表现为删旧增新。此处拦住「只改名字」的误操作：
	// 它会连带删掉该字段的历史数据，需由管理员显式删除再新增。
	if err := checkRenames(req.Fields); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 被移除的字段：其过时报告一并清理，免得日后同名字段重建时继承旧计数
	removed := removedFieldNames(req.Fields)

	// 先落定注册表，再补全数据：补全依据的是新声明，顺序颠倒会用旧声明补错
	if err := schema.Commit(req.Fields); err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if err := models.DeleteFieldReports(removed); err != nil {
		logger.Errorf("清理已删除字段的过时报告失败: %v", err)
	}

	migrated, err := models.MigrateLibraryInfo(req.Fields)
	if err != nil {
		// 注册表已生效但数据未补全：读取时 Normalize 仍会按新声明对齐，不影响可用性，
		// 只是库中记录暂未落盘为新形状，下次保存注册表会再试一次。
		logger.Errorf("注册表已更新，但补全已有记录失败: %v", err)
		utils.ResponseError(c, http.StatusInternalServerError,
			"注册表已保存并生效，但补全已有记录失败: "+err.Error())
		return
	}

	// 注册表变更会改写全表数据，记为 WARN 以便筛查
	detail := fmt.Sprintf("字段注册表已更新，共 %d 个字段，补全 %d 条记录", len(req.Fields), migrated)
	if len(removed) > 0 {
		detail += "；移除字段 " + join(removed) + "（其数据与过时报告已清除）"
	}
	audit.Warn(c, types.ActionSchemaUpdate, detail)

	// 一并回新的摘要字段：保存后前端要据此重建表格列，
	// 少了它列还是旧的，改完摘要看不到效果
	utils.ResponseOK(c, gin.H{
		"fields":         req.Fields,
		"summary_fields": schema.SummaryFieldNames(),
		"migrated":       migrated,
	})
}

// removedFieldNames 返回当前注册表中、不在新声明里的字段名
func removedFieldNames(candidates []types.InfoField) []string {
	next := make(map[string]struct{}, len(candidates))
	for _, field := range candidates {
		next[field.Name] = struct{}{}
	}

	var removed []string
	for _, field := range schema.Fields() {
		if _, kept := next[field.Name]; !kept {
			removed = append(removed, field.Name)
		}
	}
	return removed
}

// checkRenames 检出疑似改名的操作：新增字段与被删字段一一对应时，多半是想改名。
// 字段名是标识符，改名等同于丢弃原字段的全部历史数据，故要求显式分两步操作。
func checkRenames(candidates []types.InfoField) error {
	removed := removedFieldNames(candidates)

	existing := make(map[string]struct{})
	for _, field := range schema.Fields() {
		existing[field.Name] = struct{}{}
	}

	var added []string
	for _, field := range candidates {
		if _, had := existing[field.Name]; !had {
			added = append(added, field.Name)
		}
	}

	// 同一次提交里既删又增，无从判断是改名还是恰好一起做，交由管理员分两次提交
	if len(removed) > 0 && len(added) > 0 {
		return &renameError{Removed: removed, Added: added}
	}

	return nil
}

// renameError 同一次提交里既删字段又增字段
type renameError struct {
	Removed []string
	Added   []string
}

func (e *renameError) Error() string {
	return "字段名是标识符，不能改名。本次提交同时删除了 " + join(e.Removed) +
		"、新增了 " + join(e.Added) +
		"。若确实要改名，请分两次提交：先删除旧字段（其数据会一并清除），再新增新字段。"
}

// join 用顿号连接字段名
func join(names []string) string {
	return strings.Join(names, "、")
}
