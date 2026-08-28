package database

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"bookfinder-backend/config"
	"bookfinder-backend/logger"
	"bookfinder-backend/types"
	"bookfinder-backend/utils"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// appDB 应用库连接（本地 SQLite），存放管理员用户与封禁 IP。
// 图书业务数据在远程 MySQL（见 db.go），应用日志在独立的日志库（见 logger 包）。
var appDB *gorm.DB

// InitializeApp 初始化本地 SQLite 应用库，迁移数据表，并在首次运行时创建唯一管理员
func InitializeApp() error {
	cfg := config.Get()
	path := cfg.AppDatabase.Path

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create app database directory: %w", err)
		}
	}

	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.NewGormLogger(),
	})
	if err != nil {
		return fmt.Errorf("failed to open app database: %w", err)
	}
	appDB = conn

	sqlDB, err := conn.DB()
	if err != nil {
		return fmt.Errorf("failed to get sql.DB: %w", err)
	}
	// SQLite 同一时刻只允许一个写入者，限制连接数避免锁竞争
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	conn.Exec("PRAGMA journal_mode=WAL;")
	conn.Exec("PRAGMA busy_timeout=10000;")
	conn.Exec("PRAGMA synchronous=NORMAL;")

	if err := appAutoMigrate(); err != nil {
		return fmt.Errorf("failed to migrate app tables: %w", err)
	}

	if err := ensureAdmin(); err != nil {
		return fmt.Errorf("failed to ensure admin account: %w", err)
	}

	return nil
}

// appAutoMigrate 迁移本地应用库的所有数据表
func appAutoMigrate() error {
	return appDB.AutoMigrate(
		// 用户（仅管理员入库）
		&types.User{},

		// 封禁主体与其标识：封禁挂在主体上，一个主体可有多个标识，
		// 任一标识命中即视为该主体，故封禁一次可同时挡住浏览器端与安卓端
		&types.BanSubject{},
		&types.BanIdent{},

		// 封禁申诉
		&types.BanAppeal{},
	)
}

// ensureAdmin 确保唯一管理员存在。
// 已存在则不做任何改动；首次运行时生成随机密码并打印一次。
// User.Role 上有唯一索引，数据库层面保证管理员只有一个。
func ensureAdmin() error {
	var count int64
	if err := appDB.Model(&types.User{}).Where("role = ?", types.RoleAdmin).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to count admin: %w", err)
	}
	if count > 0 {
		logger.Infof("管理员账户已存在，跳过初始化")
		return nil
	}

	password, err := utils.GenerateRandomPassword(16)
	if err != nil {
		return fmt.Errorf("failed to generate admin password: %w", err)
	}

	hashed, err := utils.HashPassword(password)
	if err != nil {
		return fmt.Errorf("failed to hash admin password: %w", err)
	}

	admin := types.User{
		Username: types.AdminUsername,
		Password: hashed,
		Role:     types.RoleAdmin,
	}
	if err := appDB.Create(&admin).Error; err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}

	// 直接打印到控制台，确保这次性密码不会被日志级别过滤掉
	fmt.Println("========================================================")
	fmt.Println("  首次启动，已创建唯一管理员账户:")
	fmt.Printf("  用户名: %s\n", types.AdminUsername)
	fmt.Printf("  密码: %s\n", password)
	fmt.Println("  该密码仅显示一次，请立即保存并尽快修改。")
	fmt.Println("========================================================")

	logger.Infof("已创建唯一管理员账户: %s", types.AdminUsername)

	return nil
}

// GetAppDB 获取应用库连接
func GetAppDB() *gorm.DB {
	return appDB
}

// CloseApp 关闭应用库连接
func CloseApp() error {
	if appDB == nil {
		return nil
	}
	sqlDB, err := appDB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// IsNotFound 判断是否为记录不存在错误
func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
