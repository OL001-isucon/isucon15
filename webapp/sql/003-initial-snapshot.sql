CREATE TABLE IF NOT EXISTS initial_data_boundary AS
SELECT greatest(
  (SELECT max(created_at) FROM users),
  (SELECT max(created_at) FROM image_objects),
  (SELECT max(created_at) FROM posting_windows),
  (SELECT max(created_at) FROM posts),
  (SELECT max(created_at) FROM realmojis),
  (SELECT max(created_at) FROM comments),
  (SELECT max(created_at) FROM notifications)
) AS created_at;

CREATE TABLE IF NOT EXISTS initial_users AS SELECT * FROM users;
CREATE TABLE IF NOT EXISTS initial_friend_requests AS SELECT * FROM friend_requests;
CREATE TABLE IF NOT EXISTS initial_friendships AS SELECT * FROM friendships;
CREATE TABLE IF NOT EXISTS initial_follows AS SELECT * FROM follows;
CREATE TABLE IF NOT EXISTS initial_block_relations AS SELECT * FROM block_relations;
