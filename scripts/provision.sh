#!/usr/bin/env bash
# 計測ツールを入れて、計測用ログを有効にする。-s を省略すると全サーバーへ並列で実行する
# PostgreSQL の再起動を伴うので、ベンチの実行中には流さない
# usage: scripts/provision.sh [-s host]
source "$(dirname "$0")/lib.sh"

provision() {
  local host=$1
  on "$host" "sudo bash -s" < "$ROOT/remote/provision.sh"
  on "$host" "sudo ACCESS_LOG=$ACCESS_LOG DB_NAME=$DB_NAME bash -s on" < "$ROOT/remote/logging.sh"
}

run_parallel provision "${HOSTS[@]}"
