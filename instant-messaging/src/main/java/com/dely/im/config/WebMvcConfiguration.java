package com.dely.im.config;

import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Component;
import org.springframework.web.servlet.config.annotation.InterceptorRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;

@Slf4j
@Component
public class WebMvcConfiguration implements WebMvcConfigurer {

    @Autowired
    private TokenInterceptor tokenInterceptor;

    public void addInterceptors(InterceptorRegistry registry) {
        log.info("开始注册自定义拦截器...");
        registry.addInterceptor(tokenInterceptor)
                // 调试时开启
                // .excludePathPatterns("/**")
                .addPathPatterns("/**")
                .excludePathPatterns("/users/register")
                .excludePathPatterns("/users/login")
                .excludePathPatterns("users/refresh");
    }
}
