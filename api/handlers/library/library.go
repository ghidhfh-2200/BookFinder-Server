package library

import (
	"net/http"
	"strconv"

	"bookfinder-backend/api/middlewares"
	"bookfinder-backend/database"
	"bookfinder-backend/logger"
	"bookfinder-backend/models"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/audit"
	"bookfinder-backend/utils/checker"
	"bookfinder-backend/utils/dedup"
	"bookfinder-backend/utils/ratelimit"
	"bookfinder-backend/utils/schema"

	"github.com/gin-gonic/gin"
)

// visitorSignalHeader 客户端上报设备特征信号的请求头。
// 该信号由客户端计算后上报，可被伪造，故只用于启发式查重，不作为身份依据。
// 头名避开 fingerprint 一词：内容拦截扩展会按该关键词拦掉请求。
const visitorSignalHeader = "X-Visitor-Signal"

// GetLibraries 分页查询图书馆
func GetLibraries(c *gin.Context) {
	page, size := utils.Pagination(c)

	query := &types.LibraryQuery{
		Keyword: c.Query("keyword"),
		Page:    page,
		Size:    size,
	}

	libraries, total, err := models.GetLibraries(query)
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	items, err := withReportStats(c, libraries)
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	utils.ResponsePaginated(c, items, total, page, size)
}

// libraryItem 图书馆及其各字段的报告情况
type libraryItem struct {
	types.Library
	// Reports 各字段的报告次数与当前访问者是否已报告，键为字段名
	Reports map[string]types.FieldReportStat `json:"reports"`
	// CanDelete 当前访问者是否可删除这条记录：管理员恒为真，
	// 普通访问者仅对自己创建的记录为真。
	//
	// 由后端逐条判定后下发，而不是把 creator_key 给前端自己比：
	// 那个哈希是身份凭据的派生值，谁都能读列表，给出去等于泄露
	// 「哪些记录是同一个人建的」。真正的拦截在 DeleteLibrary 里，
	// 这一项只决定按钮显不显示。
	CanDelete bool `json:"can_delete"`
}

// withReportStats 给图书馆附上每个字段的报告统计。
// 计数与「已报告」两项各用一次聚合查询取回，不逐行查库。
func withReportStats(c *gin.Context, libraries []types.Library) ([]libraryItem, error) {
	ids := make([]int, 0, len(libraries))
	for _, library := range libraries {
		ids = append(ids, library.ID)
	}

	counts, err := models.CountFieldReports(ids)
	if err != nil {
		return nil, err
	}

	// 取不到访问者标识时「已报告」一律为 false，不影响计数展示
	reporterKey, _ := middlewares.GetVisitorKeyFromContext(c)
	reported, err := models.ListReportedFields(ids, reporterKey)
	if err != nil {
		return nil, err
	}

	// 同一来源 IP 报告过的字段，用于提前提示疑似重复
	sameOrigin, err := models.ListSameOriginFields(ids, c.ClientIP())
	if err != nil {
		return nil, err
	}

	// 管理员可删任意记录；其余人只能删自己创建的。
	// reporterKey 同时也是创建者标识，两者都是访问者令牌的哈希。
	isAdmin := middlewares.IsAdmin(c)

	items := make([]libraryItem, 0, len(libraries))
	for _, library := range libraries {
		stats := make(map[string]types.FieldReportStat, len(library.Info))
		for name := range library.Info {
			mine := reported[library.ID][name]
			stats[name] = types.FieldReportStat{
				Count:     counts[library.ID][name],
				Threshold: types.OutdatedReportThreshold,
				Reported:  mine,
				// 同来源报过但不是自己提交的，再报多半会被判重
				Suspected: !mine && sameOrigin[library.ID][name],
			}
		}
		items = append(items, libraryItem{
			Library:   library,
			Reports:   stats,
			CanDelete: isAdmin || ownedBy(library, reporterKey),
		})
	}

	return items, nil
}

// GetLibrary 获取单个图书馆
func GetLibrary(c *gin.Context) {
	id, ok := libraryID(c)
	if !ok {
		return
	}

	library, err := models.GetLibraryByID(id)
	if err != nil {
		utils.ResponseError(c, http.StatusNotFound, "图书馆不存在")
		return
	}

	utils.ResponseOK(c, library)
}

