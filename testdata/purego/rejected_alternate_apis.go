package rejected

import "syscall"

func alternateProcessAPIs() {
	_, _ = syscall.ForkExec("helper", nil, nil)
	_, _ = syscall.StartProcess("helper", nil, nil)
	_ = syscall.Exec("helper", nil, nil)
	_, _, _ = syscall.Syscall(0, 0, 0, 0)
	_, _, _ = syscall.Syscall6(0, 0, 0, 0, 0, 0, 0)
	_, _, _ = syscall.RawSyscall(0, 0, 0, 0)
	_, _, _ = syscall.RawSyscall6(0, 0, 0, 0, 0, 0, 0)
	_ = syscall.Syscall9
	_ = syscall.RawSyscall9
	_, _ = syscall.Mmap(-1, 0, 4096, syscall.PROT_READ|syscall.PROT_EXEC, syscall.MAP_PRIVATE)
	_ = syscall.Mprotect(nil, syscall.PROT_READ|syscall.PROT_EXEC)
	_ = syscall.LoadDLL
	_ = syscall.NewLazyDLL
}
