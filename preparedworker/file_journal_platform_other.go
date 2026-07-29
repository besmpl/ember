//go:build !darwin && !linux && !windows

package preparedworker

import (
	"fmt"
	"os"
)

func lockFileJournal(_ *os.File) error {
	return fmt.Errorf("exclusive ownership is unsupported on this platform")
}

func fileJournalOpenFlags() int { return 0 }

func syncFileJournalDirectory(string) error {
	return fmt.Errorf("directory durability is unsupported on this platform")
}
