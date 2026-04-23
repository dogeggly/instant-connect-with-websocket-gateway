package com.dely.im.mapper;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import com.dely.im.entity.Messages;
import org.apache.ibatis.annotations.Insert;
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

    @Insert("insert into messages values (#{msgId}, #{senderId}, #{receiverId}, #{msgType}, #{content}, #{extraData, typeHandler=com.dely.im.utils.PgJsonbTypeHandler}, #{reqId, typeHandler=com.dely.im.utils.PgUuidTypeHandler}) on conflict (req_id) do nothing")
    void saveMessage(Messages message);
}
