package com.dely.im.config;

import com.baomidou.mybatisplus.core.conditions.update.UpdateWrapper;
import com.dely.im.entity.TimelineTask;
import com.dely.im.mapper.TimelineTaskMapper;
import com.dely.im.pb.MqStorePayload;
import lombok.extern.slf4j.Slf4j;
import org.redisson.api.RLock;
import org.redisson.api.RedissonClient;
import org.springframework.amqp.core.Message;
import org.springframework.amqp.core.MessageBuilder;
import org.springframework.amqp.rabbit.connection.CorrelationData;
import org.springframework.amqp.rabbit.core.RabbitTemplate;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.scheduling.annotation.EnableScheduling;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

@Slf4j
@Component
@EnableScheduling
public class StoreTimelineTask {

    @Autowired
    private RedissonClient redissonClient;

    @Autowired
    private RabbitTemplate rabbitTemplate;

    @Autowired
    private TimelineTaskMapper timelineTaskMapper;

    private static final String STORE_TASK_LOCK_KEY = "im:task:store";

    @Scheduled(fixedDelay = 1000) // 上一次执行完毕后，等 1 秒再执行
    public void storeTimeline() {
        // 1. 获取锁对象
        RLock lock = redissonClient.getLock(STORE_TASK_LOCK_KEY);

        try {
            // 2. 尝试抢锁
            // waitTime = 0: 没抢到立刻放弃，绝不阻塞线程等待！
            // leaseTime = -1: 不指定过期时间，主动唤醒并依赖 Redisson 的 Watchdog 看门狗机制！
            boolean isLocked = lock.tryLock(0, -1, TimeUnit.MILLISECONDS);

            if (!isLocked) {
                // 别人在干活，我直接下班
                return;
            }

            // 3. 抢到锁了，看门狗已经在后台帮你保驾护航了
            // 捞出待处理任务（status=0）
            List<TimelineTask> tasks = timelineTaskMapper.harvest();
            LocalDateTime compareTime = LocalDateTime.now().minusMinutes(1);
            List<TimelineTask> processingTasks = timelineTaskMapper.processingHarvest(compareTime);
            tasks.addAll(processingTasks);

            if (tasks.isEmpty()) return;
            List<CorrelationData> correlationDataList = new ArrayList<>(tasks.size());

            for (TimelineTask task : tasks) {
                MqStorePayload mqStorePayload = MqStorePayload.newBuilder()
                        .setMsgId(task.getMsgId())
                        .setSenderId(task.getSenderId())
                        .setReceiverId(task.getReceiverId())
                        .setIsGroup(task.getIsGroup())
                        .build();

                byte[] protobufBytes = mqStorePayload.toByteArray();

                Message amqpMessage = MessageBuilder.withBody(protobufBytes)
                        // 告诉 MQ 和消费者，这是一坨二进制的 Protobuf 流，不是普通字符串
                        .setContentType("application/x-protobuf")
                        .build();

                CorrelationData cd = new CorrelationData(String.valueOf(task.getMsgId()));

                rabbitTemplate.convertAndSend(
                        RabbitmqConfig.DIRECT_STORE_EXCHANGE,
                        "store",
                        amqpMessage,
                        cd);
                correlationDataList.add(cd);
            }

            // 批量收割结果（耗时基本等同于单次网络 RTT）
            List<Long> successIds = new ArrayList<>();
            List<Long> failedIds = new ArrayList<>(); // 用于记录路由失败的

            for (CorrelationData cd : correlationDataList) {
                Long msgId = Long.valueOf(cd.getId());
                // 等待确认，最大等待 2 秒（由于之前已经全发出去了，这里的等待其实极快）
                CorrelationData.Confirm confirm = cd.getFuture().get(2, TimeUnit.SECONDS);

                if (confirm.isAck() && cd.getReturned() == null) {
                    // 完美送达且被路由
                    successIds.add(msgId);
                } else if (confirm.isAck() && cd.getReturned() != null) {
                    // 收到 ACK，但路由失败（触发了 Return）
                    failedIds.add(msgId);
                } else {
                    // 没收到 ACK，且已经经过一轮重试
                    // 搁置不删
                    log.error("MQ 拒绝了 msg_id: {}", msgId);
                }
            }

            // 成功 → 删除；失败 → status=2
            if (!successIds.isEmpty()) {
                timelineTaskMapper.deleteByIds(successIds);
            }
            if (!failedIds.isEmpty()) {
                UpdateWrapper<TimelineTask> updateWrapper = new UpdateWrapper<>();
                updateWrapper.in("msg_id", failedIds).set("status", 2);
                timelineTaskMapper.update(updateWrapper);
            }
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("抢占 store 锁被中断", e);
        } catch (ExecutionException | TimeoutException e) {
            throw new RuntimeException(e);
        } finally {
            // 4. 业务完毕，立刻释放锁，看门狗自动销毁
            // Redisson 自己实现了删锁时的 CAS 校验
            if (lock.isHeldByCurrentThread()) {
                lock.unlock();
            }
        }
    }
}