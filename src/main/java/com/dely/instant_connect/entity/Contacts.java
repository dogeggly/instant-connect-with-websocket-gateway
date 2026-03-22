package com.dely.instant_connect.entity;

import com.baomidou.mybatisplus.annotation.TableName;
import com.baomidou.mybatisplus.annotation.IdType;
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
@EqualsAndHashCode(callSuper = false)
@Accessors(chain = true)
@TableName("contacts")
public class Contacts implements Serializable {

    private static final long serialVersionUID = 1L;

    private Long ownerId;

    @TableId(value = "owner_id", type = IdType.NONE)
    private Long ownerId;

    private Long peerId;

    private String aliasName;

    private LocalDateTime createdAt;


}
