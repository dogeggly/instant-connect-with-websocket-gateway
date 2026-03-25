package com.dely.instant_connect.config;

import org.apache.ibatis.type.BaseTypeHandler;
import org.apache.ibatis.type.JdbcType;
import org.apache.ibatis.type.MappedTypes;

import java.sql.CallableStatement;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.util.UUID;

@MappedTypes(UUID.class)
public class PgUuidTypeHandler extends BaseTypeHandler<UUID> {
    @Override
    public void setNonNullParameter(PreparedStatement ps, int i, UUID parameter, JdbcType jdbcType) throws SQLException {
        // 【核心】PostgreSQL 的 JDBC 驱动要求 uuid 必须当作 Types.OTHER 传进去
        ps.setObject(i, parameter, java.sql.Types.OTHER);
    }

    @Override
    public UUID getNullableResult(ResultSet rs, String columnName) throws SQLException {
        return (UUID) rs.getObject(columnName);
    }

    @Override
    public UUID getNullableResult(ResultSet rs, int columnIndex) throws SQLException {
        return (UUID) rs.getObject(columnIndex);
    }

    @Override
    public UUID getNullableResult(CallableStatement cs, int columnIndex) throws SQLException {
        return (UUID) cs.getObject(columnIndex);
    }
}
