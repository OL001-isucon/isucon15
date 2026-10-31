#!/bin/bash

set -eu
cd -- "$(dirname -- "$0")"

if test -f /home/isucon/env.sh; then
	. /home/isucon/env.sh
fi

ISUREAL_DB_HOST=${ISUREAL_DB_HOST:-127.0.0.1}
ISUREAL_DB_PORT=${ISUREAL_DB_PORT:-5432}
ISUREAL_DB_USER=${ISUREAL_DB_USER:-isureal}
ISUREAL_DB_PASSWORD=${ISUREAL_DB_PASSWORD:-isureal}
ISUREAL_DB_NAME=${ISUREAL_DB_NAME:-isureal}

export PGPASSWORD="$ISUREAL_DB_PASSWORD"
# 既存の定義を飛ばすたびに出る NOTICE を抑える
export PGOPTIONS='-c client_min_messages=warning'
PSQL=(
	psql
	--no-psqlrc
	--no-password
	--host="$ISUREAL_DB_HOST"
	--port="$ISUREAL_DB_PORT"
	--username="$ISUREAL_DB_USER"
	--dbname="$ISUREAL_DB_NAME"
	--set=ON_ERROR_STOP=1
	--quiet
)

# 番号付きの SQL を名前順に毎回すべて流す。初期データの投入元は無いので、DROP は絶対に書かない
# 2 回目以降は何も変えないよう、どのファイルも IF NOT EXISTS などで書く
for file in [0-9]*.sql; do
	"${PSQL[@]}" --single-transaction --file="$file"
done

"${PSQL[@]}" --file=reset.sql
