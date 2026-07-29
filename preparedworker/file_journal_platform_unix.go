//go:build darwin || linux

package preparedworker

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func fileJournalOpenFlags() int { return 0 }

func lockFileJournal(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func syncFileJournalDirectory(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	return directory.Close()
}
