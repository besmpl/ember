package ember

// backendNativeArchitecture identifies the instruction set in an owner-neutral
// module image. Installation rejects an image that does not match the process.
type backendNativeArchitecture uint8

const (
	backendNativeArchitectureARM64 backendNativeArchitecture = iota + 1
	backendNativeArchitectureX8664
)

// backendNativeProgram is deterministic generated code bound to one exact
// Program. Executable memory and generation lifetime belong to the installer.
type backendNativeProgram struct {
	architecture    backendNativeArchitecture
	abiVersion      uint32
	semanticVersion uint32
	programHash     [32]byte
	modules         []backendNativeModule
}

type backendNativeModule struct {
	code      []byte
	functions []backendNativeFunction
}

type backendNativeFunction struct {
	offset         uint32
	bodyOffset     uint32
	parameterCount int
	argumentCount  int
	entry          bool
	prepared       bool
}
