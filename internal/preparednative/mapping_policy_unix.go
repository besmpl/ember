//go:build darwin || linux

package preparednative

import (
	"errors"
	"syscall"
)

func unixMappingPolicyUnavailable(err error) bool {
	return errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EINVAL)
}
