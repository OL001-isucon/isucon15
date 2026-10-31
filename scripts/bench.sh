#!/usr/bin/env bash
# ベンチの計測を準備し、Web のボタンが押されたら計測して、終了後に解析する
# ログを空にしてから /initialize を検知するまで待つので、ボタンは「準備完了」の表示後に押す
# usage: scripts/bench.sh [-s host] [メモ]
source "$(dirname "$0")/lib.sh"
require_host

memo=${1:-}
dir="$ROOT/results/$(date +%Y%m%d-%H%M%S)-$APP_HOST"
mkdir -p "$dir"

on "$WEB_HOST" "sudo truncate -s 0 $ACCESS_LOG"
on "$DB_HOST" "sudo truncate -s 0 $SLOW_LOG"

echo ">>> 準備完了。ベンチの対象が $WEB_HOST であることを確認して、Web のベンチ実行ボタンを押してください"

on "$WEB_HOST" "timeout $WAIT_START_SECONDS bash -c 'until sudo grep -q $START_PATTERN $ACCESS_LOG; do sleep 1; done'"
echo ">>> ベンチ開始を検知しました"

# /initialize のスキーマ作成とデータ投入が解析結果に混ざらないようにする
on "$DB_HOST" "sudo truncate -s 0 $SLOW_LOG"

on "$APP_HOST" "curl -fsS -o /tmp/cpu.pprof 'http://localhost:6060/debug/pprof/profile?seconds=$PPROF_SECONDS'" &
pprof_pid=$!

# アクセスログの増加が止まったらベンチ終了とみなす
on "$WEB_HOST" "
  last=-1
  quiet=0
  while [ \$quiet -lt $QUIET_SECONDS ]; do
    size=\$(sudo stat -c %s $ACCESS_LOG)
    if [ \"\$size\" = \"\$last\" ]; then quiet=\$((quiet + 1)); else quiet=0; fi
    last=\$size
    sleep 1
  done
"
echo ">>> ベンチ終了を検知しました。解析します"

pprof_ok=true
wait "$pprof_pid" || pprof_ok=false

on "$WEB_HOST" "cat > /tmp/alp.yml" < "$ROOT/etc/alp.yml"
on "$WEB_HOST" "sudo alp ltsv --config /tmp/alp.yml --file $ACCESS_LOG" > "$dir/alp.txt"
on "$DB_HOST" "sudo pt-query-digest --no-version-check $SLOW_LOG" > "$dir/slow-digest.txt"

if [ "$pprof_ok" = true ]; then
  on "$APP_HOST" "PATH=$GO_BIN_DIR:\$PATH go tool pprof -top /tmp/cpu.pprof 2>/dev/null" > "$dir/pprof-top.txt"
  on "$APP_HOST" "PATH=$GO_BIN_DIR:\$PATH go tool pprof -top -cum /tmp/cpu.pprof 2>/dev/null" > "$dir/pprof-cum.txt"
  on "$APP_HOST" "cat /tmp/cpu.pprof" > "$dir/cpu.pprof"
else
  echo "CPU プロファイルを取得できませんでした。$APP_DIR/$APP_MAIN_PKG/pprof.go を確認してください" | tee "$dir/pprof-top.txt" >&2
fi

deployed=$(on "$APP_HOST" "cat ~/DEPLOYED 2>/dev/null || echo unknown")

{
  echo "- メモ: ${memo:-なし}"
  echo "- サーバー: $APP_HOST"
  echo "- デプロイ: \`$deployed\`"
  echo "- スコア: (ポータルの結果を追記)"
  echo
  echo "### alp"
  echo '```'
  head -n 18 "$dir/alp.txt"
  echo '```'
  echo
  echo "### pt-query-digest"
  echo '```'
  awk '/^# Profile/ {p=1} p && /^$/ {exit} p' "$dir/slow-digest.txt"
  echo '```'
  echo
  echo "### pprof"
  echo '```'
  head -n 24 "$dir/pprof-top.txt"
  echo '```'
} > "$dir/summary.md"

cat "$dir/summary.md"
echo
echo "結果: $dir"
if [ -f "$dir/cpu.pprof" ]; then
  echo "フレームグラフ: go tool pprof -http=localhost:8090 $dir/cpu.pprof"
fi

if ! command -v gh >/dev/null; then
  echo "gh が無いので issue は作成しません"
  exit 0
fi

title="[bench] $APP_HOST $(date +'%H:%M:%S') ${memo}"
url=$(gh issue create --repo "$GITHUB_REPO" --title "$title" --body-file "$dir/summary.md")
# issue のコメントは約 65KB が上限
for file in alp.txt slow-digest.txt pprof-cum.txt; do
  [ -f "$dir/$file" ] || continue
  { echo "$file"; echo '```'; head -c 60000 "$dir/$file" || true; echo '```'; } > "$dir/comment.md"
  gh issue comment "$url" --body-file "$dir/comment.md" > /dev/null
done
rm -f "$dir/comment.md"
echo "issue: $url"
