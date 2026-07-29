//go:build darwin

package preparedworker

import (
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
)

func startWorkerParentWatchdog(required bool) (func(), error) {
	if !required {
		return func() {}, nil
	}
	text := os.Getenv(workerParentLeaseEnvironment)
	fd, err := strconv.Atoi(text)
	if err != nil || fd < 3 {
		return nil, fmt.Errorf("prepared worker server: parent lease is missing")
	}
	lease := os.NewFile(uintptr(fd), "ember-worker-parent")
	if lease == nil {
		return nil, fmt.Errorf("prepared worker server: parent lease is invalid")
	}
	var stopped atomic.Bool
	go func() {
		var data [1]byte
		_, _ = lease.Read(data[:])
		if !stopped.Load() {
			os.Exit(70)
		}
	}()
	return func() {
		stopped.Store(true)
		_ = lease.Close()
	}, nil
}
