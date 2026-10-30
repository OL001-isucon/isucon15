# ISUCON15

当日の手順と運用スクリプト。すべて手元の bash(WSL / Git Bash)から SSH 経由で実行する。

## 事前準備

- サーバーへ SSH できる鍵を用意する
- `gh auth login` を済ませる。無くても動くが、ベンチ結果の issue が作られない
- Go は手元に無くてもよい。ビルドはサーバーで行う

## 開始直後の手順

1. `ssh_config` の HostName を埋める
2. `conf.sh` のアプリ名、サービス名、DB 名、スキーマのパスを問題に合わせる
3. コードと設定を取り込み、push する。**最優先**。計測用の設定が混ざらないよう provision より先に行う

   ```bash
   bash scripts/pull.sh
   git push origin main
   ```

4. 計測ツールを入れて、ログを有効にする

   ```bash
   bash scripts/provision.sh
   ```

5. `conf.sh` の `PULL_EXCLUDES` を確認し、`scripts/pull.sh` が入れた `pprof.go` をデプロイする

   ```bash
   bash scripts/deploy.sh
   ```

## 改善のサイクル

```bash
git switch -c feature-x          # issue ごとにブランチを切る
# 修正して push
bash scripts/deploy.sh origin/feature-x   # ブランチ名を省略すると origin/main
bash scripts/bench.sh "何を変えたか"       # 「準備完了」が出てから Web のボタンを押す
```

`bench.sh` はボタンの実行を検知して CPU プロファイルを取り、ベンチ終了後に alp / pt-query-digest / pprof の結果を `results/<日時>/` に保存する。`gh` があれば `[bench]` issue を作る。スコアは issue に追記する。

## スキーマ変更

`webapp/sql/1-schema.sql` を編集する。**DB へ直接 ALTER しない。**

`/initialize` がこのファイルでテーブルを作り直す問題なら、デプロイするだけでベンチ開始時に反映される。すぐ反映したいときや、作り直さない問題では sqldef を使う。

```bash
bash scripts/migrate.sh           # 差分の確認
bash scripts/migrate.sh --apply   # 適用
```

`migrate.sh` はスキーマを一時 DB に流して定義を取り出し、本番の DB と比較する。

## alp の集約

ID を含むパスは `etc/alp.yml` の `matching_groups` に追記する。次の `bench.sh` から反映される。

## サーバー分割

`conf.sh` の `WEB_HOST` / `APP_HOST` / `DB_HOST` を書き換える。設定は `servers/<host>/etc/` を編集すると `deploy.sh` で反映される。`sites-enabled` の symlink は実体のファイルとして取り込むので、こちらを編集する。

## 提出前

```bash
git rm webapp/go/pprof.go   # commit して deploy する
bash scripts/logging.sh off
```

最後に全サーバーを再起動し、アプリが自動で起動すること、データが残っていることを確認する。
