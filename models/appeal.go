package models

import (
	"errors"
	"fmt"

	"bookfinder-backend/database"
	"bookfinder-backend/types"

	"gorm.io/gorm"
)

// CountAppealsByIP 统计某 IP 已占用的申诉配额。
//
// 已受理的申诉不计入：受理意味着承认那次封禁是误判，若仍占用配额，
// 三次误封之后这个 IP 就永远无法申诉了。
// 驳回的照常计入——那是「申诉过且未被认可」，正是配额要限制的情形。
//
// 这不构成自动解封：它只影响申诉次数的计算，解封仍然只有管理员手动与申诉受理两条路径。
func CountAppealsByIP(ip string) (int, error) {
	var count int64
	if err := database.GetAppDB().Model(&types.BanAppeal{}).
		Where("ip = ? AND status <> ?", ip, types.AppealAccepted).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

// CountAllAppealsByIP 统计某 IP 的申诉总数，含已受理的。
// Attempt 用它编号：序号要反映真实的提交次序，否则历史申诉会出现重复编号。
func CountAllAppealsByIP(ip string) (int, error) {
	var count int64
	if err := database.GetAppDB().Model(&types.BanAppeal{}).
		Where("ip = ?", ip).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

// ErrAppealQuotaExhausted 申诉次数已达上限。
//
// 定义成哨兵错误而非只写一句话：调用方要据此回 429 而不是 500，
// 若靠错误消息的文字匹配来判断（strings.Contains(err.Error(), "达到上限")），
// 这句中文一改，那个分支就静默失效、超限会变成服务器错误。
var ErrAppealQuotaExhausted = errors.New("申诉次数已达上限")

// CreateAppeal 提交一次申诉，自动填入这是第几次。
// 计数与插入放在同一事务里，避免并发提交拿到相同的序号或越过上限。
//
// 次数超限时返回包装了 ErrAppealQuotaExhausted 的错误，调用方用 errors.Is 判断。
func CreateAppeal(appeal *types.BanAppeal) error {
	return database.GetAppDB().Transaction(func(tx *gorm.DB) error {
		// 配额只看未受理的申诉，序号则按全部申诉编排：
		// 前者决定还能不能提交，后者保证历史记录的次序不重复
		var used int64
		if err := tx.Model(&types.BanAppeal{}).
			Where("ip = ? AND status <> ?", appeal.IP, types.AppealAccepted).
			Count(&used).Error; err != nil {
			return err
		}

		if int(used) >= types.MaxAppealsPerIP {
			return fmt.Errorf("%w：已提交 %d 次", ErrAppealQuotaExhausted, used)
		}

		var total int64
		if err := tx.Model(&types.BanAppeal{}).
			Where("ip = ?", appeal.IP).Count(&total).Error; err != nil {
			return err
		}

		appeal.Attempt = int(total) + 1
		appeal.Status = types.AppealPending

		return tx.Create(appeal).Error
	})
}

// GetAppealsByIP 查询某 IP 的全部申诉，按提交次序排列
func GetAppealsByIP(ip string) ([]types.BanAppeal, error) {
	var appeals []types.BanAppeal
	if err := database.GetAppDB().Where("ip = ?", ip).
		Order("attempt ASC").Find(&appeals).Error; err != nil {
		return nil, err
	}
	return appeals, nil
}

// GetAppealByID 按 ID 获取申诉
func GetAppealByID(id int) (*types.BanAppeal, error) {
	var appeal types.BanAppeal
	if err := database.GetAppDB().First(&appeal, id).Error; err != nil {
		return nil, err
	}
	return &appeal, nil
}

// ReviewAppeal 记录管理员的处理结果
func ReviewAppeal(id int, status types.AppealStatus, note string) error {
	return database.GetAppDB().Model(&types.BanAppeal{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":     status,
			"admin_note": note,
		}).Error
}

// CountPendingAppeals 统计待处理的申诉数，供管理页显示提醒
func CountPendingAppeals() (int, error) {
	var count int64
	if err := database.GetAppDB().Model(&types.BanAppeal{}).
		Where("status = ?", types.AppealPending).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

// CountAppealsGrouped 统计各 IP 的申诉数与待处理数，供封禁列表一次取回
func CountAppealsGrouped() (map[string]types.AppealSummary, error) {
	var rows []struct {
		IP      string
		Total   int
		Pending int
	}

	if err := database.GetAppDB().Model(&types.BanAppeal{}).
		Select("ip, COUNT(*) AS total, SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS pending",
			types.AppealPending).
		Group("ip").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	summaries := make(map[string]types.AppealSummary, len(rows))
	for _, row := range rows {
		summaries[row.IP] = types.AppealSummary{
			Total:   row.Total,
			Pending: row.Pending,
		}
	}

	return summaries, nil
}
