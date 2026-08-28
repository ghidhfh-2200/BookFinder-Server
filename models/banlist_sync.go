package models

import (
	"fmt"
	"sync"

	"bookfinder-backend/utils/banlist"
)

// reloadMu 串行化「读库 → 替换快照」。
// 缺了它，两次并发重建会互相覆盖：旧快照盖掉新快照，库里对了但内存漏掉刚写入的
// 封禁，而请求路径只查内存。实测约 10% 的并发批次会这样。
var reloadMu sync.Mutex

// ReloadBanList 从库中重新载入内存封禁名单。
// 封禁、解封之后都必须调用，否则改动不会生效（请求路径只查内存）。
//
// 不可在数据库事务内调用：应用库只有一个连接，会死锁。
func ReloadBanList() error {
	reloadMu.Lock()
	defer reloadMu.Unlock()

	subjects, err := GetBanSubjects()
	if err != nil {
		return fmt.Errorf("failed to load ban subjects: %w", err)
	}

	banlist.Replace(subjects)

	return nil
}
