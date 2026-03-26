package com.dely.im.entity;

import com.baomidou.mybatisplus.annotation.TableField;
import com.baomidou.mybatisplus.annotation.TableName;
import com.baomidou.mybatisplus.annotation.IdType;

import java.io.Serial;
import java.time.LocalDateTime;

import com.baomidou.mybatisplus.annotation.TableId;

import java.io.Serializable;
import java.util.Map;
import java.util.UUID;

import com.dely.im.utils.PgJsonbTypeHandler;
import com.dely.im.utils.PgUuidTypeHandler;
import lombok.Data;

/**
 * <p>
 *
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
@Data
@TableName(value = "messages", autoResultMap = true)
public class Messages implements Serializable {

    @Serial
    private static final long serialVersionUID = 1L;

    @TableId(value = "msg_id", type = IdType.NONE)
    private Long msgId;

    private Integer chatType;

    private Long senderId;

    private Long receiverId;

    private Integer msgType;

    @TableField(typeHandler = PgJsonbTypeHandler.class)
    private Map<String, Object> content;

    @TableField(typeHandler = PgUuidTypeHandler.class)
    private UUID reqId;

    private LocalDateTime createdAt;

}
