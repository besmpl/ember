package ember

// PreparedNativeARM64FunctionForTest describes one function in an immutable
// module image. It is exported only to the external proof tests while the
// native backend is behind its adoption gate.
type PreparedNativeARM64FunctionForTest struct {
	Offset         uint32
	BodyOffset     uint32
	ParameterCount int
	Emitted        bool
	Prepared       bool
}

// PreparedNativeARM64ModuleForTest owns one source module's machine code.
type PreparedNativeARM64ModuleForTest struct {
	Code         []byte
	Functions    []PreparedNativeARM64FunctionForTest
	RootClosures []int32
}

// PreparedNativeARM64ProgramForTest binds generated modules to one exact
// Program identity without installing executable memory.
type PreparedNativeARM64ProgramForTest struct {
	ABIVersion      uint32
	SemanticVersion uint32
	ProgramHash     [32]byte
	Modules         []PreparedNativeARM64ModuleForTest
}

// EmitPreparedNativeARM64ForTest exposes the private proof artifact without
// widening Ember's production API.
func EmitPreparedNativeARM64ForTest(program *Program) (PreparedNativeARM64ProgramForTest, error) {
	image, err := program.preparedProgramImage()
	if err != nil {
		return PreparedNativeARM64ProgramForTest{}, err
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		return PreparedNativeARM64ProgramForTest{}, err
	}
	artifact, err := emitBackendNativeARM64Program(ir)
	if err != nil {
		return PreparedNativeARM64ProgramForTest{}, err
	}
	result := PreparedNativeARM64ProgramForTest{
		ABIVersion:      artifact.abiVersion,
		SemanticVersion: artifact.semanticVersion,
		ProgramHash:     artifact.programHash,
		Modules:         make([]PreparedNativeARM64ModuleForTest, len(artifact.modules)),
	}
	for moduleIndex, module := range artifact.modules {
		result.Modules[moduleIndex].Code = append([]byte(nil), module.code...)
		result.Modules[moduleIndex].Functions = make([]PreparedNativeARM64FunctionForTest, len(module.functions))
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
			result.Modules[moduleIndex].Functions[protoIndex] = PreparedNativeARM64FunctionForTest{
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
