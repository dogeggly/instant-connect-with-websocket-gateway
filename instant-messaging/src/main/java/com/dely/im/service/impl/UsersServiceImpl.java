package com.dely.im.service.impl;

import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import com.dely.im.entity.Users;
import com.dely.im.mapper.UsersMapper;
import com.dely.im.service.IUsersService;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

import java.util.List;

/**
 * <p>
 * 服务实现类
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
@Service
public class UsersServiceImpl extends ServiceImpl<UsersMapper, Users> implements IUsersService {

    @Autowired
    private UsersMapper usersMapper;

    @Override
    public List<Users> searchByNickname(String username) {
        return usersMapper.searchByNickname(username);
    }
}
