#!/usr/bin/env bash
# サーバーのコードと設定をリポジトリへ取り込んでコミットする。push はしない
# webapp は APP_HOST(未設定なら HOSTS の先頭)から、設定は全サーバーから servers/<host>/ に取り込む
# usage: scripts/pull.sh [-s host]
source "$(dirname "$0")/lib.sh"

cd "$ROOT"

webapp_host=${APP_HOST:-${HOSTS[0]}}

find_excludes=""
for pattern in "${PULL_EXCLUDES[@]}"; do
  find_excludes+=" -not -path '$pattern'"
done

# 10MB 以上はバイナリや初期データなので取り込まず、一覧だけ表示する
# env.sh は DB のパスワードを含み、リージョンごとに値が違うので取り込まない
echo "== webapp ($webapp_host)"
on "$webapp_host" "find webapp -type f -size +10M $find_excludes" | sed 's/^/skip: /'
on "$webapp_host" "find webapp -type f -size -10M $find_excludes 2>/dev/null | tar czf - -T -" | tar xzf -

# 秘密鍵は取り込まない
# 計測用の設定も取り込まない。取り込むと logging.sh off の後のデプロイで復活する
# Windows では symlink を展開できないので実体化する。リンク切れなどの警告では止めない
for host in "${HOSTS[@]}"; do
  echo "== config ($host)"
  mkdir -p "servers/$host"
  on "$host" "cd / && { sudo tar czf - --dereference --ignore-failed-read --warning=no-file-changed \
    --exclude='*.key' --exclude='*.pem' --exclude='*.crt' \
    --exclude='etc/nginx/conf.d/00-isucon-ltsv.conf' \
    etc/nginx etc/postgresql etc/sysctl.d etc/systemd/system/$APP_SERVICE; true; }" | tar xzf - -C "servers/$host"
done

pprof_file="$APP_DIR/$APP_MAIN_PKG/pprof.go"
if [ ! -f "$pprof_file" ] && [ -d "$APP_DIR" ]; then
  cp templates/pprof.go "$pprof_file"
fi

git add -A
if git diff --cached --quiet; then
  echo "変更はありません"
  exit 0
fi
git commit -q -m "サーバーからコードと設定を取り込む"
git show --stat --format=%s HEAD | tail -n 3
echo "問題なければ git push してください"
