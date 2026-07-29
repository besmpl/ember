package ember_test

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func samplePreparedWorkerResourcePoint(rootPID int) (preparedWorkerResourcePoint, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return preparedWorkerResourcePoint{}, fmt.Errorf("list Linux processes: %w", err)
	}
	processes := make([]preparedWorkerProcessResource, 0, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		process, err := readLinuxPreparedWorkerProcessResource(pid)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			continue
		}
		if err != nil {
			return preparedWorkerResourcePoint{}, err
		}
		processes = append(processes, process)
	}
	return summarizePreparedWorkerResourceSample(rootPID, processes), nil
}

func readLinuxPreparedWorkerProcessResource(pid int) (preparedWorkerProcessResource, error) {
	path := filepath.Join("/proc", strconv.Itoa(pid), "stat")
	data, err := os.ReadFile(path)
	if err != nil {
		return preparedWorkerProcessResource{}, fmt.Errorf("read Linux process %d resources: %w", pid, err)
	}
	closingName := strings.LastIndexByte(string(data), ')')
	if closingName < 0 || closingName+2 >= len(data) {
		return preparedWorkerProcessResource{}, fmt.Errorf("read Linux process %d resources: malformed stat", pid)
	}
	fields := strings.Fields(string(data[closingName+2:]))
	if len(fields) <= 21 {
		return preparedWorkerProcessResource{}, fmt.Errorf(
			"read Linux process %d resources: stat has %d fields after command",
			pid,
			len(fields),
		)
	}
	parentPID, err := strconv.Atoi(fields[1])
	if err != nil || parentPID < 0 {
		return preparedWorkerProcessResource{}, fmt.Errorf(
			"read Linux process %d resources: invalid parent %q",
			pid,
			fields[1],
		)
	}
	residentPages, err := strconv.ParseUint(fields[21], 10, 64)
	if err != nil {
		return preparedWorkerProcessResource{}, fmt.Errorf(
			"read Linux process %d resources: invalid RSS %q",
			pid,
			fields[21],
		)
	}
	createdAt, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || createdAt > math.MaxInt64 {
		return preparedWorkerProcessResource{}, fmt.Errorf(
			"read Linux process %d resources: invalid start time %q",
			pid,
			fields[19],
		)
	}
	pageSize := uint64(os.Getpagesize())
	if residentPages > math.MaxUint64/pageSize {
		return preparedWorkerProcessResource{}, fmt.Errorf("read Linux process %d resources: RSS overflows", pid)
	}
	return preparedWorkerProcessResource{
		PID:           pid,
		ParentPID:     parentPID,
		CreatedAt:     int64(createdAt),
		ResidentBytes: residentPages * pageSize,
	}, nil
}
