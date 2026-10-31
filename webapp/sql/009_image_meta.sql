-- image_objects は created_at でパーティションされているが、アプリは id でしか引かないので
-- 毎回 37 パーティションの index を全部探していた。本体 (data) 以外の情報を 1 つの表に持ち、
-- 本体を読むときは id と created_at の両方を渡して 1 パーティションに絞る
CREATE TABLE IF NOT EXISTS image_meta AS
  SELECT id, owner_user_id, media_type, origin_region, created_at, data IS NULL AS on_disk FROM image_objects;
CREATE UNIQUE INDEX IF NOT EXISTS image_meta_pkey ON image_meta (id);

-- 投稿・プロフィール画像・RealMoji の INSERT と、プロフィール画像の DELETE に追従する。
-- reset.sql はトリガーを止めて走るので、そちらは自前で image_meta を直す
CREATE OR REPLACE FUNCTION image_meta_sync() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    INSERT INTO image_meta (id, owner_user_id, media_type, origin_region, created_at, on_disk)
    VALUES (NEW.id, NEW.owner_user_id, NEW.media_type, NEW.origin_region, NEW.created_at, NEW.data IS NULL)
    ON CONFLICT (id) DO UPDATE SET owner_user_id = EXCLUDED.owner_user_id, media_type = EXCLUDED.media_type,
      origin_region = EXCLUDED.origin_region, created_at = EXCLUDED.created_at, on_disk = EXCLUDED.on_disk;
    RETURN NEW;
  END IF;
  DELETE FROM image_meta WHERE id = OLD.id;
  RETURN OLD;
END $$;
CREATE OR REPLACE TRIGGER image_meta_sync AFTER INSERT OR DELETE ON image_objects FOR EACH ROW EXECUTE FUNCTION image_meta_sync();
