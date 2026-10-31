#!/usr/bin/env bash
# ポータルから最新のジョブのスコアを取得し、「ジョブ ID スコア」を出力する。完了するまで待つ
# usage: scripts/fetch-score.sh
source "$(dirname "$0")/lib.sh"

auth="$ROOT/.portal-auth"
[ -f "$auth" ] || exit 1
# shellcheck source=/dev/null
source "$auth"

# 応答の protobuf は ListBenchmarkJobsResponse。先頭のジョブの 1: id, 4: status, 9: score を読む
FIELD_ID=1
FIELD_STATUS=4
FIELD_SCORE=9
STATUS_FINISHED=4

# 指定の位置の varint を読み、値を value に、次の位置を next に入れる
read_varint() {
  local position=$1 shift_bits=0 byte
  value=0
  while :; do
    byte=${bytes[$position]}
    value=$((value | ((byte & 0x7f) << shift_bits)))
    position=$((position + 1))
    shift_bits=$((shift_bits + 7))
    [ "$byte" -ge 128 ] || break
  done
  next=$position
}

fetch_first_job() {
  local position end key field wire
  # 読めない入力でも止まらないよう、応答の取得だけはここで失敗させる
  bytes=($(curl -fsS -m 15 \
    -H 'accept: application/protobuf, application/vnd.google.protobuf, text/plain' \
    -H "referer: $PORTAL_URL/contestant/benchmark_jobs" \
    -b "$PORTAL_COOKIE" \
    "$PORTAL_URL/api/contestant/benchmark_jobs" | od -An -v -tu1))
  [ "${#bytes[@]}" -gt 2 ] && [ "${bytes[0]}" -eq 10 ] || return 1
  read_varint 1
  position=$next
  end=$((position + value))
  job_id=0 job_status=0 job_score=0
  while [ "$position" -lt "$end" ]; do
    read_varint "$position"
    key=$value
    position=$next
    field=$((key >> 3))
    wire=$((key & 7))
    read_varint "$position"
    position=$next
    case "$wire" in
      0)
        case "$field" in
          "$FIELD_ID") job_id=$value ;;
          "$FIELD_STATUS") job_status=$value ;;
          "$FIELD_SCORE") job_score=$value ;;
        esac
        ;;
      2) position=$((position + value)) ;;
      *) return 1 ;;
    esac
  done
}

for _ in {1..24}; do
  fetch_first_job || exit 1
  if [ "$job_status" -eq "$STATUS_FINISHED" ]; then
    echo "$job_id $job_score"
    exit 0
  fi
  sleep 5
done
exit 1
