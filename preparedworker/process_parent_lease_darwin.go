//go:build darwin

package preparedworker

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"sync"
)

const workerParentLeaseEnvironment = "EMBER_PREPARED_WORKER_PARENT_FD"

func newWorkerParentLease(command *exec.Cmd) (workerParentLease, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	childFD := 3 + len(command.ExtraFiles)
	command.ExtraFiles = append(command.ExtraFiles, reader)
	command.Env = append(command.Env, workerParentLeaseEnvironment+"="+strconv.Itoa(childFD))
	return &darwinWorkerParentLease{reader: reader, writer: writer}, nil
}

type darwinWorkerParentLease struct {
	mu     sync.Mutex
	reader *os.File
	writer *os.File
}

func (lease *darwinWorkerParentLease) AfterStart() error {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.reader == nil {
		return nil
	}
	err := lease.reader.Close()
	lease.reader = nil
	return err
}

func (lease *darwinWorkerParentLease) Close() error {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	var failures []error
	if lease.reader != nil {
		failures = append(failures, lease.reader.Close())
		lease.reader = nil
	}
	if lease.writer != nil {
		failures = append(failures, lease.writer.Close())
		lease.writer = nil
	}
	return errors.Join(failures...)
}
