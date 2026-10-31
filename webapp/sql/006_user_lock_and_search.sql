-- ユーザー名の一意性をインデックスに任せ、登録時のテーブルロックをなくす
CREATE UNIQUE INDEX IF NOT EXISTS users_username_unique ON users(username);
-- 検索で全ユーザーを読まないよう、Go で正規化した検索用の文字列を持つ。値は初期化で作る
ALTER TABLE users ADD COLUMN IF NOT EXISTS search_text text;
