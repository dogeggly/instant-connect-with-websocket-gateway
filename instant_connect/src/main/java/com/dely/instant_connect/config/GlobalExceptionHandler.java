package com.dely.instant_connect.config;

import lombok.extern.slf4j.Slf4j;
import org.postgresql.util.PSQLException;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

@Slf4j
@RestControllerAdvice
public class GlobalExceptionHandler {
    private static final String PG_UNIQUE_VIOLATION_SQL_STATE = "23505";

    @ExceptionHandler({DuplicateKeyException.class, DataIntegrityViolationException.class})
    public Result<String> handleDuplicateInsert(Exception e) {
        if (isPgUniqueViolation(e)) {
            log.warn("并发导致的唯一约束冲突: {}", e.getMessage());
            return Result.fail(409, "用户已存在，换个用户名");
        }
        log.error("数据写入失败:", e);
        return Result.fail("数据写入失败" + e.getMessage());
    }

    @ExceptionHandler
    public Result<String> handleException(Exception e) {
        log.error("服务器异常:", e);
        return Result.fail("服务器异常:" + e.getMessage());
    }

    private boolean isPgUniqueViolation(Throwable throwable) {
        Throwable current = throwable;
        while (current != null) {
            if (current instanceof PSQLException) {
                String sqlState = ((PSQLException) current).getSQLState();
                return PG_UNIQUE_VIOLATION_SQL_STATE.equals(sqlState);
            }
            current = current.getCause();
        }
        return false;
    }

}
