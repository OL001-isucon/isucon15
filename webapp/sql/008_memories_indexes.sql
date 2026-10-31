-- memories は「著者の投稿のうち、期間が指定の月・日のもの」を引く。投稿期間を日付で絞ってから
-- 著者 + 投稿期間の複合 index で投稿を引けるようにし、著者の全投稿を読まないようにする
CREATE INDEX IF NOT EXISTS posting_windows_local_date ON posting_windows (local_date);
CREATE INDEX IF NOT EXISTS posts_author_id_posting_window_id ON posts (author_id, posting_window_id);
