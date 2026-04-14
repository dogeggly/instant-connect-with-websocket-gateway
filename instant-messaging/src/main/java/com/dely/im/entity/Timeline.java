package com.dely.im.entity;

import com.baomidou.mybatisplus.annotation.TableName;

import java.io.Serial;
import java.time.LocalDateTime;
import java.io.Serializable;
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
