package com.dely.instant_connect.entity;

import com.baomidou.mybatisplus.annotation.TableName;
import com.baomidou.mybatisplus.annotation.IdType;

import java.io.Serial;
import java.time.LocalDateTime;
import com.baomidou.mybatisplus.annotation.TableId;
import java.io.Serializable;
import lombok.Data;
import lombok.EqualsAndHashCode;
import lombok.experimental.Accessors;

/**
 * <p>
 * 
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
@Data
@TableName("messages")
public class Messages implements Serializable {

    @Serial
    private static final long serialVersionUID = 1L;

    @TableId(value = "msg_id", type = IdType.NONE)
    private Long msgId;

    private Integer chatType;

    private Long senderId;

    private Long receiverId;

    private Integer msgType;

    private String content;

    private String reqId;

    private LocalDateTime createdAt;

}
