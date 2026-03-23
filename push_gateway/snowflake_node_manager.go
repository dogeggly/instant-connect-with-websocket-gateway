package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/snowflake"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	snowflakeWorkerLockPrefix     = "ws:snowflake:worker_id:"
	snowflakeWorkerLockTTL        = 30 * time.Second
	snowflakeWorkerHeartbeatCycle = 15 * time.Second

	// 这里写死。
	fixedNodeID = 1
)

var workerID int64
var lockOwner string

func InitSnowflakeNode(client *redis.Client) (*snowflake.Node, error) {
	var err error
	lockOwner = uuid.NewString()
	workerID, err = acquireSnowflakeWorkerID(client)
	if err != nil {
		return nil, err
	}

	nodeID := (fixedNodeID << 5) | workerID

	return snowflake.NewNode(nodeID)
}

func acquireSnowflakeWorkerID(client *redis.Client) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), connRedisTimeout)
	defer cancel()

	for candidate := int64(0); candidate < 32; candidate++ {
		lockKey := fmt.Sprintf("%s%d", snowflakeWorkerLockPrefix, candidate)
		err := client.SetArgs(ctx, lockKey, lockOwner, redis.SetArgs{
			TTL:  snowflakeWorkerLockTTL,
			Mode: "NX",
		}).Err()
		if err == nil {
			return candidate, nil
		}
	}

	return 0, errors.New("没有可用的 snowflake workerId(0-31)")
}

func startSnowflakeWorkerHeartbeat(ctx context.Context, client *redis.Client) {

	ticker := time.NewTicker(snowflakeWorkerHeartbeatCycle)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewed, err := renewSnowflakeWorkerLockIfOwned(client)
			if err != nil {
				log.Printf("snowflake workerId 锁续期失败 err=%v", err)
				continue
			}
			if renewed == 0 {
				log.Printf("snowflake workerId 锁已丢失 owner=%s", lockOwner)
				return
			}
		}
	}
}

var keepAliveWorkerId = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then 
	return redis.call('EXPIRE', KEYS[1], ARGV[2]) 
else return 0 end
`)

func renewSnowflakeWorkerLockIfOwned(client *redis.Client) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keepAliveTimeout)
	defer cancel()

	lockKey := fmt.Sprintf("%s%d", snowflakeWorkerLockPrefix, workerID)
	result, err := keepAliveWorkerId.Run(
		ctx,
		client,
		[]string{lockKey},
		lockOwner,
		int64(snowflakeWorkerLockTTL/time.Second)).Int64()

	if err != nil {
		return 0, err
	}
	return result, nil
}
