//go:build windows && (arm64 || amd64)

package preparednative

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestExecutableWindowsMappingIsSealedReadExecute(t *testing.T) {
	executable, err := Compile(nativeIdentityCode(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := executable.Close(); err != nil {
			t.Error(err)
		}
	})

	var information windows.MemoryBasicInformation
	if err := windows.VirtualQuery(
		executable.entry,
		&information,
		unsafe.Sizeof(information),
	); err != nil {
		t.Fatal(err)
	}
	if information.State != windows.MEM_COMMIT || information.Protect != windows.PAGE_EXECUTE_READ {
		t.Fatalf(
			"native mapping state/protection = %#x/%#x, want MEM_COMMIT/PAGE_EXECUTE_READ",
			information.State,
			information.Protect,
		)
	}
}

func TestWindowsMappingPolicyFailuresAreUnavailable(t *testing.T) {
	for _, err := range []error{
		windows.ERROR_ACCESS_DENIED,
		windows.ERROR_DYNAMIC_CODE_BLOCKED,
		windows.ERROR_NOT_SUPPORTED,
		windows.ERROR_NOT_SUPPORTED_IN_APPCONTAINER,
		windows.ERROR_INVALID_PARAMETER,
	} {
		if !windowsMappingPolicyUnavailable(err) {
			t.Fatalf("windowsMappingPolicyUnavailable(%v) = false, want true", err)
		}
	}
	if windowsMappingPolicyUnavailable(windows.ERROR_NOT_ENOUGH_MEMORY) {
		t.Fatal("windowsMappingPolicyUnavailable(ERROR_NOT_ENOUGH_MEMORY) = true, want explicit resource failure")
	}
}
