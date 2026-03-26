package com.dely.im.utils;

import lombok.Builder;
import lombok.Data;

import java.util.Map;

@Data
@Builder
public class MqPayLoad {
    private Integer type;
    private Long msgId;
    private String userId;
    private Map<String, Object> content;
}
