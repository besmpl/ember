package ember_test

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsPreparedWorkerMaximumProcessSnapshotBytes = uint32(64 << 20)

func samplePreparedWorkerResourcePoint(rootPID int) (preparedWorkerResourcePoint, error) {
	entrySize := uint32(unsafe.Sizeof(windows.SYSTEM_PROCESS_INFORMATION{}))
	bufferSize := (entrySize + uint32(unsafe.Sizeof(uintptr(0)))) * 1024
	for {
		if bufferSize == 0 || bufferSize > windowsPreparedWorkerMaximumProcessSnapshotBytes {
			return preparedWorkerResourcePoint{}, fmt.Errorf(
				"list Windows processes: required buffer %d exceeds %d bytes",
				bufferSize,
				windowsPreparedWorkerMaximumProcessSnapshotBytes,
			)
		}
		buffer := make([]byte, bufferSize)
		required := bufferSize
		err := windows.NtQuerySystemInformation(
			windows.SystemProcessInformation,
			unsafe.Pointer(&buffer[0]),
			bufferSize,
			&required,
		)
		if err == windows.STATUS_INFO_LENGTH_MISMATCH {
			if required <= bufferSize {
				required = bufferSize * 2
			}
			bufferSize = required
			continue
		}
		if err != nil {
			return preparedWorkerResourcePoint{}, fmt.Errorf("list Windows processes: %w", err)
		}
		processes, err := decodeWindowsPreparedWorkerProcessResources(buffer, entrySize)
		if err != nil {
			return preparedWorkerResourcePoint{}, err
		}
		return summarizePreparedWorkerResourceSample(rootPID, processes), nil
	}
}

func decodeWindowsPreparedWorkerProcessResources(
	buffer []byte,
	entrySize uint32,
) ([]preparedWorkerProcessResource, error) {
	processes := make([]preparedWorkerProcessResource, 0, 128)
	for offset := uint32(0); ; {
		if entrySize == 0 || offset > uint32(len(buffer)) ||
			entrySize > uint32(len(buffer))-offset {
			return nil, fmt.Errorf("decode Windows process resources: entry at %d exceeds %d bytes", offset, len(buffer))
		}
		entry := (*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&buffer[offset]))
		if entry.UniqueProcessID != 0 {
			processes = append(processes, preparedWorkerProcessResource{
				PID:           int(entry.UniqueProcessID),
				ParentPID:     int(entry.InheritedFromUniqueProcessID),
				CreatedAt:     entry.CreateTime,
				ResidentBytes: uint64(entry.WorkingSetSize),
			})
		}
		if entry.NextEntryOffset == 0 {
			return processes, nil
		}
		if entry.NextEntryOffset < entrySize || entry.NextEntryOffset > uint32(len(buffer))-offset {
			return nil, fmt.Errorf(
				"decode Windows process resources: invalid next offset %d at %d",
				entry.NextEntryOffset,
				offset,
			)
		}
		offset += entry.NextEntryOffset
	}
}
