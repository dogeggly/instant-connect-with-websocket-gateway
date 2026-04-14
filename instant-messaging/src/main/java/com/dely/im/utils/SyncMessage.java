package com.dely.im.utils;

import lombok.Builder;
import lombok.Data;

import java.util.Map;

@Data
@Builder
public class SyncMessage {
    private Integer type;
    private String msgId;
    private Long senderId;
    private Long groupId;
    private Long cursor;
    private String content;
    private Map<String, Object> extraData;
}
