package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisAddr     = "127.0.0.1:6379"
	defaultRedisPassword = ""
	defaultRedisDB       = 3
	connRedisTimeout     = 5 * time.Second
	routeTTL             = 60 * time.Second
	bitmapTTL            = 3 * time.Minute
	bitmapBaseUserID     = 10001
	registerTimeout      = 2 * time.Second
	keepAliveTimeout     = 5 * time.Second
	unregisterTimeout    = 2 * time.Second
)

type RedisRouteManager struct {
	client *redis.Client
}

func NewRedisRouteManager() (*RedisRouteManager, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     defaultRedisAddr,
		Password: defaultRedisPassword,
		DB:       defaultRedisDB,
	})

	// 测试连接
	{
		ctx, cancel := context.WithTimeout(context.Background(), connRedisTimeout)
		defer cancel()
		if err := client.Ping(ctx).Err(); err != nil {
			return nil, err
		}
	}

	manager := &RedisRouteManager{client: client}

	// 加载 keepalive 脚本
	{
		ctx, cancel := context.WithTimeout(context.Background(), connRedisTimeout)
		defer cancel()
		if err := keepAliveScript.Load(ctx, manager.client).Err(); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

// redis 的 kv 设计：
// ws:route:{userId}:{deviceId} -> workerId|connId (过期时间 60s)
// ws:online:{userId} -> ZSet (过期时间 60s，成员是 deviceId|platform，分数是过期时间戳)
// ws:global:online:{localTime} -> BitMap

func (m *RedisRouteManager) routeKey(userId, deviceId string) string {
	return fmt.Sprintf("ws:route:%s:%s", userId, deviceId)
}

func (m *RedisRouteManager) routeValue(connID string) string {
	return fmt.Sprintf("%d|%s", workerID, connID)
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
	ctx, cancel := context.WithTimeout(context.Background(), registerTimeout)
	defer cancel()

	offset, err := m.globalOnlineOffset(userId)
	if err != nil {
		return err
	}
	now := time.Now()
	score := float64(now.Add(routeTTL).Unix())
	return registerScript.Run(
		ctx,
		m.client,
		[]string{m.routeKey(userId, deviceId), m.onlineKey(userId), m.globalOnlineKey(now)},
		m.routeValue(connID),
		int64(routeTTL/time.Second),
		score,
		m.onlineValue(deviceId, platform),
		offset,
		int64(bitmapTTL/time.Second),
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

func (m *RedisRouteManager) KeepAlive(userId, deviceId, platform string, client *Client) error {
	offset, err := m.globalOnlineOffset(userId)
	if err != nil {
		return err
	}
	select {
	case keepAliveChannel <- keepAliveRequest{
		userId:   userId,
		deviceId: deviceId,
		platform: platform,
		client:   client,
		offset:   offset,
	}:
		return nil
	default:
		return ErrKeepAliveQueueFull
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), unregisterTimeout)
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
		return fmt.Errorf("连接已被顶掉")
	}
	return nil
}
