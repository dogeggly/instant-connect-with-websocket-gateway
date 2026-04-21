package com.dely.im.controller;

import cn.hutool.core.lang.Snowflake;
import com.dely.im.utils.Result;
import com.dely.im.entity.Messages;
import com.dely.im.service.IMessagesService;
import com.dely.im.utils.SyncMessage;
import com.dely.im.utils.UserHolder;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.*;

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
    private Snowflake snowflake;

    @PostMapping("/send")
    public Result sendMessage(@RequestBody Messages message) {

        // 1. 生成全局单调递增的 Message ID
        long msgId = snowflake.nextId();
        message.setMsgId(msgId);

        message.setSenderId(UserHolder.getCurrent());

        // 2. 幂等落库 (req_id 可能重复)
        iMessagesService.saveMessage(message);

        return Result.success();
    }

    @PostMapping("/send/no-store")
    public Result sendMessageWithoutStore(@RequestBody Messages message) {

        long msgId = snowflake.nextId();
        message.setMsgId(msgId);

        message.setSenderId(UserHolder.getCurrent());

        iMessagesService.sendMessageWithoutStore(message);

        return Result.success();
    }

    @PostMapping("/send/group")
    public Result sendGroupMessage(@RequestBody Messages message) {

        long msgId = snowflake.nextId();
        message.setMsgId(msgId);

        message.setSenderId(UserHolder.getCurrent());

        iMessagesService.sendGroupMessage(message);

        return Result.success();
    }

    @GetMapping("/sync")
    public Result<List<SyncMessage>> syncMessages(Long cursor, Integer limit) {
        if (cursor == null || limit == null) {
            return Result.fail("参数不完整，cursor 和 limit 必填");
        }

        // 安全防御：硬性限制单次最多拉取条数，防止恶意客户端把内存打爆
        int maxLimit = Math.min(limit, 100);

        Long userId = UserHolder.getCurrent();

        // 去 PostgreSQL 查询增量消息
        List<SyncMessage> syncMessages = iMessagesService.syncMessages(userId, cursor, maxLimit);

        return Result.success(syncMessages);
    }

    @GetMapping("/tombstone")
    public Result<List<SyncMessage>> syncTombstoneMessages(List<Long> tombstoneIds) {
        Long userId = UserHolder.getCurrent();
        List<SyncMessage> syncMessages = iMessagesService.syncTombstoneMessages(userId, tombstoneIds);
        return Result.success(syncMessages);
    }

}