# 当日に問題を見て埋める設定。トークンやパスワード以外の秘密情報は書かない

# ssh_config の Host 名
HOSTS=(isu1 isu2 isu3)

# サーバーの役割。分割したらここを書き換える
WEB_HOST=isu1
APP_HOST=isu1
DB_HOST=isu1

# アプリ(問題が公開されたら書き換える)
APP_SERVICE=isuride-go.service
APP_BIN=isuride
APP_DIR=webapp/go
GO_BIN_DIR=/home/isucon/local/golang/bin

# DB(問題が公開されたら書き換える)
DB_USER=isucon
DB_PASSWORD=isucon
DB_NAME=isuride
SCHEMA_FILE=webapp/sql/1-schema.sql

# ログ
ACCESS_LOG=/var/log/nginx/access_ltsv.log
SLOW_LOG=/var/log/mysql/mysql-slow.log

# ベンチ開始の検知に使う文字列。nginx はレスポンス完了時に記録するので、負荷が始まる直前に一致する
START_PATTERN=/initialize
# ボタンを押すまで待つ秒数 / ログが止まってから終了と判定する秒数 / CPU プロファイルの秒数
WAIT_START_SECONDS=1800
QUIET_SECONDS=20
PPROF_SECONDS=60

# ベンチ結果の issue を作るリポジトリ
GITHUB_REPO=OL001-isucon/isucon15

# サーバーからリポジトリへ取り込まないパス
PULL_EXCLUDES=(
  '*/node_modules/*'
  '*/.git/*'
  'webapp/frontend/*'
  'webapp/public/*'
  'webapp/nodejs/*'
  'webapp/perl/*'
  'webapp/php/*'
  'webapp/python/*'
  'webapp/ruby/*'
  'webapp/rust/*'
)
