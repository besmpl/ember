//go:build darwin && arm64

package architectureproof

import "testing"

func TestStaticAssemblyMatchesGo(t *testing.T) {
	for _, seed := range []int64{0, 1, 7, 29, 0x454d42455206} {
		if got, want := arithmeticForASM(seed), arithmeticFor(seed); got != want {
			t.Fatalf("seed=%d assembly=%d, Go=%d", seed, got, want)
		}
	}
}

func BenchmarkArchitectureLowering(b *testing.B) {
	b.Run("generated_go", func(b *testing.B) {
		var result int64
		for i := 0; i < b.N; i++ {
			result = arithmeticFor(int64(i))
		}
		checksumSink = result
	})
	b.Run("static_assembly", func(b *testing.B) {
		var result int64
		for i := 0; i < b.N; i++ {
			result = arithmeticForASM(int64(i))
		}
		checksumSink = result
	})
}
