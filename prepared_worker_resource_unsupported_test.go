//go:build !darwin && !linux && !windows

package ember_test

import (
	"fmt"
	"runtime"
)

func samplePreparedWorkerResourcePoint(int) (preparedWorkerResourcePoint, error) {
	return preparedWorkerResourcePoint{}, fmt.Errorf(
		"prepared worker resource observation is unsupported on %s",
		runtime.GOOS,
	)
}
