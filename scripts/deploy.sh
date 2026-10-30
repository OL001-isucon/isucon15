#!/usr/bin/env bash
# 指定した ref をサーバーへ配置してビルドし、設定を反映して再起動する
# 誰が実行しても同じ結果になるよう、作業ツリーではなく git の ref を配置する
# usage: scripts/deploy.sh [ref]  省略時は origin/main
source "$(dirname "$0")/lib.sh"

cd "$ROOT"

ref=${1:-origin/main}
git fetch -q origin
commit=$(git rev-parse --short "$ref")
echo "== deploy $ref ($commit)"

sql_dir=$(dirname "$SCHEMA_FILE")
archive=$(mktemp)
trap 'rm -f "$archive"' EXIT
git archive "$ref" "$APP_DIR" "$sql_dir" > "$archive"

# 削除したファイルが残るとビルドが壊れるので、配置前に Go のソースを消す
on "$APP_HOST" "find $APP_DIR -name '*.go' -delete"
on "$APP_HOST" "tar xf -" < "$archive"
# Windows でコミットすると実行権限が落ち、/initialize の init.sh が動かなくなる
on "$APP_HOST" "find $APP_DIR $sql_dir -name '*.sh' -exec chmod +x {} +"

on "$APP_HOST" "cd $APP_DIR && PATH=$GO_BIN_DIR:\$PATH go build -o $APP_BIN ."
on "$APP_HOST" "echo '$ref $commit' > ~/DEPLOYED"

for host in "${HOSTS[@]}"; do
  [ -n "$(git ls-tree "$ref" "servers/$host/etc")" ] || continue
  echo "== config ($host)"
  git archive "$ref" "servers/$host/etc" | on "$host" "cat > /tmp/config.tar"
  on "$host" "sudo bash -s" < "$ROOT/remote/apply-config.sh"
done

on "$APP_HOST" "sudo systemctl restart $APP_SERVICE"
echo "== deployed $ref ($commit)"
