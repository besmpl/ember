package ember

// PreparedNativeX8664FunctionForTest describes one function in an immutable
// x86-64 module image.
type PreparedNativeX8664FunctionForTest struct {
	Offset         uint32
	BodyOffset     uint32
	ParameterCount int
	Emitted        bool
	Prepared       bool
}

// PreparedNativeX8664ModuleForTest owns one source module's x86-64 code.
type PreparedNativeX8664ModuleForTest struct {
	Code         []byte
	Functions    []PreparedNativeX8664FunctionForTest
	RootClosures []int32
}

// PreparedNativeX8664ProgramForTest binds generated modules to one exact
// Program without installing executable memory.
type PreparedNativeX8664ProgramForTest struct {
	ABIVersion      uint32
	SemanticVersion uint32
	ProgramHash     [32]byte
	Modules         []PreparedNativeX8664ModuleForTest
}

// EmitPreparedNativeX8664ForTest exposes the private proof artifact without
// widening Ember's production API.
func EmitPreparedNativeX8664ForTest(program *Program) (PreparedNativeX8664ProgramForTest, error) {
	image, err := program.preparedProgramImage()
	if err != nil {
		return PreparedNativeX8664ProgramForTest{}, err
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		return PreparedNativeX8664ProgramForTest{}, err
	}
	artifact, err := emitBackendNativeX8664Program(ir)
	if err != nil {
		return PreparedNativeX8664ProgramForTest{}, err
	}
	result := PreparedNativeX8664ProgramForTest{
		ABIVersion:      artifact.abiVersion,
		SemanticVersion: artifact.semanticVersion,
		ProgramHash:     artifact.programHash,
		Modules:         make([]PreparedNativeX8664ModuleForTest, len(artifact.modules)),
	}
	for moduleIndex, module := range artifact.modules {
		result.Modules[moduleIndex].Code = append([]byte(nil), module.code...)
		result.Modules[moduleIndex].Functions = make([]PreparedNativeX8664FunctionForTest, len(module.functions))
		for pc := range ir.modules[moduleIndex].protos[0].ops {
			operation := &ir.modules[moduleIndex].protos[0].ops[pc]
			if operation.op == opClosure {
				result.Modules[moduleIndex].RootClosures = append(
					result.Modules[moduleIndex].RootClosures,
					operation.targetProto,
				)
			}
		}
		for protoIndex, function := range module.functions {
			result.Modules[moduleIndex].Functions[protoIndex] = PreparedNativeX8664FunctionForTest{
				Offset:         function.offset,
				BodyOffset:     function.bodyOffset,
				ParameterCount: function.parameterCount,
				Emitted:        function.prepared,
				Prepared:       function.entry,
			}
		}
	}
	return result, nil
}
