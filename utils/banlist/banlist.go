// Package banlist 在内存中持有封禁名单，供请求路径上的封禁判定使用。
//
// 为什么必须走内存：封禁检查发生在每个 API 请求上，而封禁数据在本地 SQLite，
// 应用库又限制为单个连接（见 database/app.go）。若每个请求都查库，封禁检查
// 本身就成了最窄的瓶颈——加固封禁反而让服务更容易被打崩。
//
// 数据量决定了这样做是安全的：封禁记录以千计而非以百万计，全量驻留内存的开销
// 可以忽略。写操作（封禁、解封）仍然落库，内存随之增量更新。
package banlist

import (
	"net"
	"sync"

	"bookfinder-backend/types"
	"bookfinder-backend/utils/netmask"
)

// Match 一次封禁判定的结果
type Match struct {
	// Kind 命中的标识种类，便于在日志与管理页说明「为什么被封」
	Kind types.BanIdentKind
	// Value 命中的标识取值
	Value string
	// Subject 该标识所属的封禁主体
	Subject types.BanSubject
}

// snapshot 名单的一份不可变快照。
//
// 读多写极少，故采用「整体替换快照」而非对每张 map 加锁：读路径全程无锁竞争，
// 写路径复制一份新快照再原子换上。封禁记录数量小，复制成本可以忽略。
type snapshot struct {
	// subjects 主体 ID → 主体
	subjects map[int]types.BanSubject
	// idents 精确匹配的标识：种类 → 取值 → 主体 ID。
	// 网段不在此列，它需要逐段比对，见 networks。
	idents map[types.BanIdentKind]map[string]int
	// networks 已封禁的网段，需逐个 Contains 比对
	networks []networkEntry
}

// networkEntry 一个已封禁的网段及其归属
type networkEntry struct {
	network   *net.IPNet
	value     string
	subjectID int
}

var (
	mu      sync.RWMutex
	current = emptySnapshot()
)

// emptySnapshot 构造一份空快照
func emptySnapshot() *snapshot {
	return &snapshot{
		subjects: make(map[int]types.BanSubject),
		idents:   make(map[types.BanIdentKind]map[string]int),
	}
}

// Replace 用给定数据整体替换名单，供启动时全量载入与写操作后重建。
//
// 采用「整体重建」而非增量维护：封禁与解封都不频繁，重建一次的成本远低于
// 维护增量正确性的复杂度——尤其是解封要连带清掉主体的全部标识与网段。
func Replace(subjects []types.BanSubject) {
	next := emptySnapshot()

	for _, subject := range subjects {
		// 标识挂在主体上，但快照里只留主体本身，避免每次命中都复制整个切片
		idents := subject.Idents
		subject.Idents = nil
		next.subjects[subject.ID] = subject

		for _, ident := range idents {
			if ident.Kind == types.IdentIPNet {
				network, err := netmask.ParseNetwork(ident.Value)
				if err != nil {
					// 库里存了非法网段，跳过即可：宁可漏封，也不要因一条坏数据崩掉
					continue
				}
				next.networks = append(next.networks, networkEntry{
					network:   network,
					value:     ident.Value,
					subjectID: subject.ID,
				})
				continue
			}

			if next.idents[ident.Kind] == nil {
				next.idents[ident.Kind] = make(map[string]int)
			}
			next.idents[ident.Kind][ident.Value] = subject.ID
		}
	}

	mu.Lock()
	current = next
	mu.Unlock()
}

// Request 一次请求携带的全部封禁标识
type Request struct {
	// IP 来源 IP，取自可信的 ClientIP
	IP string
	// VisitorKey 访问者令牌哈希，浏览器与安卓端通用
	VisitorKey string
	// DeviceKey 安卓设备标识哈希，仅在请求签名校验通过时才应填入
	DeviceKey string
}

// Check 判定本次请求是否命中封禁，命中则返回首个匹配。
//
// 判定顺序：精确 IP → 访问者令牌 → 设备标识 → 所属网段。
//
// 网段放在最后：自动封禁优先只封具体设备（访问者令牌），只有认不出异常设备时
// 才会写入网段标识，故命中网段的情形本就少见。
func Check(req Request) (Match, bool) {
	mu.RLock()
	snap := current
	mu.RUnlock()

	if ip, ok := netmask.Canonical(req.IP); ok {
		if match, hit := snap.lookup(types.IdentIP, ip); hit {
			return match, true
		}
	}

	if req.VisitorKey != "" {
		if match, hit := snap.lookup(types.IdentVisitor, req.VisitorKey); hit {
			return match, true
		}
	}

	if req.DeviceKey != "" {
		if match, hit := snap.lookup(types.IdentDevice, req.DeviceKey); hit {
			return match, true
		}
	}

	if req.IP != "" {
		for _, entry := range snap.networks {
			if !netmask.Contains(entry.network, req.IP) {
				continue
			}
			if subject, ok := snap.subjects[entry.subjectID]; ok {
				return Match{Kind: types.IdentIPNet, Value: entry.value, Subject: subject}, true
			}
		}
	}

	return Match{}, false
}

// Has 判断某个标识当前是否已在名单内。
//
// 供自动封禁在写库前滤掉已封的标识：各条规则的判据都是当日累计值，命中之后
// 该来源的每个后续请求都会重新命中同一条规则。不先滤掉的话就会反复落库——
// 而应用库只允许一个连接，且改判逻辑会把标识迁到新主体、删掉旧主体，
// 原始封禁记录的原因与时间因此被一次次覆盖。
func Has(kind types.BanIdentKind, value string) bool {
	if value == "" {
		return false
	}

	mu.RLock()
	snap := current
	mu.RUnlock()

	// 网段不在精确匹配表里（见 snapshot.networks 的说明）。此处比的是「这个网段
	// 是否已被封」而非「某个 IP 是否落在已封网段内」，故按取值比对而不用 Contains。
	if kind == types.IdentIPNet {
		for _, entry := range snap.networks {
			if entry.value == value {
				return true
			}
		}
		return false
	}

	_, hit := snap.lookup(kind, value)

	return hit
}

// lookup 查精确匹配的标识
func (s *snapshot) lookup(kind types.BanIdentKind, value string) (Match, bool) {
	values, ok := s.idents[kind]
	if !ok {
		return Match{}, false
	}

	subjectID, ok := values[value]
	if !ok {
		return Match{}, false
	}

	subject, ok := s.subjects[subjectID]
	if !ok {
		return Match{}, false
	}

	return Match{Kind: kind, Value: value, Subject: subject}, true
}

// Stats 名单规模，供启动日志与管理页展示
type Stats struct {
	// Subjects 封禁主体数，即「封了多少个人」
	Subjects int
	// Idents 标识总数，含各种类与网段
	Idents int
	// IPs 精确 IP 标识数。
	//
	// 单列出来是因为「被封禁 IP 数」与「标识总数」常被混为一谈：后者还含令牌与
	// 设备标识，而那些不是地址。面板要答的是「有多少个地址进不来」。
	IPs int
	// Networks 网段标识数
	Networks int
}

// Count 返回当前名单规模
func Count() Stats {
	mu.RLock()
	snap := current
	mu.RUnlock()

	stats := Stats{
		Subjects: len(snap.subjects),
		Networks: len(snap.networks),
		IPs:      len(snap.idents[types.IdentIP]),
	}
	for _, values := range snap.idents {
		stats.Idents += len(values)
	}
	stats.Idents += len(snap.networks)

	return stats
}
