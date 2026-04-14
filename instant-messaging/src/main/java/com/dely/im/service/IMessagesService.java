package com.dely.im.service;

import com.baomidou.mybatisplus.extension.service.IService;
import com.dely.im.entity.Messages;
import com.dely.im.utils.SyncMessage;

import java.util.List;

/**
 * <p>
 *  服务类
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
public interface IMessagesService extends IService<Messages> {

    List<SyncMessage> syncMessages(Long userId, Long cursor, int limit);

    void saveMessage(Messages message);

    void sendMessageWithoutStore(Messages message);

    void sendGroupMessage(Messages message);
}
