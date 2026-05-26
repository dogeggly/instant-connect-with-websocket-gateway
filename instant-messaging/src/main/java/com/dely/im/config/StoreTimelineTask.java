package com.dely.im.config;

import com.baomidou.mybatisplus.core.conditions.update.UpdateWrapper;
import com.dely.im.entity.TimelineTask;
import com.dely.im.mapper.TimelineTaskMapper;
import com.dely.im.pb.MqStorePayload;
import lombok.extern.slf4j.Slf4j;
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
    private RabbitTemplate rabbitTemplate;

    @Autowired
    private TimelineTaskMapper timelineTaskMapper;

    @Scheduled(fixedDelay = 1000)
    public void storeTimeline() {
        try {
            // 收割待处理任务：FOR UPDATE SKIP LOCKED 保证多实例不会重复收割
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
        } catch (InterruptedException | ExecutionException | TimeoutException e) {
            throw new RuntimeException("收割任务失败：" + e);
        }
    }
}