package library

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"bookfinder-backend/api/middlewares"
	"bookfinder-backend/database"
	"bookfinder-backend/logger"
	"bookfinder-backend/models"
	"bookfinder-backend/services"
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

	counts, err := models.CountFieldReports(ids, types.ReportOutdated)
	if err != nil {
		return nil, err
	}

	// 取不到访问者标识时「已报告」一律为 false，不影响计数展示
	reporterKey, _ := middlewares.GetVisitorKeyFromContext(c)
	reported, err := models.ListReportedFields(ids, types.ReportOutdated, reporterKey)
	if err != nil {
		return nil, err
	}

	// 同一来源 IP 报告过的字段，用于提前提示疑似重复
	sameOrigin, err := models.ListSameOriginFields(ids, types.ReportOutdated, c.ClientIP())
	if err != nil {
		return nil, err
	}

	// 确认票另算一套：未验证的网站要靠它转正，与过时票互不相干
	verifyCounts, err := models.CountFieldReports(ids, types.ReportVerify)
	if err != nil {
		return nil, err
	}
	verified, err := models.ListReportedFields(ids, types.ReportVerify, reporterKey)
	if err != nil {
		return nil, err
	}

	// 管理员可删任意记录；其余人只能删自己创建的。
	// reporterKey 同时也是创建者标识，两者都是访问者令牌的哈希。
	isAdmin := middlewares.IsAdmin(c)

	items := make([]libraryItem, 0, len(libraries))
	for _, library := range libraries {
		stats := make(map[string]types.FieldReportStat, len(library.Info))
		for name, entry := range library.Info {
			mine := reported[library.ID][name]
			stat := types.FieldReportStat{
				Count:     counts[library.ID][name],
				Threshold: types.OutdatedReportThreshold,
				Reported:  mine,
				// 同来源报过但不是自己提交的，再报多半会被判重
				Suspected: !mine && sameOrigin[library.ID][name],
			}

			// 确认进度只在未验证时有意义：已转正的字段再显示「2/3」会让人以为还差票
			if entry.Status == types.StatusUnverified {
				stat.VerifyCount = verifyCounts[library.ID][name]
				stat.VerifyThreshold = types.VerifyReportThreshold
				stat.Verified = verified[library.ID][name]
			}

			stats[name] = stat
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

	// 新填的网站一律从「未验证」起步：填一个地址进来是零成本的，
	// 而读到它的人会照着点开，故要攒够他人确认才转正
	markNewWebsitesUnverified(library.Info)

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

	// 网站地址一改就退回未验证，并清掉旧地址攒下的票：那些票是投给旧地址的，
	// 留着等于「改一次就能把已验证的状态套给任意新 URL」
	revalidated, err := resetChangedWebsites(id, existing.Info, updated.Info)
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	existing.Info = updated.Info

	if err := models.UpdateLibrary(existing); err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	audit.Infof(c, types.ActionLibraryUpdate, "修改图书馆 #%d %s", id, libraryName(existing.Info))

	if len(revalidated) > 0 {
		audit.Infof(c, types.ActionLibraryUpdate,
			"图书馆 #%d 的 %s 地址已变更，退回未验证并清空原有确认",
			id, strings.Join(revalidated, "、"))
		utils.ResponseSuccessWithCustomMessage(c,
			fmt.Sprintf("更新成功。网站地址已变更，需 %d 人确认后才会标为有效",
				types.VerifyReportThreshold))
		return
	}

	utils.ResponseSuccessWithCustomMessage(c, "更新图书馆成功")
}

// markNewWebsitesUnverified 把非空的网站字段标为未验证，用于新建记录。
func markNewWebsitesUnverified(info types.LibraryInfo) {
	for _, name := range schema.WebsiteFields() {
		entry, ok := info[name]
		if !ok {
			continue
		}
		// 空网站不需要验证：没有地址可点，标未验证只是徒增一个待处理项
		if url, _ := entry.Value.(string); strings.TrimSpace(url) == "" {
			continue
		}
		entry.Status = types.StatusUnverified
		info[name] = entry
	}
}

// resetChangedWebsites 比对新旧网站地址，地址变了就退回未验证并清空该字段的全部报告。
// 返回被重置的字段名，供留痕与提示。
//
// 两种票都清：确认票是投给旧地址的，过时票针对的也是旧地址，
// 换了地址后两者都不再是对当前内容的判断。
func resetChangedWebsites(libraryID int, before, after types.LibraryInfo) ([]string, error) {
	var reset []string

	for _, name := range schema.WebsiteFields() {
		entry, ok := after[name]
		if !ok {
			continue
		}

		newURL, _ := entry.Value.(string)
		oldURL, _ := before[name].Value.(string)
		if strings.TrimSpace(newURL) == strings.TrimSpace(oldURL) {
			continue
		}

		// 清空成了空地址：无从验证，回到 good，免得留一个永远转不正的未验证项
		if strings.TrimSpace(newURL) == "" {
			entry.Status = types.StatusGood
		} else {
			entry.Status = types.StatusUnverified
		}
		after[name] = entry

		if err := models.DeleteFieldReportsOf(libraryID, name); err != nil {
			return nil, err
		}
		reset = append(reset, name)
	}

	return reset, nil
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
	// Stale 本次因该字段的状态已变而被拒（客户端页面过期）。
	// 为真时 status 与 count 是服务端现状，前端据此就地纠正显示并提示刷新。
	Stale bool `json:"stale,omitempty"`
}

// reportOutcome 把服务层结果渲染成响应体
func reportOutcome(outcome services.FieldReportOutcome, reported bool) reportResponse {
	return reportResponse{
		Count:     outcome.Count,
		Threshold: outcome.Threshold,
		Reported:  reported,
		Status:    outcome.Status,
		Duplicate: outcome.Duplicate,
		Stale:     outcome.Stale,
	}
}

// reportErrorStatus 把服务层的业务错误映射为 HTTP 状态码。
// 服务层不认识 HTTP，故映射留在这里。
//
// 状态已变的几种（已过时、已转正）用 409 Conflict：那不是请求写错了，
// 而是客户端看到的状态过期了，与 400「参数不对」是两回事。
func reportErrorStatus(err error) int {
	switch {
	case errors.Is(err, services.ErrLibraryNotFound),
		errors.Is(err, services.ErrFieldNotFound),
		errors.Is(err, services.ErrNoSuchReport):
		return http.StatusNotFound
	case errors.Is(err, services.ErrNotWebsiteField):
		return http.StatusBadRequest
	// 状态机拒绝：客户端看到的状态过期了，与 400「参数不对」是两回事
	case errors.Is(err, services.ErrStatusLocked):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// respondReportError 写出一次被拒的报告操作。
//
// 状态已变（Stale）时附带服务端现状，让停在过期页面上的客户端就地纠正显示——
// 否则用户只看到一句错误，页面还显示着「差 1 票」，会反复点同一个按钮。
func respondReportError(c *gin.Context, outcome services.FieldReportOutcome, err error, message string) {
	code := reportErrorStatus(err)

	if outcome.Stale {
		utils.ResponseErrorWithData(c, code, message, reportOutcome(outcome, false))
		return
	}

	utils.ResponseError(c, code, message)
}

// visitorSignals 取本次请求的身份信号。取不到令牌时写出响应并返回 false：
// 没有令牌就无从去重，一人多投会让阈值形同虚设。
func visitorSignals(c *gin.Context) (dedup.Signals, bool) {
	reporterKey, ok := middlewares.GetVisitorKeyFromContext(c)
	if !ok {
		utils.ResponseError(c, http.StatusBadRequest, "无法识别访问者，请启用 Cookie 后重试")
		return dedup.Signals{}, false
	}

	return dedup.Signals{
		ReporterKey: reporterKey,
		ReporterIP:  c.ClientIP(),
		Fingerprint: utils.HashVisitorSignal(c.GetHeader(visitorSignalHeader)),
	}, true
}

// recordDuplicate 累计当日疑似重复次数，达阈值会触发自动封禁
// （见 ratelimit.EvaluateBan 规则三）。失败只告警：这是附带的风控计数，
// 不该让一次提交因此失败。
func recordDuplicate(c *gin.Context, ip string) {
	rdb := database.GetRedis()
	if rdb == nil {
		return
	}
	if _, err := ratelimit.RecordDuplicate(c.Request.Context(), rdb, ip); err != nil {
		logger.Warnf("累计疑似重复报告失败 (%s): %v", ip, err)
	}
}

// ReportFieldOutdated 报告某条记录中指定字段的信息已过时。
// 判定与计票在 services.ReportFieldOutdated，这里只做取参、留痕与响应。
func ReportFieldOutdated(c *gin.Context) {
	id, name, ok := fieldTarget(c)
	if !ok {
		return
	}

	signals, ok := visitorSignals(c)
	if !ok {
		return
	}

	outcome, err := services.ApplyFieldAction(id, name, types.ActionReportOutdated, signals)
	if err != nil {
		respondReportError(c, outcome, err, err.Error())
		return
	}

	if outcome.Duplicate {
		logger.Warnf("疑似重复报告，未计数：图书馆 %d 字段 %s，来源 IP %s", id, name, signals.ReporterIP)
		audit.Warnf(c, types.ActionFieldReportRejected,
			"疑似重复报告未计数：图书馆 #%d 字段 %s", id, name)
		recordDuplicate(c, signals.ReporterIP)

		c.JSON(http.StatusOK, utils.Response{
			Code:    http.StatusConflict,
			Message: "该信息已由相同来源报告过，这次未计入",
			Data:    reportOutcome(outcome, false),
		})
		return
	}

	// 达到阈值意味着该信息被弃用，记为 WARN
	if outcome.Status == types.StatusOutdated {
		audit.Warnf(c, types.ActionFieldReport,
			"报告图书馆 #%d 字段 %s 过时（%d/%d，已达阈值，标为过时）",
			id, name, outcome.Count, outcome.Threshold)
	} else {
		audit.Infof(c, types.ActionFieldReport,
			"报告图书馆 #%d 字段 %s 过时（%d/%d）",
			id, name, outcome.Count, outcome.Threshold)
	}

	utils.ResponseOK(c, reportOutcome(outcome, true))
}

// RevokeFieldOutdated 撤销自己对某个字段的过时报告。
// 撤销后次数降到阈值以下时，字段状态自动恢复。
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

	outcome, err := services.ApplyFieldAction(id, name, types.ActionRevokeOutdated,
		dedup.Signals{ReporterKey: reporterKey})
	if err != nil {
		message := err.Error()
		if errors.Is(err, services.ErrNoSuchReport) {
			message = "你没有报告过该信息"
		}
		respondReportError(c, outcome, err, message)
		return
	}

	audit.Infof(c, types.ActionFieldReportRevoke,
		"撤销对图书馆 #%d 字段 %s 的过时报告（剩余 %d/%d）",
		id, name, outcome.Count, outcome.Threshold)

	utils.ResponseOK(c, reportOutcome(outcome, false))
}

// VerifyFieldWebsite 确认某条记录的网站地址可用。
// 累计到 types.VerifyReportThreshold 次独立确认，字段由未验证转为有效。
func VerifyFieldWebsite(c *gin.Context) {
	id, name, ok := fieldTarget(c)
	if !ok {
		return
	}

	signals, ok := visitorSignals(c)
	if !ok {
		return
	}

	outcome, err := services.ApplyFieldAction(id, name, types.ActionVerify, signals)
	if err != nil {
		respondReportError(c, outcome, err, err.Error())
		return
	}

	if outcome.Duplicate {
		logger.Warnf("疑似重复确认，未计数：图书馆 %d 字段 %s，来源 IP %s", id, name, signals.ReporterIP)
		audit.Warnf(c, types.ActionFieldReportRejected,
			"疑似重复确认未计数：图书馆 #%d 字段 %s", id, name)
		recordDuplicate(c, signals.ReporterIP)

		c.JSON(http.StatusOK, utils.Response{
			Code:    http.StatusConflict,
			Message: "该网站已由相同来源确认过，这次未计入",
			Data:    reportOutcome(outcome, false),
		})
		return
	}

	if outcome.Reached {
		audit.Infof(c, types.ActionFieldVerify,
			"确认图书馆 #%d 字段 %s 的网站可用（%d/%d，已达阈值，转为有效）",
			id, name, outcome.Count, outcome.Threshold)
	} else {
		audit.Infof(c, types.ActionFieldVerify,
			"确认图书馆 #%d 字段 %s 的网站可用（%d/%d）",
			id, name, outcome.Count, outcome.Threshold)
	}

	utils.ResponseOK(c, reportOutcome(outcome, true))
}

// RevokeFieldVerify 撤销自己对某个网站的确认。
// 已转正的字段不会因此退回，见 services.RevokeFieldVerify。
func RevokeFieldVerify(c *gin.Context) {
	id, name, ok := fieldTarget(c)
	if !ok {
		return
	}

	reporterKey, ok := middlewares.GetVisitorKeyFromContext(c)
	if !ok {
		utils.ResponseError(c, http.StatusBadRequest, "无法识别访问者，请启用 Cookie 后重试")
		return
	}

	outcome, err := services.ApplyFieldAction(id, name, types.ActionRevokeVerify,
		dedup.Signals{ReporterKey: reporterKey})
	if err != nil {
		message := err.Error()
		if errors.Is(err, services.ErrNoSuchReport) {
			message = "你没有确认过该网站"
		}
		respondReportError(c, outcome, err, message)
		return
	}

	audit.Infof(c, types.ActionFieldVerifyRevoke,
		"撤销对图书馆 #%d 字段 %s 的网站确认（剩余 %d/%d）",
		id, name, outcome.Count, outcome.Threshold)

	utils.ResponseOK(c, reportOutcome(outcome, false))
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
