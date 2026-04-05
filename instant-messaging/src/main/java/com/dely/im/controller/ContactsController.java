package com.dely.im.controller;

import cn.hutool.core.util.StrUtil;
import com.dely.im.entity.Contacts;
import com.dely.im.entity.Users;
import com.dely.im.service.IContactsService;
import com.dely.im.service.IUsersService;
import com.dely.im.utils.Result;
import com.dely.im.utils.UserHolder;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.PutMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;

import org.springframework.web.bind.annotation.RestController;

import java.util.Collections;
import java.util.List;

/**
 * <p>
 * 前端控制器
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
@RestController
@RequestMapping("/contacts")
public class ContactsController {

    @Autowired
    private IUsersService iUsersService;

    @Autowired
    private IContactsService iContactsService;

    @PostMapping
    public Result addFriend(@RequestBody Contacts contact) {
        if (contact == null || contact.getPeerId() == null) {
            return Result.fail("参数不完整，peerId 必填");
        }

        Long userId = UserHolder.getCurrent();
        if (userId.equals(contact.getPeerId())) {
            return Result.fail("ownerId 和 peerId 不能相同");
        }

        contact.setOwnerId(userId);
        Users peer = iUsersService.lambdaQuery().eq(Users::getUserId, contact.getPeerId()).one();
        contact.setAliasName(peer.getUsername());

        boolean saved = iContactsService.save(contact);
        return saved ? Result.success() : Result.fail("新增联系人失败");
    }

    @DeleteMapping
    public Result deleteFriend(Long peerId) {
        if (peerId == null) {
            return Result.fail("参数不完整，peerId 必填");
        }

        Long userId = UserHolder.getCurrent();
        boolean removed = iContactsService.lambdaUpdate()
                .eq(Contacts::getOwnerId, userId)
                .eq(Contacts::getPeerId, peerId)
                .remove();

        return removed ? Result.success() : Result.fail("联系人不存在或删除失败");
    }

    @PutMapping
    public Result updateAlias(Long peerId, String aliasName) {
        if (peerId == null) {
            return Result.fail("参数不完整，peerId 必填");
        }

        if (StrUtil.isBlank(aliasName)) {
            return Result.fail("别名不能为空");
        }

        Long userId = UserHolder.getCurrent();
        boolean updated = iContactsService.lambdaUpdate()
                .eq(Contacts::getOwnerId, userId)
                .eq(Contacts::getPeerId, peerId)
                .set(Contacts::getAliasName, aliasName)
                .update();

        return updated ? Result.success() : Result.fail("联系人不存在或更新失败");
    }

    @GetMapping
    public Result<Contacts> getContact(Long peerId) {
        if (peerId == null) {
            return Result.fail("参数不完整，peerId 必填");
        }

        Long userId = UserHolder.getCurrent();
        Contacts contact = iContactsService.lambdaQuery()
                .eq(Contacts::getOwnerId, userId)
                .eq(Contacts::getPeerId, peerId)
                .one();

        if (contact == null) {
            return Result.fail(404, "联系人不存在");
        }

        return Result.success(contact);
    }

    @GetMapping("/list")
    public Result<List<Contacts>> listContacts() {
        Long userId = UserHolder.getCurrent();
        List<Contacts> contacts = iContactsService.lambdaQuery()
                .eq(Contacts::getOwnerId, userId)
                .list();

        if (contacts == null || contacts.isEmpty()) {
            return Result.success(Collections.emptyList());
        }
        return Result.success(contacts);
    }

}
