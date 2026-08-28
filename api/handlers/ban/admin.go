// Package ban 提供封禁与屏蔽名单的管理接口。
package ban

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"bookfinder-backend/api/middlewares"
	"bookfinder-backend/database"
	"bookfinder-backend/logger"
	"bookfinder-backend/models"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/audit"
	"bookfinder-backend/utils/describe"
	"bookfinder-backend/utils/netmask"
	"bookfinder-backend/utils/ratelimit"

	"github.com/gin-gonic/gin"
)

// banRequest 封禁请求
type banRequest struct {
	IP     string `json:"ip"`
	Reason string `json:"reason"`
	// BanNetwork 是否一并封禁该 IP 所属网段（IPv6 /64、IPv4 /24）。
	// 手动封禁默认只封精确地址：网段封禁会连坐同段的其他人，该由管理员明确选择。
	BanNetwork bool `json:"ban_network"`
}

// banItem 封禁主体及其申诉概况
type banItem struct {
	types.BanSubject
	// IPs 该主体下的精确 IP 标识，申诉按 IP 关联，故单列出来
	IPs []string `json:"ips"`
	// Appeals 该主体涉及 IP 的申诉总数与待处理数
	Appeals types.AppealSummary `json:"appeals"`
}

// GetBans 获取所有封禁记录，附带申诉概况
func GetBans(c *gin.Context) {
	subjects, err := models.GetBanSubjects()
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 一次聚合取回全部 IP 的申诉数，不逐行查询
	summaries, err := models.CountAppealsGrouped()
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]banItem, 0, len(subjects))
	for _, subject := range subjects {
		item := banItem{BanSubject: subject}

		// 申诉按 IP 记录，故主体的申诉数是其名下各 IP 的合计
		for _, ident := range subject.Idents {
			if ident.Kind != types.IdentIP {
				continue
			}
			item.IPs = append(item.IPs, ident.Value)
			summary := summaries[ident.Value]
			item.Appeals.Total += summary.Total
			item.Appeals.Pending += summary.Pending
		}

		items = append(items, item)
	}

	utils.ResponseOK(c, items)
}

// BanIP 手动封禁来源 IP
func BanIP(c *gin.Context) {
	var req banRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	ip := strings.TrimSpace(req.IP)
	if net.ParseIP(ip) == nil {
		utils.ResponseError(c, http.StatusBadRequest, "IP 地址不合法")
		return
	}

	canonical, _ := netmask.Canonical(ip)

	// 禁止管理员封禁自己当前的来源 IP，否则会把自己锁在外面
	if selfIP, ok := netmask.Canonical(middlewares.GetClientIPFromContext(c)); ok &&
		selfIP == canonical {
		utils.ResponseError(c, http.StatusBadRequest, "不能封禁自己当前的来源 IP")
		return
	}

	if len([]rune(req.Reason)) > 255 {
		utils.ResponseError(c, http.StatusBadRequest, "封禁原因长度不能超过 255 个字符")
		return
	}

	idents := []types.BanIdent{{Kind: types.IdentIP, Value: canonical}}
	scope := canonical

	if req.BanNetwork {
		prefix, ok := netmask.PrefixOf(ip)
		if !ok {
			utils.ResponseError(c, http.StatusBadRequest, "无法计算该 IP 所属网段")
			return
		}
		// 管理员自己所在的网段不能封：那等于把自己锁在外面
		if selfPrefix, ok := netmask.PrefixOf(middlewares.GetClientIPFromContext(c)); ok &&
			selfPrefix == prefix {
			utils.ResponseError(c, http.StatusBadRequest,
				"不能封禁自己所在的网段，否则将无法登录")
			return
		}
		idents = append(idents, types.BanIdent{Kind: types.IdentIPNet, Value: prefix})
		scope = canonical + " 及其所属网段（" + prefix + "）"
	}

	// 改写已有主体的原因：该来源可能先被规则自动封过，而管理员刚填的原因
	// 是一次有意的处置，此后这条记录代表的是管理员的判断
	subject, _, err := models.CreateBan(&types.BanSubject{
		Reason: strings.TrimSpace(req.Reason),
		// Detail 记下处置范围。自动封禁在这里放的是触发规则的具体数据，
		// 手动封禁没有那种数据，但同样需要一个客观事实：封的是哪些标识。
		// 被封者只看得到 Reason 与 Detail，原因一栏可能被管理员留空，
		// 此时若 Detail 也空着，封禁页除了「你已被封禁」什么都说不出来。
		Detail: manualBanDetail(scope, req.BanNetwork),
		Source: types.BanSourceManual,
	}, idents, true)
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if err := reloadBanList(c); err != nil {
		return
	}

	// 封禁会挡掉该来源的全部访问，记为 WARN
	audit.Warnf(c, types.ActionIPBan, "封禁 %s，原因: %s",
		scope, describe.Fallback(subject.Reason, "未填写"))

	utils.ResponseSuccessWithCustomMessage(c, "封禁成功")
}

