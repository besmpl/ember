//go:build !darwin

package preparedworker

import "os/exec"

func newWorkerParentLease(*exec.Cmd) (workerParentLease, error) {
	return noWorkerParentLease{}, nil
}

type noWorkerParentLease struct{}

func (noWorkerParentLease) AfterStart() error { return nil }
func (noWorkerParentLease) Close() error      { return nil }
