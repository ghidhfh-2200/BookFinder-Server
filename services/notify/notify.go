// Package notify 把需要管理员知晓的事件推送到 Telegram。
//
// 只出不进：本包只调用 sendMessage，不注册 Webhook、也不轮询 getUpdates。
// 机器人因此没有任何入口，「有人给机器人发指令」这条路径不存在。
//
// 与 services 下其他成员一样，它是一个有生命周期的后台任务：随服务启动，
// 关闭时停止（见 Start 与 Stop）。
package notify

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"bookfinder-backend/logger"
	"bookfinder-backend/types"
	"bookfinder-backend/utils/sysconfig"
)

// queueSize 待发送告警的队列容量。
//
// 与 logger 同一套取舍，但容量小得多：告警是低频事件，三类来源都有天然去重
// （见 types.NotifyConfig）。队列满意味着 Telegram 长时间不可达，此时丢弃并留痕，
// 绝不阻塞——告警的重要性远低于触发它的请求。
const queueSize = 64

// event 一条待发送的告警。
// 类别在入队前就已按配置判定（见 dispatch），故此处只需消息全文。
type event struct {
	text string
}

// kind 告警类别，与 types.NotifyConfig 的三个开关一一对应
type kind int

const (
	kindAutoBan kind = iota
	kindNetworkAnomaly
	kindAppeal
)

// enabled 该类别当前是否启用
func (k kind) enabled(cfg types.NotifyConfig) bool {
	switch k {
	case kindAutoBan:
		return cfg.AutoBan
	case kindNetworkAnomaly:
		return cfg.NetworkAnomaly
	case kindAppeal:
		return cfg.Appeal
	default:
		return false
	}
}

var (
	mu sync.RWMutex
	// queue 待发送的告警，由 worker 消费。
	// 凭据未配置或已 Stop 时为 nil，此时投递静默丢弃。
	queue chan event
	// smtpPassword 发信密码，由 Start 注入。
	//
	// 单独存放而非随发送函数闭包捕获：邮件的其余参数每次从系统配置现取
	// （管理页可改），密码却只来自环境变量、进程内不变，两者生命周期不同。
	smtpPassword string
)

// Credentials 外发所需的凭据，全部来自环境变量。
//
// 集中成一个结构体传入，而非逐个参数：调用方只需转交 config 里的对应项，
// 日后增加通道也不必改签名。
type Credentials struct {
	// TelegramToken 机器人令牌
	TelegramToken string
	// TelegramChatID 接收方
	TelegramChatID string
	// SMTPPassword 发信密码或授权码。其余发信参数在系统配置里。
	SMTPPassword string
}

// Start 启动告警外发协程，返回一个等待其退出的函数。
//
// 两条通道并行：Telegram 与邮件都配好就都发。这正是同时支持两者的意义——
// Telegram 在国内常需代理才通，邮件基本处处可达，不必赌哪一条能出去。
//
// 两条都不可用时返回 started 为 false，由调用方决定如何提示。
//
// 异步发送是必须的：三个触发点都在请求路径上（自动封禁与流量异常在限流中间件里，
// 申诉在提交处理函数里），而发一条消息要走一次跨境往返。同步发送会把这个延迟
// 加到用户的请求上，对方不可达时更是直接卡满超时。
func Start(ctx context.Context, creds Credentials) (started bool, wait func()) {
	mu.Lock()
	smtpPassword = creds.SMTPPassword
	mu.Unlock()

	var telegram sendFunc
	if creds.TelegramToken != "" && creds.TelegramChatID != "" {
		telegram = newClient(creds.TelegramToken, creds.TelegramChatID).send
	}

	// 邮件通道是否可用取决于系统配置，而那是可以随时改的：故不在此处判定，
	// 而是每次发送时按当时的配置决定（见 fanOut）。此处只看密码是否具备。
	hasEmailPassword := creds.SMTPPassword != ""

	if telegram == nil && !hasEmailPassword {
		return false, func() {}
	}

	return start(ctx, fanOut(telegram, hasEmailPassword))
}

