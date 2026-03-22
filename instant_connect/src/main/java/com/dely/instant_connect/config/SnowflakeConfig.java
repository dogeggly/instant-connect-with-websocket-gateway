package com.dely.instant_connect.config;

import cn.hutool.core.lang.Snowflake;
import cn.hutool.core.util.IdUtil;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.data.redis.core.StringRedisTemplate;

@Slf4j
@Configuration
public class SnowflakeConfig {

    @Autowired
    private StringRedisTemplate redisTemplate;

    @Bean
    public Snowflake snowflake() {
        // 1. 定义 Redis 中的分配 Key
        String WORKER_ID_KEY = "snowflake:worker_id";

        // 2. 利用 Redis 的 INCR 命令原子递增，获取一个绝对不重复的序号
        Long incrementedId = redisTemplate.opsForValue().increment(WORKER_ID_KEY);

        // 3. 雪花算法 WorkerId 占 5 bit，最大值为 31。取模保证不越界
        long workerId = (incrementedId != null ? incrementedId : 0) % 32;

        // DatacenterId 也可以用类似方法，或者针对特定机房写死配置 (此处假设单数据中心固定为 1)
        long datacenterId = 1;

        log.info("Snowflake initialized. WorkerID: {}, DatacenterID: {}", workerId, datacenterId);

        // 4. 初始化并返回明确指定了机器码的 Snowflake 实例
        return IdUtil.getSnowflake(workerId, datacenterId);
    }
}
