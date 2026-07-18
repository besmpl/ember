// Package preparedplugin opens Ember prepared bundles compiled as Go plugins.
//
// This package is an opt-in editor and development adapter. It requires cgo
// and a Go plugin platform, and loaded code remains mapped for the lifetime of
// the process. Applications should ship statically linked prepared bundles and
// use immutable, content-addressed plugin paths for reload generations.
package preparedplugin
