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
	modules   []machinePreparedBoundModule
	maxSpills int
}

type machinePreparedBoundModule struct {
	functions []machinePreparedFunction
	spills    [][][]int32
}

type machinePreparedContext = PreparedContext

type machinePreparedExitKind uint8

const (
	machinePreparedExitInvalid machinePreparedExitKind = iota
	machinePreparedExitReplayEntry
	machinePreparedExitReplayBeforeOperation
	machinePreparedExitReturnOneNumber
)

type machinePreparedExit = PreparedExit

type machinePreparedSpillKind uint8

const (
	machinePreparedSpillInvalid machinePreparedSpillKind = iota
	machinePreparedSpillNil
	machinePreparedSpillBool
	machinePreparedSpillNumber
)

type machinePreparedSpill struct {
	register int32
	kind     machinePreparedSpillKind
	bits     uint64
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
		return nil, preparedBundleErrorf("ABI version %d, want %d", program.abiVersion, ir.abiVersion)
	}
	if program.semanticVersion != ir.semanticVersion {
		return nil, preparedBundleErrorf("semantic version %d, want %d", program.semanticVersion, ir.semanticVersion)
	}
	if program.programHash != ir.programHash {
		return nil, preparedBundleErrorf("Program hash mismatch")
	}
	if len(program.modules) != len(ir.modules) {
		return nil, preparedBundleErrorf("module inventory %d, want %d", len(program.modules), len(ir.modules))
	}
	binding := &machinePreparedBinding{
		modules: make([]machinePreparedBoundModule, len(program.modules)),
	}
	for moduleIndex := range program.modules {
		module := &program.modules[moduleIndex]
		if module.moduleID != programModuleID(moduleIndex) {
			return nil, preparedBundleErrorf("module %d has ID %d", moduleIndex, module.moduleID)
		}
		wantFunctions := len(ir.modules[moduleIndex].protos)
		if len(module.functions) != wantFunctions {
			return nil, preparedBundleErrorf("module %d function inventory %d, want %d", moduleIndex, len(module.functions), wantFunctions)
		}
		bound := &binding.modules[moduleIndex]
		bound.functions = append([]machinePreparedFunction(nil), module.functions...)
		bound.spills = make([][][]int32, len(ir.modules[moduleIndex].protos))
		for protoIndex, proto := range ir.modules[moduleIndex].protos {
			bound.spills[protoIndex] = make([][]int32, len(proto.ops))
			for pc := range proto.ops {
				operation := &proto.ops[pc]
				registers := make([]int32, len(operation.spillValues))
				for spillIndex := range operation.spillValues {
					registers[spillIndex] = operation.spillValues[spillIndex].register
				}
				bound.spills[protoIndex][pc] = registers
				if len(registers) > binding.maxSpills {
					binding.maxSpills = len(registers)
				}
			}
		}
	}
	return binding, nil
}

func (binding *machinePreparedBinding) function(moduleID programModuleID, protoID int32) machinePreparedFunction {
	if binding == nil || uint64(moduleID) >= uint64(len(binding.modules)) || protoID < 0 {
		return nil
	}
	functions := binding.modules[moduleID].functions
	if int(protoID) >= len(functions) {
		return nil
	}
	return functions[protoID]
}

func (binding *machinePreparedBinding) spillRegisters(moduleID programModuleID, protoID, pc int32) []int32 {
	if binding == nil || uint64(moduleID) >= uint64(len(binding.modules)) || protoID < 0 || pc < 0 {
		return nil
	}
	protos := binding.modules[moduleID].spills
	if int(protoID) >= len(protos) || int(pc) >= len(protos[protoID]) {
		return nil
	}
	return protos[protoID][pc]
}

