package main

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisAddr     = "127.0.0.1:6379"
	defaultRedisPassword = ""
	defaultRedisDB       = 3
	defaultRouteTTL      = 60 * time.Second
)

type RedisRouteManager struct {
	client         *redis.Client
	gatewayAddress string
}

func NewRedisRouteManager(gatewayAddress string) (*RedisRouteManager, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     defaultRedisAddr,
		Password: defaultRedisPassword,
		DB:       defaultRedisDB,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &RedisRouteManager{
		client:         client,
		gatewayAddress: gatewayAddress,
	}, nil
}

// redis 的 kv 设计：
// ws:route:{userId}:{deviceId} -> gatewayAddress (过期时间 60s)
// ws:online:{userId} -> ZSet (过期时间 60s，成员是 deviceId|platform，分数是过期时间戳)
// ws:global:online -> ZSet (过期时间 60s，成员是 userId，分数是过期时间戳)

func (m *RedisRouteManager) routeKey(userId, deviceId string) string {
	return fmt.Sprintf("ws:route:%s:%s", userId, deviceId)
}

func (m *RedisRouteManager) onlineKey(userId string) string {
	return fmt.Sprintf("ws:online:%s", userId)
}

func (m *RedisRouteManager) onlineValue(deviceId, platform string) string {
	return fmt.Sprintf("%s|%s", deviceId, platform)
}

func (m *RedisRouteManager) globalOnlineKey() string {
	return "ws:global:online"
}

// Register 写入设备路由
func (m *RedisRouteManager) Register(ctx context.Context, userId, deviceId, platform string) error {
	pipeline := m.client.Pipeline()
	pipeline.Set(ctx, m.routeKey(userId, deviceId), m.gatewayAddress, defaultRouteTTL)
	pipeline.ZAdd(ctx, m.onlineKey(userId), redis.Z{
		Score:  float64(time.Now().Unix() + defaultRouteTTL.Microseconds()),
		Member: m.onlineValue(deviceId, platform),
	})
	pipeline.ZAdd(ctx, m.globalOnlineKey(), redis.Z{
		Score:  float64(time.Now().Unix() + defaultRouteTTL.Microseconds()),
		Member: userId,
	})
	_, err := pipeline.Exec(ctx)
	return err
}

// KeepAlive 刷新设备路由的续期
func (m *RedisRouteManager) KeepAlive(ctx context.Context, userId, deviceId, platform string) error {
	pipeline := m.client.Pipeline()
	pipeline.Expire(ctx, m.routeKey(userId, deviceId), defaultRouteTTL)
	pipeline.ZAdd(ctx, m.onlineKey(userId), redis.Z{
		Score:  float64(time.Now().Unix() + defaultRouteTTL.Microseconds()),
		Member: m.onlineValue(deviceId, platform),
	})
	pipeline.ZAdd(ctx, m.globalOnlineKey(), redis.Z{
		Score:  float64(time.Now().Unix() + defaultRouteTTL.Microseconds()),
		Member: userId,
	})
	_, err := pipeline.Exec(ctx)
	return err
}

// Unregister 删除指定设备的路由
func (m *RedisRouteManager) Unregister(ctx context.Context, userId, deviceId, platform string) error {
	pipeline := m.client.Pipeline()
	pipeline.Del(ctx, m.routeKey(userId, deviceId))
	pipeline.ZRem(ctx, m.onlineKey(userId), m.onlineValue(deviceId, platform))
	// TODO 网关不删全局 user 统计，需 springboot 在查人数时判断过期时间，并起一个定时任务清理过期的 user
	_, err := pipeline.Exec(ctx)
	return err
}
