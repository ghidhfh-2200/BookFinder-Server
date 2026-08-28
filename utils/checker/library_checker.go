package checker

import (
	"errors"
	"fmt"
	"strings"

	"bookfinder-backend/types"
	"bookfinder-backend/utils/schema"
)

// ValidateLibraryCreate 校验图书馆创建请求
func ValidateLibraryCreate(library *types.Library) error {
	return validateLibraryInfo(library.Info)
}

// ValidateLibraryUpdate 校验图书馆更新请求
func ValidateLibraryUpdate(library *types.Library) error {
	return validateLibraryInfo(library.Info)
}

// validateLibraryInfo 按注册表校验 Info。
// 调用前 Info 应已由 schema.Normalize 对齐，故此处只需检查必填字段的值非空、
// 字符串值去除首尾空白，以及每个字段自带的状态取值合法。
func validateLibraryInfo(info types.LibraryInfo) error {
	if info == nil {
		return errors.New("info 不能为空")
	}

	for _, field := range schema.Fields() {
		entry, ok := info[field.Name]
		if !ok {
			return fmt.Errorf("info 缺少 %s 字段", field.Name)
		}

		// 字符串值统一去除首尾空白，空白串等同于未填
		if s, isString := entry.Value.(string); isString {
			entry.Value = strings.TrimSpace(s)
			info[field.Name] = entry
		}

		if field.Required && field.Type.IsEmptyValue(entry.Value) {
			return fmt.Errorf("%s 不能为空", field.DisplayName())
		}

		// 状态属于字段自身，逐个校验
		if err := ValidateLibraryStatus(entry.Status); err != nil {
			return fmt.Errorf("%s 的%w", field.DisplayName(), err)
		}
	}

	return nil
}

// ValidateLibraryStatus 校验状态取值，仅允许 good 与 out-dated
func ValidateLibraryStatus(status types.LibraryStatus) error {
	if !status.IsValid() {
		return errors.New("状态只能为 good 或 out-dated")
	}
	return nil
}
