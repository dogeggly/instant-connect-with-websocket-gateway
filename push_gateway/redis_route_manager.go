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

// 反引号支持换行
var registerScript = redis.NewScript(`
redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[4])
redis.call('ZADD', KEYS[3], ARGV[3], ARGV[5])
return 1
`)

var keepAliveScript = redis.NewScript(`
redis.call('EXPIRE', KEYS[1], ARGV[1])
redis.call('ZADD', KEYS[2], ARGV[2], ARGV[3])
redis.call('ZADD', KEYS[3], ARGV[2], ARGV[4])
return 1
`)

var unregisterScript = redis.NewScript(`
redis.call('DEL', KEYS[1])
redis.call('ZREM', KEYS[2], ARGV[1])
return 1
`)

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
	score := float64(time.Now().Unix() + defaultRouteTTL.Microseconds())
	return registerScript.Run(
		ctx,
		m.client,
		[]string{m.routeKey(userId, deviceId), m.onlineKey(userId), m.globalOnlineKey()},
		m.gatewayAddress,
		int64(defaultRouteTTL/time.Second),
		score,
		m.onlineValue(deviceId, platform),
		userId,
	).Err()
}

// KeepAlive 刷新设备路由的续期
func (m *RedisRouteManager) KeepAlive(ctx context.Context, userId, deviceId, platform string) error {
	score := float64(time.Now().Unix() + defaultRouteTTL.Microseconds())
	return keepAliveScript.Run(
		ctx,
		m.client,
		[]string{m.routeKey(userId, deviceId), m.onlineKey(userId), m.globalOnlineKey()},
		int64(defaultRouteTTL/time.Second),
		score,
		m.onlineValue(deviceId, platform),
		userId,
	).Err()
}

// Unregister 删除指定设备的路由
func (m *RedisRouteManager) Unregister(ctx context.Context, userId, deviceId, platform string) error {
	// TODO 网关不删全局 user 统计，需 springboot 在查人数时判断过期时间，并起一个定时任务清理过期的 user
	return unregisterScript.Run(
		ctx,
		m.client,
		[]string{m.routeKey(userId, deviceId), m.onlineKey(userId)},
		m.onlineValue(deviceId, platform),
	).Err()
}
