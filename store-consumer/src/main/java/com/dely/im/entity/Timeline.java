package com.dely.im.entity;

import com.baomidou.mybatisplus.annotation.TableName;

import java.io.Serial;
import java.time.LocalDateTime;
import java.io.Serializable;

import lombok.Builder;
import lombok.Data;

/**
 * <p>
 * 
 * </p>
 *
 * @author dely
 * @since 2026-04-15
 */
@Data
@Builder
@TableName("timeline")
public class Timeline implements Serializable {

    @Serial
    private static final long serialVersionUID = 1L;

    private Long ownerId;

    private Long seqId;

    private Long msgId;

    private Boolean isGroup;

    private LocalDateTime createdAt;

}
