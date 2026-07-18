//go:build darwin && (arm64 || amd64)

package preparednative

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"

	"github.com/ebitengine/purego"
)

var (
	jitWriteMu sync.Mutex
	jitSymbols struct {
		once         sync.Once
		writeProtect uintptr
		invalidate   uintptr
		err          error
	}
)

func mapExecutable(code []byte) ([]byte, error) {
	if err := resolveJITSymbols(); err != nil {
		return nil, err
	}
	pageSize := syscall.Getpagesize()
	size := (len(code) + pageSize - 1) &^ (pageSize - 1)
	memory, err := syscall.Mmap(
		-1,
		0,
		size,
		syscall.PROT_READ|syscall.PROT_WRITE|syscall.PROT_EXEC,
		syscall.MAP_ANON|syscall.MAP_PRIVATE|syscall.MAP_JIT,
	)
	if err != nil {
		if unixMappingPolicyUnavailable(err) {
			return nil, fmt.Errorf("%w: allocate MAP_JIT region: %v", ErrUnavailable, err)
		}
		return nil, fmt.Errorf("allocate MAP_JIT region: %w", err)
	}

	jitWriteMu.Lock()
	runtime.LockOSThread()
	writeEnabled := false
	defer func() {
		if writeEnabled {
			purego.SyscallN(jitSymbols.writeProtect, 1)
		}
		runtime.UnlockOSThread()
		jitWriteMu.Unlock()
	}()
	purego.SyscallN(jitSymbols.writeProtect, 0)
	writeEnabled = true
	copy(memory, code)
	purego.SyscallN(
		jitSymbols.invalidate,
		uintptrPointer(memory),
		uintptr(len(code)),
	)
	purego.SyscallN(jitSymbols.writeProtect, 1)
	writeEnabled = false
	return memory, nil
}

func resolveJITSymbols() error {
	jitSymbols.once.Do(func() {
		jitSymbols.writeProtect, jitSymbols.err = purego.Dlsym(
			purego.RTLD_DEFAULT,
			"pthread_jit_write_protect_np",
		)
		if jitSymbols.err != nil {
			jitSymbols.err = fmt.Errorf(
				"%w: resolve pthread_jit_write_protect_np: %v",
				ErrUnavailable,
				jitSymbols.err,
			)
			return
		}
		jitSymbols.invalidate, jitSymbols.err = purego.Dlsym(
			purego.RTLD_DEFAULT,
			"sys_icache_invalidate",
		)
		if jitSymbols.err != nil {
			jitSymbols.err = fmt.Errorf(
				"%w: resolve sys_icache_invalidate: %v",
				ErrUnavailable,
				jitSymbols.err,
			)
		}
	})
	return jitSymbols.err
}

func unmapExecutable(memory []byte) error {
	return syscall.Munmap(memory)
}
