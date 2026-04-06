package com.dely.im.utils;

import lombok.Builder;
import lombok.Data;

import java.util.List;

@Data
@Builder
public class UploadFileVO {
    private Integer status;
    private String objectKey;
    private String uploadId;
    private List<Integer> partNumber;
}
