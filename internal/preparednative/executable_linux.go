//go:build linux && (arm64 || amd64)

package preparednative

import (
	"errors"
	"fmt"
	"syscall"
)

func mapExecutable(code []byte) (_ []byte, resultErr error) {
	pageSize := syscall.Getpagesize()
	size := (len(code) + pageSize - 1) &^ (pageSize - 1)
	memory, err := syscall.Mmap(
		-1,
		0,
		size,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_ANON|syscall.MAP_PRIVATE,
	)
	if err != nil {
		if unixMappingPolicyUnavailable(err) {
			return nil, fmt.Errorf("%w: allocate writable region: %v", ErrUnavailable, err)
		}
		return nil, fmt.Errorf("allocate writable region: %w", err)
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, syscall.Munmap(memory))
		}
	}()

	copy(memory, code)
	if err := syscall.Mprotect(memory, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		if unixMappingPolicyUnavailable(err) {
			return nil, fmt.Errorf("%w: seal executable region: %v", ErrUnavailable, err)
		}
		return nil, fmt.Errorf("seal executable region: %w", err)
	}
	return memory, nil
}

func unmapExecutable(memory []byte) error {
	return syscall.Munmap(memory)
}
