package com.dely.im.controller;

import cn.hutool.core.lang.Snowflake;
import cn.hutool.core.util.StrUtil;
import com.dely.im.config.RabbitmqConfig;
import com.dely.im.utils.MqPayLoad;
import com.dely.im.utils.Result;
import com.dely.im.entity.Messages;
import com.dely.im.service.IMessagesService;
import com.dely.im.utils.SyncMessage;
import com.dely.im.utils.UserHolder;
import lombok.extern.slf4j.Slf4j;
import org.postgresql.util.PSQLException;
import org.postgresql.util.ServerErrorMessage;
import org.springframework.amqp.rabbit.core.RabbitTemplate;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.core.ZSetOperations;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.*;

import java.util.*;
import java.util.stream.Stream;

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
        if (message.getReceiverId() == null || message.getSenderId() == null || message.getContent() == null || message.getContent().isEmpty()) {
            return Result.fail("参数不完整，receiverId 、senderId、 content 必填");
        }

        String receiverId = String.valueOf(message.getReceiverId());

        // 1. 生成全局单调递增的 Message ID
        long msgId = snowflake.nextId();
        message.setMsgId(msgId);

        // 2. 幂等落库 (req_id 可能重复)
        try {
            iMessagesService.save(message);
        } catch (DataIntegrityViolationException e) {
            if (isReqIdDuplicate(e)) {
                log.warn("消息重复提交 reqId={}", message.getReqId());
                return Result.fail(409, "消息重复提交");
            }
        }

        // 3. 先查 ws:online:{userId}，拿到所有在线设备 deviceId|platform
        String onlineKey = "ws:online:" + receiverId;
        Set<ZSetOperations.TypedTuple<String>> onlineMembers = stringRedisTemplate.opsForZSet().rangeWithScores(onlineKey, 0, -1);
        if (onlineMembers == null || onlineMembers.isEmpty()) {
            // 说明对方离线，不做任何处理
            return Result.success();
        }

        Set<String> routeKeys = new HashSet<>();
        long nowTimestamp = System.currentTimeMillis() / 1000;

        // 4. 再查 ws:route:{userId}:{deviceId}，拿网关地址后直推 /api/push
        for (ZSetOperations.TypedTuple<String> member : onlineMembers) {
            Double score = member.getScore();
            if (score == null || score.longValue() < nowTimestamp) {
                // 网关挂了连接没删但是已过期
                continue;
            }

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

        List<String> routeValues = stringRedisTemplate.opsForValue().multiGet(routeKeys);
        if (routeValues == null || routeValues.isEmpty()) {
            // 在两次查 redis 的间隙连接断了，或者 route 和 online 的 ttl 有极其细微的时间差
            return Result.success();
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

        MqPayLoad pushPayload = MqPayLoad.builder().
                type(1).msgId(msgId).userId(receiverId).content(message.getContent()).build();

        for (String gatewayId : pushTargets) {
            rabbitTemplate.convertAndSend(
                    RabbitmqConfig.GATEWAY_EXCHANGE,
                    gatewayId,
                    pushPayload);
            log.info("消息 {} 发送到网关 {} 进行推送，载荷: {}", msgId, gatewayId, pushPayload);
        }

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

    @GetMapping("/sync")
    public Result<List<SyncMessage>> syncMessages(Long cursor, Integer limit) {
        if (cursor == null) {
            return Result.fail("参数不完整，userId 和 lastMsgId 必填");
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
                        .msgId(String.valueOf(message.getMsgId()))
                        .type(message.getMsgType())
                        .content(message.getContent()).build()
        ).toList();

        return Result.success(syncMessages);
    }
}

// TODO 离线消息处理