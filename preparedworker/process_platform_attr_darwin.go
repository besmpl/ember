//go:build darwin

package preparedworker

import "syscall"

func workerSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
