package ember_test

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func samplePreparedWorkerResourcePoint(rootPID int) (preparedWorkerResourcePoint, error) {
	command := exec.Command("/bin/ps", "-axo", "pid=,ppid=,rss=,lstart=")
	command.Env = append(os.Environ(), "LC_ALL=C")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return preparedWorkerResourcePoint{}, fmt.Errorf("start Darwin process observer: %w", err)
	}
	observerPID := command.Process.Pid
	if err := command.Wait(); err != nil {
		return preparedWorkerResourcePoint{}, fmt.Errorf(
			"wait for Darwin process observer: %w: %s",
			err,
			strings.TrimSpace(stderr.String()),
		)
	}
	processes := make([]preparedWorkerProcessResource, 0, 128)
	for lineNumber, line := range strings.Split(stdout.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 8 {
			return preparedWorkerResourcePoint{}, fmt.Errorf(
				"parse Darwin process observer line %d: got %d fields",
				lineNumber+1,
				len(fields),
			)
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			return preparedWorkerResourcePoint{}, fmt.Errorf(
				"parse Darwin process observer line %d: invalid PID %q",
				lineNumber+1,
				fields[0],
			)
		}
		if pid == observerPID {
			continue
		}
		parentPID, err := strconv.Atoi(fields[1])
		if err != nil || parentPID < 0 {
			return preparedWorkerResourcePoint{}, fmt.Errorf(
				"parse Darwin process observer line %d: invalid parent PID %q",
				lineNumber+1,
				fields[1],
			)
		}
		residentKiB, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil || residentKiB > math.MaxUint64/1024 {
			return preparedWorkerResourcePoint{}, fmt.Errorf(
				"parse Darwin process observer line %d: invalid RSS %q",
				lineNumber+1,
				fields[2],
			)
		}
		createdAt, err := time.Parse("Mon Jan 2 15:04:05 2006", strings.Join(fields[3:], " "))
		if err != nil {
			return preparedWorkerResourcePoint{}, fmt.Errorf(
				"parse Darwin process observer line %d: invalid start time: %w",
				lineNumber+1,
				err,
			)
		}
		processes = append(processes, preparedWorkerProcessResource{
			PID:           pid,
			ParentPID:     parentPID,
			CreatedAt:     createdAt.UnixNano(),
			ResidentBytes: residentKiB * 1024,
		})
	}
	return summarizePreparedWorkerResourceSample(rootPID, processes), nil
}
