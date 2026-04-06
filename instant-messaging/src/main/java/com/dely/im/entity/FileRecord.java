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
 * @since 2026-04-06
 */
@Data
@TableName("file_record")
public class FileRecord implements Serializable {

    @Serial
    private static final long serialVersionUID = 1L;

    @TableId(value = "fileId", type = IdType.NONE)
    private Long fileId;

    private String fileHash;

    private String fileName;

    private Long fileSize;

    private String objectKey;

    private LocalDateTime createdAt;

}
