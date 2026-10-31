#!/usr/bin/env bash
# SCHEMA_FILES と DB の差分を sqldef で確認・適用する。省略時は差分の表示だけ
# reset.sql はデータしか戻さないので、スキーマの変更はベンチの /initialize では反映されない
#
# スキーマの先頭に DROP TABLE があるので本番の DB には流さない。一時 DB へ流し、pg_dump 同士を比較する
# usage: scripts/migrate.sh [--apply]
source "$(dirname "$0")/lib.sh"

cd "$ROOT"

mode=${1:-diff}
cat "${SCHEMA_FILES[@]}" | on "$DB_HOST" "cat > /tmp/schema.sql"
on "$DB_HOST" "sudo DB_NAME=$DB_NAME DB_USER=$DB_USER bash -s -- $mode" < "$ROOT/remote/migrate.sh"
