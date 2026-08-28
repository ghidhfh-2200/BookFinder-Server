package database

import (
	"context"
	"time"

	"bookfinder-backend/config"
	"bookfinder-backend/logger"

	"github.com/redis/go-redis/v9"
)

// rdb 限流计数所在的 Redis 连接。
// 计数以天为单位刷新，丢失只影响当日配额，故连不上时不阻断启动。
var rdb *redis.Client

// InitializeRedis 连接 Redis。
// Ping 失败只记警告不返回错误：限流是 fail-open 的，
// Redis 不可用时应当放行而非让整站不可写。
func InitializeRedis() {
	cfg := config.Get().Redis

	rdb = redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
		// 限流查询在请求路径上，超时必须短，否则拖慢每个请求
		DialTimeout:  2 * time.Second,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Warnf("Redis 连接失败，限流将放行所有请求 (%s): %v", cfg.Addr, err)
		return
	}

	logger.Infof("Redis 连接成功: %s", cfg.Addr)
}

// GetRedis 获取 Redis 连接，未初始化时返回 nil
func GetRedis() *redis.Client {
	return rdb
}

// CloseRedis 关闭 Redis 连接
func CloseRedis() error {
	if rdb == nil {
		return nil
	}
	client := rdb
	rdb = nil
	return client.Close()
}
