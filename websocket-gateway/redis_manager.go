package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisAddr        = "127.0.0.1:6379"
	redisDB          = 3
	connRedisTimeout = 2 * time.Second
	routeTTL         = 60 * time.Second
	bitmapTTL        = 3 * time.Minute
	bitmapBaseUserID = 10001
)

type redisManager struct {
	*redis.Client
}

func newRedisManager() (*redisManager, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       redisDB,
	})

	// 测试连接
	{
		ctx, cancel := context.WithTimeout(context.Background(), connRedisTimeout)
		defer cancel()
		if err := client.Ping(ctx).Err(); err != nil {
			return nil, err
		}
	}

	// 加载 keepalive 脚本
	{
		ctx, cancel := context.WithTimeout(context.Background(), connRedisTimeout)
		defer cancel()
		if err := keepAliveScript.Load(ctx, client).Err(); err != nil {
			return nil, err
		}
	}

	rm := &redisManager{Client: client}

	return rm, nil
}

// redis 的 kv 设计：
// ws:route:{userId}:{deviceId} -> nodeId|connId (过期时间 60s)
// ws:online:{userId} -> ZSet (过期时间 60s，成员是 deviceId|platform，分数是过期时间戳)
// ws:global:{localTime} -> BitMap

func (rm *redisManager) routeKey(userId, deviceId string) string {
	return fmt.Sprintf("ws:route:%s:%s", userId, deviceId)
}

func (rm *redisManager) routeValue(connID string) string {
	return fmt.Sprintf("%d|%s", nodeId, connID)
}

func (rm *redisManager) onlineKey(userId string) string {
	return fmt.Sprintf("ws:online:%s", userId)
}

func (rm *redisManager) onlineValue(deviceId, platform string) string {
	return fmt.Sprintf("%s|%s", deviceId, platform)
}

func (rm *redisManager) globalOnlineKey(now time.Time) string {
	return fmt.Sprintf("ws:global:%s", now.Format("200601021504"))
}

func (rm *redisManager) globalOnlineOffset(userId string) (int64, error) {
	uid, err := strconv.ParseInt(userId, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("userId 不是有效数字: %w", err)
	}
	if uid < bitmapBaseUserID {
		return 0, fmt.Errorf("userId 必须大于等于 %d", bitmapBaseUserID)
	}
	return uid - bitmapBaseUserID, nil
}

// register 写入设备路由
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

func (rm *redisManager) register(userId, deviceId, platform, connID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), connRedisTimeout)
	defer cancel()

	offset, err := rm.globalOnlineOffset(userId)
	if err != nil {
		return err
	}
	now := time.Now()
	score := float64(now.Add(routeTTL).Unix())
	return registerScript.Run(
		ctx,
		rm,
		[]string{rm.routeKey(userId, deviceId), rm.onlineKey(userId), rm.globalOnlineKey(now)},
		rm.routeValue(connID),
		int64(routeTTL/time.Second),
		score,
		rm.onlineValue(deviceId, platform),
		offset,
		int64(bitmapTTL/time.Second),
	).Err()
}

// keepAlive 刷新设备路由的续期
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

func (rm *redisManager) keepAlive(userId, deviceId, platform string, client *client) error {
	offset, err := rm.globalOnlineOffset(userId)
	if err != nil {
		return err
	}
	select {
	case keepAliveChannel <- keepAliveRequest{
		userId:   userId,
		deviceId: deviceId,
		platform: platform,
		offset:   offset,
		client:   client,
	}:
		return nil
	default:
		return errKeepAliveQueueFull
	}
}

// unregister 删除指定设备的路由
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

func (rm *redisManager) unregister(userId, deviceId, platform, connID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), connRedisTimeout)
	defer cancel()

	result, err := unregisterScript.Run(
		ctx,
		rm,
		[]string{rm.routeKey(userId, deviceId), rm.onlineKey(userId)},
		rm.routeValue(connID),
		rm.onlineValue(deviceId, platform),
	).Int64()
	if err != nil {
		return err
	}
	if result == -1 {
		return fmt.Errorf("连接已被顶掉")
	}
	return nil
}
