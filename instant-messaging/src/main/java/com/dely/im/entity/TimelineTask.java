package com.dely.im.entity;

import com.baomidou.mybatisplus.annotation.TableName;
import com.baomidou.mybatisplus.annotation.IdType;

import java.io.Serial;
import java.time.LocalDateTime;
import com.baomidou.mybatisplus.annotation.TableId;
import java.io.Serializable;

import lombok.Builder;
import lombok.Data;

/**
 * <p>
 * 
 * </p>
 *
 * @author dely
 * @since 2026-04-14
 */
@Data
@Builder
@TableName("timeline_task")
public class TimelineTask implements Serializable {

    @Serial
    private static final long serialVersionUID = 1L;

    @TableId(value = "msg_id", type = IdType.NONE)
    private Long msgId;

    private Long receiverId;

    private Boolean status;

    private Boolean isGroup;

    private LocalDateTime createdAt;


}
