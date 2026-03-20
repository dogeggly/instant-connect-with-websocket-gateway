package main

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	keepAliveFlushPeriod = 1 * time.Second
	keepAliveBatchSize   = 500
	keepAliveQueueSize   = 5000
)

type RedisRouteManager struct {
	client         *redis.Client
	gatewayAddress string
}

type keepAliveRequest struct {
	userId   string
	deviceId string
	platform string
	client   *Client
	offset   int64
}

var ErrKeepAliveQueueFull = errors.New("keepalive channel is full")

var keepAliveChannel = make(chan keepAliveRequest, keepAliveQueueSize)

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

func (m *RedisRouteManager) keepAliveAggregatorLoop(ctx context.Context) {
	ticker := time.NewTicker(keepAliveFlushPeriod)
	defer ticker.Stop()

	batch := make([]keepAliveRequest, 0, keepAliveBatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		m.flushKeepAliveBatch(batch)
		// 清空 batch 切片，冒号左边默认为 0，右边默认为 len，左开右闭
		batch = batch[:0]
	}

	for {
		select {
		case req := <-keepAliveChannel:
			batch = append(batch, req)
			// 消息堆积了 500 条也统一发一次请求
			if len(batch) >= keepAliveBatchSize {
				flush()
			}
		// 每隔一秒钟统一发一次请求
		case <-ticker.C:
			flush()
		// 优雅停机，先把积压的续期请求都发完
		case <-ctx.Done():
			flush()
			return
		}
	}
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

func (m *RedisRouteManager) flushKeepAliveBatch(batch []keepAliveRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), keepAliveTimeout)
	defer cancel()

	pipe := m.client.Pipeline()

	type keepAliveCmdResult struct {
		cmd    *redis.Cmd
		userId string
		device string
		client *Client
	}
	// 用于接收批量命令的结果，关联 userId 和 deviceId，方便后续处理
	cmds := make([]keepAliveCmdResult, 0, len(batch))

	for _, req := range batch {
		now := time.Now()
		score := float64(now.Add(defaultRouteTTL).Unix())
		cmd := keepAliveScript.Run(
			ctx,
			pipe,
			[]string{m.routeKey(req.userId, req.deviceId), m.onlineKey(req.userId), m.globalOnlineKey(now)},
			m.routeValue(req.client.ConnID),
			int64(defaultRouteTTL/time.Second),
			score,
			m.onlineValue(req.deviceId, req.platform),
			req.offset,
			int64(defaultBitmapTTL/time.Second),
		)
		cmds = append(cmds, keepAliveCmdResult{
			cmd:    cmd,
			userId: req.userId,
			device: req.deviceId,
			client: req.client,
		})
	}

	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		log.Printf("keepalive 批量 pipeline 执行失败 err=%v", err)
	}

	for _, item := range cmds {
		result, err := item.cmd.Int64()
		if err != nil {
			log.Printf("keepalive 批量续期命令失败 userId=%s deviceId=%s err=%v", item.userId, item.device, err)
			continue
		}
		if result == -1 {
			log.Printf("连接被顶掉，批量续期阶段断开 userId=%s deviceId=%s", item.userId, item.device)
			_ = item.client.Close()
		}
	}
}

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
