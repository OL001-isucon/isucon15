package isureal

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jmoiron/sqlx"
)

// WarmInitialImages は DB にある初期画像の本体をすべて手元の控え (cache/) に書く。
// 初期画像は変わらないので一度やれば以後のベンチで DB から blob を読まなくて済む。
// パーティションの単位である月ごとに流し、既に控えがあるものは飛ばす
func WarmInitialImages(ctx context.Context, db *sqlx.DB, store *ImageStore, logf func(string, ...any)) error {
	var first, last time.Time
	if err := db.QueryRowContext(ctx, `SELECT min(created_at), max(created_at) FROM image_meta WHERE NOT on_disk`).Scan(&first, &last); err != nil {
		return err
	}
	start := time.Date(first.UTC().Year(), first.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	total, written := 0, 0
	for month := start; !month.After(last); month = month.AddDate(0, 1, 0) {
		rows, err := db.QueryContext(ctx, `
   SELECT id, data FROM image_objects
   WHERE data IS NOT NULL AND created_at >= $1 AND created_at < $2
  `, month, month.AddDate(0, 1, 0))
		if err != nil {
			return err
		}
		monthTotal, monthWritten := 0, 0
		for rows.Next() {
			var id string
			var data []byte
			if err := rows.Scan(&id, &data); err != nil {
				rows.Close()
				return err
			}
			monthTotal++
			if _, err := os.Stat(store.cachePath(id)); err == nil {
				continue
			}
			if err := writeImageFile(store.cachePath(id), data, false); err != nil {
				rows.Close()
				return fmt.Errorf("write %s: %w", id, err)
			}
			monthWritten++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		total += monthTotal
		written += monthWritten
		logf("%s: %d images, %d written", month.Format("2006-01"), monthTotal, monthWritten)
	}
	logf("done: %d images, %d written", total, written)
	return nil
}
