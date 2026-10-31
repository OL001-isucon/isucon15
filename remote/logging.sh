#!/usr/bin/env bash
# 計測用ログの on / off。サーバー上で root として実行する。提出前に off にする
set -euo pipefail

mode=${1:?usage: logging.sh on|off}
ACCESS_LOG=${ACCESS_LOG:-/var/log/nginx/access_ltsv.log}
DB_NAME=${DB_NAME:-isureal}
NGINX_CONF=/etc/nginx/conf.d/00-isucon-ltsv.conf
PG_SERVICE=postgresql@16-main
PG_CONF=/etc/postgresql/16/main/conf.d/zz-isucon-stats.conf

pg() {
  sudo -u postgres psql -qAt -d "$DB_NAME" -c "$1"
}

# 使っていないサーバーにもパッケージは入っているので、動いているサービスだけを切り替える
if systemctl is-active -q nginx; then
  if [ "$mode" = on ]; then
    cat > "$NGINX_CONF" <<EOF
log_format ltsv "time:\$time_local"
                "\thost:\$remote_addr"
                "\tforwardedfor:\$http_x_forwarded_for"
                "\treq:\$request"
                "\tstatus:\$status"
                "\tmethod:\$request_method"
                "\turi:\$request_uri"
                "\tsize:\$body_bytes_sent"
                "\treferer:\$http_referer"
                "\tua:\$http_user_agent"
                "\treqtime:\$request_time"
                "\tapptime:\$upstream_response_time"
                "\tvhost:\$host";
access_log $ACCESS_LOG ltsv;
EOF
  else
    rm -f "$NGINX_CONF"
  fi
  nginx -t -q
  systemctl reload nginx
fi

# スロークエリログは bytea の画像をログへ書くので、pg_stat_statements で集計する
if systemctl is-active -q "$PG_SERVICE"; then
  loaded=$(pg 'SHOW shared_preload_libraries')
  if [ "$mode" = on ]; then
    echo "shared_preload_libraries = 'pg_stat_statements'" > "$PG_CONF"
    # 読み込みには再起動が必要なので、読み込み済みなら再起動しない
    case "$loaded" in
      *pg_stat_statements*) ;;
      *) systemctl restart "$PG_SERVICE" ;;
    esac
    pg 'CREATE EXTENSION IF NOT EXISTS pg_stat_statements'
  else
    pg 'DROP EXTENSION IF EXISTS pg_stat_statements'
    rm -f "$PG_CONF"
    case "$loaded" in
      *pg_stat_statements*) systemctl restart "$PG_SERVICE" ;;
    esac
  fi
fi

echo "logging $mode"
