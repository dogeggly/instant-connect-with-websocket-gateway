package com.dely.instant_connect.controller;

import cn.hutool.core.util.StrUtil;
import cn.hutool.crypto.digest.DigestUtil;
import com.dely.instant_connect.config.JwtProperties;
import com.dely.instant_connect.config.Result;
import com.dely.instant_connect.entity.Users;
import com.dely.instant_connect.service.IUsersService;
import org.postgresql.util.PSQLException;
import org.postgresql.util.ServerErrorMessage;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;

import org.springframework.stereotype.Controller;

import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * <p>
 * 前端控制器
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
@Controller
@RequestMapping("/users")
public class UsersController {

    @Autowired
    private IUsersService iUsersService;

    @Autowired
    private JwtProperties jwtProperties;

    /**
     * 注册
     */
    @PostMapping("/register")
    public Result register(@RequestBody Users users) {
        String username = users.getUsername();
        String password = users.getPasswordHash();
        if (StrUtil.isBlank(username) || StrUtil.isBlank(password)) {
            return Result.fail("前端传参错误，用户名和密码是必填项");
        }

        boolean hasUser = iUsersService.lambdaQuery().eq(Users::getUsername, username).exists();
        if (hasUser) return Result.fail("用户已存在，换个用户名");

        users.setPasswordHash(DigestUtil.md5Hex(password));
        // 仍然会有并发写入风险
        try {
            iUsersService.save(users);
        } catch (DataIntegrityViolationException e) {
            if (isUsernameDuplicate(e)) {
                return Result.fail(409, "用户已存在，换个用户名");
            }
        }

        return Result.success();
    }

    /**
     * 登录
     */
    @PostMapping("/login")
    public Result<String> login(@RequestBody Users users) {
        String username = users.getUsername();
        String password = users.getPasswordHash();
        if (StrUtil.isBlank(username) || StrUtil.isBlank(password)) {
            return Result.fail("前端传参错误，用户名和密码是必填项");
        }

        Users user = iUsersService.lambdaQuery().eq(Users::getUsername, username).one();

        if (user == null) return Result.fail("用户未注册");

        if (!user.getPasswordHash().equals(DigestUtil.md5Hex(password)))
            return Result.fail("密码错误");

        Map<String, Object> claims = new HashMap<>();
        claims.put("userId", user.getUserId());
        String jwt = jwtProperties.createJWT(claims);
        // 返回 token
        return Result.success(jwt);
    }

    /**
     * 根据昵称搜索用户
     */
    @GetMapping("/search")
    public Result<List<String>> findByNickname(String username) {
        // 没有加入分页逻辑，可以让前端传上一个相似度，然后查10条大于这个相似度的数据
        List<Users> userList = iUsersService.findByUsername(username);
        return Result.success(userList.stream().map(Users::getUsername).toList());
    }

    private boolean isUsernameDuplicate(Throwable throwable) {
        Throwable current = throwable;
        while (current != null) {
            // 匹配 PG 底层异常
            if (current instanceof PSQLException pgException) {
                // 1. 确认是唯一约束冲突 (SQLState 23505)
                if ("23505".equals(pgException.getSQLState())) {
                    // 2. 获取 PG 服务端结构化错误信息
                    ServerErrorMessage serverError = pgException.getServerErrorMessage();
                    if (serverError != null) {
                        // 3. 精准提取发生冲突的约束/索引名，彻底告别字符串截取！
                        String constraintName = serverError.getConstraint();
                        // 4. 精确比对
                        return "idx_users_username".equals(constraintName);
                    }
                }
            }
            current = current.getCause();
        }
        return false;
    }

}
