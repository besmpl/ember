//go:build !cgo || (!darwin && !freebsd && !linux)

package preparedplugin_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/besmpl/ember/preparedplugin"
)

func TestOpenReportsUnsupportedWithoutNativeLoading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prepared.so")
	bundle, err := preparedplugin.Open(path)
	if bundle != nil || !errors.Is(err, preparedplugin.ErrUnsupported) {
		t.Fatalf("Open unsupported = (%v, %v), want ErrUnsupported", bundle, err)
	}
}
