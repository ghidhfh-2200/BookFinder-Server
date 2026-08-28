package database

import (
	"database/sql"
	"errors"
	"fmt"

	"bookfinder-backend/config"
	"bookfinder-backend/logger"
	"bookfinder-backend/types"

	_ "github.com/go-sql-driver/mysql" // database/sql 建库连接所需的 MySQL 驱动
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// db 图书馆业务库连接（服务器本地 MySQL）。
// 用户与封禁 IP 在本地 SQLite 应用库（见 app.go），应用日志在独立的日志库（见 logger 包）。
var db *gorm.DB

// Initialize 初始化本地 MySQL 连接并迁移图书馆相关数据表。
// 目标数据库不存在时自动创建，因此首次部署无需手工建库。
func Initialize() error {
	cfg := config.Get()

	if err := ensureDatabase(cfg.Database); err != nil {
		return err
	}

	conn, err := gorm.Open(mysql.Open(cfg.Database.DSN()), &gorm.Config{
		Logger: logger.NewGormLogger(),
	})
	if err != nil {
		return fmt.Errorf("failed to connect to MySQL: %w", err)
	}
	db = conn

	// 设置连接池参数
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(200)
	sqlDB.SetMaxIdleConns(20)

	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("failed to ping MySQL: %w", err)
	}

	if err := autoMigrate(); err != nil {
		return fmt.Errorf("failed to migrate tables: %w", err)
	}

	return nil
}

// ensureDatabase 检测目标数据库是否存在，不存在则创建。
// 先连到服务器（不指定库名），查 information_schema 确认后再建。
func ensureDatabase(cfg config.DatabaseConfig) error {
	// 库名来自配置，仍需校验：它会拼进 DDL，无法用占位符参数化
	if err := validateDatabaseName(cfg.Database); err != nil {
		return err
	}

	serverDB, err := sql.Open("mysql", cfg.ServerDSN())
	if err != nil {
		return fmt.Errorf("failed to connect to MySQL server: %w", err)
	}
	defer func() {
		if closeErr := serverDB.Close(); closeErr != nil {
			logger.Errorf("关闭 MySQL 建库连接失败: %v", closeErr)
		}
	}()

	if err := serverDB.Ping(); err != nil {
		return fmt.Errorf("failed to ping MySQL server: %w", err)
	}

	var exists int
	err = serverDB.QueryRow(
		"SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?",
		cfg.Database,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check database existence: %w", err)
	}

	if exists > 0 {
		return nil
	}

	// 库名已校验为标识符安全字符，此处用反引号包裹
	stmt := fmt.Sprintf(
		"CREATE DATABASE `%s` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
		cfg.Database,
	)
	if _, err := serverDB.Exec(stmt); err != nil {
		return fmt.Errorf("failed to create database %q: %w", cfg.Database, err)
	}

	logger.Infof("数据库 %s 不存在，已自动创建", cfg.Database)

	return nil
}

// validateDatabaseName 校验库名只含标识符安全字符。
// 库名无法作为查询参数传入 CREATE DATABASE，只能拼接，故必须先校验以防注入。
func validateDatabaseName(name string) error {
	if name == "" {
		return errors.New("数据库名不能为空")
	}
	if len(name) > 64 {
		return errors.New("数据库名长度不能超过 64 个字符")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
		default:
			return fmt.Errorf("数据库名 %q 含非法字符，只允许字母、数字、下划线与连字符", name)
		}
	}
	return nil
}

// autoMigrate 自动迁移所有数据表
func autoMigrate() error {
	if err := db.AutoMigrate(
		// 图书馆
		&types.Library{},

		// 字段过时报告
		&types.FieldReport{},
	); err != nil {
		return err
	}

	return ensureSearchNameIndex()
}

// searchNameIndex 记录名的全文索引名
const searchNameIndex = "ft_libraries_search_name"

// ensureSearchNameIndex 为记录名建 ngram 全文索引。
//
// 不能交给 AutoMigrate：GORM 的索引标签生成不出 `WITH PARSER ngram`，
// 而没有这个解析器，中文会被当成一个整词——搜「大学」匹配不到「北京大学图书馆」。
//
// 用 ngram 而非普通 B-tree 索引，是因为搜索要支持任意位置匹配。
// 前导通配符的 LIKE '%kw%' 用不上任何 B-tree 索引（实测 EXPLAIN 为全表扫），
// 而全文索引可以（实测 type=fulltext）。
//
// 幂等：索引已存在则跳过，故每次启动都可安全调用。
func ensureSearchNameIndex() error {
	var count int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = 'libraries' AND index_name = ?
	`, searchNameIndex).Scan(&count).Error; err != nil {
		return fmt.Errorf("failed to check search name index: %w", err)
	}

	if count > 0 {
		return nil
	}

	if err := db.Exec(fmt.Sprintf(
		"ALTER TABLE libraries ADD FULLTEXT INDEX %s (search_name) WITH PARSER ngram",
		searchNameIndex,
	)).Error; err != nil {
		return fmt.Errorf("failed to create search name index: %w", err)
	}

	logger.Infof("已创建记录名全文索引 %s（ngram）", searchNameIndex)

	return nil
}

// GetDB 获取数据库连接
func GetDB() *gorm.DB {
	return db
}

// Close 关闭数据库连接
func Close() error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