// CreateLibrary 创建图书馆。
// Info 先按注册表对齐：未声明的字段剔除、缺失的字段补空值。ID 由数据库自增分配。
func CreateLibrary(c *gin.Context) {
	var library types.Library
	if err := c.ShouldBindJSON(&library); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	if library.Info == nil {
		utils.ResponseError(c, http.StatusBadRequest, "info 不能为空")
		return
	}

	// 规范化会把缺失或非法的状态补为 good，故此处无需再逐字段设默认值
	library.Info = schema.Normalize(library.Info)

	if err := checker.ValidateLibraryCreate(&library); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 记下创建者，供其日后删除自己创建的记录。
	// 只从令牌推出，不受请求体影响（CreatorKey 带 json:"-"，绑不进来）——
	// 否则填上别人的哈希即可冒名。取不到令牌时留空，该记录只有管理员能删。
	library.CreatorKey, _ = middlewares.GetVisitorKeyFromContext(c)

	if err := models.CreateLibrary(&library); err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	audit.Infof(c, types.ActionLibraryCreate, "新增图书馆 #%d %s", library.ID, libraryName(library.Info))

	utils.ResponseSuccessWithCustomMessage(c, "创建图书馆成功")
}

// UpdateLibrary 更新图书馆，只改 Info。ID 与创建时间不可改写。
func UpdateLibrary(c *gin.Context) {
	id, ok := libraryID(c)
	if !ok {
		return
	}

	existing, err := models.GetLibraryByID(id)
	if err != nil {
		utils.ResponseError(c, http.StatusNotFound, "图书馆不存在")
		return
	}

	// 先按原始请求体记下哪些字段显式带了 status，规范化会把缺失的状态补成 good，
	// 补完就分不清「请求要改成 good」和「请求没提状态」了
	var body struct {
		Info map[string]struct {
			Status *types.LibraryStatus `json:"status"`
		} `json:"info"`
	}
	if err := c.ShouldBindBodyWithJSON(&body); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	var updated types.Library
	if err := c.ShouldBindBodyWithJSON(&updated); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	if updated.Info == nil {
		utils.ResponseError(c, http.StatusBadRequest, "info 不能为空")
		return
	}

	updated.Info = schema.Normalize(updated.Info)

	// 请求未带某字段的状态时沿用原值，避免普通更新顺手把「已过时」抹回 good
	for name, entry := range updated.Info {
		if body.Info[name].Status != nil {
			continue
		}
		if previous, ok := existing.Info[name]; ok {
			entry.Status = previous.Status
			updated.Info[name] = entry
		}
	}

	if err := checker.ValidateLibraryUpdate(&updated); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	existing.Info = updated.Info

	if err := models.UpdateLibrary(existing); err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	audit.Infof(c, types.ActionLibraryUpdate, "修改图书馆 #%d %s", id, libraryName(existing.Info))

	utils.ResponseSuccessWithCustomMessage(c, "更新图书馆成功")
}

// DeleteLibrary 删除图书馆。
//
// 两条通路：管理员可删任意记录；普通访问者只能删自己创建的。
// 后者不持有 PermissionLibraryDelete，故权限判定放在这里而非中间件里——
// 中间件只认权限位，认不出「这条是不是你建的」。
func DeleteLibrary(c *gin.Context) {
	id, ok := libraryID(c)
	if !ok {
		return
	}

	// 名称在删除前取出，供日志留痕
	existing, err := models.GetLibraryByID(id)
	if err != nil {
		utils.ResponseError(c, http.StatusNotFound, "图书馆不存在")
		return
	}
	name := libraryName(existing.Info)

	isAdmin := middlewares.IsAdmin(c)

	if !isAdmin {
		visitorKey, _ := middlewares.GetVisitorKeyFromContext(c)
		if !ownedBy(*existing, visitorKey) {
			utils.ResponseError(c, http.StatusForbidden, "只能删除自己创建的图书馆")
			return
		}
	}

	if err := models.DeleteLibrary(id); err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 删除不可恢复，记为 WARN 以便筛查。区分是谁删的：
	// 创建者自删与管理员删除在排查时是两回事
	if isAdmin {
		audit.Warnf(c, types.ActionLibraryDelete, "删除图书馆 #%d %s", id, name)
	} else {
		audit.Warnf(c, types.ActionLibraryDelete, "创建者删除自己创建的图书馆 #%d %s", id, name)
	}

	utils.ResponseSuccessWithCustomMessage(c, "删除图书馆成功")
}

