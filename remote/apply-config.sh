#!/usr/bin/env bash
# /tmp/config.tar のうち、内容が変わったファイルだけを反映する。サーバー上で root として実行する
# git は実行権限や所有者を保持しないので、丸ごと上書きせず既存ファイルの権限を保つ
set -euo pipefail

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
tar xf /tmp/config.tar --strip-components=2 -C "$work"
cd "$work"

nginx_changed=false
mysql_changed=false

while IFS= read -r file; do
  target=/$file
  cmp -s "$file" "$target" && continue
  mkdir -p "$(dirname "$target")"
  cp "$file" "$target"
  echo "updated $target"
  case "$target" in
    /etc/nginx/*) nginx_changed=true ;;
    /etc/mysql/*) mysql_changed=true ;;
  esac
done < <(find etc -type f)

systemctl daemon-reload
if [ "$nginx_changed" = true ]; then
  nginx -t -q
  systemctl reload nginx
fi
if [ "$mysql_changed" = true ]; then
  systemctl restart mysql
fi
