package ember

import (
	"fmt"
	"math"
)

// machinePreparedFunction is the private proof ABI between an owner-bound
// Machine and generated Go. The context deliberately exposes only guarded
// reads; generated code returns one commit record after all hot work is done.
// A replay exit therefore cannot leave partially mutated Machine state.
type machinePreparedFunction func(machinePreparedContext) machinePreparedExit

type machinePreparedProgram struct {
	abiVersion      uint32
	semanticVersion uint32
	programHash     [32]byte
	modules         []machinePreparedModule
}

type machinePreparedModule struct {
	moduleID  programModuleID
	functions []machinePreparedFunction
}

type machinePreparedBinding struct {
	functions [][]machinePreparedFunction
}

type machinePreparedContext struct {
	machine *scalarMachine
	target  *machineProto
}

type machinePreparedExitKind uint8

const (
	machinePreparedExitInvalid machinePreparedExitKind = iota
	machinePreparedExitReplayEntry
	machinePreparedExitReturnOneNumber
)

type machinePreparedExit struct {
	kind       machinePreparedExitKind
	numberBits uint64
}

func bindMachinePreparedProgram(image *programImage, program *machinePreparedProgram) (*machinePreparedBinding, error) {
	if program == nil {
		return nil, nil
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		return nil, fmt.Errorf("bind prepared Program: %w", err)
	}
	if program.abiVersion != ir.abiVersion {
		return nil, fmt.Errorf("bind prepared Program: ABI version %d, want %d", program.abiVersion, ir.abiVersion)
	}
	if program.semanticVersion != ir.semanticVersion {
		return nil, fmt.Errorf("bind prepared Program: semantic version %d, want %d", program.semanticVersion, ir.semanticVersion)
	}
	if program.programHash != ir.programHash {
		return nil, fmt.Errorf("bind prepared Program: Program hash mismatch")
	}
	if len(program.modules) != len(ir.modules) {
		return nil, fmt.Errorf("bind prepared Program: module inventory %d, want %d", len(program.modules), len(ir.modules))
	}
	binding := &machinePreparedBinding{
		functions: make([][]machinePreparedFunction, len(program.modules)),
	}
	for moduleIndex := range program.modules {
		module := &program.modules[moduleIndex]
		if module.moduleID != programModuleID(moduleIndex) {
			return nil, fmt.Errorf("bind prepared Program: module %d has ID %d", moduleIndex, module.moduleID)
		}
		wantFunctions := len(ir.modules[moduleIndex].protos)
		if len(module.functions) != wantFunctions {
			return nil, fmt.Errorf("bind prepared Program: module %d function inventory %d, want %d", moduleIndex, len(module.functions), wantFunctions)
		}
		binding.functions[moduleIndex] = append([]machinePreparedFunction(nil), module.functions...)
	}
	return binding, nil
}

func (binding *machinePreparedBinding) function(moduleID programModuleID, protoID int32) machinePreparedFunction {
	if binding == nil || uint64(moduleID) >= uint64(len(binding.functions)) || protoID < 0 {
		return nil
	}
	functions := binding.functions[moduleID]
	if int(protoID) >= len(functions) {
		return nil
	}
	return functions[protoID]
}

func (context machinePreparedContext) numberParameter(index int) (float64, bool) {
	if context.machine == nil || context.target == nil ||
		index < 0 || index >= context.target.params || index >= len(context.machine.registers) {
		return 0, false
	}
	value := context.machine.registers[index]
	if slotValueKind(value) != NumberKind {
		return 0, false
	}
	number, err := context.machine.number(value)
	return number, err == nil
}

func machinePreparedReplayEntry() machinePreparedExit {
	return machinePreparedExit{kind: machinePreparedExitReplayEntry}
}

func machinePreparedReturnOneNumber(number float64) machinePreparedExit {
	return machinePreparedExit{
		kind:       machinePreparedExitReturnOneNumber,
		numberBits: math.Float64bits(number),
	}
}

func (owner *machineOwner) executePreparedStopped(
	moduleID programModuleID,
	protoID int32,
	target *machineProto,
) (handled bool, errorPC int, err error) {
	if owner == nil || target == nil || owner.prepared == nil {
		return false, 0, nil
	}
	function := owner.prepared.function(moduleID, protoID)
	if function == nil {
		return false, 0, nil
	}
	exit := function(machinePreparedContext{
		machine: &owner.scalarMachine,
		target:  target,
	})
	switch exit.kind {
	case machinePreparedExitReplayEntry:
		// Generated code has no mutation capability before it returns a commit
		// record, so replaying the canonical function entry is exact.
		owner.restartPC = 0
		owner.skipCharge = 0
		return false, 0, nil
	case machinePreparedExitReturnOneNumber:
		machine := &owner.scalarMachine
		machine.results = resizeSlots(machine.results, 1)
		machine.resultCount = 1
		machine.setNumber(len(machine.registers), math.Float64frombits(exit.numberBits))
		return true, 0, nil
	default:
		return true, 0, fmt.Errorf("prepared function returned invalid exit kind %d", exit.kind)
	}
}
