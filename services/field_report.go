package services

import (
	"errors"

	"bookfinder-backend/models"
	"bookfinder-backend/types"
	"bookfinder-backend/utils/dedup"
	"bookfinder-backend/utils/schema"
)

// 字段报告的失败原因。由调用方映射为 HTTP 状态码——
// 服务层不认识 HTTP，但要让调用方能分辨「找不到」与「不该做」。
var (
	// ErrLibraryNotFound 图书馆不存在
	ErrLibraryNotFound = errors.New("图书馆不存在")
	// ErrFieldNotFound 该记录不含此字段
	ErrFieldNotFound = errors.New("该记录不含此字段")
	// ErrNotWebsiteField 该字段不承担 website 角色，没有验证流程
	ErrNotWebsiteField = errors.New("只有网站字段需要确认")
	// ErrAlreadyVerified 该网站已转正，无需再确认
	ErrAlreadyVerified = errors.New("该网站已确认有效，无需再次确认")
	// ErrVerifyOutdated 该字段已被标为过时，确认票不该把它拉回有效
	ErrVerifyOutdated = errors.New("该信息已被标记为过时，无法确认可用")
	// ErrNothingToVerify 该字段不处于未验证状态，无需确认
	ErrNothingToVerify = errors.New("该网站无需确认")
	// ErrAlreadyOutdated 该字段已被标为过时，再报也不会改变什么
	ErrAlreadyOutdated = errors.New("该信息已被标记为过时")
	// ErrNoSuchReport 该访问者没有提交过对应的报告，无从撤销
	ErrNoSuchReport = errors.New("没有提交过该报告")
)

// FieldReportOutcome 一次报告或撤销的结果，供调用方渲染响应与留痕。
type FieldReportOutcome struct {
	// Count 该种报告当前的独立次数
	Count int
	// Threshold 触发状态变更所需次数
	Threshold int
	// Status 该字段处理后的状态
	Status types.LibraryStatus
	// Counted 本次是否计入。疑似重复时为 false
	Counted bool
	// Duplicate 本次被判为疑似重复，未计入次数
	Duplicate bool
	// Reached 本次使计数达到阈值，因而改变了字段状态
	Reached bool
	// Stale 本次因字段状态已变（客户端页面过期）而被拒。
	// 此时 Status 与 Count 是服务端的现状，供客户端就地纠正显示。
	Stale bool
}

// ReportFieldOutdated 记录一次「该字段信息已过时」的报告。
//
// 报告按人去重，累计到 types.OutdatedReportThreshold 次才把字段置为过时；
// 未达阈值只记次数。疑似重复（换了令牌但 IP 或指纹吻合）不计数，
// 返回的 Outcome 里 Duplicate 为真，由调用方决定如何提示与是否累计违规。
//
// 已过时的字段不再受理：结论已经落定，再多的票都不会改变什么，
// 而客户端可能还停在「未过时」的旧页面上——拒绝并带回现状，它才知道该刷新。
func ReportFieldOutdated(libraryID int, fieldName string, signals dedup.Signals) (FieldReportOutcome, error) {
	return addReport(libraryID, fieldName, types.ReportOutdated, signals, func(entry types.InfoValue) error {
		if entry.Status == types.StatusOutdated {
			return ErrAlreadyOutdated
		}
		return nil
	})
}

// VerifyFieldWebsite 记录一次「该网站可用」的确认。
// 累计到 types.VerifyReportThreshold 次，字段由未验证转为有效。
//
// 只受理 website 角色且当前为未验证的字段。两种拒绝各有理由：
// 已转正的再确认是无意义的写入（且客户端多半停在旧页面上，
// 静默接受会让它一直以为还差票）；已过时的更不该被确认票拉回——
// 那会让 3 票抹掉 5 票的结论。
func VerifyFieldWebsite(libraryID int, fieldName string, signals dedup.Signals) (FieldReportOutcome, error) {
	return addReport(libraryID, fieldName, types.ReportVerify, signals, func(entry types.InfoValue) error {
		if !schema.IsWebsiteField(fieldName) {
			return ErrNotWebsiteField
		}

		switch entry.Status {
		case types.StatusUnverified:
			return nil
		case types.StatusGood:
			return ErrAlreadyVerified
		case types.StatusOutdated:
			return ErrVerifyOutdated
		default:
			return ErrNothingToVerify
		}
	})
}