// fanOut 把一条告警同时送往各条可用通道。
//
// 一条失败不影响另一条：两条通道并行的意义正是互不依赖，其中一条不通时
// 另一条仍要送到。故错误用 errors.Join 汇总，而非遇错即返回。
//
// 邮件的收发参数每次从系统配置现取：管理页改完即时生效，不必重启。
func fanOut(telegram sendFunc, hasEmailPassword bool) sendFunc {
	return func(ctx context.Context, text string) error {
		var errs []error

		if telegram != nil {
			if err := telegram(ctx, text); err != nil {
				errs = append(errs, fmt.Errorf("telegram: %w", err))
			}
		}

		if hasEmailPassword {
			mu.RLock()
			password := smtpPassword
			mu.RUnlock()

			email := sysconfig.Get().Notify.Email
			// 未启用或填得不全就跳过。这不是错误：邮件是可选通道，
			// 而「启用了但没填完」由保存时的校验拦住。
			if email.Usable(password) {
				if err := newMailer(email, password).send(ctx, text); err != nil {
					errs = append(errs, fmt.Errorf("email: %w", err))
				}
			}
		}

		return errors.Join(errs...)
	}
}

// start 用给定的发送函数启动协程。供 Start 与测试共用。
func start(ctx context.Context, send sendFunc) (bool, func()) {
	ch := make(chan event, queueSize)
	done := make(chan struct{})

	mu.Lock()
	queue = ch
	mu.Unlock()

	go worker(ctx, send, ch, done)

	return true, func() { <-done }
}

// sendFunc 发送一条消息。
//
// 抽成函数类型是为了让「关闭必须及时」这条保证可测：测试用一个慢速的桩替换它，
// 即可确认关闭时队列里积压的告警确实被放弃了，而不必依赖网络——
// 真实地址连不通时反而是快速失败，掩盖了这个问题。
type sendFunc func(ctx context.Context, text string) error

// worker 顺序消费队列并发送。
//
// 单协程串行发送：Telegram 对同一会话有频率限制，并发发送只会换来 429。
// 串行也保证了管理员手机上的消息顺序与事件发生顺序一致。
//
// 发送函数由参数传入而非取全局：它在整个协程生命周期内不变，
// 没有理由每条都去取一次锁。
func worker(ctx context.Context, send sendFunc, in <-chan event, done chan<- struct{}) {
	defer close(done)
	defer logger.RecoverPanic("Notify Goroutine")

	for {
		// 先单独判一次关闭信号，不能只靠下面的 select：多个分支同时就绪时
		// select 是随机选的，故仅凭它无法保证关闭优先。队列里若还积压着告警，
		// 关闭时可能继续逐条发完，每条最多 sendTimeout——足以把关闭拖上几分钟。
		//
		// 放弃这些告警是有意的：事件本身已经落在操作日志里，通知只是提醒；
		// 而 Telegram 不可达恰恰是队列积压的主要成因，那时更不该等。
		if ctx.Err() != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case ev, ok := <-in:
			if !ok {
				return
			}
			deliver(ctx, send, ev)
		}
	}
}

// deliver 发送一条告警，失败只留痕。
//
// 发送失败绝不重试：判据是「事件已经发生」，重试改变不了这一点。
// Telegram 不可达时重试只会让队列积压，把后续告警一并拖住。
func deliver(ctx context.Context, send sendFunc, ev event) {
	// 用独立超时而非直接用 ctx：关闭时 ctx 立即取消，而已经取出的这一条
	// 值得给它一次完整的发送机会。上限即 sendTimeout，故关闭最多因此多等这么久。
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sendTimeout)
	defer cancel()

	if err := send(sendCtx, ev.text); err != nil {
		// 错误已由各通道的 redact 抹去凭据，可以安全落库。
		// 两条通道都失败时这里是一条汇总错误，各自的原因都在。
		logger.Warnf("告警发送失败: %v", err)
	}
}

