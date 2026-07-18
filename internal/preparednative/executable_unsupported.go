//go:build !darwin || (!arm64 && !amd64)

package preparednative

import "fmt"

func mapExecutable([]byte) ([]byte, error) {
	return nil, fmt.Errorf("%w on this platform", ErrUnavailable)
}

func callExecutable(uintptr, uintptr, uintptr) (uintptr, float64, error) {
	return 0, 0, ErrUnavailable
}

func unmapExecutable([]byte) error {
	return nil
}
