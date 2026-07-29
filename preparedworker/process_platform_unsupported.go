//go:build !darwin && !linux && !windows

package preparedworker

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func configureWorkerCommand(*exec.Cmd) {}

func workerProcessEnvironment() []string { return nil }

func newWorkerProcessOwner(*os.Process) (workerProcessOwner, error) {
	return nil, fmt.Errorf("prepared worker process backend: unsupported platform %s", runtime.GOOS)
}
