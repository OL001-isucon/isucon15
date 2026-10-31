-- ベンチの解析で不足が分かったインデックス
-- 手で適用済みの環境でも流せるよう IF NOT EXISTS にしている
CREATE INDEX IF NOT EXISTS realmojis_image_id_reactor_id ON realmojis(image_id, reactor_id);
CREATE INDEX IF NOT EXISTS posts_image_id ON posts(image_id);
CREATE INDEX IF NOT EXISTS friendships_user_high_id ON friendships(user_high_id);
CREATE INDEX IF NOT EXISTS friend_requests_requester_recipient ON friend_requests(requester_id, recipient_id);
CREATE INDEX IF NOT EXISTS friend_requests_recipient_status ON friend_requests(recipient_id, status);
CREATE INDEX IF NOT EXISTS users_username_id ON users(username, id);
CREATE INDEX IF NOT EXISTS login_sessions_token_hash ON login_sessions(token_hash);
CREATE INDEX IF NOT EXISTS login_sessions_user_id ON login_sessions(user_id);
CREATE INDEX IF NOT EXISTS block_relations_blocked_id ON block_relations(blocked_id);
CREATE INDEX IF NOT EXISTS follows_followee_id ON follows(followee_id);
