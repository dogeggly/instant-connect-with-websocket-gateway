package com.dely.im.service.impl;

import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import com.dely.im.entity.Messages;
import com.dely.im.mapper.MessagesMapper;
import com.dely.im.service.IMessagesService;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

import java.util.List;

/**
 * <p>
 *  服务实现类
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
@Service
public class MessagesServiceImpl extends ServiceImpl<MessagesMapper, Messages> implements IMessagesService {

    @Autowired
    private MessagesMapper messagesMapper;

    @Override
    public List<Messages> syncMessages(Long userId, Long cursor, int limit) {
         return messagesMapper.syncMessages(userId, cursor, limit);
    }
}
