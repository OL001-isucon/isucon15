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

`bench.sh` はベンチの開始を検知して 3 台の CPU プロファイルと asia2 の CPU / IO / 待ち状態を取り、終了後に alp(台ごと)/ DB の要約 / pg_stat_statements / pprof(台ごと)の結果を `results/<日時>/` に保存する。`gh` があれば `[bench]` issue を作る。スコアは issue に追記する。

`pg_stat_statements` の時間は DB 内の処理時間だけで、europe1 / america1 からの往復約 100ms は含まない。**実行回数の順も見る。**

「DB」の要約は、DB が CPU、IO、ロックのどれで詰まっているかを見るためのもの。`pg_stat_statements` の上位が `FOR UPDATE` や `LOCK TABLE` で、待ちの `Lock` が多ければロック待ちなので、インデックスではなくトランザクションを短くする。1 秒ごとの生データは `db-vmstat.txt` と `db-waits.txt`。

## 1 台だけ操作する

手元での動作確認のために 1 台だけ変えるときは、先頭の引数に `-s <host>` を付ける。`-s` を省略すると全台が対象になる。`bench.sh` には付けられない。

```bash
bash scripts/deploy.sh -s asia1 origin/feature-x
bash scripts/deploy.sh -s asia2   # asia2 の設定だけ反映する
```

リージョンごとに版が違うままベンチを実行すると、結果に注意が表示される。

## スキーマ変更

`webapp/sql/` の番号付きファイルを `/api/initialize` が名前順に**毎回すべて**流し、そのあと `reset.sql` でデータを戻す。変更は新しい番号のファイルを足すか、既存のファイルに追記する。**DB へ直接 ALTER しない。**

```sql
-- webapp/sql/004_add_hoge.sql
CREATE INDEX IF NOT EXISTS posts_posted_at ON posts(posted_at);
ALTER TABLE posts ADD COLUMN IF NOT EXISTS like_count integer NOT NULL DEFAULT 0;
```

- **`DROP` は絶対に書かない。** 初期データの投入元はサーバーに無く、消すと復元できない
- 毎回流れるので、2 回目以降は何も変えない書き方にする。`IF NOT EXISTS` が使えない文は `DO` で存在を確認する。`UPDATE` も何度流しても結果が同じになるように書く
- `posts` や `image_objects` などの大きいテーブルへの変更は、初回の初期化に時間がかかり、制限時間を超えると FAIL する。デプロイ後、ベンチの前に asia1 で `~/webapp/sql/init.sh` を 1 回実行して適用しておく
- DB は全員で共有しているので、適用した変更はブランチに関係なく全員の環境に効き、自動では戻らない
- 追加した列やテーブルに初期データとの差が出るなら、`reset.sql` にも戻す処理を入れる

## 画像の保存

初期画像は DB の `image_objects.data` にある。一度読むと、そのサーバーの `/home/isucon/image-cache/cache/` に控えを置く。

新規の画像は `data` が NULL で、本体は受けたサーバーの `/home/isucon/image-cache/new/` に置く。コミット後に他のリージョンへ配り、届いていないときは `origin_region` のサーバーから取得する。`new/` は初期化で全リージョンから消える。

権限と削除の確認は毎回 DB で行う。サーバー間の通信は private IP の `:8080` の `/internal/` を使う。公開 IP はルーターを迂回するので使わない。

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
