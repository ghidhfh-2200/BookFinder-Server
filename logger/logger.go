package logger

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"bookfinder-backend/types"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// 日志级别，取值定义在 types 包，此处别名便于调用方书写
const (
	LevelDebug = types.LevelDebug
	LevelInfo  = types.LevelInfo
	LevelWarn  = types.LevelWarn
	LevelError = types.LevelError
)

// levelOrder 日志级别顺序，用于过滤低于配置级别的日志
var levelOrder = map[string]int{
	LevelDebug: 0,
	LevelInfo:  1,
	LevelWarn:  2,
	LevelError: 3,
}

// queueSize 异步写入队列容量。
// 日志改存 MySQL 后写入要走网络，逐条同步写会拖慢请求，故先入队再由后台协程落库。
// 队列满时丢弃并在 stderr 留痕，宁可丢日志也不阻塞业务。
const queueSize = 1024

// Config 日志配置
type Config struct {
	// DSN 日志库连接串。日志与业务数据同在 bookfinder 库，但用独立连接。
	DSN string
	// AlsoToStdout 是否同时输出到控制台（开发模式）
	AlsoToStdout bool
	// Level 日志级别: debug / info / warn / error
	Level string
}

var (
	db           *gorm.DB
	minLevel     int
	alsoToStdout bool
	debugMode    bool
	mu           sync.RWMutex

	// queue 待落库的日志，由 worker 消费
	queue chan any
	// done worker 退出信号，Close 时等待队列排空
	done chan struct{}
)

// Initialize 初始化日志系统。
// 用独立的 MySQL 连接并把 GORM 自身的日志设为 Discard：
// 写日志会产生 SQL，若该连接又记录 SQL 日志就会无限递归。
func Initialize(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("logger config is required")
	}

	conn, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{
		// 关键：写日志不记录日志
		Logger: gormlogger.Discard,
	})
	if err != nil {
		return fmt.Errorf("failed to open log database: %w", err)
	}

	sqlDB, err := conn.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}
	// 单一后台协程消费队列，少量连接足够
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(2)

	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("failed to ping log database: %w", err)
	}

	if err := conn.AutoMigrate(&types.LogEntry{}, &types.OperationLog{}); err != nil {
		return fmt.Errorf("failed to migrate log tables: %w", err)
	}

	level := normalizeLevel(cfg.Level)

	mu.Lock()
	db = conn
	minLevel = levelOrder[level]
	alsoToStdout = cfg.AlsoToStdout
	debugMode = level == LevelDebug
	queue = make(chan any, queueSize)
	done = make(chan struct{})
	mu.Unlock()

	go worker(conn, queue, done)

	return nil
}

// worker 顺序消费队列并落库。
// 单协程写入，避免大量并发插入争抢连接；写失败只在 stderr 留痕，不影响业务。
func worker(conn *gorm.DB, in <-chan any, finished chan<- struct{}) {
	defer close(finished)

	for record := range in {
		if err := conn.Create(record).Error; err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write log record: %v\n", err)
		}
	}
}

// enqueue 把记录投入队列，队列满或系统未就绪时丢弃并在 stderr 留痕
func enqueue(record any, fallback string) {
	mu.RLock()
	ch := queue
	mu.RUnlock()

	if ch == nil {
		fmt.Fprintln(os.Stderr, fallback)
		return
	}

	select {
	case ch <- record:
	default:
		fmt.Fprintf(os.Stderr, "Log queue full, dropped: %s\n", fallback)
	}
}

// write 按级别过滤后投递应用日志
func write(level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)

	mu.RLock()
	ready, allowed, toStdout := db != nil, levelOrder[level] >= minLevel, alsoToStdout
	mu.RUnlock()

	if !ready {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", level, msg)
		return
	}
	if !allowed {
		return
	}

	now := time.Now()
	if toStdout {
		printStdout(level, msg)
	}

	enqueue(
		&types.LogEntry{Timestamp: now, Level: level, Message: msg},
		fmt.Sprintf("[%s] %s", level, msg),
	)
}

// printStdout 把日志打到控制台，不落库。
// 供高频日志（如正常请求的访问记录）在调试时可见，同时避免撑爆日志表。
func printStdout(level, msg string) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), level, msg)
}

// Operation 记录一条用户操作日志。
// 与应用日志分表，不受日志级别过滤：审计记录必须完整，等级只用于查询与呈现。
func Operation(entry *types.OperationLog) {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if entry.Level == "" {
		entry.Level = LevelInfo
	}

	mu.RLock()
	toStdout := alsoToStdout
	mu.RUnlock()

	if toStdout {
		fmt.Printf("%s [%s] %s %s: %s\n",
			entry.Timestamp.Format("2006-01-02 15:04:05"),
			entry.Level, entry.User, entry.Action, entry.Detail)
	}

	enqueue(entry, fmt.Sprintf("[%s] %s %s: %s", entry.Level, entry.User, entry.Action, entry.Detail))
}

// normalizeLevel 解析日志级别，无法识别时回落到 INFO
func normalizeLevel(level string) string {
	switch strings.ToLower(level) {
	case "debug":
		return LevelDebug
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// IsDebug 是否为调试模式
func IsDebug() bool {
	mu.RLock()
	defer mu.RUnlock()
	return debugMode
}

// GetDB 日志库连接，供查询日志用
func GetDB() *gorm.DB {
	mu.RLock()
	defer mu.RUnlock()
	return db
}

// Close 排空队列后关闭日志库连接
func Close() error {
	mu.Lock()
	conn, ch, finished := db, queue, done
	db, queue, done = nil, nil, nil
	mu.Unlock()

	if conn == nil {
		return nil
	}

	// 关闭队列并等 worker 把剩余日志写完，避免退出时丢日志
	if ch != nil {
		close(ch)
	}
	if finished != nil {
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			fmt.Fprintln(os.Stderr, "Timed out waiting for log queue to drain")
		}
	}

	sqlDB, err := conn.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
