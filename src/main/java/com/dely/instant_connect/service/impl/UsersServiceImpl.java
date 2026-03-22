package com.dely.instant_connect.service.impl;

import com.dely.instant_connect.entity.Users;
import com.dely.instant_connect.mapper.UsersMapper;
import com.dely.instant_connect.service.IUsersService;
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
public class UsersServiceImpl extends ServiceImpl<UsersMapper, Users> implements IUsersService {

}
