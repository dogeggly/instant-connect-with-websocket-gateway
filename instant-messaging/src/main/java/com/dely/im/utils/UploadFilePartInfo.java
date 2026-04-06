package com.dely.im.utils;

import lombok.Builder;
import lombok.Data;

@Data
@Builder
public class UploadFilePartInfo {
    private Integer partNumber;
    private String partUrl;
    private String eTag;
}
