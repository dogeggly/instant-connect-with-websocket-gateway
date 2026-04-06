package com.dely.im.controller;

import cn.hutool.core.lang.Snowflake;
import cn.hutool.core.util.StrUtil;
import com.dely.im.config.RabbitmqConfig;
import com.dely.im.pb.EventType;
import com.dely.im.pb.MqPayload;
import com.dely.im.utils.Result;
import com.dely.im.entity.Messages;
import com.dely.im.service.IMessagesService;
import com.dely.im.utils.SyncMessage;
import com.dely.im.utils.UserHolder;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.google.protobuf.ByteString;
import lombok.extern.slf4j.Slf4j;
import org.postgresql.util.PSQLException;
import org.postgresql.util.ServerErrorMessage;
import org.springframework.amqp.core.Message;
import org.springframework.amqp.core.MessageBuilder;
import org.springframework.amqp.rabbit.core.RabbitTemplate;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.core.ZSetOperations;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.*;

import java.nio.charset.StandardCharsets;
import java.util.*;

/**
 * <p>
 * 前端控制器
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
@Slf4j
@RestController
@RequestMapping("/messages")
public class MessagesController {

    @Autowired
    private IMessagesService iMessagesService;

    @Autowired
    private StringRedisTemplate stringRedisTemplate;

    @Autowired
    private RabbitTemplate rabbitTemplate;

    @Autowired
    private Snowflake snowflake;

    @PostMapping("/send")
    public Result sendMessage(@RequestBody Messages message) {

        // 1. 生成全局单调递增的 Message ID
        long msgId = snowflake.nextId();
        message.setMsgId(msgId);

        message.setSenderId(UserHolder.getCurrent());

        // 2. 幂等落库 (req_id 可能重复)
        try {
            iMessagesService.save(message);
        } catch (DataIntegrityViolationException e) {
            if (isReqIdDuplicate(e)) {
                log.warn("消息重复提交 reqId={}", message.getReqId());
                return Result.fail(409, "消息重复提交");
            }
        }

        pushMessageToOnlineGateways(message);

        return Result.success();
    }

    private boolean isReqIdDuplicate(Throwable throwable) {
        Throwable current = throwable;
        while (current != null) {
            // 匹配 PG 底层异常
            if (current instanceof PSQLException pgException) {
                // 1. 确认是唯一约束冲突 (SQLState 23505)
                if ("23505".equals(pgException.getSQLState())) {
                    // 2. 获取 PG 服务端结构化错误信息
                    ServerErrorMessage serverError = pgException.getServerErrorMessage();
                    if (serverError != null) {
                        // 3. 精准提取发生冲突的约束/索引名，彻底告别字符串截取！
                        String constraintName = serverError.getConstraint();
                        // 4. 精确比对
                        return "idx_messages_req_id".equals(constraintName);
                    }
                }
            }
            current = current.getCause();
        }
        return false;
    }

    @PostMapping("/send/no-store")
    public Result sendMessageWithoutStore(@RequestBody Messages message) {

        long msgId = snowflake.nextId();
        message.setMsgId(msgId);

        message.setSenderId(UserHolder.getCurrent());

        pushMessageToOnlineGateways(message);

        return Result.success();
    }

    private void pushMessageToOnlineGateways(Messages message) {

        Long receiverId = message.getReceiverId();

        // 3. 先查 ws:online:{userId}，拿到所有在线设备 deviceId|platform
        String onlineKey = "ws:online:" + receiverId;
        long nowTimestamp = System.currentTimeMillis() / 1000;
        Set<ZSetOperations.TypedTuple<String>> onlineMembers =
                stringRedisTemplate.opsForZSet()
                        .rangeByScoreWithScores(onlineKey, nowTimestamp, Double.POSITIVE_INFINITY);
        if (onlineMembers == null || onlineMembers.isEmpty()) {
            // 说明对方离线，不做任何处理
            return;
        }

        Set<String> routeKeys = new HashSet<>();

        // 4. 再查 ws:route:{userId}:{deviceId}
        for (ZSetOperations.TypedTuple<String> member : onlineMembers) {
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
                .setUserId(receiverId)
                .setSenderId(message.getSenderId())
                .setContent(ByteString.copyFrom(message.getContent(), StandardCharsets.UTF_8)).build();

        byte[] protobufBytes = mqPayload.toByteArray();

        Message amqpMessage = MessageBuilder.withBody(protobufBytes)
                // 告诉 MQ 和消费者，这是一坨二进制的 Protobuf 流，不是普通字符串
                .setContentType("application/x-protobuf")
                .build();

        for (String gatewayId : pushTargets) {
            rabbitTemplate.convertAndSend(
                    RabbitmqConfig.GATEWAY_EXCHANGE,
                    gatewayId,
                    amqpMessage);
            log.info("消息 {} 发送到网关 {} 进行推送，载荷: {}", message.getMsgId(), gatewayId, mqPayload);
        }
    }

    private void sysKickOut(String gatewayId, Long userId, String deviceId) {

        // 构造踢人消息
        MqPayload kickOutPayload = MqPayload.newBuilder()
                .setType(EventType.SYS_KICK_OUT)
                .setUserId(userId)
                .setContent(ByteString.copyFrom(deviceId, StandardCharsets.UTF_8))
                .build();

        byte[] protobufBytes = kickOutPayload.toByteArray();

        Message amqpMessage = MessageBuilder.withBody(protobufBytes)
                .setContentType("application/x-protobuf")
                .build();

        // 发送到指定网关
        rabbitTemplate.convertAndSend(
                RabbitmqConfig.GATEWAY_EXCHANGE,
                gatewayId,
                amqpMessage);
        log.info("发送踢人消息到网关 {}，载荷: {}", gatewayId, kickOutPayload);
    }

    @GetMapping("/sync")
    public Result<List<SyncMessage>> syncMessages(Long cursor, Integer limit) {
        if (cursor == null || limit == null) {
            return Result.fail("参数不完整，cursor 和 limit 必填");
        }

        // 安全防御：硬性限制单次最多拉取条数，防止恶意客户端把内存打爆
        int maxLimit = Math.min(limit, 100);

        Long userId = UserHolder.getCurrent();

        // 1. 去 PostgreSQL 查询增量消息
        // 对应 SQL: SELECT * FROM message_store WHERE receiver_id = #{userId} AND msg_id > #{cursor} ORDER BY msg_id ASC LIMIT #{limit}
        List<Messages> messages = iMessagesService.syncMessages(userId, cursor, maxLimit);

        // 2. 如果没查到新消息，直接返回空
        if (messages == null || messages.isEmpty()) {
            return Result.success(Collections.emptyList());
        }

        List<SyncMessage> syncMessages = messages.stream().map(message ->
                SyncMessage.builder()
                        .msgId(message.getMsgId().toString())
                        .type(message.getMsgType())
                        .senderId(message.getSenderId().toString())
                        .content(message.getContent()).build()
        ).toList();

        return Result.success(syncMessages);
    }
}