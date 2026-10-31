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

# 本文はブラウザが送る protobuf をそのまま使う。応答も protobuf なので、読める文字だけを残す
output=$(printf "$PORTAL_BENCH_BODY" | curl -sS -m 15 -w '\n%{http_code}' \
  -X POST "$PORTAL_URL/api/contestant/benchmark_jobs" \
  -H 'accept: application/protobuf, application/vnd.google.protobuf, text/plain' \
  -H 'content-type: application/vnd.google.protobuf' \
  -H "origin: $PORTAL_URL" \
  -H "referer: $PORTAL_URL/contestant/benchmark_jobs" \
  -H "x-csrf-token: $PORTAL_CSRF_TOKEN" \
  -b "$PORTAL_COOKIE" \
  --data-binary @- | tr -cd '[:print:]\n')
status=${output##*$'\n'}
body=${output%$'\n'*}

case "$status" in
  2*)
    echo "ポータルへベンチを依頼しました"
    ;;
  *)
    echo "ベンチの依頼に失敗しました (HTTP $status): ${body:0:200}" >&2
    exit 1
    ;;
esac
