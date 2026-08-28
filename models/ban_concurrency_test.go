package models

import (
	"fmt"
	"sync"
	"testing"

	"bookfinder-backend/database"
	"bookfinder-backend/types"
	"bookfinder-backend/utils/banlist"
)

// concurrentBan 模拟 middlewares.ApplyAutoBan 的动作序列：
// 落库（由 CreateBan 在事务内裁决归并），再重建内存名单。
//
// 归并不由调用方先查内存名单决定：那与库之间存在时间窗，两个并发请求会同时
// 认为「还没封过」，于是各建一条记录。
func concurrentBan(reason string, idents []types.BanIdent) (wrote bool, err error) {
	subject := &types.BanSubject{Reason: reason, Source: types.BanSourceAuto}

	_, wrote, err = CreateBan(subject, idents, false)
	if err != nil {
		return false, err
	}
	if !wrote {
		return false, nil
	}

	return true, ReloadBanList()
}

// TestConcurrentAutoBanSameIdents 多个请求同时命中同一条规则时，
// 「查名单—落库」的时间窗会造成什么后果。
//
// 现实对应：某来源触发规则一的那一刻，它往往有多个请求同时在途（浏览器并发、
// 脚本更是如此）。每个请求都会各自判定、各自尝试封禁。
func TestConcurrentAutoBanSameIdents(t *testing.T) {
	setupBanTestDB(t)

	const workers = 16
	idents := []types.BanIdent{
		{Kind: types.IdentIP, Value: "203.0.113.9"},
		{Kind: types.IdentVisitor, Value: "visitor-race"},
	}

	var wg sync.WaitGroup
	errs := make([]error, workers)
	wrote := make([]bool, workers)

	start := make(chan struct{})
	for i := range workers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start // 尽量让所有 goroutine 同时起跑，放大竞争窗口
			wrote[n], errs[n] = concurrentBan(fmt.Sprintf("并发封禁 #%d", n), idents)
		}(i)
	}
	close(start)
	wg.Wait()

	writes := 0
	for i, err := range errs {
		if err != nil {
			t.Logf("worker %d 报错: %v", i, err)
		}
		if wrote[i] {
			writes++
		}
	}

	// 落库次数体现并发窗口的代价。归并由库在事务内裁决，故第一个请求建主体、
	// 其余请求看到标识已在册就什么都不做（wrote 为 false），不会各建一条记录。
	// 单连接的 SQLite 把这些事务串行化，因此最终只该有一次真正的写入。
	t.Logf("实际落库次数 = %d（并发 %d 个请求）", writes, workers)
	if writes != 1 {
		t.Errorf("同一批标识的并发封禁应只落库一次，实际 %d 次", writes)
	}

	subjects, err := GetBanSubjects()
	if err != nil {
		t.Fatalf("GetBanSubjects 出错: %v", err)
	}

	t.Logf("并发 %d 个请求后，库里有 %d 个封禁主体", workers, len(subjects))
	for _, s := range subjects {
		t.Logf("  主体 #%d reason=%q 标识数=%d", s.ID, s.Reason, len(s.Idents))
	}

	// 不变式一：不能出现没有任何标识的主体——那种记录永远不会被命中，
	// 只会在管理页显示成一条空记录
	for _, s := range subjects {
		if len(s.Idents) == 0 {
			t.Errorf("主体 #%d 没有任何标识（悬空主体）", s.ID)
		}
	}

	// 不变式二：每个标识只能属于一个主体（靠 (kind,value) 唯一索引）
	owner := make(map[string]int)
	for _, s := range subjects {
		for _, ident := range s.Idents {
			key := string(ident.Kind) + ":" + ident.Value
			if prev, dup := owner[key]; dup {
				t.Errorf("标识 %s 同时属于主体 #%d 与 #%d", key, prev, s.ID)
			}
			owner[key] = s.ID
		}
	}

	// 不变式三：这两个标识本属于同一个人，理想情况下应落在同一个主体上。
	// 若被拆开，管理员解封一个主体后人依然进不来。
	if len(owner) == 2 {
		var ids []int
		for _, id := range owner {
			ids = append(ids, id)
		}
		if ids[0] != ids[1] {
			t.Errorf("同一个人的两个标识被拆到不同主体：%v", ids)
		}
	}

	// 最终名单必须能拦下这个来源。
	//
	// 这一条曾间歇性失败（约 10% 的并发批次），成因是 ReloadBanList 的
	// 「读库—替换快照」不是原子的：A 读到较新的数据、B 读到较旧的，若 B 后完成
	// 替换，B 那份旧快照就覆盖掉 A 的新快照。库里是对的，内存里却漏掉了刚写入的
	// 封禁——而请求路径只查内存，于是刚被封的来源照常放行。
	//
	// 现已在 ReloadBanList 内用 reloadMu 把两步收进同一个临界区，故这里是硬断言。
	if _, hit := banlist.Check(banlist.Request{
		IP: "203.0.113.9", VisitorKey: "visitor-race",
	}); !hit {
		t.Error("并发封禁之后该来源未被名单命中（内存快照被旧数据覆盖）")

		// 库里应当是对的，用它证明问题只在内存快照
		subjects, err := GetBanSubjects()
		if err != nil {
			t.Fatalf("GetBanSubjects 出错: %v", err)
		}
		found := 0
		for _, s := range subjects {
			found += len(s.Idents)
		}
		t.Logf("此时库里有 %d 个主体、共 %d 个标识；若落库正常则问题只在内存快照",
			len(subjects), found)

		// 再刷一次即可恢复，进一步说明是覆盖而非丢数据
		if err := ReloadBanList(); err != nil {
			t.Fatalf("ReloadBanList 出错: %v", err)
		}
		if _, hit := banlist.Check(banlist.Request{
			IP: "203.0.113.9", VisitorKey: "visitor-race",
		}); !hit {
			// 重新载入后仍不命中就不是快照覆盖，而是真的丢了数据——那要当错误报
			t.Error("重新载入后仍未命中，说明不只是快照陈旧")
		}
	}
}

