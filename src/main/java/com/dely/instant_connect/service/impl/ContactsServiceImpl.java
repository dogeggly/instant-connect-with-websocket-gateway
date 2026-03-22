package com.dely.instant_connect.service.impl;

import com.dely.instant_connect.entity.Contacts;
import com.dely.instant_connect.mapper.ContactsMapper;
import com.dely.instant_connect.service.IContactsService;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import org.springframework.stereotype.Service;

/**
 * <p>
 *  服务实现类
 * </p>
 *
 * @author dely
 * @since 2026-03-22
 */
@Service
public class ContactsServiceImpl extends ServiceImpl<ContactsMapper, Contacts> implements IContactsService {

}
