//go:build !cgo || (!darwin && !freebsd && !linux)

package preparedplugin

import "github.com/besmpl/ember"

func openPreparedBundle(string) (*ember.PreparedBundle, error) {
	return nil, ErrUnsupported
}