// TestConcurrentAutoBanPartialOverlap 部分标识已被封时的并发情形。
//
// 对应现实：某人的 IP 先因规则一被封，随后他换了个令牌又来——两个请求同时
// 判定，各自看到「IP 已封、令牌未封」，于是都只带着令牌去落库。
func TestConcurrentAutoBanPartialOverlap(t *testing.T) {
	setupBanTestDB(t)

	// 先封掉 IP
	first := &types.BanSubject{Reason: "先封 IP", Source: types.BanSourceAuto}
	if _, _, err := CreateBan(first, []types.BanIdent{
		{Kind: types.IdentIP, Value: "203.0.113.10"},
	}, false); err != nil {
		t.Fatalf("CreateBan 出错: %v", err)
	}
	if err := ReloadBanList(); err != nil {
		t.Fatalf("ReloadBanList 出错: %v", err)
	}

	const workers = 8
	idents := []types.BanIdent{
		{Kind: types.IdentIP, Value: "203.0.113.10"}, // 已封，故新令牌归入它所属的主体
		{Kind: types.IdentVisitor, Value: "visitor-partial"},
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range workers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			_, _ = concurrentBan(fmt.Sprintf("并发部分命中 #%d", n), idents)
		}(i)
	}
	close(start)
	wg.Wait()

	subjects, err := GetBanSubjects()
	if err != nil {
		t.Fatalf("GetBanSubjects 出错: %v", err)
	}

	t.Logf("部分命中并发后，库里有 %d 个主体", len(subjects))
	for _, s := range subjects {
		t.Logf("  主体 #%d reason=%q 标识数=%d", s.ID, s.Reason, len(s.Idents))
		for _, id := range s.Idents {
			t.Logf("      %s:%s", id.Kind, id.Value)
		}
	}

	for _, s := range subjects {
		if len(s.Idents) == 0 {
			t.Errorf("主体 #%d 没有任何标识（悬空主体）", s.ID)
		}
	}

	// IP 与令牌属于同一个人，必须落在同一个主体上。
	//
	// 曾经不是这样：那时「部分命中」只写没在册的那些标识、并为它们新建一个主体，
	// 于是 IP 归旧主体、令牌归新主体。后果是管理员在管理页看到两条记录，
	// 删掉一条之后人依然进不来。
	ipOwner, visitorOwner := 0, 0
	for _, s := range subjects {
		for _, ident := range s.Idents {
			switch {
			case ident.Kind == types.IdentIP && ident.Value == "203.0.113.10":
				ipOwner = s.ID
			case ident.Kind == types.IdentVisitor && ident.Value == "visitor-partial":
				visitorOwner = s.ID
			}
		}
	}
	t.Logf("IP 归属主体 #%d，令牌归属主体 #%d", ipOwner, visitorOwner)

	if ipOwner == 0 || visitorOwner == 0 {
		t.Fatalf("两个标识都应在册，实际 IP=%d 令牌=%d", ipOwner, visitorOwner)
	}
	if ipOwner != visitorOwner {
		t.Errorf("同一个人的标识被拆到主体 #%d 与 #%d——解封一个另一个仍生效",
			ipOwner, visitorOwner)
	}

	// 归入已有主体时不应改写其原因：最初那条记录的原因与时间要保留，
	// 否则申诉里存的快照对不上
	for _, s := range subjects {
		if s.ID == ipOwner && s.Reason != "先封 IP" {
			t.Errorf("已有主体的原因被改写为 %q，应保留「先封 IP」", s.Reason)
		}
	}

	// 解封这一个主体，人就该彻底出来
	if err := DeleteBanSubject(ipOwner); err != nil {
		t.Fatalf("DeleteBanSubject 出错: %v", err)
	}
	if err := ReloadBanList(); err != nil {
		t.Fatalf("ReloadBanList 出错: %v", err)
	}
	if _, hit := banlist.Check(banlist.Request{
		IP: "203.0.113.10", VisitorKey: "visitor-partial",
	}); hit {
		t.Error("解封唯一的主体后，该来源仍被名单命中")
	}
}

// setupBanTestDB 用临时 SQLite 顶替应用库，并清空内存名单。
// 连接数限制与生产一致（单连接），故这里测出的并发行为与线上同构。
func setupBanTestDB(t *testing.T) {
	t.Helper()

	database.UseAppDBForTest(t)
	banlist.Replace(nil)
}
