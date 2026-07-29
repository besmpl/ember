//go:build darwin || linux

package preparedworker

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

func configureWorkerCommand(command *exec.Cmd) {
	command.SysProcAttr = workerSysProcAttr()
}

func workerProcessEnvironment() []string { return []string{} }

func newWorkerProcessOwner(process *os.Process) (workerProcessOwner, error) {
	if process == nil || process.Pid <= 0 {
		return nil, errors.New("invalid worker process")
	}
	return &unixWorkerProcessOwner{groupID: process.Pid}, nil
}

type unixWorkerProcessOwner struct {
	mu      sync.Mutex
	groupID int
	closed  bool
}

func (owner *unixWorkerProcessOwner) Terminate() error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.closed || owner.groupID <= 0 {
		return nil
	}
	if err := syscall.Kill(-owner.groupID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func (owner *unixWorkerProcessOwner) Close() error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.closed {
		return nil
	}
	owner.closed = true
	if owner.groupID <= 0 {
		return nil
	}
	if err := syscall.Kill(-owner.groupID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
