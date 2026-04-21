package com.dely.im.mq;

import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.dely.im.entity.GroupMembers;
import com.dely.im.entity.Timeline;
import com.dely.im.mapper.GroupMembersMapper;
import com.dely.im.mapper.TimelineMapper;
import com.dely.im.pb.MqStorePayload;
import com.google.protobuf.InvalidProtocolBufferException;
import com.rabbitmq.client.Channel;
import org.springframework.amqp.core.Message;
import lombok.extern.slf4j.Slf4j;
import org.springframework.amqp.rabbit.annotation.Exchange;
import org.springframework.amqp.rabbit.annotation.Queue;
import org.springframework.amqp.rabbit.annotation.QueueBinding;
import org.springframework.amqp.rabbit.annotation.RabbitListener;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.data.redis.core.script.DefaultRedisScript;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.core.RedisCallback;
import org.springframework.data.redis.connection.StringRedisConnection;
import org.springframework.stereotype.Component;

import java.io.IOException;
import java.util.ArrayList;
import java.util.List;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.stream.Collectors;

@Slf4j
@Component
public class RabbitmqListener {

    @Autowired
    private TimelineMapper timelineMapper;

    @Autowired
    private GroupMembersMapper groupMembersMapper;

    @Autowired
    private StringRedisTemplate stringRedisTemplate;

    private static final String DIRECT_STORE_EXCHANGE = "im.direct.store.exchange";
    private static final String STORE_QUEUE = "im.store.queue";
    private static final String GROUP_MEMBERS_PREFIX = "im:group_members:";
    private static final String SEQ_ID_PREFIX = "im:seq_id:";
    private static final long LAST_SEQ_FIELD_TTL_SECONDS = TimeUnit.MINUTES.toSeconds(1);

    private static final DefaultRedisScript<List> NEXT_SEQ_ID_BATCH_SCRIPT = new DefaultRedisScript<>(
            "local msgId = ARGV[1] " +
                    "local ttlSeconds = tonumber(ARGV[2]) " +
                    "local ownerCount = #KEYS / 2 " +
                    "local result = {} " +
                    "for i = 1, ownerCount do " +
                    "  local seqKey = KEYS[i] " +
                    "  local lastHashKey = KEYS[i + ownerCount] " +
                    "  local existedSeqId = redis.call('HGET', lastHashKey, msgId) " +
                    "  if existedSeqId then " +
                    "    table.insert(result, tonumber(existedSeqId)) " +
                    "  else " +
                    "    local seqId = redis.call('INCR', seqKey) " +
                    "    redis.call('HSET', lastHashKey, msgId, seqId) " +
                    "    redis.call('HEXPIRE', lastHashKey, ttlSeconds, 'FIELDS', 1, msgId) " +
                    "    table.insert(result, seqId) " +
                    "  end " +
                    "end " +
                    "return result",
            List.class
    );

    @RabbitListener(bindings = @QueueBinding(
            value = @Queue(value = STORE_QUEUE, durable = "true", autoDelete = "false", exclusive = "false"),
            exchange = @Exchange(value = DIRECT_STORE_EXCHANGE, type = "direct"),
            key = "store"
    ))
    public void listenDirectQueue(Message message, Channel channel) throws IOException {
        long deliveryTag = message.getMessageProperties().getDeliveryTag();
        try {
            MqStorePayload payload = MqStorePayload.parseFrom(message.getBody());
            Long msgId = payload.getMsgId();
            Long receiverId = payload.getReceiverId();
            Long senderId = payload.getSenderId();

            if (payload.getIsGroup()) {
                handleGroupTimeline(msgId, receiverId);
            } else {
                handSingleTimeline(msgId, receiverId, senderId);
            }
            // 确认消息
            channel.basicAck(deliveryTag, false);
        } catch (InvalidProtocolBufferException e) {
            // 【第一类：不可恢复的致命异常】
            // 比如 Protobuf 格式完全乱了，重试 100 次也没用。
            log.error("致命的数据解析错误，直接扔进死信队列！");
            // 第一个 false 是单条消息的确认，第二个 false 是不要重入队列，直接投递死信队列
            channel.basicNack(deliveryTag, false, false);
        } catch (Exception e) {
            // 【第二类：可能恢复的运行时异常】
            // 比如 Redis 超时、PgSQL 死锁。
            // 我们不在这里执行 ACK 或 NACK，而是直接往外抛出异常！
            // 异常抛出后，会被 Spring 的 Retry 机制捕获，执行 1s -> 2s 的本地重试。
            // 如果 3 次之后依然失败，Spring 底层会自动向 RabbitMQ 发送 Nack(requeue=false)，消息最终平稳落入死信队列。
            log.warn("落库遇到临时异常，交由 Spring 进行退避重试...");
            throw e;
        }
    }

