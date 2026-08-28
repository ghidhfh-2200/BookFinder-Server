package logger

import (
	"fmt"
	"os"
	"runtime/debug"
)

// Debugf 记录 Debug 级别日志
func Debugf(format string, args ...any) {
	write(LevelDebug, format, args...)
}

// Infof 记录 Info 级别日志
func Infof(format string, args ...any) {
	write(LevelInfo, format, args...)
}

// Warnf 记录 Warn 级别日志
func Warnf(format string, args ...any) {
	write(LevelWarn, format, args...)
}

// Errorf 记录 Error 级别日志
func Errorf(format string, args ...any) {
	write(LevelError, format, args...)
}

// Fatalf 记录 Error 级别日志后退出进程
func Fatalf(format string, args ...any) {
	write(LevelError, format, args...)
	if err := Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", err)
	}
	os.Exit(1)
}

// RecoverPanic 在 goroutine 中通过 defer 调用，捕获 panic 并记录堆栈
func RecoverPanic(name string) {
	if r := recover(); r != nil {
		Errorf("[%s] panic: %v\n%s", name, r, debug.Stack())
	}
}
