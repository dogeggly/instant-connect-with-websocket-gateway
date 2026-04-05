-- users: 用户信息表
-- 创建用户 ID 自增序列，起始值为 10001
CREATE SEQUENCE IF NOT EXISTS users_user_id_seq START WITH 10001;
CREATE TABLE IF NOT EXISTS users
(
    user_id       BIGINT PRIMARY KEY    DEFAULT NEXTVAL('users_user_id_seq'), -- 主键：用户唯一 ID（自增）
    username      VARCHAR(64)  NOT NULL,                                      -- 登录名
    password_hash VARCHAR(255) NOT NULL,                                      -- 加密后的密码哈希
    created_at    TIMESTAMP    NOT NULL DEFAULT NOW()                         -- 注册时间
);
-- 登录名去重与按用户名极速检索
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users (username);
-- gin索引的三元组模型扩展
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX trgm_idx_users_username ON users USING gin (username gin_trgm_ops);


-- contacts: 联系人/好友关系表（双向冗余）
-- 说明：A 加 B 时写入两条记录 (A,B) 与 (B,A)
CREATE TABLE IF NOT EXISTS contacts
(
    owner_id   BIGINT    NOT NULL,              -- 联合主键1：所属用户 ID
    peer_id    BIGINT    NOT NULL,              -- 联合主键2：对方用户 ID
    PRIMARY KEY (owner_id, peer_id),            -- 联合主键自带索引
    alias_name VARCHAR(64),                     -- 备注名
    created_at TIMESTAMP NOT NULL DEFAULT NOW() -- 成为好友时间
);


-- messages: 全局消息流水表
CREATE TABLE IF NOT EXISTS messages
(
    msg_id      BIGINT PRIMARY KEY,              -- 主键：消息唯一 ID（雪花算法）
    sender_id   BIGINT    NOT NULL,              -- 发送方用户 ID
    receiver_id BIGINT    NOT NULL,              -- 接收方 ID（群聊为群 ID）
    msg_type    SMALLINT  NOT NULL,              -- 消息类型：1文本/2图片/3文件/4信令/5系统
    content     TEXT      NOT NULL,              -- 消息载体
    extra_data  JSONB,                           -- 扩展字段：JSONB（预留，灵活存储额外信息）
    req_id      uuid      NOT NULL,              -- 防重放 Token（前端 UUID）
    created_at  TIMESTAMP NOT NULL DEFAULT NOW() -- 入库时间
);
-- 保证接口幂等，防止前端重试导致重复入库
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_req_id ON messages (req_id);
-- 典型查询：WHERE receiver_id = ? AND msg_id > ?
CREATE INDEX IF NOT EXISTS idx_messages_receiver_msg ON messages (receiver_id, msg_id);


-- 以下表由前端维护
/*-- user_sessions: 用户最近会话列表（用户私有）
CREATE TABLE IF NOT EXISTS user_sessions
(
    user_id        BIGINT    NOT NULL,               -- 联合主键1：归属用户 ID
    peer_id        BIGINT    NOT NULL,               -- 联合主键2：聊天对象 ID（或群 ID）
    PRIMARY KEY (user_id, peer_id),                  -- 联合主键
    chat_type      SMALLINT  NOT NULL,               -- 聊天类型：1-单聊，2-群聊
    last_msg_id    BIGINT,                           -- 最后一条消息 ID
    last_msg_brief VARCHAR(255),                     -- 最后一条消息摘要（用于快速渲染）
    unread_count   INT       NOT NULL DEFAULT 0,     -- 未读数
    updated_at     TIMESTAMP NOT NULL DEFAULT NOW(), -- 更新时间（用于会话排序）
    CHECK (chat_type IN (1, 2))                      -- 聊天类型约束
);
-- 支撑“某用户会话列表按最近更新时间倒序”的高频查询。
CREATE INDEX IF NOT EXISTS idx_user_sessions_user_update ON user_sessions (user_id, updated_at DESC);


-- sync_cursors: 多端同步漫游游标
-- 说明：每个用户每个设备独立维护最后拉取位置
CREATE TABLE IF NOT EXISTS sync_cursors
(
    user_id      BIGINT      NOT NULL,              -- 联合主键1：用户 ID
    device_id    VARCHAR(64) NOT NULL,              -- 联合主键2：设备 ID
    PRIMARY KEY (user_id, device_id),               -- 联合主键
    last_pull_id BIGINT      NOT NULL DEFAULT 0,    -- 最后成功拉取到的 msg_id
    updated_at   TIMESTAMP   NOT NULL DEFAULT NOW() -- 最后拉取时间
);*/