package database

import (
	"os"
	"path/filepath"
	"testing"

	"bookfinder-backend/types"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// UseAppDBForTest 把应用库指向一个临时 SQLite 文件，供测试使用，
// 并在测试结束时还原原先的连接。
//
// 连接数与 PRAGMA 与 InitializeApp 保持一致（单连接、WAL、busy_timeout）：
// 并发行为正是由这些设置决定的，若测试用一套不同的配置，测出来的结论
// 对线上没有意义。
//
// 参数类型取 *testing.T 而非 interface{}，使这个函数只可能被测试调用。
func UseAppDBForTest(t *testing.T) *gorm.DB {
	t.Helper()

	// 不用 t.TempDir()：Windows 下 SQLite 仍持有文件句柄时，它的自动清理会
	// 报「文件被占用」并把测试判为失败——那与被测逻辑无关。
	dir, err := os.MkdirTemp("", "bf-app-test-")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}

	conn, err := gorm.Open(sqlite.Open(filepath.Join(dir, "app_test.db")), &gorm.Config{
		// 测试里不需要 SQL 日志，「record not found」是查重的正常分支，
		// 打出来只会盖住真正的断言输出
		Logger: gormlogger.Discard,
	})
	if err != nil {
		t.Fatalf("打开测试应用库失败: %v", err)
	}

	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatalf("取 sql.DB 失败: %v", err)
	}
	// 与生产一致：SQLite 同一时刻只允许一个写入者
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	conn.Exec("PRAGMA journal_mode=WAL;")
	conn.Exec("PRAGMA busy_timeout=10000;")
	conn.Exec("PRAGMA synchronous=NORMAL;")

	if err := conn.AutoMigrate(
		&types.User{},
		&types.BanSubject{},
		&types.BanIdent{},
		&types.BanAppeal{},
	); err != nil {
		t.Fatalf("迁移测试应用库失败: %v", err)
	}

	previous := appDB
	appDB = conn
	t.Cleanup(func() {
		appDB = previous
		if sqlDB, err := conn.DB(); err == nil {
			sqlDB.Close()
		}
		// 清理失败不影响测试结论（临时目录由系统回收），故忽略错误
		_ = os.RemoveAll(dir)
	})

	return conn
}
