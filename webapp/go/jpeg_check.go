package isureal

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
)

const (
	jpegMarkerDHT = 0xc4
	jpegMarkerSOS = 0xda
	jpegMarkerDQT = 0xdb
)

var errInvalidJPEG = errors.New("invalid jpeg")

// inspectJPEG はヘッダーと構造だけを確認する。jpeg.Decode は画素まで展開するので、
// 投稿の大きな画像では CPU の 3 割を使っていた。
// 全デコードとの違いは、圧縮データの途中だけが壊れた画像を受け入れること。
func inspectJPEG(data []byte) (image.Config, error) {
	config, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return image.Config{}, err
	}
	if !hasCompleteJPEGScan(data) {
		return image.Config{}, errInvalidJPEG
	}
	return config, nil
}

// hasCompleteJPEGScan はテーブルの後に圧縮データがあり、EOI で終わっていることを確かめる
func hasCompleteJPEGScan(data []byte) bool {
	hasQuantization, hasHuffman := false, false
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xff {
			return false
		}
		marker := data[i+1]
		if marker == 0xff {
			i++
			continue
		}
		switch marker {
		case jpegMarkerSOS:
			return hasQuantization && hasHuffman && bytes.LastIndex(data[i:], []byte{0xff, 0xd9}) > 0
		case jpegMarkerDQT:
			hasQuantization = true
		case jpegMarkerDHT:
			hasHuffman = true
		}
		i += 2 + int(data[i+2])<<8 + int(data[i+3])
	}
	return false
}
