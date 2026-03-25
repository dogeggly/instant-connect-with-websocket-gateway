package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	flushPeriod = 1 * time.Second
	batchSize   = 500
	queueSize   = 5000
)

type keepAliveRequest struct {
	userId   string
	deviceId string
	platform string
	offset   int64
	client   *client
}

var errKeepAliveQueueFull = errors.New("keepalive channel is full")

var keepAliveChannel = make(chan keepAliveRequest, queueSize)

func (rm *redisManager) keepAliveAggregator(ctx context.Context) error {
	ticker := time.NewTicker(flushPeriod)
	defer ticker.Stop()

	batch := make([]keepAliveRequest, 0, batchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		rm.flushKeepAliveBatch(batch)
		// 清空 batch 切片，冒号左边默认为 0，右边默认为 len，左开右闭
		batch = batch[:0]
	}

	for {
		select {
		case req, ok := <-keepAliveChannel:
			if !ok {
				return errors.New("续期通道已关闭")
			}
			batch = append(batch, req)
			// 消息堆积了 500 条也统一发一次请求
			if len(batch) >= batchSize {
				flush()
			}
		// 每隔一秒钟统一发一次请求
		case <-ticker.C:
			flush()
		// 优雅停机，先把积压的续期请求都发完
		case <-ctx.Done():
			flush()
			return nil
		}
	}
}

func (rm *redisManager) flushKeepAliveBatch(batch []keepAliveRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), connRedisTimeout)
	defer cancel()

	pipe := rm.Pipeline()

	type keepAliveResult struct {
		cmd    *redis.Cmd
		userId string
		device string
		client *client
	}
	// 用于接收批量命令的结果，关联 userId 和 deviceId，方便后续处理
	cmds := make([]keepAliveResult, 0, len(batch))

	for _, req := range batch {
		now := time.Now()
		score := float64(now.Add(routeTTL).Unix())
		cmd := keepAliveScript.Run(
			ctx,
			pipe,
			[]string{rm.routeKey(req.userId, req.deviceId), rm.onlineKey(req.userId), rm.globalOnlineKey(now)},
			rm.routeValue(req.client.connID),
			int64(routeTTL/time.Second),
			score,
			rm.onlineValue(req.deviceId, req.platform),
			req.offset,
			int64(bitmapTTL/time.Second),
		)
		cmds = append(cmds, keepAliveResult{
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
			if err = keepAliveScript.Load(ctx, rm).Err(); err != nil {
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