// reportResponse 报告与撤销的返回体，供前端即时更新计数与按钮状态
type reportResponse struct {
	Count     int                 `json:"count"`
	Threshold int                 `json:"threshold"`
	Reported  bool                `json:"reported"`
	Status    types.LibraryStatus `json:"status,omitempty"`
	// Duplicate 本次被判为疑似重复，未计入次数
	Duplicate bool `json:"duplicate,omitempty"`
}

// ReportFieldOutdated 报告某条记录中指定字段的信息已过时。
// 报告按访问者去重，累计到 types.OutdatedReportThreshold 次才把字段置为过时；
// 未达阈值只记次数，字段状态不变。
func ReportFieldOutdated(c *gin.Context) {
	id, name, ok := fieldTarget(c)
	if !ok {
		return
	}

	reporterKey, ok := middlewares.GetVisitorKeyFromContext(c)
	if !ok {
		utils.ResponseError(c, http.StatusBadRequest, "无法识别访问者，请启用 Cookie 后重试")
		return
	}

	existing, err := models.GetLibraryByID(id)
	if err != nil {
		utils.ResponseError(c, http.StatusNotFound, "图书馆不存在")
		return
	}
	if _, ok := existing.Info[name]; !ok {
		utils.ResponseError(c, http.StatusNotFound, "该记录不含此字段")
		return
	}

	signals := dedup.Signals{
		ReporterKey: reporterKey,
		ReporterIP:  c.ClientIP(),
		Fingerprint: utils.HashVisitorSignal(c.GetHeader(visitorSignalHeader)),
	}

	verdict, err := dedup.Check(signals, dedup.Lookup{
		AlreadyCounted: func() (bool, error) {
			return models.HasFieldReport(id, name, signals.ReporterKey)
		},
		SimilarCount: func() (int64, error) {
			return models.CountSuspectedDuplicates(id, name, signals.ReporterIP, signals.Fingerprint)
		},
	})
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if verdict == dedup.VerdictSuspectedDuplicate {
		logger.Warnf("疑似重复报告，未计数：图书馆 %d 字段 %s，来源 IP %s", id, name, signals.ReporterIP)

		// 一并回传当前计数，前端据此提示「疑似重复，未计数」并同步进度
		count, countErr := models.CountFieldReport(id, name)
		if countErr != nil {
			utils.ResponseError(c, http.StatusInternalServerError, countErr.Error())
			return
		}

		audit.Warnf(c, types.ActionFieldReportRejected,
			"疑似重复报告未计数：图书馆 #%d 字段 %s", id, name)

		// 累计到当日计数，达阈值会触发自动封禁（见 ratelimit.EvaluateBan 规则三）
		if rdb := database.GetRedis(); rdb != nil {
			if _, err := ratelimit.RecordDuplicate(c.Request.Context(), rdb, signals.ReporterIP); err != nil {
				logger.Warnf("累计疑似重复报告失败 (%s): %v", signals.ReporterIP, err)
			}
		}

		c.JSON(http.StatusOK, utils.Response{
			Code:    http.StatusConflict,
			Message: "该信息已由相同来源报告过，这次未计入",
			Data: reportResponse{
				Count:     count,
				Threshold: types.OutdatedReportThreshold,
				Reported:  false,
				Duplicate: true,
			},
		})
		return
	}

	// 已计数过的重复提交照常走下去：唯一索引会忽略插入，只把当前次数回给前端
	count, err := models.AddFieldReport(&types.FieldReport{
		LibraryID:   id,
		FieldName:   name,
		ReporterKey: signals.ReporterKey,
		ReporterIP:  signals.ReporterIP,
		Fingerprint: signals.Fingerprint,
	})
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	status, err := syncFieldStatus(id, name, count)
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 达到阈值意味着该信息被弃用，记为 WARN
	if status == types.StatusOutdated {
		audit.Warnf(c, types.ActionFieldReport,
			"报告图书馆 #%d 字段 %s 过时（%d/%d，已达阈值，标为过时）",
			id, name, count, types.OutdatedReportThreshold)
	} else {
		audit.Infof(c, types.ActionFieldReport,
			"报告图书馆 #%d 字段 %s 过时（%d/%d）",
			id, name, count, types.OutdatedReportThreshold)
	}

	utils.ResponseOK(c, reportResponse{
		Count:     count,
		Threshold: types.OutdatedReportThreshold,
		Reported:  true,
		Status:    status,
	})
}

