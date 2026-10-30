#!/usr/bin/env bash
# SCHEMA_FILE と DB の差分を sqldef で確認・適用する。省略時は差分の表示だけ
# /initialize がスキーマを作り直す問題では、ファイルを直せばベンチ開始時に反映される。
# このスクリプトはベンチを待たずに反映したいときに使う
#
# sqldef は問題のスキーマやトリガーを解釈できないことがあるので、DB には接続させない。
# スキーマを一時 DB へ流し、トリガーを除いた mysqldump 同士を sqldef のファイル比較に渡す
# usage: scripts/migrate.sh [--apply]
source "$(dirname "$0")/lib.sh"

cd "$ROOT"

tmp_db="${DB_NAME}_migrate_tmp"
dump="mysqldump --no-data --skip-triggers --skip-comments --compact --no-tablespaces"

on "$DB_HOST" "sudo mysql -e 'DROP DATABASE IF EXISTS $tmp_db; CREATE DATABASE $tmp_db;'"

# USE が残ると一時 DB ではなく本番の DB を作り直してしまう
sed -e '/^[[:space:]]*[Uu][Ss][Ee][[:space:]]/d' "$SCHEMA_FILE" | on "$DB_HOST" "sudo mysql $tmp_db"

on "$DB_HOST" "
  set -e
  sudo $dump $DB_NAME | grep -v '^/\*!' > /tmp/current.sql
  sudo $dump $tmp_db | grep -v '^/\*!' > /tmp/desired.sql
  sudo mysql -e 'DROP DATABASE $tmp_db;'
  mysqldef /tmp/current.sql < /tmp/desired.sql > /tmp/migrate.sql
  cat /tmp/migrate.sql
"

if [ "${1:-}" = "--apply" ]; then
  on "$DB_HOST" "grep -v '^-- ' /tmp/migrate.sql | sudo mysql $DB_NAME"
  echo "適用しました"
fi
