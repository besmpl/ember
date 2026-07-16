//go:build darwin && arm64

package architectureproof

// arithmeticForASM is the static Plan 9 assembly lowering of arithmeticFor.
// The declaration gives the Go linker the stable ABI0 boundary and pointer map.
func arithmeticForASM(seed int64) int64
