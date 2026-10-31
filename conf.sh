# 当日に問題を見て埋める設定。トークンやパスワード以外の秘密情報は書かない

# ssh_config の Host 名。router はログイン禁止なので入れない
HOSTS=(asia1 europe1 america1 asia2)

# 役割。ベンチは常に 3 リージョンへ流れるので、アプリと nginx は全台を揃える
APP_HOSTS=(asia1 europe1 america1)
DB_HOST=asia2

# アプリ
APP_SERVICE=isureal-go.service
APP_BIN=isureal
APP_DIR=webapp/go
# main パッケージの場所。APP_DIR からの相対パス
APP_MAIN_PKG=./cmd/isureal
GO_BIN_DIR=/home/isucon/local/go/bin

# DB
DB_USER=isureal
DB_NAME=isureal
SCHEMA_FILES=(webapp/sql/001-schema.sql webapp/sql/003-initial-snapshot.sql)

# ログ
ACCESS_LOG=/var/log/nginx/access_ltsv.log

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
  '*/._*'
  '*/AGENTS.md'
  'webapp/frontend/*'
  'webapp/public/*'
  'webapp/nodejs/*'
  'webapp/perl/*'
  'webapp/php/*'
  'webapp/python/*'
  'webapp/ruby/*'
  'webapp/rust/*'
)
