package preparednative

import "testing"

func TestNativeExecutionPlatformAdmission(t *testing.T) {
	for _, test := range []struct {
		name       string
		goos       string
		goarch     string
		hasSSE41   bool
		wantNative bool
	}{
		{name: "Darwin ARM64", goos: "darwin", goarch: "arm64", wantNative: true},
		{name: "Darwin x86-64", goos: "darwin", goarch: "amd64", hasSSE41: true, wantNative: true},
		{name: "old Darwin x86-64", goos: "darwin", goarch: "amd64", wantNative: false},
		{name: "Linux ARM64", goos: "linux", goarch: "arm64", wantNative: true},
		{name: "Linux x86-64", goos: "linux", goarch: "amd64", hasSSE41: true, wantNative: true},
		{name: "old Linux x86-64", goos: "linux", goarch: "amd64", wantNative: false},
		{name: "Windows ARM64", goos: "windows", goarch: "arm64", wantNative: true},
		{name: "Windows x86-64", goos: "windows", goarch: "amd64", hasSSE41: true, wantNative: true},
		{name: "old Windows x86-64", goos: "windows", goarch: "amd64", wantNative: false},
		{name: "unsupported ISA", goos: "darwin", goarch: "riscv64", wantNative: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nativeExecutionPlatformAvailable(test.goos, test.goarch, test.hasSSE41); got != test.wantNative {
				t.Fatalf("nativeExecutionPlatformAvailable(%q, %q, %t) = %t, want %t", test.goos, test.goarch, test.hasSSE41, got, test.wantNative)
			}
		})
	}
}
