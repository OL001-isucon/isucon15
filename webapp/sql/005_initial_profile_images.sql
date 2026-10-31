-- プロフィール画像の差し替えと削除は image_objects の行を消す。投入元が無いので、初期画像の控えを持つ
CREATE TABLE IF NOT EXISTS initial_profile_images AS
SELECT i.id, i.owner_user_id, i.data, i.media_type, i.created_at
FROM image_objects i JOIN initial_users u ON u.profile_image_id = i.id;
