# ISUCON2026 (ISUReal)

当日の手順と運用スクリプト。すべて手元の bash(WSL / Git Bash)から SSH 経由で実行する。

## 構成

| サーバ | 役割 |
|---|---|
| asia1 / europe1 / america1 | nginx + Go アプリ |
| asia2 | 共有 PostgreSQL 16 |
| ルーター | 国別の転送、遅延と帯域の制限。**ログイン禁止** |

ベンチは常にルーター経由で 3 リージョンへ流れ、チームで同時に 1 本だけ走る。対象サーバーは選べないので、アプリは全台を同じ版に揃えて計測する。

## 事前準備

- サーバーへ SSH できる鍵を用意する
- `gh auth login` を済ませる。無くても動くが、ベンチ結果の issue が作られない
- Go は手元に無くてもよい。ビルドはサーバーで行う

## 開始直後の手順

1. `ssh_config` の HostName を埋める
2. `conf.sh` を問題に合わせる
3. コードと設定を取り込み、push する。**最優先**。計測用の設定が混ざらないよう provision より先に行う

   ```bash
   bash scripts/pull.sh
   git push origin main
   ```

4. EBS の初期化が終わっていることを確認し、変更前のベンチを 1 回走らせる
5. 計測ツールを入れて、ログを有効にする。PostgreSQL が再起動するのでベンチ中には流さない

   ```bash
   bash scripts/provision.sh
   ```

6. `scripts/pull.sh` が `webapp/go/cmd/isureal/` に入れた `pprof.go` をデプロイする

   ```bash
   bash scripts/deploy.sh
   ```

7. 4 台で `sudo /usr/local/bin/envcheck` を実行する

## 改善のサイクル

```bash
git switch -c feature-x          # issue ごとにブランチを切る
# 修正して push
bash scripts/deploy.sh origin/feature-x   # ブランチ名を省略すると origin/main。アプリ 3 台へ並列で配置する
bash scripts/bench.sh "何を変えたか"       # 「準備完了」が出てからポータルでベンチを実行する
```

`bench.sh` はベンチの開始を検知して 3 台の CPU プロファイルを取り、終了後に alp(台ごと)/ pg_stat_statements / pprof(台ごと)の結果を `results/<日時>/` に保存する。`gh` があれば `[bench]` issue を作る。スコアは issue に追記する。

`pg_stat_statements` の時間は DB 内の処理時間だけで、europe1 / america1 からの往復約 100ms は含まない。**実行回数の順も見る。**

## 1 台だけ操作する

手元での動作確認のために 1 台だけ変えるときは、先頭の引数に `-s <host>` を付ける。`-s` を省略すると全台が対象になる。`bench.sh` には付けられない。

```bash
bash scripts/deploy.sh -s asia1 origin/feature-x
bash scripts/deploy.sh -s asia2   # asia2 の設定だけ反映する
```

リージョンごとに版が違うままベンチを実行すると、結果に注意が表示される。

## スキーマ変更

`webapp/sql/001-schema.sql` を編集する。**DB へ直接 ALTER しない。**

`/api/initialize` の `reset.sql` はデータしか戻さないので、スキーマの変更はベンチを実行しても反映されない。sqldef で適用する。

```bash
bash scripts/migrate.sh           # 差分の確認
bash scripts/migrate.sh --apply   # 適用
```

`migrate.sh` はスキーマを一時 DB に流して定義を取り出し、本番の DB と比較する。DROP は表示だけで実行しない。`image_objects` の子パーティションは比較しないので、変更するときは別途 SQL を流す。

追加したテーブルやキャッシュに初期データとの差が出るなら、`webapp/sql/reset.sql` や初期化処理にも戻す処理を入れる。

## alp の集約

ID を含むパスは `etc/alp.yml` の `matching_groups` に追記する。次の `bench.sh` から反映される。

## 設定変更

`servers/<host>/etc/` を編集すると `deploy.sh` で反映される。`/etc/postgresql` を変えると PostgreSQL が再起動する。`sites-enabled` の symlink は実体のファイルとして取り込むので、こちらを編集する。`servers/` はサーバーごとに別なので、全台に効かせるなら 3 台分を編集する。

変更後は 4 台で `sudo /usr/local/bin/envcheck` を実行する。

## 守ること

- ルーターへログインしない。サーバー間通信はルーターを迂回しない
- セキュリティグループの 22 / 443 / 8080 を変えない
- `isucon-register.service`、`isuadmin`、envcheck 関連のファイルに触らない
- 競技終了後はアナウンスまで環境に接続しない

## 提出前

最後の走行が最終スコアになる。FAIL すると受賞対象外なので、17:00 以降も終了時刻に余裕を持って最後の走行を終える。

```bash
git rm webapp/go/cmd/isureal/pprof.go   # commit して deploy する
bash scripts/logging.sh off             # PostgreSQL が再起動する
```

最後に 4 台を再起動し、アプリが自動で起動すること、データが残っていること、`envcheck` が通ることを確認する。`initialize.go` の `agent` / `harness` / `model` が実態と合っていることも確認する。競技終了時は 4 台すべてを起動しておく。
