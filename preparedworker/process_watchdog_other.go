//go:build !darwin

package preparedworker

func startWorkerParentWatchdog(bool) (func(), error) {
	return func() {}, nil
}
