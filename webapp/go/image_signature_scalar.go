//go:build !(goexperiment.simd && amd64)

package isureal

// signImageLanes は SIMD なしのビルドでは呼ばれない。ワーカーを起動しないので順番待ちの計算が使われる
func signImageLanes(*[imageSignatureLanes]*imageSignatureJob) {
	panic("image signature lanes are not available")
}
