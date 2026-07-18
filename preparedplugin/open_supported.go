//go:build cgo && (darwin || freebsd || linux)

package preparedplugin

import (
	"fmt"
	"plugin"

	"github.com/besmpl/ember"
)

func openPreparedBundle(path string) (*ember.PreparedBundle, error) {
	loaded, err := plugin.Open(path)
	if err != nil {
		return nil, err
	}
	symbol, err := loaded.Lookup("Bundle")
	if err != nil {
		return nil, fmt.Errorf("lookup Bundle: %w", err)
	}
	bundlePointer, ok := symbol.(**ember.PreparedBundle)
	if !ok {
		return nil, fmt.Errorf("Bundle has type %T, want **ember.PreparedBundle", symbol)
	}
	if bundlePointer == nil || *bundlePointer == nil {
		return nil, fmt.Errorf("Bundle is nil")
	}
	return *bundlePointer, nil
}
