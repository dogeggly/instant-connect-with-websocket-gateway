-- users: 用户信息表
CREATE TABLE IF NOT EXISTS users
(
    user_id       BIGINT PRIMARY KEY,                 -- 主键：用户唯一 ID（自增）
    username      VARCHAR(64)  NOT NULL,              -- 登录名
    password_hash VARCHAR(255) NOT NULL,              -- 加密后的密码哈希
    created_at    TIMESTAMP    NOT NULL DEFAULT NOW() -- 注册时间
);

-- contacts: 联系人/好友关系表（双向冗余）
-- 说明：A 加 B 时写入两条记录 (A,B) 与 (B,A)。
CREATE TABLE IF NOT EXISTS contacts
(
    owner_id   BIGINT    NOT NULL,               -- 联合主键1：所属用户 ID
    peer_id    BIGINT    NOT NULL,               -- 联合主键2：对方用户 ID
    PRIMARY KEY (owner_id, peer_id),             -- 联合主键自带索引
    alias_name VARCHAR(64),                      -- 备注名
    created_at TIMESTAMP NOT NULL DEFAULT NOW(), -- 成为好友时间
);

-- messages: 全局消息流水表
CREATE TABLE IF NOT EXISTS messages
(
    msg_id      BIGINT PRIMARY KEY,                 -- 主键：消息唯一 ID（雪花算法）
    chat_type   SMALLINT    NOT NULL,               -- 聊天类型：1-单聊，2-群聊
    sender_id   BIGINT      NOT NULL,               -- 发送方用户 ID
    receiver_id BIGINT,                             -- 接收方 ID（群聊可为空或存群 ID）
    msg_type    SMALLINT    NOT NULL,               -- 消息类型：1文本/2图片/3文件/4信令/5系统
    content JSONB NOT NULL,                         -- 消息载体：JSONB（适配多种消息结构）
    -- TODO uuid 该用什么数据结构存？
    req_id      VARCHAR(64) NOT NULL,               -- 防重放 Token（前端 UUID）
    created_at  TIMESTAMP   NOT NULL DEFAULT NOW(), -- 入库时间
    CHECK (chat_type IN (1, 2)),                    -- 聊天类型约束
    CHECK (msg_type IN (1, 2, 3, 4, 5))             -- 消息类型约束
);

-- user_sessions: 用户最近会话列表（用户私有）
-- 说明：A 删除自己的会话列表不会影响 B 的会话列表。
CREATE TABLE IF NOT EXISTS user_sessions
(
    user_id        BIGINT    NOT NULL,               -- 联合主键1：归属用户 ID
    peer_id        BIGINT    NOT NULL,               -- 联合主键2：聊天对象 ID（或群 ID）
    chat_type      SMALLINT  NOT NULL,               -- 聊天类型：1-单聊，2-群聊
    last_msg_id    BIGINT,                           -- 最后一条消息 ID
    last_msg_brief VARCHAR(255),                     -- 最后一条消息摘要（用于快速渲染）
    unread_count   INT       NOT NULL DEFAULT 0,     -- 未读数
    updated_at     TIMESTAMP NOT NULL DEFAULT NOW(), -- 更新时间（用于会话排序）
    PRIMARY KEY (user_id, peer_id),                  -- 联合主键
    CHECK (chat_type IN (1, 2)),                     -- 聊天类型约束
    CHECK (unread_count >= 0)                        -- 未读数不允许负数
);

-- sync_cursors: 多端同步漫游游标
-- 说明：每个用户每个设备独立维护最后拉取位置。
CREATE TABLE IF NOT EXISTS sync_cursors
(
    user_id      BIGINT      NOT NULL,               -- 联合主键1：用户 ID
    device_type  VARCHAR(32) NOT NULL,               -- 联合主键2：设备类型（Web/Android/iOS）
    last_pull_id BIGINT      NOT NULL DEFAULT 0,     -- 最后成功拉取到的 msg_id
    updated_at   TIMESTAMP   NOT NULL DEFAULT NOW(), -- 最后拉取时间
    PRIMARY KEY (user_id, device_type)               -- 联合主键
);
