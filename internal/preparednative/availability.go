package preparednative

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"

	"golang.org/x/sys/cpu"
)

// Available reports whether this process can install and execute Ember's
// current native-code subset without changing the canonical fallback.
func Available() error {
	return validateNativeExecutionPlatform()
}

func validateNativeExecutionPlatform() error {
	if nativeExecutionPlatformAvailable(runtime.GOOS, runtime.GOARCH, cpu.X86.HasSSE41) {
		return nil
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "amd64" && !cpu.X86.HasSSE41 {
		return fmt.Errorf("%w: x86-64 native code requires SSE4.1", ErrUnavailable)
	}
	return fmt.Errorf("%w on %s/%s", ErrUnavailable, runtime.GOOS, runtime.GOARCH)
}

func nativeExecutionPlatformAvailable(goos, goarch string, hasSSE41 bool) bool {
	if goos != "darwin" {
		return false
	}
	switch goarch {
	case "arm64":
		return true
	case "amd64":
		return hasSSE41
	default:
		return false
	}
}

func nativeMappingPolicyUnavailable(err error) bool {
	return errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EINVAL)
}
