#!/usr/bin/env bash
# 初期画像の本体 (13 GB、183 万件) を指定ホストの控えに書く。ベンチの合間に実行する
# usage: scripts/warm-images.sh host
source "$(dirname "$0")/lib.sh"
host=${1:?usage: warm-images.sh host}
on "$host" "cd $APP_DIR && PATH=$GO_BIN_DIR:\$PATH go build -o warm-images ./cmd/warm-images && set -a && . ~/env.sh && set +a && ./warm-images"
