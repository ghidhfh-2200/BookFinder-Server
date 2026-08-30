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
	// ErrNoSuchReport 该访问者没有提交过对应的报告，无从撤销
	ErrNoSuchReport = errors.New("没有提交过该报告")
	// ErrStatusLocked 当前状态下不受理该动作，原因由状态机给出。
	//
	// 具体缘由（已转正、已过时……）随错误信息返回，不为每种情形单列一个
	// 错误值：那样每加一个状态就要同时改这里和状态机表，而判定本就只有一处。
	ErrStatusLocked = errors.New("当前状态下无法执行该操作")
)

// StatusLockedError 状态机拒绝了这个动作。
// 包装 ErrStatusLocked，故调用方可用 errors.Is 统一识别，也能读到具体原因。
type StatusLockedError struct {
	Action types.FieldAction
	Status types.LibraryStatus
	Reason string
}

func (e *StatusLockedError) Error() string { return e.Reason }

func (e *StatusLockedError) Unwrap() error { return ErrStatusLocked }

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

// ApplyFieldAction 执行一次字段表态：报告过时、撤销过时、确认可用、撤销确认。
//
// 四个动作走同一条路，差异全部来自状态机表（types.FieldTransition）：
// 哪个状态受理哪个动作、达到阈值迁往哪里、跌破阈值是否回落。
// 这样加一个状态或动作只需改那张表，不必逐个接口补前置检查——
// 「已转正后撤销确认仍返回成功」那个 bug 正是漏补了一处检查造成的。
//
// signals 用于按人去重，撤销动作只需其中的 ReporterKey。
func ApplyFieldAction(libraryID int, fieldName string,
	action types.FieldAction, signals dedup.Signals) (FieldReportOutcome, error) {

	kind := action.Kind()
	threshold := kind.Threshold()

	library, err := models.GetLibraryByID(libraryID)
	if err != nil {
		return FieldReportOutcome{}, ErrLibraryNotFound
	}

	entry, ok := library.Info[fieldName]
	if !ok {
		return FieldReportOutcome{}, ErrFieldNotFound
	}

	// 角色不符是请求本身不对，不是状态过期：不带 Stale，
	// 客户端刷新也不会让那个按钮变得合理
	if action.WebsiteOnly() && !schema.IsWebsiteField(fieldName) {
		return FieldReportOutcome{Threshold: threshold}, ErrNotWebsiteField
	}

	rule := types.FieldTransition(action, entry.Status)
	if !rule.Allowed {
		// 状态已变而被拒：带回现状，客户端据此就地纠正显示。
		// 只回一句错误的话它无从知道真实状态，用户会反复点同一个按钮。
		count, countErr := models.CountFieldReport(libraryID, fieldName, kind)
		if countErr != nil {
			count = 0
		}
		return FieldReportOutcome{
			Count:     count,
			Threshold: threshold,
			Status:    entry.Status,
			Stale:     true,
		}, &StatusLockedError{Action: action, Status: entry.Status, Reason: rule.Reason}
	}

	if action.IsRevoke() {
		return revoke(libraryID, fieldName, kind, rule, entry.Status, threshold, signals.ReporterKey)
	}

	return submit(libraryID, fieldName, kind, rule, entry.Status, threshold, signals)
}

// submit 投一票：查重、计票、按状态机推导新状态。
func submit(libraryID int, fieldName string, kind types.ReportKind, rule types.FieldRule,
	current types.LibraryStatus, threshold int, signals dedup.Signals) (FieldReportOutcome, error) {

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
			Status:    current,
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

	status, err := persistStatus(libraryID, fieldName, rule, current, count, threshold)
	if err != nil {
		return FieldReportOutcome{}, err
	}

	return FieldReportOutcome{
		Count:     count,
		Threshold: threshold,
		Status:    status,
		Counted:   true,
		Reached:   status != current,
	}, nil
}

// revoke 撤回自己那一票，并按状态机推导新状态。
// 只删自己那一行，别人的票不受影响。
func revoke(libraryID int, fieldName string, kind types.ReportKind, rule types.FieldRule,
	current types.LibraryStatus, threshold int, reporterKey string) (FieldReportOutcome, error) {

	count, removed, err := models.RemoveFieldReport(libraryID, fieldName, kind, reporterKey)
	if err != nil {
		return FieldReportOutcome{}, err
	}

	// 没有可撤销的票，说明客户端页面上的「已投过」是过期的
	// （票被地址变更清掉了，或换了身份），故同样带回现状
	if !removed {
		return FieldReportOutcome{
			Count:     count,
			Threshold: threshold,
			Status:    current,
			Stale:     true,
		}, ErrNoSuchReport
	}

	status, err := persistStatus(libraryID, fieldName, rule, current, count, threshold)
	if err != nil {
		return FieldReportOutcome{}, err
	}

	return FieldReportOutcome{
		Count:     count,
		Threshold: threshold,
		Status:    status,
	}, nil
}

// persistStatus 按状态机推导目标状态，与当前不同才写库。
// 相同就不写：那是一次无谓的 UPDATE，也会让「状态变过」的判断失真。
func persistStatus(libraryID int, fieldName string, rule types.FieldRule,
	current types.LibraryStatus, count, threshold int) (types.LibraryStatus, error) {

	next := rule.NextStatus(current, count, threshold)
	if next == current {
		return current, nil
	}

	if err := models.SetFieldStatus(libraryID, fieldName, next); err != nil {
		return "", err
	}

	return next, nil
}
