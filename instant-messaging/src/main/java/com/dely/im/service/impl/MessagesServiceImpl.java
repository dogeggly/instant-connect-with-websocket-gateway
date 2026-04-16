package com.dely.im.service.impl;

import cn.hutool.core.util.StrUtil;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import com.dely.im.config.RabbitmqConfig;
import com.dely.im.entity.GroupMembers;
import com.dely.im.entity.Messages;
import com.dely.im.entity.Timeline;
import com.dely.im.entity.TimelineTask;
import com.dely.im.mapper.GroupMembersMapper;
import com.dely.im.mapper.MessagesMapper;
import com.dely.im.mapper.TimelineMapper;
import com.dely.im.mapper.TimelineTaskMapper;
import com.dely.im.pb.EventType;
import com.dely.im.pb.MqPayload;
import com.dely.im.service.IMessagesService;
import com.dely.im.utils.SyncMessage;
import com.google.protobuf.ByteString;
import lombok.extern.slf4j.Slf4j;
import org.springframework.amqp.core.Message;
import org.springframework.amqp.core.MessageBuilder;
import org.springframework.amqp.core.MessageDeliveryMode;
import org.springframework.amqp.rabbit.core.RabbitTemplate;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.core.task.TaskExecutor;
import org.springframework.data.redis.core.RedisCallback;
import org.springframework.data.redis.connection.StringRedisConnection;
import org.springframework.data.redis.core.ZSetOperations;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.stream.Collectors;

