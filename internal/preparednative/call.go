//go:build (darwin || linux || windows) && (arm64 || amd64)

package preparednative

import (
	"sync"
	"unsafe"
)

var nativeCallPool = sync.Pool{New: func() any { return new(nativeCallFrame) }}

type nativeCallFrame struct {
	entry     uintptr
	arguments uintptr
	count     uintptr
	result    float64
	status    uintptr
}

var nativeCallTrampolineABI0 uintptr

// runtimeCGOCall enters foreign code on the runtime's system stack. Windows
// supports this transition directly; cgo-disabled Unix builds install purego's
// all-Go runtime/cgo substitute from call_unix_runtime.go.
//
//go:linkname runtimeCGOCall runtime.cgocall
func runtimeCGOCall(function uintptr, argument unsafe.Pointer) int32

func callExecutable(entry, arguments, count uintptr) (uintptr, float64, error) {
	frame := nativeCallPool.Get().(*nativeCallFrame)
	*frame = nativeCallFrame{entry: entry, arguments: arguments, count: count}
	runtimeCGOCall(nativeCallTrampolineABI0, unsafe.Pointer(frame))
	status := frame.status
	result := frame.result
	*frame = nativeCallFrame{}
	nativeCallPool.Put(frame)
	return status, result, nil
}
