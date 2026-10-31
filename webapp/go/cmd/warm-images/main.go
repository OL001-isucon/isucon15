// 初期画像の本体をすべて手元の控えに書く。ベンチの合間にアプリサーバーで実行する
package main

import (
	"context"
	"log"

	"github.com/jmoiron/sqlx"

	"example.com/isureal-company/isureal"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	config := isureal.ConfigFromEnv()
	db, err := sqlx.Open("pgx", config.DatabaseDSN())
	if err != nil {
		log.Fatal(err)
	}
	if err := isureal.WarmInitialImages(context.Background(), db, isureal.NewImageStore(config), log.Printf); err != nil {
		log.Fatal(err)
	}
}
