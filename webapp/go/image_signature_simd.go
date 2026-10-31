//go:build goexperiment.simd && amd64

package isureal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"simd/archsimd"
	"strconv"
)

// AVX-512 で 16 本の SHA-256 を同時に計算する。SHA-NI のない asia では 1 本ずつの 7 倍速い
type signatureVector = archsimd.Uint32x16

var sha256Constants = [64]uint32{
	0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
	0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
	0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
	0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
	0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
	0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
	0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
	0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

var sha256InitialState = [8]uint32{0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19}

var sha256ConstantVectors [64]signatureVector

func init() {
	if !archsimd.X86.AVX512() {
		return
	}
	for i, k := range sha256Constants {
		sha256ConstantVectors[i] = archsimd.BroadcastUint32x16(k)
	}
	startImageSignatureWorker()
}

// compressSignatureLanes は state に 1 ブロックを畳み込み、結果を out へ書く。block は壊す
func compressSignatureLanes(state *[8]signatureVector, block *[16]signatureVector, out *[8]signatureVector) {
	a, b, c, d, e, f, g, h := state[0], state[1], state[2], state[3], state[4], state[5], state[6], state[7]
	for i := range 64 {
		var word signatureVector
		if i < 16 {
			word = block[i]
		} else {
			w15, w2 := block[(i-15)&15], block[(i-2)&15]
			s0 := w15.RotateAllRight(7).Xor(w15.RotateAllRight(18)).Xor(w15.ShiftAllRight(3))
			s1 := w2.RotateAllRight(17).Xor(w2.RotateAllRight(19)).Xor(w2.ShiftAllRight(10))
			word = block[i&15].Add(s0).Add(block[(i-7)&15]).Add(s1)
			block[i&15] = word
		}
		s1 := e.RotateAllRight(6).Xor(e.RotateAllRight(11)).Xor(e.RotateAllRight(25))
		ch := g.Xor(e.And(f.Xor(g)))
		t1 := h.Add(s1).Add(ch).Add(sha256ConstantVectors[i]).Add(word)
		s0 := a.RotateAllRight(2).Xor(a.RotateAllRight(13)).Xor(a.RotateAllRight(22))
		maj := a.And(b).Or(c.And(a.Or(b)))
		h, g, f, e, d, c, b, a = g, f, e, d.Add(t1), c, b, a, t1.Add(s0).Add(maj)
	}
	out[0], out[1], out[2], out[3] = state[0].Add(a), state[1].Add(b), state[2].Add(c), state[3].Add(d)
	out[4], out[5], out[6], out[7] = state[4].Add(e), state[5].Add(f), state[6].Add(g), state[7].Add(h)
}

// hmacLaneState は HMAC の鍵を 1 ブロック処理した状態を作る
func hmacLaneState(jobs *[imageSignatureLanes]*imageSignatureJob, pad byte) [8]signatureVector {
	var state, out [8]signatureVector
	var block [16]signatureVector
	for j := range 8 {
		state[j] = archsimd.BroadcastUint32x16(sha256InitialState[j])
	}
	for j := range 16 {
		var words [imageSignatureLanes]uint32
		for lane, job := range jobs {
			word := [4]byte{pad, pad, pad, pad}
			for k := range word {
				if j*4+k < len(job.key) {
					word[k] ^= job.key[j*4+k]
				}
			}
			words[lane] = binary.BigEndian.Uint32(word[:])
		}
		block[j] = archsimd.LoadUint32x16(&words)
	}
	compressSignatureLanes(&state, &block, &out)
	return out
}

// imageSignatureBlock は 32 バイトの値を HMAC の 2 ブロック目に詰める
func imageSignatureBlock(value *[8]signatureVector, block *[16]signatureVector) {
	copy(block[:8], value[:])
	block[8] = archsimd.BroadcastUint32x16(0x80000000)
	var zero signatureVector
	for j := 9; j < 15; j++ {
		block[j] = zero
	}
	block[15] = archsimd.BroadcastUint32x16((sha256.BlockSize + sha256.Size) * 8)
}

// signImageLanes は全レーンの署名を imageSignature と同じ値で計算し、各 job の sig へ書く。
// 1 周目だけ入力の長さが変わるので 1 本ずつ計算し、残りは 32 バイトの固定長なのでまとめて計算する
func signImageLanes(jobs *[imageSignatureLanes]*imageSignatureJob) {
	var words [8][imageSignatureLanes]uint32
	for lane, job := range jobs {
		mac := hmac.New(sha256.New, job.key)
		mac.Write([]byte(strconv.FormatInt(job.exp, 10) + ":" + job.imageID))
		sum := mac.Sum(nil)
		for j := range words {
			words[j][lane] = binary.BigEndian.Uint32(sum[j*4:])
		}
	}
	var value [8]signatureVector
	for j := range value {
		value[j] = archsimd.LoadUint32x16(&words[j])
	}
	inner, outer := hmacLaneState(jobs, 0x36), hmacLaneState(jobs, 0x5c)
	var block [16]signatureVector
	for range imageSignatureIterations - 1 {
		imageSignatureBlock(&value, &block)
		compressSignatureLanes(&inner, &block, &value)
		imageSignatureBlock(&value, &block)
		compressSignatureLanes(&outer, &block, &value)
	}
	for j := range value {
		value[j].Store(&words[j])
	}
	for lane, job := range jobs {
		sig := make([]byte, sha256.Size)
		for j := range words {
			binary.BigEndian.PutUint32(sig[j*4:], words[j][lane])
		}
		job.sig = sig
	}
}