/**
 * <p>
 * 服务实现类
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
@Slf4j
@Service
public class MessagesServiceImpl extends ServiceImpl<MessagesMapper, Messages> implements IMessagesService {

    @Autowired
    private MessagesMapper messagesMapper;

    @Autowired
    private TimelineTaskMapper timelineTaskMapper;

    @Autowired
    private GroupMembersMapper groupMembersMapper;

    @Autowired
    private TimelineMapper timelineMapper;

    @Autowired
    private RabbitTemplate rabbitTemplate;

    @Autowired
    private StringRedisTemplate stringRedisTemplate;

    @Autowired
    private TaskExecutor taskExecutor;

    private static final String GROUP_MEMBERS_PREFIX = "im:group_members:";

    @Override
    @Transactional
    public void saveMessage(Messages message) {
        messagesMapper.saveMessage(message);
        timelineTaskMapper.insert(TimelineTask.builder()
                .msgId(message.getMsgId())
                .senderId(message.getSenderId())
                .receiverId(message.getReceiverId())
                .status(false)
                .isGroup(false)
                .build());
        asyncPushMessageToOnlineGateways(message, false);
    }

    @Override
    public void sendMessageWithoutStore(Messages message) {
        asyncPushMessageToOnlineGateways(message, false);
    }

    @Override
    @Transactional
    public void sendGroupMessage(Messages message) {
        messagesMapper.saveMessage(message);
        timelineTaskMapper.insert(TimelineTask.builder()
                .msgId(message.getMsgId())
                .receiverId(message.getReceiverId())
                .status(false)
                .isGroup(true)
                .build());
        asyncPushMessageToOnlineGateways(message, true);
    }

    private void asyncPushMessageToOnlineGateways(Messages message, boolean isGroup) {
        taskExecutor.execute(() -> {
            try {
                if (isGroup) {
                    // 群聊消息：先查 Redis 缓存的组员，没有就查数据库并缓存
                    pushGroupMessage(message);
                } else {
                    // 单聊消息：直接投递给单个接收者
                    pushSingleMessage(message);
                }
            } catch (Exception e) {
                log.error("异步推送消息失败，msgId={}", message.getMsgId(), e);
            }
        });
    }

    private void pushGroupMessage(Messages message) {
        Long groupId = message.getReceiverId();
        String groupMembersCacheKey = GROUP_MEMBERS_PREFIX + groupId;

        // 尝试从 Redis 获取群组成员列表
        Set<String> memberIds = stringRedisTemplate.opsForSet().members(groupMembersCacheKey);

        // 如果 Redis 没有缓存，从数据库查询
        if (memberIds == null || memberIds.isEmpty()) {
            QueryWrapper<GroupMembers> queryWrapper = new QueryWrapper<>();
            queryWrapper.eq("group_id", groupId);
            List<GroupMembers> groupMembersList = groupMembersMapper.selectList(queryWrapper);

            if (groupMembersList == null || groupMembersList.isEmpty()) {
                log.warn("未找到群组 {} 的成员", groupId);
                return;
            }

            // 将成员 ID 存入 Redis
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

        MqPayload mqPayload = MqPayload.newBuilder()
                .setType(EventType.forNumber(message.getMsgType()))
                .setMsgId(message.getMsgId())
                .addAllUserIds(memberIds.stream().map(Long::valueOf).collect(Collectors.toSet()))
                .setSenderId(message.getSenderId())
                .setGroupId(message.getReceiverId())
                .setContent(ByteString.copyFrom(message.getContent(), StandardCharsets.UTF_8)).build();

        byte[] protobufBytes = mqPayload.toByteArray();

        Message amqpMessage = MessageBuilder.withBody(protobufBytes)
                // 告诉 MQ 和消费者，这是一坨二进制的 Protobuf 流，不是普通字符串
                .setContentType("application/x-protobuf")
                .build();

        rabbitTemplate.convertAndSend(
                RabbitmqConfig.FANOUT_EXCHANGE,
                "", // fanout 交换机不需要 routing key
                amqpMessage,
                m -> {
                    // 强行修改为非持久化 (纯内存飞行，极致速度)
                    m.getMessageProperties().setDeliveryMode(MessageDeliveryMode.NON_PERSISTENT);
                    return m;
                });
        log.info("群消息 {} 已推送，载荷: {}", message.getMsgId(), mqPayload);
    }

    private void pushSingleMessage(Messages message) {
        Long receiverId = message.getReceiverId();
        Long senderId = message.getSenderId();

        // 3. 先查 ws:online:{userId}，拿到所有在线设备 deviceId|platform
        String receiverOnlineKey = "ws:online:" + receiverId;
        String senderOnlineKey = "ws:online:" + senderId;
        long nowTimestamp = System.currentTimeMillis() / 1000;
        Set<ZSetOperations.TypedTuple<String>> receiverOnlineMembers =
                stringRedisTemplate.opsForZSet()
                        .rangeByScoreWithScores(receiverOnlineKey, nowTimestamp, Double.POSITIVE_INFINITY);
        Set<ZSetOperations.TypedTuple<String>> senderOnlineMembers =
                stringRedisTemplate.opsForZSet()
                        .rangeByScoreWithScores(senderOnlineKey, nowTimestamp, Double.POSITIVE_INFINITY);

        if ((receiverOnlineMembers == null || receiverOnlineMembers.isEmpty())
                && (senderOnlineMembers == null || senderOnlineMembers.isEmpty())) {
            // 说明对方和自己的其他设备都离线，不做任何处理
            return;
        }

        Set<String> routeKeys = new HashSet<>();

        // 4. 再查 ws:route:{userId}:{deviceId}
        if (receiverOnlineMembers != null) {
            for (ZSetOperations.TypedTuple<String> member : receiverOnlineMembers) {
                String deviceWithPlatform = member.getValue();
                if (StrUtil.isBlank(deviceWithPlatform)) {
                    continue;
                }

                String[] deviceAndPlatform = deviceWithPlatform.split("\\|", 2);
                String deviceId = deviceAndPlatform[0];
                // 暂时还没什么用，后续可以根据平台做差异化推送
                // String platform = deviceAndPlatform[1];

                routeKeys.add("ws:route:" + receiverId + ":" + deviceId);
            }
        }

        if (senderOnlineMembers != null) {
            for (ZSetOperations.TypedTuple<String> member : senderOnlineMembers) {
                String deviceWithPlatform = member.getValue();
                if (StrUtil.isBlank(deviceWithPlatform)) {
                    continue;
                }

                String[] deviceAndPlatform = deviceWithPlatform.split("\\|", 2);
                String deviceId = deviceAndPlatform[0];
                // String platform = deviceAndPlatform[1];

                routeKeys.add("ws:route:" + receiverId + ":" + deviceId);
            }
        }

        if (routeKeys.isEmpty()) return;
        List<String> routeValues = stringRedisTemplate.opsForValue().multiGet(routeKeys);
        if (routeValues == null || routeValues.isEmpty()) {
            // 在两次查 redis 的间隙连接断了，或者 route 和 online 的 ttl 有极其细微的时间差
            return;
        }

        Set<String> pushTargets = new HashSet<>();

        for (String routeValue : routeValues) {
            if (StrUtil.isBlank(routeValue)) {
                continue;
            }
            String[] routeParts = routeValue.split("\\|", 2);
            String gatewayId = routeParts[0];
            if (StrUtil.isBlank(gatewayId)) {
                continue;
            }
            pushTargets.add(gatewayId);
        }

        if (pushTargets.isEmpty()) return;

        MqPayload mqPayload = MqPayload.newBuilder()
                .setType(EventType.forNumber(message.getMsgType()))
                .setMsgId(message.getMsgId())
                .addUserIds(receiverId)
                .setSenderId(message.getSenderId())
                .setContent(ByteString.copyFrom(message.getContent(), StandardCharsets.UTF_8))
                .build();

        byte[] protobufBytes = mqPayload.toByteArray();

        Message amqpMessage = MessageBuilder.withBody(protobufBytes)
                // 告诉 MQ 和消费者，这是一坨二进制的 Protobuf 流，不是普通字符串
                .setContentType("application/x-protobuf")
                .build();

        for (String gatewayId : pushTargets) {
            rabbitTemplate.convertAndSend(
                    RabbitmqConfig.DIRECT_EXCHANGE,
                    gatewayId,
                    amqpMessage,
                    m -> {
                        // 强行修改为非持久化 (纯内存飞行，极致速度)
                        m.getMessageProperties().setDeliveryMode(MessageDeliveryMode.NON_PERSISTENT);
                        return m;
                    });
            log.info("消息 {} 发送到网关 {} 进行推送，载荷: {}", message.getMsgId(), gatewayId, mqPayload);
        }
    }

    @Override
    public List<SyncMessage> syncMessages(Long userId, Long cursor, int limit) {
        List<Timeline> timelines = timelineMapper.syncMessages(userId, cursor, limit);
        if (timelines == null || timelines.isEmpty()) {
            return List.of();
        }

        List<Long> msgIds = timelines.stream()
                .map(Timeline::getMsgId)
                .toList();

        List<Messages> messages = messagesMapper.selectByIds(msgIds);
        if (messages == null || messages.isEmpty()) {
            return List.of();
        }

        Map<Long, Messages> messageByMsgId = new HashMap<>(messages.size());
        for (Messages message : messages) {
            messageByMsgId.put(message.getMsgId(), message);
        }

        List<SyncMessage> result = new ArrayList<>(timelines.size());
        for (Timeline timeline : timelines) {
            Messages message = messageByMsgId.get(timeline.getMsgId());
            if (message == null) {
                continue;
            }

            Long groupId = Boolean.TRUE.equals(timeline.getIsGroup()) ? message.getReceiverId() : null;
            result.add(SyncMessage.builder()
                    .type(message.getMsgType())
                    .msgId(String.valueOf(message.getMsgId()))
                    .senderId(message.getSenderId())
                    .groupId(groupId)
                    .cursor(timeline.getSeqId())
                    .content(message.getContent())
                    .extraData(message.getExtraData())
                    .build());
        }

        return result;
    }

    public void sysKickOut(String gatewayId, Long userId, String deviceId) {

        // 构造踢人消息
        MqPayload kickOutPayload = MqPayload.newBuilder()
                .setType(EventType.SYS_KICK_OUT)
                .addUserIds(userId)
                .setContent(ByteString.copyFrom(deviceId, StandardCharsets.UTF_8))
                .build();

        byte[] protobufBytes = kickOutPayload.toByteArray();

        Message amqpMessage = MessageBuilder.withBody(protobufBytes)
                .setContentType("application/x-protobuf")
                .build();

        // 发送到指定网关
        rabbitTemplate.convertAndSend(
                RabbitmqConfig.DIRECT_EXCHANGE,
                gatewayId,
                amqpMessage);
        log.info("发送踢人消息到网关 {}，载荷: {}", gatewayId, kickOutPayload);
    }
}
