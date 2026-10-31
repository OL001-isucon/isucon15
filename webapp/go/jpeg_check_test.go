package isureal

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func sampleJPEG(t testing.TB, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	random := rand.New(rand.NewSource(1))
	for y := range size {
		for x := range size {
			img.Set(x, y, color.RGBA{uint8(random.Intn(256)), uint8(x), uint8(y), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// 全デコードと判定が同じことを確かめる
func assertSameAsFullDecode(t *testing.T, name string, data []byte) {
	t.Helper()
	full, fullErr := jpeg.Decode(bytes.NewReader(data))
	config, err := inspectJPEG(data)
	if (fullErr == nil) != (err == nil) {
		t.Errorf("%s: full decode err=%v, inspect err=%v", name, fullErr, err)
		return
	}
	if err == nil && (full.Bounds().Dx() != config.Width || full.Bounds().Dy() != config.Height) {
		t.Errorf("%s: size differs", name)
	}
}

func TestInspectJPEGAcceptsValidImages(t *testing.T) {
	for _, size := range []int{1, 8, 64, 200} {
		assertSameAsFullDecode(t, "valid", sampleJPEG(t, size))
	}
	data := sampleJPEG(t, 64)
	comment := append([]byte{0xff, 0xfe, 0x00, 0x09}, []byte("ISUReal")...)
	assertSameAsFullDecode(t, "comment", append(append(append([]byte{}, data[:2]...), comment...), data[2:]...))
	assertSameAsFullDecode(t, "trailing", append(append([]byte{}, data...), 1, 2, 3))
}

func TestInspectJPEGRejectsBrokenImages(t *testing.T) {
	data := sampleJPEG(t, 64)
	for i := 0; i < len(data)-2; i += 37 {
		assertSameAsFullDecode(t, "truncated", data[:i])
	}
	assertSameAsFullDecode(t, "without EOI", data[:len(data)-2])
	assertSameAsFullDecode(t, "empty", nil)
	assertSameAsFullDecode(t, "text", []byte("this is not an image at all"))
	assertSameAsFullDecode(t, "SOI only", []byte{0xff, 0xd8})
	assertSameAsFullDecode(t, "SOI and EOI", []byte{0xff, 0xd8, 0xff, 0xd9})
	random := make([]byte, 4096)
	rand.New(rand.NewSource(2)).Read(random)
	assertSameAsFullDecode(t, "random", random)
	assertSameAsFullDecode(t, "random with SOI", append([]byte{0xff, 0xd8}, random...))

	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	assertSameAsFullDecode(t, "png", pngData.Bytes())
}

// 実際にベンチが投稿した画像で確かめる。JPEG_SAMPLE_DIR を指定したときだけ動く
func TestInspectJPEGOnSamples(t *testing.T) {
	root := os.Getenv("JPEG_SAMPLE_DIR")
	if root == "" {
		t.Skip("JPEG_SAMPLE_DIR is not set")
	}
	count := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		count++
		assertSameAsFullDecode(t, path, data)
		assertSameAsFullDecode(t, path+" truncated", data[:len(data)/2])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("checked %d samples", count)
}

func benchmarkJPEG(b *testing.B, check func([]byte)) {
	data := sampleJPEG(b, 2000)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		check(data)
	}
}

func BenchmarkFullDecode(b *testing.B) {
	benchmarkJPEG(b, func(data []byte) { jpeg.Decode(bytes.NewReader(data)) })
}

func BenchmarkInspectJPEG(b *testing.B) {
	benchmarkJPEG(b, func(data []byte) { inspectJPEG(data) })
}
