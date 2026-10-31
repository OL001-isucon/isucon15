#!/usr/bin/env bash
# 初期画像の本体 (13 GB、183 万件) を指定ホストの控えに書く。ベンチの合間に実行する
# 手元でビルドして送るので、サーバーに配置してあるコードの版に依存しない
# usage: scripts/warm-images.sh host
source "$(dirname "$0")/lib.sh"
host=${1:?usage: warm-images.sh host}
bin=$(mktemp)
trap 'rm -f "$bin"' EXIT
(cd "$ROOT/$APP_DIR" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$bin" ./cmd/warm-images)
on "$host" "cat > ~/warm-images && chmod +x ~/warm-images" < "$bin"
on "$host" "set -a && . ~/env.sh && set +a && ~/warm-images"
