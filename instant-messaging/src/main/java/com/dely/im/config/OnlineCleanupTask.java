package com.dely.im.config;

import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.data.redis.connection.RedisConnection;
import org.springframework.data.redis.core.*;
import org.springframework.scheduling.annotation.EnableScheduling;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

import java.time.Duration;
import java.util.ArrayList;
import java.util.List;

@Slf4j
@Component
@EnableScheduling
public class OnlineCleanupTask {

    @Autowired
    private StringRedisTemplate stringRedisTemplate;

    private static final String TASK_LOCK_KEY = "im:task";
    private static final long TASK_LOCK_TTL = 60L; // 秒

    @Scheduled(cron = "0 */5 * * * *")
    public void cleanupOnlineZSet() {

        // 尝试获取锁，设置 1 分钟 ttl 防止节点时钟漂移
        Boolean locked = stringRedisTemplate.opsForValue()
                .setIfAbsent(TASK_LOCK_KEY, "locked", Duration.ofSeconds(TASK_LOCK_TTL));

        // 如果返回 false，说明别的节点已经抢到锁了，当前节点直接退出
        if (Boolean.FALSE.equals(locked)) {
            return;
        }

        long scannedKeyCount = 0L;
        long removedMemberCount = 0L;
        ScanOptions options = ScanOptions.scanOptions()
                .match("ws:online:*")
                .count(200)
                .build();
        try (RedisConnection connection = stringRedisTemplate.getConnectionFactory().getConnection();
             Cursor<byte[]> cursor = connection.keyCommands().scan(options)
        ) {
            List<byte[]> batchKeys = new ArrayList<>(200);
            while (cursor.hasNext()) {
                batchKeys.add(cursor.next());
                scannedKeyCount++;
                if (batchKeys.size() >= 200) {
                    long removed = flushBatch(connection, batchKeys);
                    removedMemberCount += removed;
                    batchKeys.clear();
                }
            }
            if (!batchKeys.isEmpty()) {
                long removed = flushBatch(connection, batchKeys);
                removedMemberCount += removed;
            }
            if (removedMemberCount > 0) {
                log.info("在线设备清理完成, 扫描Key数={}, 删除成员数={}", scannedKeyCount, removedMemberCount);
            }
        } catch (Exception e) {
            log.error("在线设备清理任务执行失败", e);
        }
    }

    private long flushBatch(RedisConnection connection, List<byte[]> batchKeys) {
        long removed = 0;
        long nowTimestamp = System.currentTimeMillis() / 1000;
        connection.openPipeline();
        for (byte[] key : batchKeys) {
            connection.zSetCommands().zRemRangeByScore(
                    key,
                    Double.NEGATIVE_INFINITY,
                    nowTimestamp
            );
        }
        List<Object> results = connection.closePipeline();
        for (Object res : results) {
            if (res instanceof Long) {
                removed += (Long) res;
            }
        }
        return removed;
    }
}