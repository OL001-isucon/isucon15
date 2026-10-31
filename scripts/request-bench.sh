#!/usr/bin/env bash
# ポータルへ負荷走行を依頼する。セッションは .portal-auth に置き、コミットしない
# usage: scripts/request-bench.sh
source "$(dirname "$0")/lib.sh"

auth="$ROOT/.portal-auth"
if [ ! -f "$auth" ]; then
  echo ".portal-auth がありません。ポータルからベンチを実行してください" >&2
  exit 1
fi
# shellcheck source=/dev/null
source "$auth"

response=$(mktemp)
trap 'rm -f "$response"' EXIT

# 本文はブラウザが送る protobuf をそのまま使う
status=$(printf "$PORTAL_BENCH_BODY" | curl -sS -m 15 -o "$response" -w '%{http_code}' \
  -X POST "$PORTAL_URL/api/contestant/benchmark_jobs" \
  -H 'accept: application/protobuf, application/vnd.google.protobuf, text/plain' \
  -H 'content-type: application/vnd.google.protobuf' \
  -H "origin: $PORTAL_URL" \
  -H "referer: $PORTAL_URL/contestant/benchmark_jobs" \
  -H "x-csrf-token: $PORTAL_CSRF_TOKEN" \
  -b "$PORTAL_COOKIE" \
  --data-binary @-)

case "$status" in
  2*)
    echo "ポータルへベンチを依頼しました"
    ;;
  *)
    # 応答は protobuf なので、読める文字だけを表示する
    echo "ベンチの依頼に失敗しました (HTTP $status): $(tr -cd '[:print:]' < "$response" | cut -c1-200)" >&2
    exit 1
    ;;
esac
