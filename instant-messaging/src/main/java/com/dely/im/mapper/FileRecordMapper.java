package com.dely.im.mapper;

import com.dely.im.entity.FileRecord;
import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import org.apache.ibatis.annotations.Insert;
import org.apache.ibatis.annotations.Mapper;

/**
 * <p>
 * Mapper 接口
 * </p>
 *
 * @author dely
 * @since 2026-04-06
 */
@Mapper
public interface FileRecordMapper extends BaseMapper<FileRecord> {

    @Insert("insert into file_record values (#{fileId}, #{filehash}, #{fileName}, #{fileSize}, #{objectKey}) on conflict (file_hash) do nothing")
    void saveFile(FileRecord fileRecord);
}
