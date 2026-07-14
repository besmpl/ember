package rejected

import (
	"plugin"
	"syscall"
)

func dynamicFixture() {
	_, _ = plugin.Open("fixture.so")
	_, _ = syscall.Mmap(0, 4096, syscall.PROT_READ|syscall.PROT_EXEC, syscall.MAP_PRIVATE, -1, 0)
}
