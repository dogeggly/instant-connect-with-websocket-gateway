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
@TableName("user_sessions")
public class UserSessions implements Serializable {

    @Serial
    private static final long serialVersionUID = 1L;

    private Long userId;

    private Long peerId;

    private Integer chatType;

    private Long lastMsgId;

    private String lastMsgBrief;

    private Integer unreadCount;

    private LocalDateTime updatedAt;

}
