package logger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// GormLogger GORM 日志适配器，将图书业务库（MySQL）的 GORM 日志写入日志系统。
// 日志库自身使用 gormlogger.Discard，不经过此适配器，避免递归写入。
type GormLogger struct {
	LogLevel gormlogger.LogLevel
}

// NewGormLogger 创建 GORM 日志适配器
func NewGormLogger() *GormLogger {
	level := gormlogger.Error
	if IsDebug() {
		level = gormlogger.Info
	}
	return &GormLogger{LogLevel: level}
}

// LogMode 设置日志级别
func (l *GormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	newLogger := *l
	newLogger.LogLevel = level
	return &newLogger
}

// Info 记录 Info 级别日志
func (l *GormLogger) Info(_ context.Context, msg string, data ...any) {
	if l.LogLevel >= gormlogger.Info {
		Infof("[GORM] "+msg, data...)
	}
}

// Warn 记录 Warn 级别日志
func (l *GormLogger) Warn(_ context.Context, msg string, data ...any) {
	if l.LogLevel >= gormlogger.Warn {
		Warnf("[GORM] "+msg, data...)
	}
}

// Error 记录 Error 级别日志
func (l *GormLogger) Error(_ context.Context, msg string, data ...any) {
	if l.LogLevel >= gormlogger.Error {
		Errorf("[GORM] "+msg, data...)
	}
}

// Trace 记录 SQL 执行日志
func (l *GormLogger) Trace(
	_ context.Context,
	begin time.Time,
	fc func() (sql string, rowsAffected int64),
	err error,
) {
	if l.LogLevel <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	switch {
	case err != nil && l.LogLevel >= gormlogger.Error && !errors.Is(err, gorm.ErrRecordNotFound):
		// 记录错误（排除 RecordNotFound，这属于正常业务分支）
		Errorf("[GORM] SQL Error: %v | elapsed=%v rows=%d | %s", err, elapsed, rows, sql)
	case elapsed > 200*time.Millisecond && l.LogLevel >= gormlogger.Warn:
		// 记录慢查询（超过 200ms）
		Warnf("[GORM] Slow SQL | elapsed=%v rows=%d | %s", elapsed, rows, sql)
	case l.LogLevel >= gormlogger.Info && IsDebug():
		// 成功的 SQL 只打控制台，不落库：每个请求若干条 SQL，
		// 逐条入库会让日志表随流量线性膨胀，而排障时看控制台即可。
		printStdout(LevelDebug, fmt.Sprintf("[GORM] SQL | elapsed=%v rows=%d | %s",
			elapsed, rows, sql))
	}
}
