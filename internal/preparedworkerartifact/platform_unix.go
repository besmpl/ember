//go:build darwin || linux

package preparedworkerartifact

import (
	"os"
	"path/filepath"
)

func replaceFile(source, target string) error { return os.Rename(source, target) }

func syncParent(path string) error {
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
