package middlewares

import (
	"fmt"

	"bookfinder-backend/logger"
	"bookfinder-backend/models"
	"bookfinder-backend/services/notify"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"
	"bookfinder-backend/utils/banlist"
	"bookfinder-backend/utils/describe"
	"bookfinder-backend/utils/netmask"

	"github.com/gin-gonic/gin"
)

// BanContextKey 上下文中封禁匹配结果的键。
// 申诉接口据此确认「该来源确实被封」，无需再查库。
const BanContextKey = "ban_match"

// BanMiddleware 拦截被封禁的访问者。
//
// 判定按「主体」而非单个 IP：一次请求携带的来源 IP、所属网段、访问者令牌、
// 已验签的设备标识都参与匹配，命中任一即拦下。因此换 IP、清 cookie、
// 或换用另一端（浏览器 ↔ 安卓）都不足以脱身。
//
// 判定只查内存名单（见 utils/banlist）：本中间件在每个 API 请求上执行，
// 若每次都查 SQLite，它自身就成了最容易被打崩的瓶颈。
//
// 已登录的管理员不受影响，可自行解封。但未登录时无从识别管理员身份，
// 故被封来源连管理员登录入口也访问不到——这是有意为之：
// 被封者因此无法继续探测入口口令，响应与页面不存在完全一致。
// 代价是管理员误封自己后只能从别的来源登录，或直接改库解封。
//
// 申诉接口对被封者开放：封禁若把申诉入口一并挡住，申诉功能就无从使用。
// 这些接口自身要求来源确实被封，故不会成为公开留言通道。
func BanMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsAdmin(c) {
			c.Next()
			return
		}

		match, banned := banlist.Check(banlist.Request{
			IP:         GetClientIPFromContext(c),
			VisitorKey: contextString(c, VisitorKeyContextKey),
			DeviceKey:  contextString(c, DeviceKeyContextKey),
		})
		if !banned {
			c.Next()
			return
		}

		// 申诉接口需对被封者可达，故放在封禁判定之后：
		// 先确认确实被封，再放行申诉——接口因此拿得到封禁详情，
		// 也不会因为「未被封」而变成公开留言通道。
		if isAppealPath(c.Request.URL.Path) {
			c.Set(BanContextKey, match)
			c.Next()
			return
		}

		utils.ResponseBanned(c, &match.Subject)
	}
}

// isAppealPath 判断是否为申诉相关接口。
// 这些接口需对被封者可达，否则申诉功能形同虚设。
func isAppealPath(path string) bool {
	switch path {
	case "/api/appeal", "/api/appeal/quota":
		return true
	default:
		return false
	}
}

// GetBanFromContext 取出本次请求的封禁匹配结果。
// 仅在申诉接口中有值：其余被封请求已被中间件拦下，走不到处理函数。
func GetBanFromContext(c *gin.Context) (banlist.Match, bool) {
	value, ok := c.Get(BanContextKey)
	if !ok {
		return banlist.Match{}, false
	}
	match, ok := value.(banlist.Match)
	return match, ok
}

// BanIdentsForRequest 收集本次请求可用于封禁的全部标识：精确 IP 挡住当前来源，
// 访问者令牌与设备标识跨 IP、跨端跟着这个人。
//
// 网段标识只在 IPv6 时写入其 /64，与 EvaluateNetworkBan 的策略必须一致：
// IPv6 段内换址不受限制，不封 /64 等于没封；而 IPv4 的 /24 可能是整个校园网出口，
// 按令牌的规则一旦命中就连坐整段，故自动流程绝不写。
func BanIdentsForRequest(c *gin.Context) []types.BanIdent {
	idents := make([]types.BanIdent, 0, 4)

	ip := GetClientIPFromContext(c)
	if canonical, ok := netmask.Canonical(ip); ok {
		idents = append(idents, types.BanIdent{Kind: types.IdentIP, Value: canonical})

		if netmask.IsIPv6(ip) {
			if prefix, ok := netmask.PrefixOf(ip); ok {
				idents = append(idents, types.BanIdent{Kind: types.IdentIPNet, Value: prefix})
			}
		}
	}

	if visitorKey := contextString(c, VisitorKeyContextKey); visitorKey != "" {
		idents = append(idents, types.BanIdent{Kind: types.IdentVisitor, Value: visitorKey})
	}

	// 设备标识只在请求签名校验通过时才会入上下文，否则任何人改一个请求头
	// 就能把他人的设备标识挂到自己的封禁上
	if deviceKey := contextString(c, DeviceKeyContextKey); deviceKey != "" {
		idents = append(idents, types.BanIdent{Kind: types.IdentDevice, Value: deviceKey})
	}

	return idents
}

