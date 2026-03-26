package com.dely.im.service.impl;

import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import com.dely.im.entity.Contacts;
import com.dely.im.mapper.ContactsMapper;
import com.dely.im.service.IContactsService;
import org.springframework.stereotype.Service;

/**
 * <p>
 * 服务实现类
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
@Service
public class ContactsServiceImpl extends ServiceImpl<ContactsMapper, Contacts> implements IContactsService {

}
