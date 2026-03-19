package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisAddr     = "127.0.0.1:6379"
	defaultRedisPassword = ""
	defaultRedisDB       = 3
	defaultRouteTTL      = 60 * time.Second
	defaultBitmapTTL     = 3 * time.Minute
	bitmapBaseUserID     = 10001
	registerTimeout      = 2 * time.Second
	keepAliveTimeout     = 2 * time.Second
	unregisterTimeout    = 5 * time.Second
)

type RedisRouteManager struct {
	client         *redis.Client
	gatewayAddress string
}

var ErrRouteOwnershipLost = errors.New("route ownership lost")

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
// ws:route:{userId}:{deviceId} -> gatewayAddress|connId (过期时间 60s)
// ws:online:{userId} -> ZSet (过期时间 60s，成员是 deviceId|platform，分数是过期时间戳)
// ws:global:online:{localTime} -> BitMap

func (m *RedisRouteManager) routeKey(userId, deviceId string) string {
	return fmt.Sprintf("ws:route:%s:%s", userId, deviceId)
}

func (m *RedisRouteManager) routeValue(connID string) string {
	return fmt.Sprintf("%s|%s", m.gatewayAddress, connID)
}

func (m *RedisRouteManager) onlineKey(userId string) string {
	return fmt.Sprintf("ws:online:%s", userId)
}

func (m *RedisRouteManager) onlineValue(deviceId, platform string) string {
	return fmt.Sprintf("%s|%s", deviceId, platform)
}

func (m *RedisRouteManager) globalOnlineKey(now time.Time) string {
	return fmt.Sprintf("ws:global:online:%s", now.Format("200601021504"))
}

func (m *RedisRouteManager) globalOnlineOffset(userId string) (int64, error) {
	uid, err := strconv.ParseInt(userId, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("userId 不是有效数字: %w", err)
	}
	if uid < bitmapBaseUserID {
		return 0, fmt.Errorf("userId 必须大于等于 %d", bitmapBaseUserID)
	}
	return uid - bitmapBaseUserID, nil
}

func withTimeoutContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// Register 写入设备路由
// 反引号支持换行
var registerScript = redis.NewScript(`
redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[4])
redis.call('SETBIT', KEYS[3], ARGV[5], 1)
if redis.call('TTL', KEYS[3]) < 0 then
	redis.call('EXPIRE', KEYS[3], ARGV[6])
end
return 1
`)

func (m *RedisRouteManager) Register(userId, deviceId, platform, connID string) error {
	ctx, cancel := withTimeoutContext(registerTimeout)
	defer cancel()

	offset, err := m.globalOnlineOffset(userId)
	if err != nil {
		return err
	}
	now := time.Now()
	score := float64(now.Add(defaultRouteTTL).Unix())
	return registerScript.Run(
		ctx,
		m.client,
		[]string{m.routeKey(userId, deviceId), m.onlineKey(userId), m.globalOnlineKey(now)},
		m.routeValue(connID),
		int64(defaultRouteTTL/time.Second),
		score,
		m.onlineValue(deviceId, platform),
		offset,
		int64(defaultBitmapTTL/time.Second),
	).Err()
}

// KeepAlive 刷新设备路由的续期
var keepAliveScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current then
	return 0
end
if current ~= ARGV[1] then
	return -1
end
redis.call('EXPIRE', KEYS[1], ARGV[2])
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[4])
redis.call('SETBIT', KEYS[3], ARGV[5], 1)
if redis.call('TTL', KEYS[3]) < 0 then
	redis.call('EXPIRE', KEYS[3], ARGV[6])
end
return 1
`)

func (m *RedisRouteManager) KeepAlive(userId, deviceId, platform, connID string) error {
	ctx, cancel := withTimeoutContext(keepAliveTimeout)
	defer cancel()

	offset, err := m.globalOnlineOffset(userId)
	if err != nil {
		return err
	}
	now := time.Now()
	score := float64(now.Add(defaultRouteTTL).Unix())
	result, err := keepAliveScript.Run(
		ctx,
		m.client,
		[]string{m.routeKey(userId, deviceId), m.onlineKey(userId), m.globalOnlineKey(now)},
		m.routeValue(connID),
		int64(defaultRouteTTL/time.Second),
		score,
		m.onlineValue(deviceId, platform),
		offset,
		int64(defaultBitmapTTL/time.Second),
	).Int64()
	if err != nil {
		return err
	}
	if result == -1 {
		return ErrRouteOwnershipLost
	}
	return nil
}

// Unregister 删除指定设备的路由
var unregisterScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current then
	return 0
end
if current ~= ARGV[1] then
	return -1
end
redis.call('DEL', KEYS[1])
redis.call('ZREM', KEYS[2], ARGV[2])
return 1
`)

func (m *RedisRouteManager) Unregister(userId, deviceId, platform, connID string) error {
	ctx, cancel := withTimeoutContext(unregisterTimeout)
	defer cancel()

	// TODO 如果网关挂了，需要处理 zset 里的分数过期，打算后端开定时任务解决
	result, err := unregisterScript.Run(
		ctx,
		m.client,
		[]string{m.routeKey(userId, deviceId), m.onlineKey(userId)},
		m.routeValue(connID),
		m.onlineValue(deviceId, platform),
	).Int64()
	if err != nil {
		return err
	}
	if result == -1 {
		return ErrRouteOwnershipLost
	}
	return nil
}
