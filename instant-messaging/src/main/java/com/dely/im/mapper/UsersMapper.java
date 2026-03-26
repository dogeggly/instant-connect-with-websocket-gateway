package com.dely.im.mapper;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import com.dely.im.entity.Users;
import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Select;

import java.util.List;

/**
 * <p>
 * Mapper 接口
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
@Mapper
public interface UsersMapper extends BaseMapper<Users> {

    @Select("select * from users " +
            "where username like concat('%', #{username}, '%') or username % #{username} " +
            "order by (username like concat('%', #{username}, '%')) desc, " +
            "similarity(username, #{username}) desc " +
            "limit 10")
    List<Users> searchByNickname(String username);
}