func (context PreparedContext) numberParameter(index int) (float64, bool) {
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

func (context PreparedContext) intrinsicUnchanged(pc int32) bool {
	return context.intrinsicUnchangedTarget(context.target, pc)
}

func (context PreparedContext) intrinsicUnchangedAt(protoID, pc int32) bool {
	if context.machine == nil ||
		context.machine.persistentOwner == nil ||
		context.machine.persistentOwner.image == nil ||
		protoID < 0 {
		return false
	}
	owner := context.machine.persistentOwner
	if uint64(context.machine.activeModule) >= uint64(len(owner.image.modules)) {
		return false
	}
	image := owner.image.modules[context.machine.activeModule].code
	if image == nil || int(protoID) >= len(image.prototypes) {
		return false
	}
	return context.intrinsicUnchangedTarget(&image.prototypes[protoID], pc)
}

func (context PreparedContext) intrinsicUnchangedTarget(target *machineProto, pc int32) bool {
	if context.machine == nil ||
		context.machine.persistentOwner == nil ||
		target == nil ||
		pc < 0 ||
		int(pc) >= len(target.operations) {
		return false
	}
	operation := &target.operations[pc]
	if operation.op != opFastCall ||
		operation.nativeID <= int32(nativeFuncUnknown) {
		return false
	}
	owner := context.machine.persistentOwner
	dense, err := owner.globalIndexStopped(context.machine.activeModule, operation.globalIndex)
	if err != nil {
		return false
	}
	callee, present, err := owner.globals.getAt(dense)
	if err != nil || !present {
		return false
	}
	if operation.guardField != invalidMachineStringID {
		globalTable, err := context.machine.tableID(callee)
		if err != nil {
			return false
		}
		field, err := owner.translateImageStringIDStopped(context.machine.activeModule, operation.guardField)
		if err != nil {
			return false
		}
		callee, err = context.machine.tables.rawGet(globalTable, machineTableStringKey(field))
		if err != nil {
			return false
		}
	}
	actual, err := slotNativeIDValue(callee)
	return err == nil && actual == nativeFuncID(operation.nativeID)
}

func machinePreparedReplayEntry() machinePreparedExit {
	return machinePreparedExit{kind: machinePreparedExitReplayEntry}
}

func (context PreparedContext) replayBeforeOperation(pc int32, spillCount int) machinePreparedExit {
	if context.machine == nil || context.target == nil ||
		pc < 0 || int(pc) >= len(context.target.operations) ||
		spillCount < 0 || spillCount > cap(context.machine.preparedSpills) {
		return machinePreparedExit{}
	}
	context.machine.preparedSpills = context.machine.preparedSpills[:spillCount]
	clear(context.machine.preparedSpills)
	return machinePreparedExit{
		kind:       machinePreparedExitReplayBeforeOperation,
		pc:         pc,
		spillCount: uint32(spillCount),
	}
}

func (context PreparedContext) spillNil(index int, register int32) {
	context.stageSpill(index, machinePreparedSpill{register: register, kind: machinePreparedSpillNil})
}

func (context PreparedContext) spillBool(index int, register int32, value bool) {
	bits := uint64(0)
	if value {
		bits = 1
	}
	context.stageSpill(index, machinePreparedSpill{register: register, kind: machinePreparedSpillBool, bits: bits})
}

func (context PreparedContext) spillNumber(index int, register int32, value float64) {
	context.stageSpill(index, machinePreparedSpill{
		register: register,
		kind:     machinePreparedSpillNumber,
		bits:     math.Float64bits(value),
	})
}

func (context PreparedContext) stageSpill(index int, spill machinePreparedSpill) {
	if context.machine == nil || index < 0 || index >= len(context.machine.preparedSpills) {
		return
	}
	context.machine.preparedSpills[index] = spill
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
	case machinePreparedExitReplayBeforeOperation:
		if err := owner.applyPreparedReplayStopped(moduleID, protoID, exit); err != nil {
			return true, int(exit.pc), err
		}
		return false, int(exit.pc), nil
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

func (owner *machineOwner) applyPreparedReplayStopped(
	moduleID programModuleID,
	protoID int32,
	exit machinePreparedExit,
) error {
	if owner == nil || owner.prepared == nil || exit.pc < 0 {
		return fmt.Errorf("prepared replay has invalid owner or PC")
	}
	if uint64(moduleID) >= uint64(len(owner.image.modules)) ||
		protoID < 0 || int(protoID) >= len(owner.image.modules[moduleID].code.prototypes) {
		return fmt.Errorf("prepared replay has invalid module or Proto")
	}
	target := &owner.image.modules[moduleID].code.prototypes[protoID]
	if int(exit.pc) >= len(target.operations) ||
		backendEffects(target.operations[exit.pc].op) == 0 {
		return fmt.Errorf("prepared replay PC %d is not a pre-operation exit", exit.pc)
	}
	registers := owner.prepared.spillRegisters(moduleID, protoID, exit.pc)
	if int(exit.spillCount) != len(registers) ||
		len(owner.preparedSpills) != len(registers) {
		return fmt.Errorf("prepared replay PC %d spill inventory %d, want %d", exit.pc, exit.spillCount, len(registers))
	}
	for index, register := range registers {
		spill := owner.preparedSpills[index]
		if spill.register != register || register < 0 || int(register) >= len(owner.registers) {
			return fmt.Errorf("prepared replay PC %d spill %d has invalid register %d", exit.pc, index, spill.register)
		}
		switch spill.kind {
		case machinePreparedSpillNil:
			if spill.bits != 0 {
				return fmt.Errorf("prepared replay PC %d spill %d has invalid nil payload", exit.pc, index)
			}
		case machinePreparedSpillBool:
			if spill.bits > 1 {
				return fmt.Errorf("prepared replay PC %d spill %d has invalid bool payload", exit.pc, index)
			}
		case machinePreparedSpillNumber:
		default:
			return fmt.Errorf("prepared replay PC %d spill %d has invalid kind %d", exit.pc, index, spill.kind)
		}
	}
	for _, spill := range owner.preparedSpills {
		register := int(spill.register)
		switch spill.kind {
		case machinePreparedSpillNil:
			owner.setCell(register, slotNil)
		case machinePreparedSpillBool:
			owner.setCell(register, slotBool(spill.bits != 0))
		case machinePreparedSpillNumber:
			owner.setNumber(register, math.Float64frombits(spill.bits))
		}
	}
	owner.restartPC = int(exit.pc)
	owner.skipCharge = 0
	return nil
}
