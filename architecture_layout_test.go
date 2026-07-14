package ember

import (
	"testing"
	"unsafe"
)

// expectedArchitectureLayoutSize records the intentional pointer-width
// variation in compact records that contain one or more pointers. Fixed-width
// scalar layouts should not use this helper.
func expectedArchitectureLayoutSize(size32, size64 uintptr) uintptr {
	switch unsafe.Sizeof(uintptr(0)) {
	case 4:
		return size32
	case 8:
		return size64
	default:
		panic("unsupported pointer width")
	}
}

func TestArchitecturePointerWidthAndFixedScalars(t *testing.T) {
	pointerSize := unsafe.Sizeof(uintptr(0))
	if pointerSize != 4 && pointerSize != 8 {
		t.Fatalf("pointer size = %d, want 4 or 8 bytes", pointerSize)
	}
	t.Logf("pointer width = %d bits", pointerSize*8)
	if got := unsafe.Sizeof(slot(0)); got != 8 {
		t.Fatalf("slot size = %d bytes, want fixed 8-byte storage", got)
	}
	if got := unsafe.Sizeof(wordcodeWord(0)); got != 4 {
		t.Fatalf("wordcodeWord size = %d bytes, want fixed 4-byte storage", got)
	}
}
