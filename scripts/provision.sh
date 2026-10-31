#!/usr/bin/env bash
# 計測ツールを入れて、計測用ログを有効にする。-s を省略すると全サーバーへ並列で実行する
# usage: scripts/provision.sh [-s host]
source "$(dirname "$0")/lib.sh"

log_dir=$(mktemp -d)

provision() {
  local host=$1
  on "$host" "sudo SLOW_LOG=$SLOW_LOG bash -s" < "$ROOT/remote/provision.sh"
  on "$host" "sudo ACCESS_LOG=$ACCESS_LOG SLOW_LOG=$SLOW_LOG bash -s on" < "$ROOT/remote/logging.sh"
}

pids=()
for host in "${HOSTS[@]}"; do
  provision "$host" > "$log_dir/$host.log" 2>&1 &
  pids+=($!)
done

status=0
for i in "${!HOSTS[@]}"; do
  host=${HOSTS[$i]}
  if wait "${pids[$i]}"; then
    echo "== $host: ok"
  else
    echo "== $host: FAILED"
    status=1
  fi
  tail -n 6 "$log_dir/$host.log"
done

exit "$status"