    private void handSingleTimeline(Long msgId, Long receiverId, Long senderId) {
        List<Long> ownerIds = List.of(senderId, receiverId);
        List<Long> seqIds = nextSeqIdBatch(msgId, ownerIds);

        List<Timeline> timelines = new ArrayList<>(2);
        timelines.add(Timeline.builder()
                .ownerId(ownerIds.get(0))
                .seqId(seqIds.get(0))
                .msgId(msgId)
                .isGroup(false)
                .build());
        timelines.add(Timeline.builder()
                .ownerId(ownerIds.get(1))
                .seqId(seqIds.get(1))
                .msgId(msgId)
                .isGroup(false)
                .build());
        timelineMapper.insertBatch(timelines);
    }

    private void handleGroupTimeline(Long msgId, Long groupId) {
        String groupMembersCacheKey = GROUP_MEMBERS_PREFIX + groupId;
        Set<String> memberIds = stringRedisTemplate.opsForSet().members(groupMembersCacheKey);

        if (memberIds == null || memberIds.isEmpty()) {
            QueryWrapper<GroupMembers> queryWrapper = new QueryWrapper<>();
            queryWrapper.eq("group_id", groupId);
            List<GroupMembers> groupMembersList = groupMembersMapper.selectList(queryWrapper);

            if (groupMembersList == null || groupMembersList.isEmpty()) {
                log.warn("群 {} 无成员，跳过 timeline 写入，msgId={}", groupId, msgId);
                return;
            }

            memberIds = groupMembersList.stream()
                    .map(member -> member.getUserId().toString())
                    .collect(Collectors.toSet());

            String[] memberIdArray = memberIds.toArray(new String[0]);
            long expireSeconds = TimeUnit.HOURS.toSeconds(1);
            stringRedisTemplate.executePipelined((RedisCallback<Object>) connection -> {
                StringRedisConnection stringConnection = (StringRedisConnection) connection;
                stringConnection.sAdd(groupMembersCacheKey, memberIdArray);
                stringConnection.expire(groupMembersCacheKey, expireSeconds);
                return null;
            });
        }

        List<Long> ownerIds = memberIds.stream().map(Long::valueOf).toList();
        List<Long> seqIds = nextSeqIdBatch(msgId, ownerIds);

        List<Timeline> timelines = new ArrayList<>(ownerIds.size());
        for (int index = 0; index < ownerIds.size(); index++) {
            timelines.add(Timeline.builder()
                    .ownerId(ownerIds.get(index))
                    .seqId(seqIds.get(index))
                    .msgId(msgId)
                    .isGroup(true)
                    .build());
        }

        timelineMapper.insertBatch(timelines);
    }

    private List<Long> nextSeqIdBatch(Long msgId, List<Long> ownerIds) {
        List<String> keys = new ArrayList<>(ownerIds.size() * 2);
        for (Long ownerId : ownerIds) {
            keys.add(SEQ_ID_PREFIX + ownerId);
        }
        for (Long ownerId : ownerIds) {
            keys.add(SEQ_ID_PREFIX + ownerId + ":last");
        }

        List<Object> rawResults = stringRedisTemplate.execute(
                NEXT_SEQ_ID_BATCH_SCRIPT,
                keys,
                msgId.toString(),
                String.valueOf(LAST_SEQ_FIELD_TTL_SECONDS)
        );

        if (rawResults.size() != ownerIds.size()) {
            throw new IllegalStateException("批量获取 seqId 失败");
        }

        List<Long> seqIds = new ArrayList<>(rawResults.size());
        for (int index = 0; index < rawResults.size(); index++) {
            Object raw = rawResults.get(index);
            if (!(raw instanceof Number seqId)) {
                throw new IllegalStateException("seqId 类型异常, ownerId=" + ownerIds.get(index));
            }
            seqIds.add(seqId.longValue());
        }
        return seqIds;
    }

}
