//go:build darwin || linux

package preparednative

import (
	"syscall"
	"testing"
)

func TestUnixMappingPolicyFailuresAreUnavailable(t *testing.T) {
	for _, err := range []error{syscall.EPERM, syscall.EACCES, syscall.ENOTSUP, syscall.EINVAL} {
		if !unixMappingPolicyUnavailable(err) {
			t.Fatalf("unixMappingPolicyUnavailable(%v) = false, want true", err)
		}
	}
	if unixMappingPolicyUnavailable(syscall.ENOMEM) {
		t.Fatal("unixMappingPolicyUnavailable(ENOMEM) = true, want explicit resource failure")
	}
}
