package com.dely.im.mapper;

import com.dely.im.entity.Timeline;
import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import org.apache.ibatis.annotations.Mapper;

import java.util.List;

/**
 * <p>
 * Mapper 接口
 * </p>
 *
 * @author dely
 * @since 2026-04-15
 */
@Mapper
public interface TimelineMapper extends BaseMapper<Timeline> {

    void insertBatch(List<Timeline> timelines);
}
