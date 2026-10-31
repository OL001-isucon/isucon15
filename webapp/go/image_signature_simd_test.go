//go:build goexperiment.simd && amd64

package isureal

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"testing"
)

func signatureTestJobs() [imageSignatureLanes]*imageSignatureJob {
	var jobs [imageSignatureLanes]*imageSignatureJob
	for i := range jobs {
		key := sha256.Sum256(fmt.Appendf(nil, "key-%d", i))
		// 長さと exp が違う入力を混ぜる
		jobs[i] = &imageSignatureJob{key: key[:], imageID: fmt.Sprintf("image-%0*d", i+1, i), exp: 1793432460 + int64(i)*60}
	}
	return jobs
}

func TestSignImageLanesMatchesReference(t *testing.T) {
	jobs := signatureTestJobs()
	signImageLanes(&jobs)
	for i, job := range jobs {
		if want := referenceImageSignature(job.key, job.imageID, job.exp); !bytes.Equal(job.sig, want) {
			t.Fatalf("lane %d: signature differs", i)
		}
	}
}

// 同時に来た署名をワーカーがまとめても、1 本ずつの少ない場合でも同じ値を返す
func TestLimitedImageSignatureThroughWorker(t *testing.T) {
	if imageSignatureJobs == nil {
		t.Skip("AVX-512 is not available")
	}
	for _, concurrency := range []int{1, 2, 3, 15, 16, 17, 100} {
		var wg sync.WaitGroup
		for i := range concurrency {
			wg.Go(func() {
				job := signatureTestJobs()[i%imageSignatureLanes]
				got := limitedImageSignature(imageSignatureCacheKey(job.key, job.imageID), job.key, job.imageID, job.exp)
				if want := referenceImageSignature(job.key, job.imageID, job.exp); !bytes.Equal(got, want) {
					t.Errorf("concurrency %d lane %d: signature differs", concurrency, i)
				}
			})
		}
		wg.Wait()
	}
}

// 先に計算した署名は signedImageURL が計算し直さず、1 本ずつ計算した場合と同じ URL になる
func TestPresignImages(t *testing.T) {
	if imageSignatureJobs == nil {
		t.Skip("AVX-512 is not available")
	}
	profileID := "presign-profile"
	key := sha256.Sum256([]byte("presign-key"))
	user := User{ImageKey: key[:], ProfileImageID: &profileID}
	refs := appendUserImageRefs(nil, user, "presign-image")
	for i := range 40 {
		refs = append(refs, imageSignatureRef{user, fmt.Sprintf("presign-%d", i)})
	}
	presignImages(append(refs, refs...))
	misses := imageSignatureMisses.Value()
	for _, ref := range refs {
		got, err := url.Parse(signedImageURL(ref.user, ref.imageID))
		if err != nil {
			t.Fatal(err)
		}
		exp, err := strconv.ParseInt(got.Query().Get("exp"), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		sig, err := base64.RawURLEncoding.DecodeString(got.Query().Get("sig"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(sig, referenceImageSignature(key[:], ref.imageID, exp)) {
			t.Fatalf("%s: signature differs", ref.imageID)
		}
	}
	// 分の切り替わりをまたぐと計算し直すので、1 周分の余裕を見る
	if extra := imageSignatureMisses.Value() - misses; extra != 0 && extra != int64(len(refs)) {
		t.Fatalf("recomputed %d signatures", extra)
	}
}

func BenchmarkSignImageLanes(b *testing.B) {
	jobs := signatureTestJobs()
	for b.Loop() {
		signImageLanes(&jobs)
	}
}
