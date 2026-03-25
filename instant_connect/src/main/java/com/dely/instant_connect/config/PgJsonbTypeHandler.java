package com.dely.instant_connect.config;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.apache.ibatis.type.BaseTypeHandler;
import org.apache.ibatis.type.JdbcType;
import org.postgresql.util.PGobject;

import java.sql.CallableStatement;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.SQLException;

public class PgJsonbTypeHandler extends BaseTypeHandler<Object> {
    private static final ObjectMapper mapper = new ObjectMapper();
    private final Class<?> type;

    // 必须有这个构造函数，MyBatis 会把实体类中字段的具体类型传进来
    public PgJsonbTypeHandler(Class<?> type) {
        if (type == null) {
            throw new IllegalArgumentException("Type argument cannot be null");
        }
        this.type = type;
    }

    @Override
    public void setNonNullParameter(PreparedStatement ps, int i, Object parameter, JdbcType jdbcType) throws SQLException {
        // 【核心逻辑】：构建 PostgreSQL 专属的 PGobject，并指定类型为 jsonb
        PGobject pgObject = new PGobject();
        pgObject.setType("jsonb");
        try {
            // 将 Java 对象序列化为 JSON 字符串，塞入 PGobject
            pgObject.setValue(mapper.writeValueAsString(parameter));
        } catch (JsonProcessingException e) {
            throw new SQLException("将对象转换为 JSON 字符串失败", e);
        }
        // 告诉 JDBC 使用对象方式插入
        ps.setObject(i, pgObject);
    }

    @Override
    public Object getNullableResult(ResultSet rs, String columnName) throws SQLException {
        return parseJson(rs.getString(columnName));
    }

    @Override
    public Object getNullableResult(ResultSet rs, int columnIndex) throws SQLException {
        return parseJson(rs.getString(columnIndex));
    }

    @Override
    public Object getNullableResult(CallableStatement cs, int columnIndex) throws SQLException {
        return parseJson(cs.getString(columnIndex));
    }

    private Object parseJson(String json) throws SQLException {
        if (json == null || json.trim().isEmpty()) {
            return null;
        }
        try {
            // 将查询出来的 JSON 字符串反序列化为具体的 Java 对象
            return mapper.readValue(json, type);
        } catch (JsonProcessingException e) {
            throw new SQLException("将 JSON 字符串转换为对象失败", e);
        }
    }
}
