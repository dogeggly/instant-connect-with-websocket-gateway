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
@TableName("sync_cursors")
public class SyncCursors implements Serializable {

    @Serial
    private static final long serialVersionUID = 1L;

    private Long userId;

    private String deviceId;

    private Long lastPullId;

    private LocalDateTime updatedAt;

}
