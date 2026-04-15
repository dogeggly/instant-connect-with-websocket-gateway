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
CREATE INDEX IF NOT EXISTS trgm_idx_users_username ON users USING gin (username gin_trgm_ops);


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
    req_id      UUID      NOT NULL,              -- 防重放 Token（前端 UUID）
    created_at  TIMESTAMP NOT NULL DEFAULT NOW() -- 入库时间
);
-- 保证接口幂等，防止前端重试导致重复入库
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_req_id ON messages (req_id);


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


CREATE TABLE IF NOT EXISTS file_record
(
    file_id    BIGINT PRIMARY KEY,                 -- 采用 Snowflake ID 方案
    file_hash  VARCHAR(64)  NOT NULL,              -- 文件的唯一标识 (通常为 SHA256 或 MD5)
    file_name  VARCHAR(255) NOT NULL,              -- 文件原始名称 (如：design_v1.mp4)
    file_size  BIGINT       NOT NULL,              -- 文件大小 (字节)
    object_key VARCHAR(255) NOT NULL,              -- OSS 中的相对路径或对象 Key (如：2026/04/06/hash.mp4)
    created_at TIMESTAMP    NOT NULL DEFAULT NOW() -- 入库时间
);
-- 核心约束：保证同一个文件（通过 Hash 判定）在库中只有一条记录，这是实现“秒传”的关键
CREATE UNIQUE INDEX IF NOT EXISTS idx_file_record_file_hash ON file_record (file_hash);


CREATE TABLE IF NOT EXISTS timeline
(
    owner_id   BIGINT    NOT NULL,  -- 信箱的主人（分库分表的 Sharding Key）
    seq_id     BIGINT    NOT NULL,  -- 该用户的全局连续自增游标（拉取消息的绝对凭证）
    PRIMARY KEY (owner_id, seq_id), -- 联合主键，天然防重且自带聚簇索引排序
    msg_id     BIGINT    NOT NULL,  -- 关联全局消息表的外键（信件单号）
    is_group   BOOLEAN   NOT NULL,  -- 是否为群消息（true: 群消息，false: 单聊消息）
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_timeline_msg_id ON timeline (msg_id);


CREATE TABLE IF NOT EXISTS timeline_task
(
    msg_id      BIGINT PRIMARY KEY, -- 直接用消息ID作为主键
    sender_id   BIGINT,             -- 发送方用户 ID（如果是单聊也要存进发送者的邮箱里）
    receiver_id BIGINT    NOT NULL, -- 投递接收方（单聊为接收方，群聊为群ID）
    status      BOOLEAN   NOT NULL, -- true:待处理, false:处理中
    is_group    BOOLEAN   NOT NULL, -- 是否为群消息（true: 群消息，false: 单聊消息）
    created_at  TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_timeline_task_status ON timeline_task (status);


CREATE SEQUENCE IF NOT EXISTS groups_group_id_seq START WITH 10001;
CREATE TABLE IF NOT EXISTS groups
(
    group_id   BIGINT PRIMARY KEY    DEFAULT NEXTVAL('groups_group_id_seq'), -- 群组唯一 ID（雪花算法）
    group_name VARCHAR(128) NOT NULL,                                        -- 群名称
    owner_id   BIGINT       NOT NULL,                                        -- 群主 ID
    group_type SMALLINT              DEFAULT 1,                              -- 群类型（1普通群，2大群/广播群等）
    created_at TIMESTAMP    NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_group_name ON groups (group_name);
CREATE INDEX IF NOT EXISTS trgm_idx_groups_group_name ON groups USING gin (group_name gin_trgm_ops);


CREATE TABLE IF NOT EXISTS group_members
(
    group_id   BIGINT    NOT NULL,             -- 关联群 ID
    user_id    BIGINT    NOT NULL,             -- 群成员的用户 ID
    PRIMARY KEY (group_id, user_id),           -- 联合主键，防止重复加群，同时天然形成针对 group_id 的聚簇索引
    created_at TIMESTAMP NOT NULL DEFAULT NOW()-- 加入时间

);
-- 作用：用户在断网重连时，前端需要拉取“我加入了哪些群”，这个索引是必用的。
CREATE INDEX IF NOT EXISTS idx_group_members_user_id ON group_members (user_id);