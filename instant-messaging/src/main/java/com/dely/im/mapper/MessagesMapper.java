package com.dely.im.mapper;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import com.dely.im.entity.Messages;
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
public interface MessagesMapper extends BaseMapper<Messages> {

    @Select("select * from messages where receiver_id = #{userId} and msg_id > #{cursor} limit #{limit}")

    List<Messages> syncMessages(Long userId, Long cursor, int limit);
}
