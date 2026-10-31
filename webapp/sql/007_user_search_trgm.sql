-- ユーザー検索を部分一致のまま index で引けるようにする。LIKE '%q%' が trigram index を使う
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS users_search_text_trgm ON users USING gin (search_text gin_trgm_ops);
-- search_text が未設定の行数を数える副問い合わせを、空の部分 index で済ませる
CREATE INDEX IF NOT EXISTS users_search_text_null ON users (id) WHERE search_text IS NULL;
