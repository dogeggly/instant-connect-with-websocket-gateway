package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keepAliveFlushPeriod = 1 * time.Second
	keepAliveBatchSize   = 500
	keepAliveQueueSize   = 5000
)

type keepAliveRequest struct {
	userId   string
	deviceId string
	platform string
	client   *Client
	offset   int64
}

var ErrKeepAliveQueueFull = errors.New("keepalive channel is full")

var keepAliveChannel = make(chan keepAliveRequest, keepAliveQueueSize)

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
		score := float64(now.Add(routeTTL).Unix())
		cmd := keepAliveScript.Run(
			ctx,
			pipe,
			[]string{m.routeKey(req.userId, req.deviceId), m.onlineKey(req.userId), m.globalOnlineKey(now)},
			m.routeValue(req.client.ConnID),
			int64(routeTTL/time.Second),
			score,
			m.onlineValue(req.deviceId, req.platform),
			req.offset,
			int64(bitmapTTL/time.Second),
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
		// 检测脚本是否失效（Redis 重启导致）
		if err.Error() == "NOSCRIPT No matching script. Please use EVAL." {
			log.Printf("检测到 Redis 脚本缓存失效，重新加载 keepalive 脚本")
			ctx, cannel := context.WithTimeout(context.Background(), connRedisTimeout)
			defer cannel()
			if err = keepAliveScript.Load(ctx, m.client).Err(); err != nil {
				log.Printf("keepalive 脚本重新加载失败 err=%v", err)
			}
		}
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
