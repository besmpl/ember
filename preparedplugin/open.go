package preparedplugin

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/besmpl/ember"
)

// ErrUnsupported reports that this build cannot open Go plugins. Go's plugin
// loader requires cgo and supports only Darwin, FreeBSD, and Linux.
var ErrUnsupported = errors.New("ember prepared plugin: unsupported platform or cgo-disabled build")

// Open loads the generated Bundle symbol from an immutable Go plugin artifact.
// path must be absolute. The artifact is trusted native application code and
// must have been built with the same Go toolchain, build configuration, and
// shared dependencies as the host process.
func Open(path string) (*ember.PreparedBundle, error) {
	if path == "" {
		return nil, fmt.Errorf("open prepared plugin: empty path")
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("open prepared plugin: path must be absolute: %q", path)
	}
	bundle, err := openPreparedBundle(path)
	if err != nil {
		return nil, fmt.Errorf("open prepared plugin %q: %w", path, err)
	}
	return bundle, nil
}
