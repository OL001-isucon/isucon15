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

# -s <host> はそのサーバーだけを対象にし、1 台で全役割を担うものとして扱う
# 引数なしの source なので、shift は呼び出し元のスクリプトの引数に効く
if [ "${1:-}" = "-s" ]; then
  HOSTS=("${2:?usage: $0 -s <host> ...}")
  WEB_HOST=${HOSTS[0]}
  APP_HOST=${HOSTS[0]}
  DB_HOST=${HOSTS[0]}
  shift 2
fi

# 分割前に指定を忘れると他の人のサーバーを上書きするので、既定のサーバーは持たない
require_host() {
  if [ -z "$WEB_HOST" ] || [ -z "$APP_HOST" ] || [ -z "$DB_HOST" ]; then
    echo "サーバーを指定してください: $0 -s ${HOSTS[0]} ...(分割後は conf.sh の WEB_HOST / APP_HOST / DB_HOST を埋める)" >&2
    exit 1
  fi
}
