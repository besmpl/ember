//go:build !darwin && !linux

package preparedworkerbuild

func syncDirectory(string) error { return nil }
