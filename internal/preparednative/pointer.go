package preparednative

import "unsafe"

func uintptrPointer(memory []byte) uintptr {
	if len(memory) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&memory[0]))
}
