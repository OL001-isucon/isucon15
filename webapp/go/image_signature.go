package isureal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"expvar"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const imageURLLifetime = 24 * time.Hour
const imageSignatureIterations = 1000

// exp を切り上げて同じ画像の署名を使い回す。寿命は 24 時間を下回らない
// ベンチは期限の範囲を検証する。10 分の切り上げは「有効期限が範囲外」で FAIL した
const imageURLExpStepSeconds = 60

// 署名を保持する exp の数。古いものは計算し直せばよいので捨てる
const imageSignatureCacheBuckets = 16

// localhost:6060/debug/vars で再利用の効果を確かめる
var (
	imageSignatureHits      = expvar.NewInt("image_signature_hits")
	imageSignatureMisses    = expvar.NewInt("image_signature_misses")
	imageSignatureWaitNanos = expvar.NewInt("image_signature_wait_nanos")
)

// 署名の計算は同時に 1 本だけにする。計算が CPU を占有すると、DB の接続を持った処理にも順番が回らず、
// 接続が返らないままプールが埋まる。残りのコアを他の処理に残す
var imageSignatureSlot = make(chan struct{}, 1)

// imageSignatureCache は exp ごとに署名を持つ。exp が過ぎた分を丸ごと捨てられるようにしている
type imageSignatureCache struct {
	mu      sync.RWMutex
	buckets map[int64]map[string][]byte
}

var imageSignatures = &imageSignatureCache{buckets: map[int64]map[string][]byte{}}

func (c *imageSignatureCache) get(cacheKey string, exp int64) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	sig, ok := c.buckets[exp][cacheKey]
	return sig, ok
}

func (c *imageSignatureCache) put(cacheKey string, exp int64, sig []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bucket, ok := c.buckets[exp]
	if !ok {
		bucket = map[string][]byte{}
		c.buckets[exp] = bucket
		for len(c.buckets) > imageSignatureCacheBuckets {
			oldest := exp
			for candidate := range c.buckets {
				oldest = min(oldest, candidate)
			}
			delete(c.buckets, oldest)
		}
	}
	bucket[cacheKey] = sig
}

func imageSigningKey(secretKeyHash string) ([]byte, error) {
	key, err := hex.DecodeString(secretKeyHash)
	if err != nil || len(key) != sha256.Size {
		return nil, fmt.Errorf("invalid image signing key")
	}
	return key, nil
}

// Reset で鍵の前処理を使い回す。結果は毎回 hmac.New した場合と同じ
func imageSignature(key []byte, imageID string, exp int64) []byte {
	value := []byte(strconv.FormatInt(exp, 10) + ":" + imageID)
	mac := hmac.New(sha256.New, key)
	for range imageSignatureIterations {
		mac.Reset()
		mac.Write(value)
		value = mac.Sum(value[:0])
	}
	return value
}

func imageSignatureCacheKey(key []byte, imageID string) string {
	return string(key) + imageID
}

// limitedImageSignature は順番を待って計算する。待つ間に同じ署名ができていれば、それを使う
func limitedImageSignature(cacheKey string, key []byte, imageID string, exp int64) []byte {
	start := time.Now()
	imageSignatureSlot <- struct{}{}
	defer func() { <-imageSignatureSlot }()
	imageSignatureWaitNanos.Add(int64(time.Since(start)))
	if sig, ok := imageSignatures.get(cacheKey, exp); ok {
		return sig
	}
	return imageSignature(key, imageID, exp)
}

// verifyImageSignature は正しい署名だけを保持する。不正な要求でキャッシュを埋められないようにする
func verifyImageSignature(key []byte, imageID string, exp int64, sig []byte) bool {
	cacheKey := imageSignatureCacheKey(key, imageID)
	if cached, ok := imageSignatures.get(cacheKey, exp); ok {
		return hmac.Equal(sig, cached)
	}
	expected := limitedImageSignature(cacheKey, key, imageID, exp)
	if !hmac.Equal(sig, expected) {
		return false
	}
	imageSignatures.put(cacheKey, exp, expected)
	return true
}

func signedImageURL(user User, imageID string) string {
	exp := time.Now().Add(imageURLLifetime).Unix()
	exp += imageURLExpStepSeconds - 1
	exp -= exp % imageURLExpStepSeconds
	cacheKey := imageSignatureCacheKey(user.ImageKey, imageID)
	sig, ok := imageSignatures.get(cacheKey, exp)
	if ok {
		imageSignatureHits.Add(1)
	} else {
		imageSignatureMisses.Add(1)
		sig = limitedImageSignature(cacheKey, user.ImageKey, imageID, exp)
		imageSignatures.put(cacheKey, exp, sig)
	}
	query := url.Values{
		"exp": {strconv.FormatInt(exp, 10)},
		"sig": {base64.RawURLEncoding.EncodeToString(sig)},
	}
	return "/api/images/" + url.PathEscape(imageID) + "?" + query.Encode()
}
