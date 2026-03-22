package com.dely.instant_connect.service.impl;

import com.dely.instant_connect.entity.Messages;
import com.dely.instant_connect.mapper.MessagesMapper;
import com.dely.instant_connect.service.IMessagesService;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import org.springframework.stereotype.Service;

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

}
