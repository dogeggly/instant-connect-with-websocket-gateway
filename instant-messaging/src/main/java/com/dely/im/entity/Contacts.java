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
 * @since 2026-03-22
 */
@Data
@TableName("contacts")
public class Contacts implements Serializable {

    @Serial
    private static final long serialVersionUID = 1L;

    private Long ownerId;

    private Long peerId;

    private String aliasName;

    private LocalDateTime createdAt;

}
