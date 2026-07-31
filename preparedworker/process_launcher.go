package preparedworker

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

const processStderrBytes = 64 << 10

type workerProcessOwner interface {
	Terminate() error
	Close() error
}

type workerParentLease interface {
	AfterStart() error
	Close() error
}

type osProcessLauncher struct{}

func (osProcessLauncher) Launch(artifact *processArtifact) (*processChild, error) {
	if artifact == nil || artifact.executable == "" {
		return nil, fmt.Errorf("prepared worker process backend: missing executable")
	}
	command := exec.Command(artifact.executable)
	command.Env = workerProcessEnvironment()
	return launchWorkerCommand(command)
}

func launchWorkerCommand(command *exec.Cmd) (*processChild, error) {
	if command == nil {
		return nil, fmt.Errorf("prepared worker process backend: nil command")
	}
	if command.Stdin != nil || command.Stdout != nil {
		return nil, fmt.Errorf("prepared worker process backend: command transport is already configured")
	}
	configureWorkerCommand(command)
	lease, err := newWorkerParentLease(command)
	if err != nil {
		return nil, fmt.Errorf("prepared worker process backend: establish parent lease: %w", err)
	}
	workerInput, input, err := os.Pipe()
	if err != nil {
		_ = lease.Close()
		return nil, fmt.Errorf("prepared worker process backend: open stdin: %w", err)
	}
	output, workerOutput, err := os.Pipe()
	if err != nil {
		_ = workerInput.Close()
		_ = input.Close()
		_ = lease.Close()
		return nil, fmt.Errorf("prepared worker process backend: open stdout: %w", err)
	}
	command.Stdin = workerInput
	command.Stdout = workerOutput
	stderr := &boundedProcessBuffer{limit: processStderrBytes}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		_ = workerInput.Close()
		_ = input.Close()
		_ = output.Close()
		_ = workerOutput.Close()
		_ = lease.Close()
		return nil, err
	}
	workerPipeErr := errors.Join(workerInput.Close(), workerOutput.Close())
	if err := errors.Join(workerPipeErr, lease.AfterStart()); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = input.Close()
		_ = output.Close()
		_ = lease.Close()
		return nil, fmt.Errorf("prepared worker process backend: activate parent lease: %w", err)
	}
	owner, err := newWorkerProcessOwner(command.Process)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = input.Close()
		_ = output.Close()
		_ = lease.Close()
		return nil, fmt.Errorf("prepared worker process backend: establish child ownership: %w", err)
	}
	wait := make(chan error, 1)
	go func() {
		waitErr := command.Wait()
		ownerErr := owner.Close()
		leaseErr := lease.Close()
		wait <- errors.Join(waitErr, ownerErr, leaseErr)
	}()
	return &processChild{
		input:     input,
		output:    output,
		wait:      wait,
		terminate: owner.Terminate,
		stderr:    stderr.String,
	}, nil
}

type boundedProcessBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func (buffer *boundedProcessBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	written := len(data)
	if buffer.limit <= 0 || len(data) == 0 {
		return written, nil
	}
	if len(data) >= buffer.limit {
		buffer.data = append(buffer.data[:0], data[len(data)-buffer.limit:]...)
		return written, nil
	}
	overflow := len(buffer.data) + len(data) - buffer.limit
	if overflow > 0 {
		copy(buffer.data, buffer.data[overflow:])
		buffer.data = buffer.data[:len(buffer.data)-overflow]
	}
	buffer.data = append(buffer.data, data...)
	return written, nil
}

func (buffer *boundedProcessBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return string(append([]byte(nil), buffer.data...))
}
