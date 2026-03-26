package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	nodeLockPrefix     = "ws:node_id"
	nodeLockTTL        = 30 * time.Second
	nodeHeartbeatCycle = 20 * time.Second
)

func initNodeId(rm *redisManager) (int64, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), connRedisTimeout)
	defer cancel()

	lockOwner := uuid.NewString()
	for candidate := int64(0); candidate < 32; candidate++ {
		lockKey := fmt.Sprintf("%s:%d", nodeLockPrefix, candidate)
		err := rm.SetArgs(ctx, lockKey, lockOwner, redis.SetArgs{
			TTL:  nodeLockTTL,
			Mode: "NX",
		}).Err()
		if err == nil {
			return candidate, lockOwner, nil
		}
	}

	return 0, "", errors.New("没有可用的 snowflake workerId(0-31)")
}

func (rm *redisManager) startNodeHeartbeat(ctx context.Context, lockOwner string) error {
	ticker := time.NewTicker(nodeHeartbeatCycle)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			renewed, err := rm.renewNodeLockIfOwned(lockOwner)
			if err != nil {
				log.Printf("snowflake workerId 锁续期失败 err=%v", err)
				if renewed == 0 {
					log.Printf("snowflake workerId 锁已丢失 owner=%s", lockOwner)
				}
				return err
			}
		}
	}
}

var keepAliveWorkerId = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then 
	return redis.call('EXPIRE', KEYS[1], ARGV[2]) 
else return 0 end
`)

func (rm *redisManager) renewNodeLockIfOwned(lockOwner string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), connRedisTimeout)
	defer cancel()

	lockKey := fmt.Sprintf("%s:%d", nodeLockPrefix, nodeId)
	result, err := keepAliveWorkerId.Run(
		ctx,
		rm,
		[]string{lockKey},
		lockOwner,
		int64(nodeLockTTL/time.Second)).Int64()

	if err != nil {
		return 0, err
	}
	return result, nil
}
