package com.dely.instant_connect.service;

import com.dely.instant_connect.entity.Users;
import com.baomidou.mybatisplus.extension.service.IService;

import java.util.List;

/**
 * <p>
 *  服务类
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
public interface IUsersService extends IService<Users> {

    List<Users> findByUsername(String username);
}
