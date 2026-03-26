package com.dely.im.service;

import com.baomidou.mybatisplus.extension.service.IService;
import com.dely.im.entity.Messages;

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

    List<Messages> syncMessages(Long userId, Long cursor, int limit);
}
