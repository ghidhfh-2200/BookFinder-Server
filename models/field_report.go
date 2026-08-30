package models

import (
	"bookfinder-backend/database"
	"bookfinder-backend/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AddFieldReport 记录一次报告。
// 唯一索引保证同一访问者对同一字段的同一种报告只留一行，重复提交由数据库忽略，
// 因此并发报告不会把次数算多，也无需应用层加锁。
// 返回该字段该种报告当前的独立次数。
func AddFieldReport(report *types.FieldReport) (int, error) {
	var count int64

	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		// 已存在则什么都不做，不覆盖首次报告的 IP 与时间
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(report).Error; err != nil {
			return err
		}

		// 带上 kind：两种票各自计数，混在一起算会让确认票把过时阈值顶满
		return tx.Model(&types.FieldReport{}).
			Where("library_id = ? AND field_name = ? AND kind = ?",
				report.LibraryID, report.FieldName, report.Kind).
			Count(&count).Error
	})
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

// RemoveFieldReport 撤销某访问者对某字段某种报告，返回撤销后的次数与是否确有删除。
// 条件里带上 ReporterKey，故只删自己那一行、次数 -1，不会清空他人的报告。
func RemoveFieldReport(libraryID int, fieldName string, kind types.ReportKind, reporterKey string) (int, bool, error) {
	var (
		count   int64
		removed bool
	)

	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		result := tx.Where("library_id = ? AND field_name = ? AND kind = ? AND reporter_key = ?",
			libraryID, fieldName, kind, reporterKey).
			Delete(&types.FieldReport{})
		if result.Error != nil {
			return result.Error
		}
		removed = result.RowsAffected > 0

		return tx.Model(&types.FieldReport{}).
			Where("library_id = ? AND field_name = ? AND kind = ?", libraryID, fieldName, kind).
			Count(&count).Error
	})
	if err != nil {
		return 0, false, err
	}

	return int(count), removed, nil
}

// CountFieldReport 统计单个字段某种报告的独立次数
func CountFieldReport(libraryID int, fieldName string, kind types.ReportKind) (int, error) {
	var count int64
	if err := database.GetDB().Model(&types.FieldReport{}).
		Where("library_id = ? AND field_name = ? AND kind = ?", libraryID, fieldName, kind).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

// DeleteFieldReportsOf 清空某图书馆某字段的全部报告（两种票都清）。
// 网站地址变更时调用：旧票是对旧地址的判断，留着会让新地址继承旧结论。
func DeleteFieldReportsOf(libraryID int, fieldName string) error {
	return database.GetDB().
		Where("library_id = ? AND field_name = ?", libraryID, fieldName).
		Delete(&types.FieldReport{}).Error
}

// CountFieldReports 统计给定图书馆各字段某种报告的次数，键为「图书馆 ID + 字段名」。
// 列表页一次查出所有记录的计数，避免逐行查询。
func CountFieldReports(libraryIDs []int, kind types.ReportKind) (map[int]map[string]int, error) {
	counts := make(map[int]map[string]int)
	if len(libraryIDs) == 0 {
		return counts, nil
	}

	var rows []struct {
		LibraryID int
		FieldName string
		Total     int
	}

	if err := database.GetDB().Model(&types.FieldReport{}).
		Select("library_id, field_name, COUNT(*) AS total").
		Where("library_id IN ? AND kind = ?", libraryIDs, kind).
		Group("library_id, field_name").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		if counts[row.LibraryID] == nil {
			counts[row.LibraryID] = make(map[string]int)
		}
		counts[row.LibraryID][row.FieldName] = row.Total
	}

	return counts, nil
}

// ListReportedFields 查出给定访问者在这些图书馆中已提交过某种报告的字段，
// 供前端判断该显示「报告」还是「撤销」。
func ListReportedFields(libraryIDs []int, kind types.ReportKind, reporterKey string) (map[int]map[string]bool, error) {
	return listFields(libraryIDs, kind, "reporter_key = ?", reporterKey)
}

// ListSameOriginFields 查出这些图书馆中、由给定来源 IP 提交过某种报告的字段。
// 与 ListReportedFields 的差集即「同一来源报过但不是你提交的」，前端据此提前提示疑似重复。
func ListSameOriginFields(libraryIDs []int, kind types.ReportKind, reporterIP string) (map[int]map[string]bool, error) {
	return listFields(libraryIDs, kind, "reporter_ip = ?", reporterIP)
}

// listFields 按给定条件查出「图书馆 ID → 字段名」的集合
func listFields(libraryIDs []int, kind types.ReportKind, condition string, arg string) (map[int]map[string]bool, error) {
	found := make(map[int]map[string]bool)
	if len(libraryIDs) == 0 || arg == "" {
		return found, nil
	}

	var rows []types.FieldReport
	if err := database.GetDB().
		Select("library_id, field_name").
		Where("library_id IN ? AND kind = ?", libraryIDs, kind).
		Where(condition, arg).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		if found[row.LibraryID] == nil {
			found[row.LibraryID] = make(map[string]bool)
		}
		found[row.LibraryID][row.FieldName] = true
	}

	return found, nil
}

// HasFieldReport 判断某访问者是否已提交过该字段的该种报告
func HasFieldReport(libraryID int, fieldName string, kind types.ReportKind, reporterKey string) (bool, error) {
	var count int64
	if err := database.GetDB().Model(&types.FieldReport{}).
		Where("library_id = ? AND field_name = ? AND kind = ? AND reporter_key = ?",
			libraryID, fieldName, kind, reporterKey).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// CountSuspectedDuplicates 统计同一 IP 或同一指纹已就该字段提交过该种报告的次数。
// 用于启发式判断：令牌是新的但 IP 与指纹都和已有报告吻合，多半是同一人换了身份。
// 指纹可伪造，故只用来提示与拒绝计数，不作为身份依据。
//
// 按 kind 区分：确认过网站可用的人日后报告它失效，不该被判为重复。
func CountSuspectedDuplicates(libraryID int, fieldName string, kind types.ReportKind, reporterIP, fingerprint string) (int64, error) {
	query := database.GetDB().Model(&types.FieldReport{}).
		Where("library_id = ? AND field_name = ? AND kind = ?", libraryID, fieldName, kind)

	if fingerprint == "" {
		query = query.Where("reporter_ip = ?", reporterIP)
	} else {
		query = query.Where("reporter_ip = ? OR fingerprint = ?", reporterIP, fingerprint)
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}

	return count, nil
}

// DeleteFieldReports 清空指定字段的全部报告，字段从注册表移除时一并清理
func DeleteFieldReports(fieldNames []string) error {
	if len(fieldNames) == 0 {
		return nil
	}
	return database.GetDB().Where("field_name IN ?", fieldNames).
		Delete(&types.FieldReport{}).Error
}

// DeleteLibraryReports 删除某个图书馆的全部报告，随图书馆删除一并清理
func DeleteLibraryReports(tx *gorm.DB, libraryID int) error {
	return tx.Where("library_id = ?", libraryID).Delete(&types.FieldReport{}).Error
}
