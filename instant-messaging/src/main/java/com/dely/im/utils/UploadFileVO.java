package com.dely.im.utils;

import lombok.Builder;
import lombok.Data;

import java.util.List;

@Data
@Builder
public class UploadFileVO {
    // 1：正在初始化，2：正在上传，3：完成
    private Integer status;
    private String objectKey;
    private String uploadId;
    private List<UploadFilePartInfo> uploadFilePartInfoList;
}
