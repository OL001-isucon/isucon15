# 各スクリプトの先頭で読み込む共通処理
set -euo pipefail

# Git Bash がリモート用のパスを Windows 形式に変換しないようにする
export MSYS_NO_PATHCONV=1

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=../conf.sh
source "$ROOT/conf.sh"

on() {
  local host=$1
  shift
  ssh -F "$ROOT/ssh_config" "$host" "$@"
}

# 関数 $1 を残りの引数の各ホストに並列で実行する。1 台でも失敗すると 1 を返す
run_parallel() {
  local func=$1 log_dir status=0 i
  shift
  local hosts=("$@") pids=()
  log_dir=$(mktemp -d)
  for i in "${!hosts[@]}"; do
    "$func" "${hosts[$i]}" > "$log_dir/${hosts[$i]}.log" 2>&1 &
    pids+=($!)
  done
  for i in "${!hosts[@]}"; do
    if wait "${pids[$i]}"; then
      echo "== ${hosts[$i]}: ok"
    else
      echo "== ${hosts[$i]}: FAILED"
      status=1
    fi
    tail -n 6 "$log_dir/${hosts[$i]}.log"
  done
  rm -rf "$log_dir"
  return "$status"
}

# -s <host> は指定した 1 台だけを対象にする
# 引数なしの source なので、shift は呼び出し元のスクリプトの引数に効く
if [ "${1:-}" = "-s" ]; then
  HOSTS=("${2:?usage: $0 -s <host> ...}")
  case " ${APP_HOSTS[*]} " in
    *" ${HOSTS[0]} "*) APP_HOSTS=("${HOSTS[0]}") ;;
    *) APP_HOSTS=() ;;
  esac
  case " ${WEB_HOSTS[*]} " in
    *" ${HOSTS[0]} "*) WEB_HOSTS=("${HOSTS[0]}") ;;
    *) WEB_HOSTS=() ;;
  esac
  shift 2
fi
