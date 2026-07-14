package allowed

import (
	os "os"
	exec "os/exec"
	"syscall"
)

type fakeExec struct{}

func (fakeExec) Command(string, ...string) {}

type fakeOS struct{}

func (fakeOS) StartProcess(string, []string, any) {}

type fakeSyscall struct{}

func (fakeSyscall) Mmap() {}
func (fakeSyscall) Syscall() {}

func harmlessShadowedIdentifiers() {
	exec := fakeExec{}
	exec.Command("not-a-process")
	os := fakeOS{}
	os.StartProcess("not-a-process", nil, nil)
	syscall := fakeSyscall{}
	syscall.Mmap()
	syscall.Syscall()
	Mmap := func() {}
	Mmap()
}
