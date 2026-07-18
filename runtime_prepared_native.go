package ember

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/besmpl/ember/internal/preparednative"
)

// preparedNativeGeneration owns every executable image captured by one
// reload-time PreparedBundle. The bundle never escapes the slot candidate that
// owns this generation, so its generated closures cannot outlive the images.
type preparedNativeGeneration struct {
	bundle      *PreparedBundle
	executables []*preparednative.Executable
}

func prepareNativeGeneration(program *Program) (*preparedNativeGeneration, error) {
	image, err := program.preparedProgramImage()
	if err != nil {
		return nil, fmt.Errorf("prepare native generation: %w", err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		return nil, fmt.Errorf("prepare native generation: %w", err)
	}
	architecture, ok := backendNativeProcessArchitecture()
	if !ok {
		return canonicalPreparedNativeGeneration(ir), nil
	}
	if err := preparednative.Available(); err != nil {
		if errors.Is(err, preparednative.ErrUnavailable) {
			return canonicalPreparedNativeGeneration(ir), nil
		}
		return nil, fmt.Errorf("prepare native generation: %w", err)
	}
	artifact, err := emitBackendNativeProgram(ir, architecture)
	if err != nil {
		return nil, fmt.Errorf("prepare native generation: %w", err)
	}
	generation, err := installBackendNativeProgram(ir, artifact, architecture)
	if errors.Is(err, preparednative.ErrUnavailable) {
		return canonicalPreparedNativeGeneration(ir), nil
	}
	if err != nil {
		return nil, fmt.Errorf("prepare native generation: %w", err)
	}
	return generation, nil
}

func backendNativeProcessArchitecture() (backendNativeArchitecture, bool) {
	switch runtime.GOARCH {
	case "arm64":
		return backendNativeArchitectureARM64, true
	case "amd64":
		return backendNativeArchitectureX8664, true
	default:
		return 0, false
	}
}

func emitBackendNativeProgram(
	ir *backendProgramIR,
	architecture backendNativeArchitecture,
) (backendNativeProgram, error) {
	switch architecture {
	case backendNativeArchitectureARM64:
		return emitBackendNativeARM64Program(ir)
	case backendNativeArchitectureX8664:
		return emitBackendNativeX8664Program(ir)
	default:
		return backendNativeProgram{}, fmt.Errorf("unsupported architecture %d", architecture)
	}
}

func installBackendNativeProgram(
	ir *backendProgramIR,
	artifact backendNativeProgram,
	architecture backendNativeArchitecture,
) (_ *preparedNativeGeneration, resultErr error) {
	if err := validateBackendNativeProgram(ir, artifact, architecture); err != nil {
		return nil, err
	}
	generation := &preparedNativeGeneration{
		executables: make([]*preparednative.Executable, len(artifact.modules)),
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, generation.Close())
		}
	}()

	functions := make([][]PreparedFunction, len(artifact.modules))
	for moduleIndex, module := range artifact.modules {
		functions[moduleIndex] = make([]PreparedFunction, len(module.functions))
		if len(module.code) == 0 {
			continue
		}
		executable, err := preparednative.Compile(module.code)
		if err != nil {
			return nil, fmt.Errorf("install module %d: %w", moduleIndex, err)
		}
		generation.executables[moduleIndex] = executable
		for protoIndex, function := range module.functions {
			if function.entry {
				functions[moduleIndex][protoIndex] = bindPreparedNativeFunction(executable, function)
			}
		}
	}
	generation.bundle = NewPreparedBundle(
		artifact.abiVersion,
		artifact.semanticVersion,
		artifact.programHash,
		functions,
	)
	return generation, nil
}

