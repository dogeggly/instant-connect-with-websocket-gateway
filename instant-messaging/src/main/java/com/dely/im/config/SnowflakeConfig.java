package com.dely.im.config;

import cn.hutool.core.lang.Snowflake;
import cn.hutool.core.util.IdUtil;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.redisson.api.RLock;
import org.redisson.api.RedissonClient;
import org.springframework.beans.factory.annotation.Autowired;

import java.util.concurrent.TimeUnit;

@Slf4j
@Configuration
public class SnowflakeConfig {

    @Value("${snowflake.datacenter-id}")
    private long DATACENTER_ID;

    private static final String NODE_LOCK_KEY_PREFIX = "im:node_id:";

    @Autowired
    private RedissonClient redissonClient;

    @Bean
    public Snowflake snowflake() {
        long nodeId = acquireNodeId();
        log.info("已获取本机器的 nodeId: {}", nodeId);

        return IdUtil.getSnowflake(nodeId, DATACENTER_ID);
    }

    // 基于 Redisson 看门狗抢占 nodeId
    private long acquireNodeId() {
        for (long candidate = 0; candidate < 32; candidate++) {
            String lockKey = NODE_LOCK_KEY_PREFIX + candidate;
            RLock lock = redissonClient.getLock(lockKey);
            try {
                // waitTime=0：不阻塞等待，抢不到立即尝试下一个
                // leaseTime=-1：开启 Watchdog 自动续期，直到显式解锁或进程退出
                boolean locked = lock.tryLock(0, -1, TimeUnit.MILLISECONDS);
                if (locked) {
                    return candidate;
                }
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new IllegalStateException("抢占 nodeId 被中断", e);
            }
        }
        throw new IllegalStateException("没有足够的 nodeId(0-31) 了");
    }

}
