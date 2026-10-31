#!/usr/bin/env bash
# /tmp/schema.sql と DB の差分を表示し、--apply なら適用する。サーバー上で root として実行する
set -euo pipefail

mode=${1:-diff}
tmp_db="${DB_NAME}_migrate_tmp"

pg() {
  sudo -u postgres psql -q -v ON_ERROR_STOP=1 "$@"
}

# sqldef が解釈できない psql のメタコマンドと SET、計測用の拡張、パーティションの子テーブルを除く
# 子テーブルは比較しないので、パーティションの変更は手で行う
dump() {
  sudo -u postgres pg_dump --schema-only --no-owner --no-privileges "$1" |
    sed -E '/^.(un)?restrict /d; /^--/d; /^SET /d; /^SELECT pg_catalog\.set_config/d' |
    awk 'BEGIN { RS = ""; ORS = "\n\n" } !/image_objects_[0-9d]/ && !/ATTACH PARTITION/ && !/pg_stat_statements/'
}

pg -c "DROP DATABASE IF EXISTS $tmp_db" -c "CREATE DATABASE $tmp_db"
trap 'pg -c "DROP DATABASE IF EXISTS $tmp_db"' EXIT
pg -d "$tmp_db" > /dev/null < /tmp/schema.sql

dump "$DB_NAME" > /tmp/current.sql
dump "$tmp_db" > /tmp/desired.sql
migration=$(psqldef /tmp/current.sql < /tmp/desired.sql)
echo "$migration"

if [ "$mode" = --apply ]; then
  # postgres のまま作ると、新しいテーブルをアプリが読めない
  { echo "SET ROLE $DB_USER;"; grep -v '^-- ' <<< "$migration" || true; } | pg -d "$DB_NAME"
  echo "適用しました"
fi
