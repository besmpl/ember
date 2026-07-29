//go:build windows

package preparedworker

import (
	"os"

	"golang.org/x/sys/windows"
)

func fileJournalOpenFlags() int { return os.O_SYNC }

func lockFileJournal(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&overlapped,
	)
}

func syncFileJournalDirectory(string) error {
	// FILE_FLAG_WRITE_THROUGH on the creating handle flushes the file data and
	// metadata produced by CreateFile. Windows does not support flushing a
	// directory handle with FlushFileBuffers.
	return nil
}
