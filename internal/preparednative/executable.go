package preparednative

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// ErrUnavailable reports that this process cannot safely execute reload-time
// native code on the current platform or under its current security policy.
var ErrUnavailable = errors.New("prepared native execution is unavailable")

// Executable owns one immutable native-code mapping. Calls hold a read lease,
// so Close cannot unmap code while another goroutine is executing it.
type Executable struct {
	mu             sync.RWMutex
	memory         []byte
	codeSize       uint32
	entryAlignment uint32
	entry          uintptr
	closed         bool
}

// Compile validates and materializes a complete native function. The supplied
// bytes are copied into an executable mapping and are never writable again
// through this API.
func Compile(code []byte) (*Executable, error) {
	if err := validateNativeExecutionPlatform(); err != nil {
		return nil, fmt.Errorf("compile prepared native code: %w", err)
	}
	alignment := nativeInstructionAlignment()
	if len(code) == 0 || uint64(len(code)) > uint64(^uint32(0)) || len(code)%int(alignment) != 0 {
		return nil, fmt.Errorf(
			"compile prepared native code: length %d is not a valid %d-byte-aligned instruction stream",
			len(code),
			alignment,
		)
	}
	memory, err := mapExecutable(code)
	if err != nil {
		return nil, fmt.Errorf("compile prepared native code: %w", err)
	}
	return &Executable{
		memory:         memory,
		codeSize:       uint32(len(code)),
		entryAlignment: alignment,
		entry:          uintptr(unsafe.Pointer(&memory[0])),
	}, nil
}

// Call executes the mapping's narrow boundary-adapter ABI. The first three
// platform integer argument registers carry the f64 argument pointer, count,
// and result pointer; the integer return register reports 1 for prepared
// completion or 0 for canonical replay. Generated code cannot retain either
// Go pointer after returning.
func (executable *Executable) Call(arguments ...float64) (float64, bool, error) {
	return executable.CallAt(0, arguments...)
}

// CallAt executes a function at an instruction-aligned byte offset in the
// immutable image. One mapping can therefore own every function in a module.
func (executable *Executable) CallAt(offset uint32, arguments ...float64) (float64, bool, error) {
	if executable == nil {
		return 0, false, errors.New("call prepared native code: nil executable")
	}
	executable.mu.RLock()
	defer executable.mu.RUnlock()
	if executable.closed {
		return 0, false, errors.New("call prepared native code: executable is closed")
	}
	if offset%executable.entryAlignment != 0 || offset >= executable.codeSize {
		return 0, false, fmt.Errorf("call prepared native code: invalid entry offset %d", offset)
	}
	var argumentPointer uintptr
	if len(arguments) != 0 {
		argumentPointer = uintptr(unsafe.Pointer(&arguments[0]))
	}
	status, result, err := callExecutable(
		executable.entry+uintptr(offset),
		argumentPointer,
		uintptr(len(arguments)),
	)
	runtime.KeepAlive(arguments)
	if err != nil {
		return 0, false, fmt.Errorf("call prepared native code: %w", err)
	}
	switch status {
	case 0:
		return 0, false, nil
	case 1:
		return result, true, nil
	default:
		return 0, false, fmt.Errorf("call prepared native code: invalid status %d", status)
	}
}

// Close waits for in-flight calls and then releases the executable mapping.
func (executable *Executable) Close() error {
	if executable == nil {
		return nil
	}
	executable.mu.Lock()
	defer executable.mu.Unlock()
	if executable.closed {
		return nil
	}
	if err := unmapExecutable(executable.memory); err != nil {
		return fmt.Errorf("close prepared native code: %w", err)
	}
	executable.memory = nil
	executable.codeSize = 0
	executable.entryAlignment = 0
	executable.entry = 0
	executable.closed = true
	return nil
}

func nativeInstructionAlignment() uint32 {
	if runtime.GOARCH == "arm64" {
		return 4
	}
	return 1
}
