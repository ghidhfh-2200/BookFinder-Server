package notify

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"bookfinder-backend/types"
	"bookfinder-backend/utils/sysconfig"
)

// resetState 清空包级状态，使各测试互不影响
func resetState() {
	mu.Lock()
	queue = nil
	mu.Unlock()
}

// TestStartWithoutCredentials 两条通道都没有凭据时不该启动协程。
//
// Telegram 只配一项也算缺失：config 层会拒绝这种组合，此处是第二道保险。
// 邮件只看密码——其余参数在系统配置里，可随时改动，故每次发送时才判定。
func TestStartWithoutCredentials(t *testing.T) {
	resetState()

	for name, creds := range map[string]Credentials{
		"全都空":            {},
		"只有 Telegram 令牌": {TelegramToken: fakeToken},
		"只有 Telegram 会话": {TelegramChatID: "12345"},
	} {
		started, wait := Start(context.Background(), creds)
		if started {
			t.Errorf("%s：不应启动", name)
		}
		// 未启动时 wait 必须立即返回，否则关闭流程会永久阻塞
		done := make(chan struct{})
		go func() {
			wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Errorf("%s：wait 未立即返回", name)
		}
	}
}

// TestShutdownIsPrompt 关闭必须及时，即便队列里还积压着告警。
//
// 这是回归测试。此前的实现只在一个 select 里同时等 ctx 与队列，而多个分支
// 就绪时 select 是随机选的：关闭时可能继续把积压的告警逐条发完。
//
// 用一个慢速桩而非真实网络：连不通的地址是快速失败的，反而掩盖了这个问题。
// 桩每条耗时 200ms，故若关闭时仍在逐条发送，64 条要 12 秒以上。
func TestShutdownIsPrompt(t *testing.T) {
	resetState()
	enableAllNotify(t)

	var sent atomic.Int32
	slowSend := func(ctx context.Context, text string) error {
		sent.Add(1)
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	_, wait := start(ctx, slowSend)

	// 塞满队列：这些告警都不该在关闭时被发出
	for i := 0; i < queueSize; i++ {
		AutoBan(i, "1.2.3.4", "原因", "详情")
	}

	// 先取消 ctx 再关通道，与 main 的关闭顺序一致
	cancel()
	Stop()

	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()

	// 已出队的那一条会被给完整的发送机会，剩下的必须被放弃
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("关闭未及时完成：已发出 %d 条，队列中积压的告警仍在逐条发送",
			sent.Load())
	}

	// 关闭时最多只应有一条已经出队并在发送中
	if n := sent.Load(); n > 1 {
		t.Errorf("关闭时发出了 %d 条告警，积压的应当被放弃", n)
	}
}

// TestStopBlocksFurtherDispatch Stop 之后的投递应被丢弃而非 panic。
// 关闭期间仍可能有在途请求触发告警。
func TestStopBlocksFurtherDispatch(t *testing.T) {
	resetState()
	enableAllNotify(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	creds := Credentials{TelegramToken: fakeToken, TelegramChatID: "12345"}
	if started, _ := Start(ctx, creds); !started {
		t.Fatal("应当启动")
	}

	cancel()
	Stop()

	// 向已关闭的通道发送会 panic，故 Stop 必须把 queue 置 nil
	AutoBan(1, "1.2.3.4", "原因", "详情")
	NetworkAnomaly("原因", "详情")
	Appeal("1.2.3.4", 1, 3, "封禁原因", "申诉内容")
}

// TestDispatchRespectsConfig 关掉的类别不该入队
func TestDispatchRespectsConfig(t *testing.T) {
	resetState()

	// 只开申诉
	applyNotifyConfig(t, types.NotifyConfig{Appeal: true})

	ch := make(chan event, 8)
	mu.Lock()
	queue = ch
	mu.Unlock()
	defer resetState()

	AutoBan(1, "1.2.3.4", "原因", "详情")
	NetworkAnomaly("原因", "详情")
	if len(ch) != 0 {
		t.Errorf("已关闭的类别仍然入队了 %d 条", len(ch))
	}

	Appeal("1.2.3.4", 1, 3, "封禁原因", "申诉内容")
	if len(ch) != 1 {
		t.Errorf("已启用的类别未入队，队列中有 %d 条", len(ch))
	}
}

// TestDispatchDropsWhenFull 队列满时丢弃而非阻塞。
// 三个触发点都在请求路径上，阻塞会把用户的请求一并卡住。
func TestDispatchDropsWhenFull(t *testing.T) {
	resetState()
	enableAllNotify(t)

	// 容量 1 且无人消费
	ch := make(chan event, 1)
	mu.Lock()
	queue = ch
	mu.Unlock()
	defer resetState()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			AutoBan(i, "1.2.3.4", "原因", "详情")
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("队列满时投递发生了阻塞")
	}
}

// enableAllNotify 打开全部告警类别
func enableAllNotify(t *testing.T) {
	t.Helper()
	applyNotifyConfig(t, types.NotifyConfig{
		AutoBan:        true,
		NetworkAnomaly: true,
		Appeal:         true,
	})
}

// applyNotifyConfig 把给定的告警开关写入系统配置。
//
// 经 sysconfig.Commit 而非直接改包内变量：dispatch 读的是 sysconfig.Get()，
// 绕过它设置就测不到真实路径。Commit 会落盘，故写到测试临时目录。
func applyNotifyConfig(t *testing.T, notify types.NotifyConfig) {
	t.Helper()

	if err := sysconfig.Load(t.TempDir() + "/system_config.json"); err != nil {
		t.Fatalf("加载系统配置失败: %v", err)
	}

	config := sysconfig.Get()
	config.Notify = notify
	if err := sysconfig.Commit(&config); err != nil {
		t.Fatalf("保存系统配置失败: %v", err)
	}
}
