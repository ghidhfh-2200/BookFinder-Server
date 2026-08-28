package models

import (
	"fmt"

	"bookfinder-backend/database"
	"bookfinder-backend/types"

	"gorm.io/gorm"
)

// GetBanSubjects 取回全部封禁主体及其标识，供内存名单载入与管理页展示
func GetBanSubjects() ([]types.BanSubject, error) {
	var subjects []types.BanSubject
	if err := database.GetAppDB().
		Preload("Idents").
		Order("created_at DESC").
		Find(&subjects).Error; err != nil {
		return nil, err
	}
	return subjects, nil
}

// GetBanSubjectByID 按 ID 取回封禁主体及其标识
func GetBanSubjectByID(id int) (*types.BanSubject, error) {
	var subject types.BanSubject
	if err := database.GetAppDB().Preload("Idents").First(&subject, id).Error; err != nil {
		return nil, err
	}
	return &subject, nil
}

// FindBanByIdent 按标识查出所属的封禁主体。
// 供申诉等需要「该来源是否确实被封」的场景使用；请求路径上的判定走内存名单。
func FindBanByIdent(kind types.BanIdentKind, value string) (*types.BanSubject, error) {
	var ident types.BanIdent
	if err := database.GetAppDB().
		Where("kind = ? AND value = ?", kind, value).
		First(&ident).Error; err != nil {
		return nil, err
	}
	return GetBanSubjectByID(ident.SubjectID)
}

// FindBanByAnyIdent 依次按给定标识查封禁主体，命中即返回。
//
// 申诉受理需要它：受理是按 IP 找主体的，而网段级的精准处置只写令牌标识
// （见 middlewares 的 networkBanIdents），那类主体名下根本没有 IP 标识。
// 只按 IP 查的话会查不到、被当成「已被手动解封」，于是接口回「已受理并解封」
// 而人其实还在封禁里——申诉就成了一句空话。
//
// 全部标识都查不到时返回 gorm.ErrRecordNotFound，由调用方判断是否确实已解封。
func FindBanByAnyIdent(idents []types.BanIdent) (*types.BanSubject, error) {
	for _, ident := range idents {
		if !ident.Kind.IsValid() || ident.Value == "" {
			continue
		}

		subject, err := FindBanByIdent(ident.Kind, ident.Value)
		if err == nil {
			return subject, nil
		}
		if !database.IsNotFound(err) {
			return nil, err
		}
	}

	return nil, gorm.ErrRecordNotFound
}

