package com.dely.instant_connect.config;

import cn.hutool.core.lang.Snowflake;
import cn.hutool.core.util.IdUtil;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.core.script.DefaultRedisScript;

import java.time.Duration;
import java.util.List;
import java.util.UUID;

@Slf4j
@Configuration
public class SnowflakeConfig {

    @Value("${snowflake.datacenter-id}")
    private long DATACENTER_ID;

    private static final String WORKER_LOCK_KEY_PREFIX = "ic:snowflake:worker_id:";
    private static final Duration LOCK_TTL = Duration.ofSeconds(30);
    private static final long HEARTBEAT_CYCLE = 15000L;

    @Autowired
    private StringRedisTemplate redisTemplate;

    @Bean
    public Snowflake snowflake() {
        String lockOwner = UUID.randomUUID().toString();
        long workerId = acquireWorkerId(lockOwner);
        log.info("已获取本机器的雪花 workerId: {}", workerId);

        // 开启心跳续期
        startHeartbeatThread(workerId, lockOwner);

        return IdUtil.getSnowflake(workerId, DATACENTER_ID);
    }

    // SETNX 抢占 workerId
    private long acquireWorkerId(String lockOwner) {
        for (long candidate = 0; candidate < 32; candidate++) {
            String lockKey = WORKER_LOCK_KEY_PREFIX + candidate;
            Boolean locked = redisTemplate.opsForValue().setIfAbsent(lockKey, lockOwner, LOCK_TTL);
            if (Boolean.TRUE.equals(locked)) {
                return candidate;
            }
        }
        throw new IllegalStateException("没有足够的雪花 workerId 了");
    }

    private void startHeartbeatThread(Long workerId, String lockOwner) {
        Thread heartbeat = new Thread(() -> {
            while (true) {
                try {
                    Thread.sleep(HEARTBEAT_CYCLE);
                } catch (InterruptedException e) {
                    // 恢复中断标志
                    Thread.currentThread().interrupt();
                    throw new IllegalStateException("雪花 workerId 锁续期线程退出", e);
                }
                String lockKey = WORKER_LOCK_KEY_PREFIX + workerId;
                Long renewed = renewLockIfOwned(lockKey, lockOwner);
                if (renewed == 0L) {
                    log.error("雪花 workerId 锁被抢占, key={}, owner={}", lockKey, lockOwner);
                    return;
                }
            }
        }, "snowflake-worker-heartbeat");
        // 标记为守护线程，当 jvm 中只有守护线程时会直接退出
        heartbeat.setDaemon(true);
        heartbeat.start();
    }

    private Long renewLockIfOwned(String lockKey, String lockOwner) {
        DefaultRedisScript<Long> script = new DefaultRedisScript<>();
        script.setScriptText(
                "if redis.call('GET', KEYS[1]) == ARGV[1] then " +
                        "return redis.call('EXPIRE', KEYS[1], ARGV[2]) " +
                        "else return 0 end"
        );
        script.setResultType(Long.class);
        return redisTemplate.execute(
                script,
                List.of(lockKey),
                lockOwner,
                String.valueOf(LOCK_TTL)
        );
    }

}
