#!/usr/bin/env bash
# ベンチの計測を準備し、ポータルで負荷走行が始まったら計測して、終了後に全リージョン分を解析する
# ログを空にしてから /initialize を検知するまで待つので、ボタンは「準備完了」の表示後に押す
# usage: scripts/bench.sh [メモ]
if [ "${1:-}" = "-s" ]; then
  echo "ベンチは常に全リージョンへ流れるので -s は指定できません" >&2
  exit 1
fi
source "$(dirname "$0")/lib.sh"

memo=${1:-}
dir="$ROOT/results/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$dir"

truncate_log() {
  on "$1" "sudo truncate -s 0 $ACCESS_LOG"
}

reset_db_stats() {
  on "$DB_HOST" "sudo -u postgres psql -d $DB_NAME -qAtc 'SELECT pg_stat_statements_reset()' > /dev/null"
}

# アクセスログの増加が止まったら、そのリージョンのベンチ終了とみなす
wait_quiet() {
  on "$1" "
    last=-1
    quiet=0
    while [ \$quiet -lt $QUIET_SECONDS ]; do
      size=\$(sudo stat -c %s $ACCESS_LOG)
      if [ \"\$size\" = \"\$last\" ]; then quiet=\$((quiet + 1)); else quiet=0; fi
      last=\$size
      sleep 1
    done
  "
}

take_profile() {
  on "$1" "rm -f /tmp/cpu.pprof; curl -fsS -o /tmp/cpu.pprof 'http://localhost:6060/debug/pprof/profile?seconds=$PPROF_SECONDS'"
}

collect() {
  local host=$1
  on "$host" "cat > /tmp/alp.yml" < "$ROOT/etc/alp.yml"
  on "$host" "sudo alp ltsv --config /tmp/alp.yml --file $ACCESS_LOG" > "$dir/alp-$host.txt"
  if on "$host" "test -s /tmp/cpu.pprof"; then
    on "$host" "PATH=$GO_BIN_DIR:\$PATH go tool pprof -top /tmp/cpu.pprof 2>/dev/null" > "$dir/pprof-top-$host.txt"
    on "$host" "PATH=$GO_BIN_DIR:\$PATH go tool pprof -top -cum /tmp/cpu.pprof 2>/dev/null" > "$dir/pprof-cum-$host.txt"
    on "$host" "cat /tmp/cpu.pprof" > "$dir/cpu-$host.pprof"
  else
    echo "CPU プロファイルを取得できませんでした。$APP_DIR/$APP_MAIN_PKG/pprof.go を確認してください" > "$dir/pprof-top-$host.txt"
  fi
}

run_parallel truncate_log "${APP_HOSTS[@]}"
reset_db_stats

echo ">>> 準備完了。ポータルからベンチを実行してください"

# /initialize がどのリージョンへ届いても検知できるように全台を見る
deadline=$((SECONDS + WAIT_START_SECONDS))
started=""
until [ -n "$started" ]; do
  for host in "${APP_HOSTS[@]}"; do
    if on "$host" "sudo grep -q $START_PATTERN $ACCESS_LOG"; then
      started=$host
      break
    fi
  done
  if [ -z "$started" ] && [ "$SECONDS" -ge "$deadline" ]; then
    echo "ベンチの開始を検知できませんでした" >&2
    exit 1
  fi
done
echo ">>> ベンチ開始を検知しました ($started)"

# /initialize の reset.sql が解析結果に混ざらないようにする
reset_db_stats

pprof_pids=()
for host in "${APP_HOSTS[@]}"; do
  take_profile "$host" > /dev/null 2>&1 &
  pprof_pids+=($!)
done

run_parallel wait_quiet "${APP_HOSTS[@]}"
echo ">>> ベンチ終了を検知しました。解析します"

for pid in "${pprof_pids[@]}"; do
  wait "$pid" || true
done

run_parallel collect "${APP_HOSTS[@]}"
if ! on "$DB_HOST" "sudo -u postgres psql -d $DB_NAME -P pager=off" < "$ROOT/remote/pg-stats.sql" > "$dir/pg-stats.txt"; then
  echo "pg_stat_statements を取得できませんでした。scripts/logging.sh on を確認してください" | tee -a "$dir/pg-stats.txt" >&2
fi

deployed_lines=()
for host in "${APP_HOSTS[@]}"; do
  deployed_lines+=("$(on "$host" "cat ~/DEPLOYED 2>/dev/null || echo unknown")")
done

{
  echo "- メモ: ${memo:-なし}"
  echo "- デプロイ:"
  for i in "${!APP_HOSTS[@]}"; do
    echo "  - ${APP_HOSTS[$i]}: \`${deployed_lines[$i]}\`"
  done
  if [ "$(printf '%s\n' "${deployed_lines[@]}" | sort -u | wc -l)" -gt 1 ]; then
    echo "- **注意: リージョンごとにデプロイした版が違います**"
  fi
  echo "- スコア: (ポータルの結果を追記)"
  for host in "${APP_HOSTS[@]}"; do
    echo
    echo "### alp ($host)"
    echo '```'
    head -n 18 "$dir/alp-$host.txt"
    echo '```'
  done
  echo
  echo "### pg_stat_statements"
  echo '```'
  head -n 28 "$dir/pg-stats.txt"
  echo '```'
  for host in "${APP_HOSTS[@]}"; do
    echo
    echo "### pprof ($host)"
    echo '```'
    head -n 24 "$dir/pprof-top-$host.txt"
    echo '```'
  done
} > "$dir/summary.md"

cat "$dir/summary.md"
echo
echo "結果: $dir"
for file in "$dir"/cpu-*.pprof; do
  if [ -f "$file" ]; then
    echo "フレームグラフ: go tool pprof -http=localhost:8090 $file"
  fi
done

if ! command -v gh >/dev/null; then
  echo "gh が無いので issue は作成しません"
  exit 0
fi

title="[bench] $(date +'%H:%M:%S') ${memo}"
url=$(gh issue create --repo "$GITHUB_REPO" --title "$title" --body-file "$dir/summary.md")
# issue のコメントは約 65KB が上限
for file in "$dir"/alp-*.txt "$dir/pg-stats.txt" "$dir"/pprof-cum-*.txt; do
  [ -f "$file" ] || continue
  { basename "$file"; echo '```'; head -c 60000 "$file" || true; echo '```'; } > "$dir/comment.md"
  gh issue comment "$url" --body-file "$dir/comment.md" > /dev/null
done
rm -f "$dir/comment.md"
echo "issue: $url"
