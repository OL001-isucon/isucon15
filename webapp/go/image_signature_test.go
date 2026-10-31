package isureal

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// 元の実装。署名の形式を変えていないことを確かめる
func referenceImageSignature(key []byte, imageID string, exp int64) []byte {
	value := []byte(strconv.FormatInt(exp, 10) + ":" + imageID)
	for range imageSignatureIterations {
		mac := hmac.New(sha256.New, key)
		mac.Write(value)
		value = mac.Sum(nil)
	}
	return value
}

func TestImageSignatureMatchesReference(t *testing.T) {
	key := bytes.Repeat([]byte{0x5a}, sha256.Size)
	for _, id := range []string{"a", "33333333-3333-4333-8333-333333333333", "長い画像 ID を想定した非常に長い文字列のテストケースです。"} {
		for _, exp := range []int64{0, 1, 1793000000} {
			want := referenceImageSignature(key, id, exp)
			if got := imageSignature(key, id, exp); !bytes.Equal(got, want) {
				t.Errorf("signature differs: id=%q exp=%d", id, exp)
			}
		}
	}
}

func TestVerifyImageSignature(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, sha256.Size)
	id, exp := "33333333-3333-4333-8333-333333333333", int64(1793000040)
	sig := referenceImageSignature(key, id, exp)
	wrong := bytes.Repeat([]byte{0}, sha256.Size)
	for range 2 {
		if verifyImageSignature(key, id, exp, wrong) {
			t.Error("wrong signature accepted")
		}
		if !verifyImageSignature(key, id, exp, sig) {
			t.Error("valid signature rejected")
		}
	}
	otherKey := bytes.Repeat([]byte{0x22}, sha256.Size)
	if verifyImageSignature(otherKey, id, exp, sig) {
		t.Error("signature for another key accepted")
	}
	if verifyImageSignature(key, id, exp+60, sig) {
		t.Error("signature for another exp accepted")
	}
}

func TestImageSignatureCacheIsBounded(t *testing.T) {
	cache := &imageSignatureCache{buckets: map[int64]map[string][]byte{}}
	for exp := int64(0); exp < imageSignatureCacheBuckets*3; exp++ {
		cache.put("k", exp, []byte{1})
	}
	if len(cache.buckets) != imageSignatureCacheBuckets {
		t.Errorf("buckets = %d", len(cache.buckets))
	}
	if _, ok := cache.get("k", 0); ok {
		t.Error("oldest bucket was kept")
	}
}

func TestSignedImageURLRoundsUpExpiry(t *testing.T) {
	before := time.Now().Add(imageURLLifetime).Unix()
	user := User{ImageKey: bytes.Repeat([]byte{0x33}, sha256.Size)}
	parsed, err := url.Parse(signedImageURL(user, "x"))
	if err != nil {
		t.Fatal(err)
	}
	exp, err := strconv.ParseInt(parsed.Query().Get("exp"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if exp%imageURLExpStepSeconds != 0 || exp < before || exp >= before+imageURLExpStepSeconds+1 {
		t.Errorf("exp = %d, before = %d", exp, before)
	}
}

func BenchmarkReference(b *testing.B) {
	key := bytes.Repeat([]byte{0x5a}, sha256.Size)
	for range b.N {
		referenceImageSignature(key, "33333333-3333-4333-8333-333333333333", 1793000000)
	}
}

func BenchmarkImageSignature(b *testing.B) {
	key := bytes.Repeat([]byte{0x5a}, sha256.Size)
	for range b.N {
		imageSignature(key, "33333333-3333-4333-8333-333333333333", 1793000000)
	}
}
