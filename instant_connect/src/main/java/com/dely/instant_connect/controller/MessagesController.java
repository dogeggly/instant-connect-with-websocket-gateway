package com.dely.instant_connect.controller;

import cn.hutool.core.lang.Snowflake;
import cn.hutool.core.util.StrUtil;
import com.dely.instant_connect.config.Result;
import com.dely.instant_connect.entity.Messages;
import com.dely.instant_connect.service.IMessagesService;
import com.dely.instant_connect.config.PushGatewayClient;
import lombok.extern.slf4j.Slf4j;
import org.postgresql.util.PSQLException;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.core.ZSetOperations;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

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
    private PushGatewayClient pushGatewayClient;

    @Autowired
    private Snowflake snowflake;

    @PostMapping("/send")
    public Result<Long> sendMessage(@RequestBody Messages message) {
        if (message.getReceiverId() == null || message.getSenderId() == null || StrUtil.isBlank(message.getContent())) {
            return Result.fail("参数不完整，receiverId 、senderId、 content 必填");
        }

        String receiverId = String.valueOf(message.getReceiverId());

        // 1. 生成全局单调递增的 Message ID
        long msgId = snowflake.nextId();
        message.setMsgId(msgId);

        // 2. 幂等落库 (req_id 可能重复)
        try {
            iMessagesService.save(message);
        } catch (DuplicateKeyException e) {
            if (isReqIdDuplicate(e)) {
                log.warn("消息重复提交 reqId={}", message.getReqId());
                return Result.fail(409, "消息重复提交");
            }
            throw e;
        }

        // 3. 先查 ws:online:{userId}，拿到所有在线设备 deviceId|platform
        String onlineKey = "ws:online:" + receiverId;
        Set<ZSetOperations.TypedTuple<String>> onlineMembers = stringRedisTemplate.opsForZSet().rangeWithScores(onlineKey, 0, -1);
        if (onlineMembers != null && !onlineMembers.isEmpty()) {
            // TODO 还没处理离线状态
            return Result.fail(404, "接收方离线");
        }

        Set<String> routeKeys = new HashSet<>();

        // 4. 再查 ws:route:{userId}:{deviceId}，拿网关地址后直推 /api/push
        for (ZSetOperations.TypedTuple<String> member : onlineMembers) {
            String deviceWithPlatform = member.getValue();
            if (StrUtil.isBlank(deviceWithPlatform)) {
                continue;
            }

            String[] deviceAndPlatform = deviceWithPlatform.split("\\|", 2);
            String deviceId = deviceAndPlatform[0];
            // 暂时还没什么用，后续可以根据平台做差异化推送
            String platform = deviceAndPlatform[1];

            routeKeys.add("ws:route:" + receiverId + ":" + deviceId);
        }

        List<String> routeValues = stringRedisTemplate.opsForValue().multiGet(routeKeys);
        if (routeValues == null || routeValues.isEmpty()) {
            // TODO 同上
            return Result.fail(404, "接收方路由不存在");
        }

        Set<String> pushTargets = new HashSet<>();

        for (String routeValue : routeValues) {
            if (StrUtil.isBlank(routeValue)) {
                continue;
            }
            String[] routeParts = routeValue.split("\\|", 2);
            String gatewayAddress = routeParts[0];
            if (StrUtil.isBlank(gatewayAddress)) {
                continue;
            }
            pushTargets.add(gatewayAddress);
        }

        for (String gatewayAddress : pushTargets) {
            pushGatewayClient.push(gatewayAddress, message.getContent());
        }

        return Result.success(msgId);
    }

    private boolean isReqIdDuplicate(Throwable throwable) {
        Throwable current = throwable;
        while (current != null) {
            if (current instanceof PSQLException pgException) {
                String sqlState = pgException.getSQLState();
                if ("23505".equals(sqlState)) {
                    String message = pgException.getMessage();
                    return message != null && message.contains("idx_messages_req_id");
                }
            }
            current = current.getCause();
        }
        return false;
    }
}

// TODO 审查GlobalExceptionHandler，PushGatewayClient，SnowflakeConfig