// UnbanSubject 解封：删除封禁主体及其全部标识。
// 这是解封的两条路径之一，另一条是申诉受理（见 handlers.ReviewAppeal）。
func UnbanSubject(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		utils.ResponseError(c, http.StatusBadRequest, "封禁 ID 不合法")
		return
	}

	subject, err := models.GetBanSubjectByID(id)
	if err != nil {
		if database.IsNotFound(err) {
			utils.ResponseError(c, http.StatusNotFound, "该封禁记录不存在")
			return
		}
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if err := models.DeleteBanSubject(id); err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if err := reloadBanList(c); err != nil {
		return
	}

	// 清掉触发封禁的当日计数，否则解封后第一个请求就会重新命中规则、立刻再封
	resetCounters(c, subject)

	audit.Infof(c, types.ActionIPUnban, "解封 #%d（%s）",
		id, describe.Idents(subject.Idents))

	utils.ResponseSuccessWithCustomMessage(c, "解封成功")
}

// resetCounters 重置该主体名下全部标识的限流与封禁判据。
//
// 必须覆盖全部标识种类，不能只看 IP：网段级的精准处置只写令牌标识
// （见 middlewares 的 networkBanIdents），那类主体名下没有任何 IP 标识，
// 只重置 IP 的话一次都不会执行，解封后第一个请求就会重新命中、立刻再封。
func resetCounters(c *gin.Context, subject *types.BanSubject) {
	rdb := database.GetRedis()
	if rdb == nil {
		return
	}

	if err := ratelimit.ResetForSubject(c.Request.Context(), rdb, subject.Idents); err != nil {
		logger.Warnf("解封后重置限流计数失败 (#%d): %v", subject.ID, err)
	}
}

// reloadBanList 刷新内存封禁名单，失败时写出错误响应并返回 error。
//
// 请求路径上的封禁判定只查内存，故刷新失败必须让调用方知道：
// 此时库已改而内存未改，封禁或解封看似成功却没有生效。
func reloadBanList(c *gin.Context) error {
	if err := models.ReloadBanList(); err != nil {
		logger.Errorf("刷新内存封禁名单失败: %v", err)
		utils.ResponseError(c, http.StatusInternalServerError,
			"操作已入库，但刷新内存名单失败，请重试或重启服务: "+err.Error())
		return err
	}
	return nil
}

// manualBanDetail 手动封禁的处置范围说明，写入封禁记录的 Detail。
//
// 这段文字被封者看得到（封禁页的「触发详情」一栏），故只写客观事实：
// 由管理员处置、范围多大。不写管理员是谁，也不解释判断依据——
// 那些属于审计日志，不该回给被封的人。
func manualBanDetail(scope string, banNetwork bool) string {
	detail := "由管理员手动封禁，处置范围：" + scope
	if banNetwork {
		// 说明连坐范围：被封者据此判断是否该以「共用出口」为由申诉
		detail += "。同一网段内的其他访问者也会被一并挡住"
	}
	return detail
}