// Stop 停止投递。与 Start 返回的 wait 配合使用：先 Stop，再 wait。
//
// 只关通道、不等发送完成：worker 由 ctx 取消退出（见 worker 的说明）。
func Stop() {
	mu.Lock()
	ch := queue
	queue = nil
	mu.Unlock()

	if ch != nil {
		close(ch)
	}
}

// dispatch 按配置把告警投入队列。
//
// 队列满时丢弃：告警排在业务之后，宁可少一条通知也不阻塞触发它的请求。
func dispatch(k kind, text string) {
	if !k.enabled(sysconfig.Get().Notify) {
		return
	}

	mu.RLock()
	ch := queue
	mu.RUnlock()

	// 凭据未配置：三类开关默认开启，故这是最常见的分支，不该留痕
	if ch == nil {
		return
	}

	select {
	case ch <- event{text: text}:
	default:
		logger.Warnf("Telegram 告警队列已满，丢弃一条通知")
	}
}

// AutoBan 通知一次自动封禁。
//
// scope 说明处置范围（哪个 IP、哪个网段、还是几个设备），reason 与 detail
// 来自封禁记录本身。detail 里是触发规则的具体数据，正是复核误封时要看的东西。
func AutoBan(subjectID int, scope, reason, detail string) {
	dispatch(kindAutoBan, buildMessage("🚫 自动封禁", []field{
		{label: "处置范围", value: scope},
		{label: "原因", value: reason},
		{label: "详情", value: detail},
		{label: "封禁记录", value: fmt.Sprintf("#%d", subjectID)},
		{label: "时间", value: now()},
	}))
}

// BanSkipped 通知一次「判据成立但未处置」。
//
// 与 AutoBan 分开而非共用：那条消息的标题是「自动封禁」，用它来报告一次没有
// 发生的封禁会直接误导——收到推送的人会以为有人被封了。
//
// 仍然要通知，因为它往往指向一个需要处理的状况：本机有脚本在密集调用，
// 或者反向代理没有透传真实来源（后者意味着封禁与限流对所有访问者都失了准）。
func BanSkipped(ip, reason, detail string) {
	dispatch(kindAutoBan, buildMessage("⚠️ 判据成立但未封禁", []field{
		{label: "来源", value: ip},
		{label: "原因", value: "回环地址不自动封禁（本机请求，或反代未透传真实来源）"},
		{label: "触发规则", value: reason},
		{label: "详情", value: detail},
		{label: "时间", value: now()},
	}))
}

// NetworkAnomaly 通知一次网段流量异常。
//
// 这一类专指「判据命中但服务未做处置」的情形：IPv4 网段流量异常且认不出具体
// 设备时，封 /24 或封共享出口都会连坐无关的人，故只记录待人工核查。
// 没有这条通知，那条记录要等管理员主动翻日志才会被发现。
func NetworkAnomaly(reason, detail string) {
	dispatch(kindNetworkAnomaly, buildMessage("⚠️ 网段流量异常（未自动处置）", []field{
		{label: "判定", value: reason},
		{label: "详情", value: detail},
		{label: "时间", value: now()},
	}))
}

// Appeal 通知一次封禁申诉。
//
// message 是用户提交的正文，会经 sanitizeValue 压成单行——这套「标签：取值」
// 的结构可以被换行伪造，而这是整条消息里唯一由用户完全控制的部分。
func Appeal(ip string, attempt, max int, banReason, message string) {
	dispatch(kindAppeal, buildMessage("📩 收到封禁申诉", []field{
		{label: "来源", value: ip},
		{label: "次数", value: fmt.Sprintf("第 %d 次（上限 %d）", attempt, max)},
		{label: "封禁原因", value: banReason},
		{label: "申诉内容", value: message},
		{label: "时间", value: now()},
	}))
}

// now 当前时刻，写入告警。
// 消息可能因队列积压而延迟到达，故时间取自事件发生时而非发送时。
func now() string {
	return time.Now().Format("2006-01-02 15:04:05")
}
