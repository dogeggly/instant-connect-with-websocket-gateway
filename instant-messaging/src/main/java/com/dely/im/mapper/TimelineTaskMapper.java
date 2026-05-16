package com.dely.im.mapper;

import com.dely.im.entity.TimelineTask;
import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import org.apache.ibatis.annotations.Mapper;
import org.apache.ibatis.annotations.Select;

import java.time.LocalDateTime;
import java.util.List;

/**
 * <p>
 * Mapper 接口
 * </p>
 *
 * @author dely
 * @since 2026-04-14
 */
@Mapper
public interface TimelineTaskMapper extends BaseMapper<TimelineTask> {

    List<TimelineTask> harvest();

    @Select("SELECT * FROM timeline_task WHERE status = 1 AND update_at < #{compareTime}")
    List<TimelineTask> processingHarvest(LocalDateTime compareTime);
}
