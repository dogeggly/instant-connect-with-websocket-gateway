package com.dely.im.entity;

import com.baomidou.mybatisplus.annotation.TableName;
import com.baomidou.mybatisplus.annotation.IdType;

import java.io.Serial;
import java.time.LocalDateTime;
import com.baomidou.mybatisplus.annotation.TableId;
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
@TableName("groups")
public class Groups implements Serializable {

    @Serial
    private static final long serialVersionUID = 1L;

    @TableId(value = "group_id", type = IdType.AUTO)
    private Long groupId;

    private String groupName;

    private Long ownerId;

    private Integer groupType;

    private LocalDateTime createdAt;


}
