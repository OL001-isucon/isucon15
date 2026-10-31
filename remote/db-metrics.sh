#!/usr/bin/env bash
# ベンチ中の DB サーバーの CPU / IO と、PostgreSQL の待ち状態を 1 秒ごとに記録する。サーバー上で実行する
# DB が CPU、IO、ロックのどれで詰まっているかを、pg_stat_statements と合わせて判断するために使う
# usage: db-metrics.sh seconds
set -euo pipefail

seconds=${1:?usage: db-metrics.sh seconds}
DB_NAME=${DB_NAME:-isureal}

rm -f /tmp/db-vmstat.txt /tmp/db-waits.txt /tmp/db-activity.txt

# 1 行目は起動からの平均なので、集計では読み飛ばす
vmstat -n -t 1 "$seconds" > /tmp/db-vmstat.txt &

# 「時刻 待ちの種類 セッション数」を 1 秒ごとに書く。Lock が多ければロック待ち、IO が多ければディスク待ち
for ((i = 0; i < seconds; i++)); do
  sudo -u postgres psql -qAt -F ' ' -d "$DB_NAME" -c "
    SELECT to_char(clock_timestamp(), 'HH24:MI:SS'), coalesce(wait_event_type, 'running'), count(*)
    FROM pg_stat_activity
    WHERE datname = '$DB_NAME' AND state <> 'idle' AND pid <> pg_backend_pid()
    GROUP BY 2 ORDER BY 2
  " >> /tmp/db-waits.txt || true
  # 接続を握っている処理を調べるため、状態と処理中のクエリごとの本数と、最長のトランザクション時間を書く
  sudo -u postgres psql -qAt -F '|' -d "$DB_NAME" -c "
    SELECT to_char(clock_timestamp(), 'HH24:MI:SS'), state, coalesce(wait_event_type, 'running'), count(*),
           round(max(extract(epoch FROM clock_timestamp() - xact_start)) * 1000),
           left(regexp_replace(query, '\s+', ' ', 'g'), 100)
    FROM pg_stat_activity
    WHERE datname = '$DB_NAME' AND state <> 'idle' AND pid <> pg_backend_pid()
    GROUP BY 2, 3, 6
  " >> /tmp/db-activity.txt || true
  sleep 1
done
wait
