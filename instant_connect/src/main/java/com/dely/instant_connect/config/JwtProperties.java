package com.dely.instant_connect.config;

import io.jsonwebtoken.Claims;
import io.jsonwebtoken.Jwts;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import javax.crypto.spec.SecretKeySpec;
import java.nio.charset.StandardCharsets;
import java.util.Map;

/**
 * 没有设置过期时间方便测试
 */
@Component
public class JwtProperties {

    @Value("${jwt.secret-key}")
    private String jwtSecretKey;
    
    public String createJWT(Map<String, Object> claims) {

        //创建密钥即Key对象
        SecretKeySpec secretKeySpec = new SecretKeySpec(jwtSecretKey.getBytes(StandardCharsets.UTF_8), "HmacSHA256");

        return Jwts.builder()
                .setClaims(claims)
                // 设置签名使用的签名秘钥
                .signWith(secretKeySpec)
                .compact();
    }

    public Claims parseJWT(String token) {

        //创建密钥即Key对象
        SecretKeySpec secretKeySpec = new SecretKeySpec(jwtSecretKey.getBytes(StandardCharsets.UTF_8), "HmacSHA256");

        return Jwts.parserBuilder()
                .setSigningKey(secretKeySpec)
                .build()
                .parseClaimsJws(token)
                .getBody();
    }
}
