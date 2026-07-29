//go:build windows

package preparedworker

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func configureWorkerCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func workerProcessEnvironment() []string {
	keys := []string{"SYSTEMROOT", "WINDIR", "TEMP", "TMP"}
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok && value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	return environment
}

func newWorkerProcessOwner(process *os.Process) (workerProcessOwner, error) {
	if process == nil || process.Pid <= 0 {
		return nil, errors.New("invalid worker process")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	information := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	information.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&information)),
		uint32(unsafe.Sizeof(information)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	processHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(process.Pid),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	err = windows.AssignProcessToJobObject(job, processHandle)
	_ = windows.CloseHandle(processHandle)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &windowsWorkerProcessOwner{job: job}, nil
}

type windowsWorkerProcessOwner struct {
	mu     sync.Mutex
	job    windows.Handle
	closed bool
}

func (owner *windowsWorkerProcessOwner) Terminate() error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.closed || owner.job == 0 {
		return nil
	}
	return windows.TerminateJobObject(owner.job, 1)
}

func (owner *windowsWorkerProcessOwner) Close() error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.closed || owner.job == 0 {
		return nil
	}
	owner.closed = true
	err := windows.CloseHandle(owner.job)
	owner.job = 0
	return err
}
