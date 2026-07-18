//go:build windows && (arm64 || amd64)

package preparednative

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var flushInstructionCache = windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushInstructionCache")

func mapExecutable(code []byte) (_ []byte, resultErr error) {
	if err := flushInstructionCache.Find(); err != nil {
		return nil, fmt.Errorf("%w: resolve FlushInstructionCache: %v", ErrUnavailable, err)
	}
	address, err := windows.VirtualAlloc(
		0,
		uintptr(len(code)),
		windows.MEM_COMMIT|windows.MEM_RESERVE,
		windows.PAGE_READWRITE,
	)
	if err != nil {
		return nil, windowsExecutableMemoryError("allocate writable region", err)
	}
	memory := unsafe.Slice((*byte)(unsafe.Pointer(address)), len(code))
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, windows.VirtualFree(address, 0, windows.MEM_RELEASE))
		}
	}()

	copy(memory, code)
	var previousProtection uint32
	if err := windows.VirtualProtect(
		address,
		uintptr(len(memory)),
		windows.PAGE_EXECUTE_READ,
		&previousProtection,
	); err != nil {
		return nil, windowsExecutableMemoryError("seal executable region", err)
	}
	if previousProtection != windows.PAGE_READWRITE {
		return nil, fmt.Errorf(
			"seal executable region: previous protection %#x, want PAGE_READWRITE",
			previousProtection,
		)
	}
	if result, _, callErr := flushInstructionCache.Call(
		uintptr(windows.CurrentProcess()),
		address,
		uintptr(len(memory)),
	); result == 0 {
		if errors.Is(callErr, windows.ERROR_SUCCESS) {
			callErr = errors.New("FlushInstructionCache returned false")
		}
		return nil, windowsExecutableMemoryError("flush instruction cache", callErr)
	}
	return memory, nil
}

func unmapExecutable(memory []byte) error {
	if len(memory) == 0 {
		return nil
	}
	return windows.VirtualFree(
		uintptr(unsafe.Pointer(&memory[0])),
		0,
		windows.MEM_RELEASE,
	)
}

func windowsExecutableMemoryError(operation string, err error) error {
	if windowsMappingPolicyUnavailable(err) {
		return fmt.Errorf("%w: %s: %v", ErrUnavailable, operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func windowsMappingPolicyUnavailable(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_DYNAMIC_CODE_BLOCKED) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED_IN_APPCONTAINER) ||
		errors.Is(err, windows.ERROR_INVALID_PARAMETER)
}
