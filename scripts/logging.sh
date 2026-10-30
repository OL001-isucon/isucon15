#!/usr/bin/env bash
# 計測用ログを全サーバーで切り替える。提出前に off にする
# usage: scripts/logging.sh on|off
source "$(dirname "$0")/lib.sh"

mode=${1:?usage: scripts/logging.sh on|off}

for host in "${HOSTS[@]}"; do
  echo "== $host"
  on "$host" "sudo ACCESS_LOG=$ACCESS_LOG SLOW_LOG=$SLOW_LOG bash -s $mode" < "$ROOT/remote/logging.sh"
done
