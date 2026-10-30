#!/usr/bin/env bash
# 計測用ログの on / off。サーバー上で root として実行する。提出前に off にする
set -euo pipefail

mode=${1:?usage: logging.sh on|off}
ACCESS_LOG=${ACCESS_LOG:-/var/log/nginx/access_ltsv.log}
SLOW_LOG=${SLOW_LOG:-/var/log/mysql/mysql-slow.log}
NGINX_CONF=/etc/nginx/conf.d/00-isucon-ltsv.conf
MYSQL_CONF=/etc/mysql/conf.d/zz-isucon-slow.cnf

if command -v nginx >/dev/null; then
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

if command -v mysql >/dev/null; then
  if [ "$mode" = on ]; then
    cat > "$MYSQL_CONF" <<EOF
[mysqld]
slow_query_log = 1
slow_query_log_file = $SLOW_LOG
long_query_time = 0
EOF
    touch "$SLOW_LOG"
    chown mysql:adm "$SLOW_LOG"
    mysql -e "SET GLOBAL slow_query_log_file = '$SLOW_LOG'; SET GLOBAL long_query_time = 0; SET GLOBAL slow_query_log = 1;"
  else
    rm -f "$MYSQL_CONF"
    mysql -e "SET GLOBAL slow_query_log = 0;"
  fi
fi

echo "logging $mode"
