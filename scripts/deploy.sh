#!/usr/bin/env bash
# 指定した ref をアプリサーバーへ配置してビルドし、設定を反映して再起動する
# 誰が実行しても同じ結果になるよう、作業ツリーではなく git の ref を配置する
# 全台のビルドが通ってから再起動するので、失敗しても動いているアプリはそのまま残る
# ベンチは全リージョンへ流れるので、-s で 1 台だけ変えた状態ではスコアを計測しない
# usage: scripts/deploy.sh [-s host] [ref]  ref の省略時は origin/main
source "$(dirname "$0")/lib.sh"

cd "$ROOT"

ref=${1:-origin/main}
git fetch -q origin
commit=$(git rev-parse --short "$ref")
echo "== deploy $ref ($commit) to ${HOSTS[*]}"

archive=$(mktemp)
trap 'rm -f "$archive"' EXIT
git archive "$ref" "$APP_DIR" "$SQL_DIR" > "$archive"

build_app() {
  local host=$1
  # 削除したファイルが残るとビルドが壊れるので、配置前に Go のソースを消す
  on "$host" "find $APP_DIR -name '*.go' -delete"
  on "$host" "tar xf -" < "$archive"
  # Windows でコミットすると実行権限が落ち、/initialize の init.sh が動かなくなる
  on "$host" "find $APP_DIR $SQL_DIR -name '*.sh' -exec chmod +x {} +"
  on "$host" "cd $APP_DIR && PATH=$GO_BIN_DIR:\$PATH go build -o $APP_BIN $APP_MAIN_PKG"
  on "$host" "echo '$ref $commit' > ~/DEPLOYED"
}

apply_config() {
  local host=$1
  [ -n "$(git ls-tree "$ref" "servers/$host/etc")" ] || return 0
  git archive "$ref" "servers/$host/etc" | on "$host" "cat > /tmp/config.tar"
  on "$host" "sudo bash -s" < "$ROOT/remote/apply-config.sh"
}

restart_app() {
  on "$1" "sudo systemctl restart $APP_SERVICE"
}

[ "${#APP_HOSTS[@]}" -eq 0 ] || run_parallel build_app "${APP_HOSTS[@]}"
run_parallel apply_config "${HOSTS[@]}"
[ "${#APP_HOSTS[@]}" -eq 0 ] || run_parallel restart_app "${APP_HOSTS[@]}"
echo "== deployed $ref ($commit) to ${HOSTS[*]}"
