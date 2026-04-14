package com.dely.im.mapper;

import com.dely.im.entity.Timeline;
import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Select;

import java.util.List;

/**
 * <p>
 * Mapper 接口
 * </p>
 *
 * @author dely
 * @since 2026-04-14
 */
@Mapper
public interface TimelineMapper extends BaseMapper<Timeline> {

    @Select("select seq_id, msg_id from timeline where owner_id = #{userid} and seq_id > #{cursor} order by seq_id desc limit #{limit}")
    List<Timeline> syncMessages(Long userId, Long cursor, int limit);
}
