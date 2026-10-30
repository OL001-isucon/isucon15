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
