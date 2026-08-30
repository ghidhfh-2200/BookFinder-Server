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
	// RoleFields 角色到字段名的映射，供客户端按角色定位字段而不硬编码键名。
	//
	// 各字段的 role 已在 Fields 里，这里再给一份反向索引，是因为按角色取字段是
	// 客户端的常用动作（「网站那一格填了什么」），每处都自己遍历一遍容易漏掉
	// 角色缺席的情况。
	RoleFields map[string]string `json:"role_fields"`
	// ReservedFields 内置字段及其锁定项，前端据此禁用对应的控件。
	//
	// 由后端给出而非前端按角色自己判断：哪几项锁定是内置字段表说了算
	// （记录名强制必填与摘要，网站两者都不强制），两端各写一遍就会各错一次。
	ReservedFields []reservedFieldInfo `json:"reserved_fields"`
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
		RoleFields:      schema.RoleFields(),
		ReservedFields:  reservedFieldNames(),
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
			string(types.StatusUnverified),
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

	// 先校正再校验：角色与内置字段的形状不是管理员的输入（见 types.InfoField.Role），
	// 请求里带错了应当被改写而非报错。校验因此只面对已经规整的声明。
	// 缺失的内置字段这里也会补回，故删掉内置字段这一操作等同于无效，不必单独报错。
	candidates, _ := types.ReconcileFields(req.Fields)

	if err := schema.Validate(candidates); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 字段名不可改，改名只能表现为删旧增新。此处拦住「只改名字」的误操作：
	// 它会连带删掉该字段的历史数据，需由管理员显式删除再新增。
	if err := checkRenames(candidates); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 被移除的字段：其过时报告一并清理，免得日后同名字段重建时继承旧计数
	removed := removedFieldNames(candidates)

	// 先落定注册表，再补全数据：补全依据的是新声明，顺序颠倒会用旧声明补错
	committed, err := schema.Commit(candidates)
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if err := models.DeleteFieldReports(removed); err != nil {
		logger.Errorf("清理已删除字段的过时报告失败: %v", err)
	}

	migrated, err := models.MigrateLibraryInfo(committed)
	if err != nil {
		// 注册表已生效但数据未补全：读取时 Normalize 仍会按新声明对齐，不影响可用性，
		// 只是库中记录暂未落盘为新形状，下次保存注册表会再试一次。
		logger.Errorf("注册表已更新，但补全已有记录失败: %v", err)
		utils.ResponseError(c, http.StatusInternalServerError,
			"注册表已保存并生效，但补全已有记录失败: "+err.Error())
		return
	}

	// 注册表变更会改写全表数据，记为 WARN 以便筛查
	detail := fmt.Sprintf("字段注册表已更新，共 %d 个字段，补全 %d 条记录", len(committed), migrated)
	if len(removed) > 0 {
		detail += "；移除字段 " + join(removed) + "（其数据与过时报告已清除）"
	}
	audit.Warn(c, types.ActionSchemaUpdate, detail)

	// 回落定后的字段与摘要字段：保存后前端要据此重建表格列与锁定状态，
	// 回请求原样的话，内置字段被校正掉的那几项在界面上看不出来
	utils.ResponseOK(c, gin.H{
		"fields":         committed,
		"summary_fields": schema.SummaryFieldNames(),
		"migrated":       migrated,
	})
}

// reservedFieldInfo 一个内置字段的锁定项。
// 内置字段一律不可删、类型不可改，故那两项不必逐个下发；
// 必填与摘要是否锁定则各字段不同。
type reservedFieldInfo struct {
	Name string `json:"name"`
	Role string `json:"role"`
	// LockRequired 必填不可改（该字段强制必填）
	LockRequired bool `json:"lock_required"`
	// LockSummary 摘要不可改（该字段强制作为列显示）
	LockSummary bool `json:"lock_summary"`
}

// reservedFieldNames 返回内置字段及其锁定项，供前端禁用对应控件
func reservedFieldNames() []reservedFieldInfo {
	out := make([]reservedFieldInfo, 0, len(types.ReservedFields))
	for _, field := range types.ReservedFields {
		out = append(out, reservedFieldInfo{
			Name:         field.Name,
			Role:         string(field.Role),
			LockRequired: field.ForceRequired,
			LockSummary:  field.ForceSummary,
		})
	}
	return out
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
