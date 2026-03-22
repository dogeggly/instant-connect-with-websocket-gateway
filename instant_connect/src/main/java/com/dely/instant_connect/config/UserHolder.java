package com.dely.instant_connect.config;

public class UserHolder {
    private static final ThreadLocal<Long> CURRENT_LOCAL = new ThreadLocal<>();

    public static void setCurrent(Long userId) {
        CURRENT_LOCAL.set(userId);
    }

    public static Long getCurrent() {
        return CURRENT_LOCAL.get();
    }

    public static void remove() {
        CURRENT_LOCAL.remove();
    }
}
