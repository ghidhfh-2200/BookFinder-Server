package dashboard

import (
	"context"

	"bookfinder-backend/models"
	"bookfinder-backend/utils/banlist"

	"github.com/redis/go-redis/v9"
)

// Overview 监控面板的一份数据
type Overview struct {
	// Libraries 图书馆总数
	Libraries int64 `json:"libraries"`
	// LibrariesAvailable 图书馆数是否取到。
	// 图书馆库在远端 MySQL，取不到时前端应显示「暂无数据」而非 0。
	LibrariesAvailable bool `json:"libraries_available"`
	// Bans 封禁情况，取自内存名单
	Bans BanCounts `json:"bans"`
	// Traffic 流量指标，取自 Redis
	Traffic Traffic `json:"traffic"`
}

// BanCounts 封禁规模。
//
// 分开给出主体数与标识数，因为两者含义不同且常被混淆：封禁挂在「主体」上，
// 一个主体可有多个标识（IP、网段、令牌、设备）。「被封禁 IP 数」直觉上问的是
// 有多少个地址进不来，而那是 IP 类标识的数量，不是主体数。
type BanCounts struct {
	// Subjects 封禁主体数，即「封了多少个人」
	Subjects int `json:"subjects"`
	// IPs 精确 IP 标识数
	IPs int `json:"ips"`
	// Networks 网段标识数
	Networks int `json:"networks"`
	// Idents 标识总数，含 IP、网段、访问者令牌与设备标识。
	// 与 IPs 的差额即跨 IP 跟人的那些标识，它们是「换 IP 也脱不了身」的依据。
	Idents int `json:"idents"`
}

// Read 汇总一份面板数据。
//
// 三个来源各自独立取，任一不可用不影响其余：图书馆数在远端 MySQL、封禁数在内存、
// 流量在 Redis。面板是观察工具，缺一项应当显示「暂无数据」，而不是整页失败。
func Read(ctx context.Context, rdb *redis.Client) Overview {
	overview := Overview{
		Bans:    readBans(),
		Traffic: ReadTraffic(ctx, rdb),
	}

	if total, err := models.CountLibraries(); err == nil {
		overview.Libraries = total
		overview.LibrariesAvailable = true
	}

	return overview
}

// readBans 从内存名单取封禁规模。
//
// 不查库：名单本就是库的全量镜像（见 utils/banlist 的说明），而应用库只允许
// 一个连接，面板每隔几十秒刷一次不该去占用它。
func readBans() BanCounts {
	stats := banlist.Count()

	return BanCounts{
		Subjects: stats.Subjects,
		IPs:      stats.IPs,
		Networks: stats.Networks,
		Idents:   stats.Idents,
	}
}
