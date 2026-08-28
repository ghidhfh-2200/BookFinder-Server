package ratelimit

import (
	"context"

	"bookfinder-backend/types"
	"bookfinder-backend/utils/netmask"

	"github.com/redis/go-redis/v9"
)

// ResetForSubject 重置一个被解封主体名下全部标识对应的封禁判据。
//
// 解封只删封禁记录是不够的：各条规则的判据都是当日累计值，留着的话解封后第一个
// 请求就会重新命中、立刻再封一次。而判据分散在不同的计数主体上，故须按标识种类
// 分别处理——只遍历 IP 标识是不够的：
//
//   - 网段级的精准处置只写令牌标识，那类主体名下没有任何 IP 标识，
//     只处理 IP 的话一次都不会重置，解封后立刻复发；
//   - IPv6 的见习计数主体是 /64，与网段标识同粒度，故网段标识也要清见习计数。
//
// 令牌的网段贡献需要知道其所属网段，而令牌标识本身不含这个信息，
// 故先从主体的 IP 与网段标识推出网段，再传给 ResetVisitorAfterUnban。
// 主体只含令牌标识时推不出来，此时退而只清该令牌的各类计数——网段画像里残留的
// 分数不足以立即让它重新命中（异常设备的门槛是「网段预算 × 每日超额倍数」）。
//
// 逐个标识独立重置，中途出错也继续处理其余标识：漏清一项只是可能复发，
// 而提前返回会让后面的标识一项都没清。返回首个错误供调用方记录。
func ResetForSubject(ctx context.Context, rdb *redis.Client,
	idents []types.BanIdent) error {

	if rdb == nil {
		return nil
	}

	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	scope := networkScopeOf(idents)

	for _, ident := range idents {
		switch ident.Kind {
		case types.IdentIP:
			note(ResetAfterUnban(ctx, rdb, ident.Value))

		case types.IdentIPNet:
			// 网段本身的画像与告警标记。必须用按网段的变体：封禁记录里存的是
			// "2001:db8::/64" 这样的网段串，当 IP 解析会失败、静默什么都不做。
			note(ResetNetworkScope(ctx, rdb, ident.Value))
			// IPv6 的见习计数按 /64 计，主体就是这个网段
			note(ResetProbationScope(ctx, rdb, ident.Value))

		case types.IdentVisitor:
			note(ResetVisitorAfterUnban(ctx, rdb, ident.Value, scope))

		case types.IdentDevice:
			// 设备标识不参与任何限流计数，无需重置
		}
	}

	return firstErr
}

// networkScopeOf 从主体的标识里推出其所属网段，供清理令牌的网段贡献。
// 推不出来时返回空串，调用方据此跳过那一步。
func networkScopeOf(idents []types.BanIdent) string {
	// 网段标识本身就是答案
	for _, ident := range idents {
		if ident.Kind == types.IdentIPNet && ident.Value != "" {
			return ident.Value
		}
	}

	// 退而从精确 IP 推算
	for _, ident := range idents {
		if ident.Kind != types.IdentIP {
			continue
		}
		if prefix, ok := netmask.PrefixOf(ident.Value); ok {
			return prefix
		}
	}

	return ""
}
