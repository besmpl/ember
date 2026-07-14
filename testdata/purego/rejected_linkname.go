package rejected

import _ "unsafe"

//go:linkname privateRuntimeSymbol runtime.privateRuntimeSymbol
func privateRuntimeSymbol()
