//go:build linux && (arm64 || amd64)

package preparednative

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestExecutableLinuxMappingIsSealedReadExecute(t *testing.T) {
	executable, err := Compile(nativeIdentityCode(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := executable.Close(); err != nil {
			t.Error(err)
		}
	})

	permissions, err := linuxMappingPermissions(executable.entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(permissions, "r") || !strings.Contains(permissions, "x") || strings.Contains(permissions, "w") {
		t.Fatalf("native mapping permissions = %q, want readable executable and not writable", permissions)
	}
}

func linuxMappingPermissions(address uintptr) (string, error) {
	maps, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(maps), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		bounds := strings.SplitN(fields[0], "-", 2)
		if len(bounds) != 2 {
			continue
		}
		start, startErr := strconv.ParseUint(bounds[0], 16, 64)
		end, endErr := strconv.ParseUint(bounds[1], 16, 64)
		if startErr == nil && endErr == nil && uint64(address) >= start && uint64(address) < end {
			return fields[1], nil
		}
	}
	return "", os.ErrNotExist
}
