package preparednative

import (
	"encoding/binary"
	"runtime"
	"testing"
)

func TestExecutableCallsImmutableNativeKernel(t *testing.T) {
	code := nativeIdentityCode(t)
	executable, err := Compile(code)
	if err != nil {
		t.Fatal(err)
	}
	clear(code)
	if got, prepared, err := executable.Call(17.25); err != nil || !prepared || got != 17.25 {
		t.Fatalf("native identity = %v/%t, %v", got, prepared, err)
	}
	if err := executable.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := executable.Call(1); err == nil {
		t.Fatal("closed executable accepted a call")
	}
}

func TestCompileRejectsMalformedNativeCode(t *testing.T) {
	codes := [][]byte{nil}
	if runtime.GOARCH == "arm64" {
		codes = append(codes, []byte{0, 1, 2})
	}
	for _, code := range codes {
		if executable, err := Compile(code); err == nil {
			_ = executable.Close()
			t.Fatalf("Compile(%x) succeeded", code)
		}
	}
}

func BenchmarkExecutableCallIdentity(b *testing.B) {
	code := nativeIdentityCode(b)
	executable, err := Compile(code)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := executable.Close(); err != nil {
			b.Error(err)
		}
	})
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, prepared, err := executable.Call(17.25); err != nil || !prepared {
			b.Fatalf("native call = prepared %t, %v", prepared, err)
		}
	}
}

type nativeTestHelper interface {
	Helper()
	Skip(args ...any)
}

func nativeIdentityCode(test nativeTestHelper) []byte {
	test.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		test.Skip("prepared native execution is currently implemented on Darwin, Linux, and Windows")
		return nil
	}
	switch runtime.GOARCH {
	case "arm64":
		// ldr d0, [x0]; str d0, [x2]; mov x0, #1; ret
		words := []uint32{0xfd400000, 0xfd000040, 0xd2800020, 0xd65f03c0}
		code := make([]byte, len(words)*4)
		for index, word := range words {
			binary.LittleEndian.PutUint32(code[index*4:], word)
		}
		return code
	case "amd64":
		// movsd xmm0,[rdi]; movsd [rdx],xmm0; mov eax,1; ret
		return []byte{
			0xf2, 0x0f, 0x10, 0x07,
			0xf2, 0x0f, 0x11, 0x02,
			0xb8, 0x01, 0x00, 0x00, 0x00,
			0xc3,
		}
	default:
		test.Skip("prepared native execution is unavailable on " + runtime.GOARCH)
		return nil
	}
}