// addReport 两种报告共用的主流程：取记录、校验前提、查重、计票、推导状态。
//
// guard 是该种报告特有的前置检查，可为 nil。
func addReport(libraryID int, fieldName string, kind types.ReportKind,
	signals dedup.Signals, guard func(types.InfoValue) error) (FieldReportOutcome, error) {

	threshold := kind.Threshold()

	library, err := models.GetLibraryByID(libraryID)
	if err != nil {
		return FieldReportOutcome{}, ErrLibraryNotFound
	}

	entry, ok := library.Info[fieldName]
	if !ok {
		return FieldReportOutcome{}, ErrFieldNotFound
	}

	if guard != nil {
		if err := guard(entry); err != nil {
			// 「这个字段根本没有验证流程」是请求本身不对，不是状态过期：
			// 标成 stale 会让客户端白刷一次，而刷完那个按钮依然不该存在
			if errors.Is(err, ErrNotWebsiteField) {
				return FieldReportOutcome{Threshold: threshold}, err
			}

			// 状态已变：带回当前状态与票数。客户端多半停在过期页面上，
			// 只回一句错误的话它无从纠正显示，用户会反复点同一个按钮。
			// 计数取不到就算了，那只是让进度条少一次刷新，不该盖掉真正的拒绝原因。
			count, countErr := models.CountFieldReport(libraryID, fieldName, kind)
			if countErr != nil {
				count = 0
			}
			return FieldReportOutcome{
				Count:     count,
				Threshold: threshold,
				Status:    entry.Status,
				Stale:     true,
			}, err
		}
	}

	verdict, err := dedup.Check(signals, dedup.Lookup{
		AlreadyCounted: func() (bool, error) {
			return models.HasFieldReport(libraryID, fieldName, kind, signals.ReporterKey)
		},
		SimilarCount: func() (int64, error) {
			return models.CountSuspectedDuplicates(libraryID, fieldName, kind,
				signals.ReporterIP, signals.Fingerprint)
		},
	})
	if err != nil {
		return FieldReportOutcome{}, err
	}

	// 疑似重复：不计数，但把当前次数带回去，前端据此同步进度条
	if verdict == dedup.VerdictSuspectedDuplicate {
		count, err := models.CountFieldReport(libraryID, fieldName, kind)
		if err != nil {
			return FieldReportOutcome{}, err
		}
		return FieldReportOutcome{
			Count:     count,
			Threshold: threshold,
			Status:    entry.Status,
			Duplicate: true,
		}, nil
	}

	// 已计数过的重复提交照常走下去：唯一索引会忽略插入，只把当前次数取回
	count, err := models.AddFieldReport(&types.FieldReport{
		LibraryID:   libraryID,
		FieldName:   fieldName,
		Kind:        kind,
		ReporterKey: signals.ReporterKey,
		ReporterIP:  signals.ReporterIP,
		Fingerprint: signals.Fingerprint,
	})
	if err != nil {
		return FieldReportOutcome{}, err
	}

	status, err := applyStatus(libraryID, fieldName, kind, entry.Status, count)
	if err != nil {
		return FieldReportOutcome{}, err
	}

	return FieldReportOutcome{
		Count:     count,
		Threshold: threshold,
		Status:    status,
		Counted:   true,
		Reached:   count >= threshold && status != entry.Status,
	}, nil
}

// RevokeFieldOutdated 撤销自己的过时报告。
// 只删自己那一行，次数随之 -1；跌破阈值时状态自动恢复
// （未验证的网站回到未验证，而非被抹成有效）。
func RevokeFieldOutdated(libraryID int, fieldName, reporterKey string) (FieldReportOutcome, error) {
	return revokeReport(libraryID, fieldName, types.ReportOutdated, reporterKey, true)
}

// RevokeFieldVerify 撤销自己的网站确认。
//
// 已转正的字段不会因此退回：转正是既成事实，退回会让
// 「先攒票转正、再逐个撤票」成为一种破坏手段。故此处只减票数，不动状态。
func RevokeFieldVerify(libraryID int, fieldName, reporterKey string) (FieldReportOutcome, error) {
	return revokeReport(libraryID, fieldName, types.ReportVerify, reporterKey, false)
}

// revokeReport 两种撤销共用的主流程。
// resync 决定撤销后是否重新推导状态：过时票要，确认票不要（见 RevokeFieldVerify）。
func revokeReport(libraryID int, fieldName string, kind types.ReportKind,
	reporterKey string, resync bool) (FieldReportOutcome, error) {

	threshold := kind.Threshold()

	library, err := models.GetLibraryByID(libraryID)
	if err != nil {
		return FieldReportOutcome{}, ErrLibraryNotFound
	}
	entry, ok := library.Info[fieldName]
	if !ok {
		return FieldReportOutcome{}, ErrFieldNotFound
	}

	count, removed, err := models.RemoveFieldReport(libraryID, fieldName, kind, reporterKey)
	if err != nil {
		return FieldReportOutcome{}, err
	}
	// 没有可撤销的票，说明客户端页面上的「已投过」是过期的
	// （票被地址变更清掉了，或换了浏览器身份），故同样带回现状
	if !removed {
		return FieldReportOutcome{
			Count:     count,
			Threshold: threshold,
			Status:    entry.Status,
			Stale:     true,
		}, ErrNoSuchReport
	}

	status := entry.Status
	if resync {
		status, err = applyStatus(libraryID, fieldName, kind, entry.Status, count)
		if err != nil {
			return FieldReportOutcome{}, err
		}
	}

	return FieldReportOutcome{
		Count:     count,
		Threshold: threshold,
		Status:    status,
	}, nil
}

// applyStatus 按票数推导字段状态并写回，返回写回后的状态。
//
// 两种票各自决定不同的迁移方向，故按 kind 分派：
// 过时票在 good/out-dated（以及 unverified）之间来回，确认票只做 unverified → good。
func applyStatus(libraryID int, fieldName string, kind types.ReportKind,
	current types.LibraryStatus, count int) (types.LibraryStatus, error) {

	next := current

	switch kind {
	case types.ReportOutdated:
		switch {
		case count >= types.OutdatedReportThreshold:
			next = types.StatusOutdated
		case current == types.StatusUnverified:
			// 未验证的网站撤销过时报告后回到未验证，不能被抹成 good——
			// 那等于绕过验证流程白拿一个「有效」
			next = types.StatusUnverified
		default:
			next = types.StatusGood
		}

	case types.ReportVerify:
		// 只从未验证转正。已 good 无需再转，已过时不该被确认票拉回
		if current == types.StatusUnverified && count >= types.VerifyReportThreshold {
			next = types.StatusGood
		}
	}

	if next == current {
		return current, nil
	}

	if err := models.SetFieldStatus(libraryID, fieldName, next); err != nil {
		return "", err
	}

	return next, nil
}
