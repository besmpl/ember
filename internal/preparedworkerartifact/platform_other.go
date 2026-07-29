//go:build !darwin && !linux && !windows

package preparedworkerartifact

import "os"

func replaceFile(source, target string) error { return os.Rename(source, target) }

func syncParent(string) error { return nil }
