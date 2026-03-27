package com.dely.im.utils;

import lombok.Builder;
import lombok.Data;

import java.util.Map;

@Data
@Builder
public class SyncMessage {
    private Integer type;
    private String msgId;
    private String senderId;
    private Map<String, Object> content;
}