// RevokeFieldOutdated 撤销自己对某个字段的过时报告。
// 只删除自己那一行，报告数随之 -1，别人的报告不受影响；
// 撤销后次数降到阈值以下时，字段状态自动恢复为有效。
func RevokeFieldOutdated(c *gin.Context) {
	id, name, ok := fieldTarget(c)
	if !ok {
		return
	}

	reporterKey, ok := middlewares.GetVisitorKeyFromContext(c)
	if !ok {
		utils.ResponseError(c, http.StatusBadRequest, "无法识别访问者，请启用 Cookie 后重试")
		return
	}

	count, removed, err := models.RemoveFieldReport(id, name, reporterKey)
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed {
		utils.ResponseError(c, http.StatusNotFound, "你没有报告过该信息")
		return
	}

	status, err := syncFieldStatus(id, name, count)
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	audit.Infof(c, types.ActionFieldReportRevoke,
		"撤销对图书馆 #%d 字段 %s 的过时报告（剩余 %d/%d）",
		id, name, count, types.OutdatedReportThreshold)

	utils.ResponseOK(c, reportResponse{
		Count:     count,
		Threshold: types.OutdatedReportThreshold,
		Reported:  false,
		Status:    status,
	})
}

// syncFieldStatus 按报告次数推导并写回字段状态，返回写回后的状态。
// 达到阈值为过时，低于阈值为有效，故撤销报告可让状态自动恢复。
func syncFieldStatus(libraryID int, fieldName string, count int) (types.LibraryStatus, error) {
	status := types.StatusGood
	if count >= types.OutdatedReportThreshold {
		status = types.StatusOutdated
	}

	if err := models.SetFieldStatus(libraryID, fieldName, status); err != nil {
		return "", err
	}

	return status, nil
}

// ownedBy 判断某条记录是否由持有该标识的访问者创建。
//
// 两个空串不算相等：存量记录的 CreatorKey 为空，而取不到令牌的访问者
// visitorKey 也是空——直接比会把这些记录的删除权发给每个人。
//
// 列表下发 can_delete 与删除时的实际拦截共用这一个判断，
// 否则两处各写一遍，按钮显示与真正的权限迟早对不上。
func ownedBy(library types.Library, visitorKey string) bool {
	return visitorKey != "" && library.CreatorKey != "" && library.CreatorKey == visitorKey
}

// libraryName 取记录名，供日志留痕；缺失时返回占位
func libraryName(info types.LibraryInfo) string {
	if name, ok := info.GetString(schema.SearchNameField()); ok && name != "" {
		return name
	}
	return "(未命名)"
}

// fieldTarget 取出并校验路径中的图书馆 ID 与字段名
func fieldTarget(c *gin.Context) (int, string, bool) {
	id, ok := libraryID(c)
	if !ok {
		return 0, "", false
	}

	name := c.Param("field")
	if _, registered := schema.Field(name); !registered {
		utils.ResponseError(c, http.StatusBadRequest, "字段不在注册表中")
		return 0, "", false
	}

	return id, name, true
}

// libraryID 取出并校验路径中的图书馆 ID，不合法时直接写出响应并返回 false
func libraryID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		utils.ResponseError(c, http.StatusBadRequest, "图书馆 ID 不合法")
		return 0, false
	}
	return id, true
}
