#!/usr/bin/env bash
# ベンチの計測を準備し、ポータルで負荷走行が始まったら計測して、終了後に全リージョン分を解析する
# ログを空にしてから /initialize を検知するまで待つ。.portal-auth があればベンチも自動で依頼する
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

take_pool_metrics() {
  on "$1" "bash -s $DB_METRICS_SECONDS" < "$ROOT/remote/pool-metrics.sh"
}

# 負荷の最中に、どこで止まっている goroutine が多いかを記録する。接続を借りたままの処理を探すため
take_goroutine_dumps() {
  local host=$1 i
  for i in 1 2 3; do
    sleep "$GOROUTINE_DUMP_INTERVAL_SECONDS"
    on "$host" "curl -fsS -m 10 'http://localhost:6060/debug/pprof/goroutine?debug=1'" > "$dir/goroutine-$host-$i.txt" || true
  done
}

take_db_metrics() {
  on "$DB_HOST" "sudo DB_NAME=$DB_NAME bash -s $DB_METRICS_SECONDS" < "$ROOT/remote/db-metrics.sh"
}

# DB の CPU / IO と待ち状態を回収し、ピークと平均を 1 行ずつにまとめる
collect_db() {
  on "$DB_HOST" "cat /tmp/db-vmstat.txt 2>/dev/null" > "$dir/db-vmstat.txt" || true
  on "$DB_HOST" "cat /tmp/db-waits.txt 2>/dev/null" > "$dir/db-waits.txt" || true
  on "$DB_HOST" "cat /tmp/db-activity.txt 2>/dev/null" > "$dir/db-activity.txt" || true
  on "${APP_HOSTS[0]}" "cat /tmp/pool-metrics.txt 2>/dev/null" > "$dir/pool-metrics.txt" || true
  {
    if [ -s "$dir/db-vmstat.txt" ]; then
      # 列名から位置を引く。vmstat の 1 行目は起動からの平均なので飛ばす
      awk '
        NR == 2 { for (i = 1; i <= NF; i++) col[$i] = i; next }
        NR > 3 && $1 ~ /^[0-9]+$/ {
          n++
          us += $col["us"]; sy += $col["sy"]; wa += $col["wa"]
          if ($col["us"] + $col["sy"] > cpu_max) cpu_max = $col["us"] + $col["sy"]
          if ($col["wa"] > wa_max) wa_max = $col["wa"]
          if ($col["bi"] > bi_max) bi_max = $col["bi"]
          if ($col["bo"] > bo_max) bo_max = $col["bo"]
          if ($col["b"] > b_max) b_max = $col["b"]
        }
        END {
          if (n == 0) { print "vmstat: 記録なし"; exit }
          printf "CPU 使用率 (user+sys): 平均 %d%% / 最大 %d%%\n", (us + sy) / n, cpu_max
          printf "IO 待ち: 平均 %d%% / 最大 %d%%, IO 待ちプロセス最大 %d\n", wa / n, wa_max, b_max
          printf "ディスク: 読み最大 %d KB/s, 書き最大 %d KB/s\n", bi_max, bo_max
        }' "$dir/db-vmstat.txt"
    else
      echo "vmstat: 取得できませんでした"
    fi
    if [ -s "$dir/db-waits.txt" ]; then
      # 待ちの種類ごとに同時セッション数の最大と平均。Lock が多ければロック待ち、IO が多ければディスク待ち
      awk '
        { t[$1] = 1; sum[$2] += $3; if ($3 > max[$2]) max[$2] = $3; total[$1] += $3 }
        END {
          n = 0; for (k in t) n++
          if (n == 0) { print "pg_stat_activity: 記録なし"; exit }
          tmax = 0; for (k in total) if (total[k] > tmax) tmax = total[k]
          printf "同時セッション数 (最大 / 平均): 合計 %d / %.1f", tmax, tsum(total) / n
          for (k in max) printf ", %s %d / %.1f", k, max[k], sum[k] / n
          print ""
        }
        function tsum(a, k, s) { for (k in a) s += a[k]; return s }' "$dir/db-waits.txt"
    else
      echo "pg_stat_activity: 取得できませんでした"
    fi
  } > "$dir/db-summary.txt"
}

collect() {
  local host=$1
  on "$host" "curl -fsS http://localhost:6060/debug/vars" > "$dir/vars-$host.json" || true
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

echo ">>> 準備完了"
if ! bash "$ROOT/scripts/request-bench.sh"; then
  echo ">>> ポータルからベンチを実行してください"
fi

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
take_goroutine_dumps "${APP_HOSTS[0]}" > /dev/null 2>&1 &
pprof_pids+=($!)
take_pool_metrics "${APP_HOSTS[0]}" > /dev/null 2>&1 &
pprof_pids+=($!)
take_db_metrics > /dev/null 2>&1 &
pprof_pids+=($!)

run_parallel wait_quiet "${APP_HOSTS[@]}"
echo ">>> ベンチ終了を検知しました。解析します"

for pid in "${pprof_pids[@]}"; do
  wait "$pid" || true
done

run_parallel collect "${APP_HOSTS[@]}"
collect_db
if ! on "$DB_HOST" "sudo -u postgres psql -d $DB_NAME -P pager=off" < "$ROOT/remote/pg-stats.sql" > "$dir/pg-stats.txt"; then
  echo "pg_stat_statements を取得できませんでした。scripts/logging.sh on を確認してください" | tee -a "$dir/pg-stats.txt" >&2
fi

deployed_lines=()
for host in "${APP_HOSTS[@]}"; do
  deployed_lines+=("$(on "$host" "cat ~/DEPLOYED 2>/dev/null || echo unknown")")
done

score="(ポータルの結果を追記)"
if job=$(bash "$ROOT/scripts/fetch-score.sh"); then
  score="${job#* } (ジョブ ${job% *})"
fi
echo ">>> スコア: $score"

{
  echo "- メモ: ${memo:-なし}"
  echo "- デプロイ:"
  for i in "${!APP_HOSTS[@]}"; do
    echo "  - ${APP_HOSTS[$i]}: \`${deployed_lines[$i]}\`"
  done
  if [ "$(printf '%s\n' "${deployed_lines[@]}" | sort -u | wc -l)" -gt 1 ]; then
    echo "- **注意: リージョンごとにデプロイした版が違います**"
  fi
  echo "- スコア: $score"
  for host in "${APP_HOSTS[@]}"; do
    echo
    echo "### alp ($host)"
    echo '```'
    head -n 18 "$dir/alp-$host.txt"
    echo '```'
  done
  echo
  echo "### DB ($DB_HOST)"
  echo '```'
  cat "$dir/db-summary.txt"
  echo '```'
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

# Windows の gh は /c/... 形式のパスを開けないので、相対パスで渡す
cd "$dir"
title="[bench] $(date +'%H:%M:%S') ${score%% *} ${memo}"
url=$(gh issue create --repo "$GITHUB_REPO" --title "$title" --body-file summary.md)
# issue のコメントは約 65KB が上限
for file in alp-*.txt pg-stats.txt db-vmstat.txt db-waits.txt pprof-cum-*.txt; do
  [ -f "$file" ] || continue
  { echo "$file"; echo '```'; head -c 60000 "$file" || true; echo '```'; } > comment.md
  gh issue comment "$url" --body-file comment.md > /dev/null
done
rm -f comment.md
echo "issue: $url"
