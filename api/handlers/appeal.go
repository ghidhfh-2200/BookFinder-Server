package handlers

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
	"bookfinder-backend/services/notify"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/audit"
	"bookfinder-backend/utils/checker"
	"bookfinder-backend/utils/netmask"
	"bookfinder-backend/utils/ratelimit"

	"github.com/gin-gonic/gin"
)

// SubmitAppeal 提交封禁申诉。
// 仅被封禁的来源可提交，每个 IP 最多 types.MaxAppealsPerIP 次。
func SubmitAppeal(c *gin.Context) {
	ip := middlewares.GetClientIPFromContext(c)

	// 只有确实被封的来源才能申诉，否则此接口会变成公开留言通道。
	// 封禁中间件已在放行申诉路径时把匹配结果放进上下文，无需再查库。
	match, banned := middlewares.GetBanFromContext(c)
	if !banned {
		utils.ResponseError(c, http.StatusBadRequest, "当前来源未被封禁，无需申诉")
		return
	}

	var req types.AppealRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 清洗为纯文本：剥离控制字符与零宽字符，内容不参与任何解析
	message, err := checker.ValidateAppealMessage(req.Message)
	if err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	visitorKey, _ := middlewares.GetVisitorKeyFromContext(c)

	appeal := &types.BanAppeal{
		IP:         ip,
		Message:    message,
		VisitorKey: visitorKey,
		BanReason:  banSnapshot(&match.Subject),
	}

	// 次数上限由 CreateAppeal 在事务内判定，避免并发提交越过上限
	if err := models.CreateAppeal(appeal); err != nil {
		if errors.Is(err, models.ErrAppealQuotaExhausted) {
			utils.ResponseError(c, http.StatusTooManyRequests,
				"已达申诉次数上限，请等待管理员处理")
			return
		}
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	logger.Warnf("收到封禁申诉：IP %s 第 %d 次", ip, appeal.Attempt)
	logger.Operation(&types.OperationLog{
		User:       ip,
		Action:     types.ActionAppealSubmit,
		Level:      types.LevelWarn,
		Detail:     "提交第 " + strconv.Itoa(appeal.Attempt) + " 次封禁申诉",
		IP:         ip,
		VisitorKey: visitorKey,
	})

	// 推一条通知给管理员：申诉在处理前一直挂着，而被封者除了等没有别的办法。
	//
	// message 已经过 ValidateAppealMessage 清洗，notify 还会把它压成单行——
	// 告警是「标签：取值」的逐行结构，而这是其中唯一由用户完全控制的部分，
	// 不压的话一段带换行的申诉可以在管理员手机上伪造出额外的告警行。
	notify.Appeal(ip, appeal.Attempt, types.MaxAppealsPerIP,
		appeal.BanReason, message)

	// 剩余次数按「已占用配额」算，而非按 Attempt：
	// 已受理的申诉不占配额，故序号可能大于已用次数
	used, err := models.CountAppealsByIP(ip)
	if err != nil {
		used = appeal.Attempt
	}

	utils.ResponseOK(c, gin.H{
		"attempt":   appeal.Attempt,
		"remaining": max(types.MaxAppealsPerIP-used, 0),
		"max":       types.MaxAppealsPerIP,
	})
}

// GetAppealQuota 返回当前来源的申诉配额，供封禁页决定是否显示提交入口
func GetAppealQuota(c *gin.Context) {
	ip := middlewares.GetClientIPFromContext(c)

	used, err := models.CountAppealsByIP(ip)
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	utils.ResponseOK(c, types.AppealQuota{
		Used:      used,
		Max:       types.MaxAppealsPerIP,
		Remaining: max(types.MaxAppealsPerIP-used, 0),
	})
}

// GetAppealsByIP 查询指定 IP 的申诉详情（管理员）
func GetAppealsByIP(c *gin.Context) {
	ip := strings.TrimSpace(c.Param("ip"))
	if ip == "" {
		utils.ResponseError(c, http.StatusBadRequest, "IP 不能为空")
		return
	}

	appeals, err := models.GetAppealsByIP(ip)
	if err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	utils.ResponseOK(c, gin.H{
		"appeals": appeals,
		"max":     types.MaxAppealsPerIP,
	})
}

// ReviewAppeal 处理申诉（管理员）：受理则一并解封，驳回只记结果
func ReviewAppeal(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		utils.ResponseError(c, http.StatusBadRequest, "申诉 ID 不合法")
		return
	}

	var req types.AppealReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	note, err := checker.ValidateAppealReview(&req)
	if err != nil {
		utils.ResponseError(c, http.StatusBadRequest, err.Error())
		return
	}

	appeal, err := models.GetAppealByID(id)
	if err != nil {
		utils.ResponseError(c, http.StatusNotFound, "申诉不存在")
		return
	}

	if err := models.ReviewAppeal(id, req.Status, note); err != nil {
		utils.ResponseError(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 受理即解封：否则申诉通过了人还进不来。
	// 这是解封的两条路径之一，另一条是管理员手动解封。
	if req.Status == types.AppealAccepted {
		if err := unbanForAppeal(c, appeal); err != nil {
			utils.ResponseError(c, http.StatusInternalServerError,
				"申诉已受理，但解封失败: "+err.Error())
			return
		}

		audit.Warnf(c, types.ActionAppealAccepted,
			"受理 %s 的第 %d 次申诉并解封", appeal.IP, appeal.Attempt)
		utils.ResponseSuccessWithCustomMessage(c, "已受理并解封")
		return
	}

	audit.Infof(c, types.ActionAppealRejected,
		"驳回 %s 的第 %d 次申诉", appeal.IP, appeal.Attempt)
	utils.ResponseSuccessWithCustomMessage(c, "已驳回")
}

// unbanForAppeal 找到该申诉对应的封禁主体并整体解除。
//
// 解除的是整个主体而非单个标识：主体下还挂着网段、访问者令牌、设备标识，
// 只删 IP 标识的话人依然进不来，申诉受理就成了一句空话。
//
// 查找不能只按 IP：网段级的精准处置只写令牌标识，那类主体名下没有任何 IP 标识。
// 只按 IP 查会查不到、被当成「已被手动解封」，于是接口回「已受理并解封」
// 而人还在封禁里——这是最坏的一种失败，因为它看起来是成功的。
// 故依次按精确 IP、所属网段、申诉记录里的访问者令牌查找。
func unbanForAppeal(c *gin.Context, appeal *types.BanAppeal) error {
	canonical, ok := netmask.Canonical(appeal.IP)
	if !ok {
		return fmt.Errorf("申诉记录中的 IP 不合法: %s", appeal.IP)
	}

	candidates := []types.BanIdent{{Kind: types.IdentIP, Value: canonical}}
	if prefix, ok := netmask.PrefixOf(appeal.IP); ok {
		candidates = append(candidates, types.BanIdent{Kind: types.IdentIPNet, Value: prefix})
	}
	if appeal.VisitorKey != "" {
		candidates = append(candidates,
			types.BanIdent{Kind: types.IdentVisitor, Value: appeal.VisitorKey})
	}

	subject, err := models.FindBanByAnyIdent(candidates)
	if err != nil {
		if database.IsNotFound(err) {
			// 已被手动解封：受理照常完成，不必因此报错
			logger.Infof("受理申诉时该来源已不在封禁名单内 (%s)", canonical)
			return nil
		}
		return err
	}

	if err := models.DeleteBanSubject(subject.ID); err != nil {
		return err
	}

	// 内存名单是请求路径上的唯一判据，不刷新则解封不会生效
	if err := models.ReloadBanList(); err != nil {
		return fmt.Errorf("刷新内存封禁名单失败: %w", err)
	}

	// 清掉触发封禁的当日计数，否则解封后第一个请求就会重新命中规则、立刻再封。
	// 须覆盖主体名下的全部标识种类，理由同上。
	if rdb := database.GetRedis(); rdb != nil {
		if err := ratelimit.ResetForSubject(c.Request.Context(), rdb, subject.Idents); err != nil {
			logger.Warnf("解封后重置限流计数失败 (#%d): %v", subject.ID, err)
		}
	}

	return nil
}

// banSnapshot 拼出封禁原因快照，供申诉在解封后仍可理解
func banSnapshot(subject *types.BanSubject) string {
	if subject.Detail == "" {
		return subject.Reason
	}
	return subject.Reason + "（" + subject.Detail + "）"
}
