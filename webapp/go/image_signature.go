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

const imageSignatureLanes = 16

// 2 本以下なら空きレーンを無駄にする分、1 本ずつ計算する方が速い
const imageSignatureMinLanes = 3

type imageSignatureJob struct {
	cacheKey string
	key      []byte
	imageID  string
	exp      int64
	sig      []byte
	done     chan struct{}
}

func newImageSignatureJob(cacheKey string, key []byte, imageID string, exp int64) *imageSignatureJob {
	return &imageSignatureJob{cacheKey: cacheKey, key: key, imageID: imageID, exp: exp, done: make(chan struct{})}
}

// imageSignatureJobs は順番待ちの署名を束で持つ。nil ならワーカーがなく、1 本ずつ計算する
var imageSignatureJobs chan []*imageSignatureJob

// startImageSignatureWorker は順番待ちの署名をまとめて計算するワーカーを 1 つ起動する。
// ワーカーが 1 つなので、計算が使うコアは今までと同じ 1 つに収まる
func startImageSignatureWorker() {
	imageSignatureJobs = make(chan []*imageSignatureJob, imageSignatureLanes)
	go func() {
		var pending []*imageSignatureJob
		for jobs := range imageSignatureJobs {
			pending = append(pending[:0], jobs...)
			for len(pending) > 0 {
				pending = takeImageSignatureJobs(pending)
				n := min(len(pending), imageSignatureLanes)
				runImageSignatureJobs(pending[:n])
				pending = pending[n:]
			}
		}
	}()
}

// takeImageSignatureJobs はレーンが埋まるまで、待っている束を足す
func takeImageSignatureJobs(pending []*imageSignatureJob) []*imageSignatureJob {
	for len(pending) < imageSignatureLanes {
		select {
		case jobs := <-imageSignatureJobs:
			pending = append(pending, jobs...)
		default:
			return pending
		}
	}
	return pending
}

// waitImageSignatureJobs はレーンの数ごとの束にしてワーカーへ渡し、全部終わるまで待つ
func waitImageSignatureJobs(jobs []*imageSignatureJob) {
	for i := 0; i < len(jobs); i += imageSignatureLanes {
		imageSignatureJobs <- jobs[i:min(i+imageSignatureLanes, len(jobs))]
	}
	for _, job := range jobs {
		<-job.done
	}
}

type imageSignatureRef struct {
	user    User
	imageID string
}

// appendUserImageRefs は利用者の画像とプロフィール画像の署名を refs に足す
func appendUserImageRefs(refs []imageSignatureRef, user User, imageIDs ...string) []imageSignatureRef {
	if user.ProfileImageID != nil {
		refs = append(refs, imageSignatureRef{user, *user.ProfileImageID})
	}
	for _, imageID := range imageIDs {
		refs = append(refs, imageSignatureRef{user, imageID})
	}
	return refs
}

// presignImages は応答に必要な署名を先にまとめて計算し、キャッシュへ入れる。
// signedImageURL は 1 本ずつ待つので、同時に待つ本数が少なくレーンが埋まらない。
// 応答に必要な本数が分かる処理は、ここを通すとレーンを埋められる
func presignImages(refs []imageSignatureRef) {
	if imageSignatureJobs == nil {
		return
	}
	exp := imageURLExp()
	seen := make(map[string]bool, len(refs))
	jobs := make([]*imageSignatureJob, 0, len(refs))
	for _, ref := range refs {
		cacheKey := imageSignatureCacheKey(ref.user.ImageKey, ref.imageID)
		if seen[cacheKey] {
			continue
		}
		seen[cacheKey] = true
		if _, ok := imageSignatures.get(cacheKey, exp); !ok {
			jobs = append(jobs, newImageSignatureJob(cacheKey, ref.user.ImageKey, ref.imageID, exp))
		}
	}
	if len(jobs) == 0 {
		return
	}
	imageSignatureMisses.Add(int64(len(jobs)))
	start := time.Now()
	waitImageSignatureJobs(jobs)
	imageSignatureWaitNanos.Add(int64(time.Since(start)))
	for _, job := range jobs {
		imageSignatures.put(job.cacheKey, job.exp, job.sig)
	}
}

func runImageSignatureJobs(jobs []*imageSignatureJob) {
	if len(jobs) < imageSignatureMinLanes {
		for _, job := range jobs {
			job.sig = imageSignature(job.key, job.imageID, job.exp)
		}
	} else {
		// 空きレーンは同じ job で埋める。同じ値を計算するだけで結果は変わらない
		var lanes [imageSignatureLanes]*imageSignatureJob
		for i := range lanes {
			lanes[i] = jobs[i%len(jobs)]
		}
		signImageLanes(&lanes)
	}
	for _, job := range jobs {
		close(job.done)
	}
}

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
	if imageSignatureJobs != nil {
		job := newImageSignatureJob(cacheKey, key, imageID, exp)
		waitImageSignatureJobs([]*imageSignatureJob{job})
		imageSignatureWaitNanos.Add(int64(time.Since(start)))
		return job.sig
	}
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

func imageURLExp() int64 {
	exp := time.Now().Add(imageURLLifetime).Unix()
	exp += imageURLExpStepSeconds - 1
	return exp - exp%imageURLExpStepSeconds
}

func signedImageURL(user User, imageID string) string {
	exp := imageURLExp()
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
