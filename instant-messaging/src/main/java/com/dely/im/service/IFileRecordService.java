package com.dely.im.service;

import com.dely.im.entity.FileRecord;
import com.baomidou.mybatisplus.extension.service.IService;

/**
 * <p>
 *  服务类
 * </p>
 *
 * @author dely
 * @since 2026-04-06
 */
public interface IFileRecordService extends IService<FileRecord> {

    void saveFile(FileRecord fileRecord);
}
