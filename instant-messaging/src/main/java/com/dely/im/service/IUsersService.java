package com.dely.im.service;

import com.baomidou.mybatisplus.extension.service.IService;
import com.dely.im.entity.Users;

import java.util.List;

/**
 * <p>
 * 服务类
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
public interface IUsersService extends IService<Users> {

    List<Users> searchByNickname(String username);
}
