-- 利用者登録の LOCK TABLE users を外す代わりに、username の一意性を DB の制約で守る
-- 初期データに重複が無いことは 2026-10-31 に asia2 で確認済み (5,091 人、重複 0)
CREATE UNIQUE INDEX IF NOT EXISTS users_username_key ON users(username);
