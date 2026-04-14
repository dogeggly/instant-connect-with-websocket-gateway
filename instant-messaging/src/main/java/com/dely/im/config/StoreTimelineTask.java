package com.dely.im.config;

import lombok.extern.slf4j.Slf4j;
import org.springframework.scheduling.annotation.EnableScheduling;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

@Slf4j
@Component
@EnableScheduling
public class StoreTimelineTask {

    @Scheduled(cron = "*/1 * * * * *")
    public void storeTimeline() {
        log.info("执行存储时间线的定时任务");
    }
}