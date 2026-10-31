-- 新規の画像の本体はリージョンごとのディスクに置き、DB には持ち主などの情報だけを入れる
-- 初期画像は data に本体があり、origin_region は NULL のまま
ALTER TABLE image_objects ALTER COLUMN data DROP NOT NULL;
ALTER TABLE image_objects ADD COLUMN IF NOT EXISTS origin_region text;