// ApplyAutoBan 执行一次自动封禁，返回最终生效的封禁主体；无需处置时返回 nil。
//
// 这是全部自动封禁的唯一出口。判据都是当日累计值，命中之后该来源的每个后续请求
// 都会重新命中同一条规则，故归并必须由 models.CreateBan 在事务内裁决——
// 先查内存名单再决定是不行的：名单与库之间有时间窗，两个并发请求会同时认为
// 「还没封过」，于是各建一条记录，而解封是按主体进行的。
//
// 不写响应：网段级的精准处置只封那几个异常设备，触发判定的可能是同网段一个
// 正常用户的请求，该由调用方决定回什么。
//
// 返回的主体带着库回填的 CreatedAt，供响应显示封禁时间；手工拼一个等价主体
// 会留下零值时间，封禁页会显示 0001-01-01。
func ApplyAutoBan(c *gin.Context, reason, detail string,
	idents []types.BanIdent) *types.BanSubject {

	if len(idents) == 0 {
		return nil
	}

	ip := GetClientIPFromContext(c)

	// 回环来源：丢掉地址类标识，只按令牌与设备封禁。
	//
	// 为什么不能封这个地址：回环地址不指向任何特定访问者。它要么是本机自己发的
	// 请求（健康检查、监控探针、脚本），要么是反代没有透传真实来源——后者会把
	// 全部访问者都算作 127.0.0.1，封它就是整站自封。
	//
	// 为什么仍要封令牌：令牌是跨 IP 跟人的标识，与来源地址无关。反代配错时
	// 按 IP 的判定已经失准（所有人共用一个「来源」），而按令牌的判定依然准确——
	// 刷接口的那个客户端有自己的令牌，封它不波及别人。这恰是令牌标识存在的理由。
	//
	// 判定放在这里而不在各条规则里：这是全部自动封禁的唯一出口，
	// 在此处理一次即覆盖所有触发路径（按令牌的规则、网段判定、见习额度）。
	if netmask.IsLoopback(ip) {
		kept := identsExcludingAddress(idents)

		logger.Warnf("回环来源触发封禁判据，已跳过地址类标识（保留 %d 个设备标识）：%s（%s：%s）",
			len(kept), ip, reason, detail)
		logger.Operation(&types.OperationLog{
			User:   ip,
			Action: types.ActionIPBanSkipped,
			Level:  types.LevelWarn,
			Detail: fmt.Sprintf("回环来源不封禁地址（本机请求，或反代未透传真实来源），"+
				"改按 %d 个设备标识处置：%s：%s", len(kept), reason, detail),
			IP:         ip,
			VisitorKey: contextString(c, VisitorKeyContextKey),
		})

		// 连令牌都没有：无从处置，只能告警。这正是「本机脚本不带 cookie 猛刷」
		// 的形态，而那种情况下没有任何安全的处置对象。
		if len(kept) == 0 {
			notify.BanSkipped(ip, reason, detail)
			return nil
		}

		idents = kept
	}

	// 不改写已有主体的原因：同一个人会被规则反复命中，每次都改写会把最初那条
	// 记录的原因与时间覆盖掉，申诉里存的快照也就再也对不上了
	ban, wrote, err := models.CreateBan(&types.BanSubject{
		Reason: reason,
		Detail: detail,
		Source: types.BanSourceAuto,
	}, idents, false)
	if err != nil {
		// 落库失败不阻断请求，交由限流判定继续处理
		logger.Errorf("自动封禁写入失败 (%s): %v", ip, err)
		return nil
	}

	// 这些标识早已全部在册且同属一个主体：先前已经处置过，本次不必再写日志、
	// 也不必重建名单。仍然返回主体，调用方据此回封禁响应。
	if !wrote {
		return ban
	}

	// 内存名单是请求路径上的唯一判据，不刷新则本次封禁不会生效
	if err := models.ReloadBanList(); err != nil {
		logger.Errorf("自动封禁后刷新内存名单失败 (%s): %v", ip, err)
	}

	logger.Warnf("自动封禁 %s：%s（%s），封禁记录 #%d 现有 %d 个标识",
		ip, reason, detail, ban.ID, len(ban.Idents))
	logger.Operation(&types.OperationLog{
		User:       ip,
		Action:     types.ActionIPBanAuto,
		Level:      types.LevelWarn,
		Detail:     reason + "：" + detail,
		IP:         ip,
		VisitorKey: contextString(c, VisitorKeyContextKey),
	})

	// 推一条通知给管理员。放在这里而非各个判定点：这是全部自动封禁的唯一出口，
	// 且此处已经过 wrote 判定——同一个人被规则反复命中时不会重复通知。
	//
	// 异步投递，不阻塞本次请求（见 notify.Start 的说明）。
	notify.AutoBan(ban.ID, describe.BanScope(ban.Idents), reason, detail)

	return ban
}

// identsExcludingAddress 滤掉地址类标识（精确 IP 与网段），保留令牌与设备标识。
//
// 供回环来源使用：那个地址不指向特定访问者，封它可能连坐全部访问者，
// 而令牌与设备标识仍然准确地指向某一个人。
func identsExcludingAddress(idents []types.BanIdent) []types.BanIdent {
	kept := make([]types.BanIdent, 0, len(idents))

	for _, ident := range idents {
		switch ident.Kind {
		case types.IdentIP, types.IdentIPNet:
			continue
		default:
			kept = append(kept, ident)
		}
	}

	return kept
}

// contextString 从上下文取字符串值，缺失或类型不符时返回空串
func contextString(c *gin.Context, key string) string {
	value, ok := c.Get(key)
	if !ok {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return str
}
