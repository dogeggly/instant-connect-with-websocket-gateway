-- users.username 唯一索引：
-- 用于登录名去重与按用户名极速检索。
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username
    ON users (username);

-- messages.req_id 唯一索引：
-- 保证接口幂等，防止前端重试导致重复入库。
CREATE UNIQUE INDEX IF NOT EXISTS uk_messages_req_id
    ON messages (req_id);

-- messages(receiver_id, msg_id) 游标拉取索引（核心）：
-- 典型查询：WHERE receiver_id = ? AND msg_id > ?
CREATE INDEX IF NOT EXISTS idx_messages_receiver_msg
    ON messages (receiver_id, msg_id);

-- messages(session_id, msg_id DESC) 会话历史索引：
-- 打开聊天窗口时，按时间倒序快速读取最近 N 条消息。
CREATE INDEX IF NOT EXISTS idx_messages_session_msg
    ON messages (session_id, msg_id DESC);

-- user_sessions(user_id, updated_at DESC) 会话列表索引：
-- 支撑“某用户会话列表按最近更新时间倒序”的高频查询。
CREATE INDEX IF NOT EXISTS idx_usersessions_user_update
    ON user_sessions (user_id, updated_at DESC);