func validateBackendNativeProgram(
	ir *backendProgramIR,
	artifact backendNativeProgram,
	architecture backendNativeArchitecture,
) error {
	if err := verifyBackendProgramIR(ir); err != nil {
		return fmt.Errorf("validate native Program: %w", err)
	}
	if artifact.architecture != architecture {
		return fmt.Errorf(
			"validate native Program: architecture %d, want %d",
			artifact.architecture,
			architecture,
		)
	}
	if artifact.abiVersion != ir.abiVersion {
		return fmt.Errorf("validate native Program: ABI version %d, want %d", artifact.abiVersion, ir.abiVersion)
	}
	if artifact.semanticVersion != ir.semanticVersion {
		return fmt.Errorf(
			"validate native Program: semantic version %d, want %d",
			artifact.semanticVersion,
			ir.semanticVersion,
		)
	}
	if artifact.programHash != ir.programHash {
		return fmt.Errorf("validate native Program: Program hash mismatch")
	}
	if len(artifact.modules) != len(ir.modules) {
		return fmt.Errorf(
			"validate native Program: module inventory %d, want %d",
			len(artifact.modules),
			len(ir.modules),
		)
	}

	alignment := uint32(1)
	if architecture == backendNativeArchitectureARM64 {
		alignment = 4
	}
	for moduleIndex, module := range artifact.modules {
		wantFunctions := len(ir.modules[moduleIndex].protos)
		if len(module.functions) != wantFunctions {
			return fmt.Errorf(
				"validate native Program: module %d function inventory %d, want %d",
				moduleIndex,
				len(module.functions),
				wantFunctions,
			)
		}
		if len(module.code)%int(alignment) != 0 {
			return fmt.Errorf(
				"validate native Program: module %d code length %d is not %d-byte aligned",
				moduleIndex,
				len(module.code),
				alignment,
			)
		}
		var priorOffset uint32
		var priorBodyOffset uint32
		hasPrepared := false
		for protoIndex, function := range module.functions {
			if !function.prepared {
				if function.offset != 0 || function.bodyOffset != 0 || function.parameterCount != 0 ||
					function.argumentCount != 0 || function.entry {
					return fmt.Errorf(
						"validate native Program: module %d Proto %d has metadata without code",
						moduleIndex,
						protoIndex,
					)
				}
				continue
			}
			if function.parameterCount < 0 ||
				function.parameterCount > backendNativeMaximumParameters ||
				function.parameterCount != ir.modules[moduleIndex].protos[protoIndex].params {
				return fmt.Errorf(
					"validate native Program: module %d Proto %d parameter inventory %d is invalid",
					moduleIndex,
					protoIndex,
					function.parameterCount,
				)
			}
			if function.argumentCount < function.parameterCount ||
				function.argumentCount > backendNativeMaximumParameters ||
				function.entry != (function.argumentCount == function.parameterCount) {
				return fmt.Errorf(
					"validate native Program: module %d Proto %d argument inventory %d/%d is invalid",
					moduleIndex,
					protoIndex,
					function.parameterCount,
					function.argumentCount,
				)
			}
			if len(module.code) == 0 || function.offset%alignment != 0 ||
				function.bodyOffset%alignment != 0 ||
				uint64(function.offset) >= uint64(len(module.code)) ||
				uint64(function.bodyOffset) >= uint64(len(module.code)) ||
				function.bodyOffset >= function.offset {
				return fmt.Errorf(
					"validate native Program: module %d Proto %d body/entry offsets %d/%d are invalid",
					moduleIndex,
					protoIndex,
					function.bodyOffset,
					function.offset,
				)
			}
			if hasPrepared && (function.offset <= priorOffset || function.bodyOffset <= priorBodyOffset) {
				return fmt.Errorf(
					"validate native Program: module %d function offsets are not strictly ordered",
					moduleIndex,
				)
			}
			priorOffset = function.offset
			priorBodyOffset = function.bodyOffset
			hasPrepared = true
		}
		if !hasPrepared && len(module.code) != 0 {
			return fmt.Errorf("validate native Program: module %d has code without functions", moduleIndex)
		}
	}
	return nil
}

func canonicalPreparedNativeGeneration(ir *backendProgramIR) *preparedNativeGeneration {
	functions := make([][]PreparedFunction, len(ir.modules))
	for moduleIndex := range ir.modules {
		functions[moduleIndex] = make([]PreparedFunction, len(ir.modules[moduleIndex].protos))
	}
	return &preparedNativeGeneration{bundle: NewPreparedBundle(
		ir.abiVersion,
		ir.semanticVersion,
		ir.programHash,
		functions,
	)}
}

func bindPreparedNativeFunction(
	executable *preparednative.Executable,
	function backendNativeFunction,
) PreparedFunction {
	return func(context PreparedContext) PreparedExit {
		var arguments [backendNativeMaximumParameters]float64
		for index := 0; index < function.parameterCount; index++ {
			number, ok := context.NumberParameter(index)
			if !ok {
				return PreparedReplayEntry()
			}
			arguments[index] = number
		}
		result, prepared, err := callPreparedNativeFunction(
			executable,
			function.offset,
			arguments,
			function.parameterCount,
		)
		if err != nil {
			return PreparedExit{}
		}
		if !prepared {
			return PreparedReplayEntry()
		}
		return PreparedReturnOneNumber(result)
	}
}

func callPreparedNativeFunction(
	executable *preparednative.Executable,
	offset uint32,
	arguments [backendNativeMaximumParameters]float64,
	count int,
) (float64, bool, error) {
	switch count {
	case 0:
		return executable.CallAt(offset)
	case 1:
		return executable.CallAt(offset, arguments[0])
	case 2:
		return executable.CallAt(offset, arguments[0], arguments[1])
	case 3:
		return executable.CallAt(offset, arguments[0], arguments[1], arguments[2])
	case 4:
		return executable.CallAt(offset, arguments[0], arguments[1], arguments[2], arguments[3])
	case 5:
		return executable.CallAt(offset, arguments[0], arguments[1], arguments[2], arguments[3], arguments[4])
	case 6:
		return executable.CallAt(offset, arguments[0], arguments[1], arguments[2], arguments[3], arguments[4], arguments[5])
	case 7:
		return executable.CallAt(offset, arguments[0], arguments[1], arguments[2], arguments[3], arguments[4], arguments[5], arguments[6])
	case 8:
		return executable.CallAt(offset, arguments[0], arguments[1], arguments[2], arguments[3], arguments[4], arguments[5], arguments[6], arguments[7])
	default:
		return 0, false, fmt.Errorf("call prepared native function: invalid parameter count %d", count)
	}
}

func (generation *preparedNativeGeneration) Close() error {
	if generation == nil {
		return nil
	}
	var closeErrors []error
	for index := len(generation.executables) - 1; index >= 0; index-- {
		if err := generation.executables[index].Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close module %d: %w", index, err))
		} else {
			generation.executables[index] = nil
		}
	}
	if len(closeErrors) == 0 {
		generation.executables = nil
		generation.bundle = nil
	}
	return errors.Join(closeErrors...)
}
