package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"
)

// サーバー内からのみ接続できる。提出前にこのファイルを削除する
func init() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
}