// CreateBan 把给定标识归入一个封禁主体，并返回最终生效的那个主体。
//
// 「一个人一条记录」是这里的核心约束：封禁挂在主体上，而解封是按主体进行的。
// 若同一个人的标识散落在多个主体上，管理员删掉一条之后人依然进不来。故传入的
// 标识只要有任何一个已在册，就归入它所属的那个主体，而不是新建。
//
// 标识分属多个主体时合并：取最早的一个（ID 最小，即这个人最初被封的那条记录）
// 作为落点，其余主体的标识全部迁入、空主体删除。这与「网段判定一次封多个设备时
// 共用一条封禁记录」的既有做法一致。
//
// overwriteReason 决定归入已有主体时是否改写其封禁原因：
//
//   - 手动封禁传 true：管理员刚填的原因是一次有意的处置，理应生效。
//   - 自动封禁传 false：同一个人会被规则反复命中（判据都是当日累计值），
//     每次都改写的话，最初那条记录的原因与时间会被一次次覆盖，
//     申诉里存的快照也就再也对不上了。
//
// 第二个返回值表示是否确有改动入库。为 false 时说明这些标识早已全部在册、
// 同属一个主体且无需改写原因，调用方可据此跳过日志与内存名单重建。
//
// 整个过程在一个事务内完成。归并由库裁决而非调用方先查内存名单：后者与库之间
// 存在时间窗，两个并发请求会同时认为「还没封过」，于是各建一条记录。
func CreateBan(subject *types.BanSubject, idents []types.BanIdent,
	overwriteReason bool) (*types.BanSubject, bool, error) {

	valid := dedupIdents(idents)
	if len(valid) == 0 {
		return nil, false, fmt.Errorf("封禁至少需要一个合法标识")
	}

	var (
		effective types.BanSubject
		wrote     bool
	)

	err := database.GetAppDB().Transaction(func(tx *gorm.DB) error {
		// 先查清这些标识现有的归属：已有归属的用于定位这个人的主体，
		// 没有归属的稍后挂上去
		owners := make(map[int]struct{})
		missing := make([]types.BanIdent, 0, len(valid))

		for _, ident := range valid {
			var existing types.BanIdent
			err := tx.Where("kind = ? AND value = ?", ident.Kind, ident.Value).
				First(&existing).Error

			switch {
			case err == nil:
				owners[existing.SubjectID] = struct{}{}
			case database.IsNotFound(err):
				missing = append(missing, ident)
			default:
				return err
			}
		}

		hostID := lowestID(owners)

		switch {
		case hostID == 0:
			// 一个标识都没在册：这是个新主体
			subject.Idents = nil
			if err := tx.Create(subject).Error; err != nil {
				return err
			}
			hostID = subject.ID
			wrote = true

		case overwriteReason:
			// 归入已有主体，且调用方要求改写原因（手动封禁）。
			// Source 一并改写：这条记录此后代表的是管理员的处置。
			if err := tx.Model(&types.BanSubject{}).
				Where("id = ?", hostID).
				Updates(map[string]any{
					"reason": subject.Reason,
					"detail": subject.Detail,
					"source": subject.Source,
				}).Error; err != nil {
				return err
			}
			wrote = true
		}

		// 涉及多个主体时合并到最早的那个
		for id := range owners {
			if id == hostID {
				continue
			}
			if err := tx.Model(&types.BanIdent{}).
				Where("subject_id = ?", id).
				Update("subject_id", hostID).Error; err != nil {
				return err
			}
			if err := tx.Delete(&types.BanSubject{}, id).Error; err != nil {
				return err
			}
			wrote = true
		}

		// 尚未在册的标识挂到落点主体名下
		for _, ident := range missing {
			ident.ID = 0
			ident.SubjectID = hostID
			if err := tx.Create(&ident).Error; err != nil {
				return err
			}
			wrote = true
		}

		// 回读落点主体：调用方要用它的 CreatedAt 写响应，而合并时那个时间
		// 来自已有记录、不在入参里
		return tx.Preload("Idents").First(&effective, hostID).Error
	})
	if err != nil {
		return nil, false, err
	}

	return &effective, wrote, nil
}

// dedupIdents 滤掉不合法的标识，并去掉重复的 (kind, value)。
// 去重是必要的：同一个标识出现两次会让后续的「查归属—插入」逻辑
// 对第二次误判为「尚未在册」，从而撞上唯一索引。
func dedupIdents(idents []types.BanIdent) []types.BanIdent {
	valid := make([]types.BanIdent, 0, len(idents))
	seen := make(map[string]struct{}, len(idents))

	for _, ident := range idents {
		if !ident.Kind.IsValid() || ident.Value == "" {
			continue
		}

		key := string(ident.Kind) + ":" + ident.Value
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		valid = append(valid, ident)
	}

	return valid
}

// lowestID 取最小的主体 ID，空集合返回 0。
// 主键自增，故最小的就是最早创建的那个——合并时以它为落点，
// 保留的是这个人最初被封的原因与时间。
func lowestID(ids map[int]struct{}) int {
	lowest := 0
	for id := range ids {
		if lowest == 0 || id < lowest {
			lowest = id
		}
	}
	return lowest
}

// DeleteBanSubject 解封：删除主体及其全部标识。
// 封禁是永久的，解封即删除记录——没有「过期」这一状态。
func DeleteBanSubject(id int) error {
	return database.GetAppDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("subject_id = ?", id).
			Delete(&types.BanIdent{}).Error; err != nil {
			return err
		}
		return tx.Delete(&types.BanSubject{}, id).Error
	})
}
