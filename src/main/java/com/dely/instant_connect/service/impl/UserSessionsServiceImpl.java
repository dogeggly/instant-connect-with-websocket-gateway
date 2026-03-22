package com.dely.instant_connect.service.impl;

import com.dely.instant_connect.entity.UserSessions;
import com.dely.instant_connect.mapper.UserSessionsMapper;
import com.dely.instant_connect.service.IUserSessionsService;
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
public class UserSessionsServiceImpl extends ServiceImpl<UserSessionsMapper, UserSessions> implements IUserSessionsService {

}
