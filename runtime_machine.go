package ember

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

var errCompactMachineFellOffEnd = errors.New("run: compact Machine reached the end without return")

type scalarMachine struct {
	image             *codeImage
	persistentOwner   *machineOwner
	effects           machineRunEffects
	activeModule      programModuleID
	registers         []slot
	results           []slot
	continuations     []machineContinuation
	transfer          []machineTransferValue
	captureScratch    []machineCaptureDescriptor
	stringScratch     []byte
	operandScratch    []machineScalarOperand
	callScratch       []slot
	generatedStrings  []machineStringID
	scratch           slot
	numberBits        []uint64
	tableNumbers      []uint64
	resultCount       int
	activeProto       int32
	activeBase        int
	strings           machineStringArena
	tables            machineTableArena
	closures          machineClosureArena
	activeClosure     machineClosureHandle
	activeVarargStart int
	activeVarargCount int
	activeOpenStart   int
	activeOpenCount   int
	activeCellStart   int
	window            executionWindow
	bound             bool
	skipCharge        uint8
	resume            machineSemanticResume
	restartPC         int
	activeCoroutine   machineCoroutineHandle
	coroutineTransfer []machineTransferValue
	indexName         machineStringID
	newIndexName      machineStringID
	iterName          machineStringID
	callName          machineStringID
	metatableName     machineStringID
}

type machineSemanticResumeKind uint8

const (
	machineResumeNone machineSemanticResumeKind = iota
	machineResumeMethodLookup
	machineResumeGetStringFieldIndex
	machineResumeSetStringFieldIndex
)

type machineSemanticResume struct {
	fieldIndex   machineStringFieldIndexContinuation
	method       machineMethodOneAction
	temporaryReg int32
	stackLength  int32
	kind         machineSemanticResumeKind
	_            [3]byte
}

// machineContinuation is the pointer-free caller state needed to resume one
// fixed-result script call. Proto IDs refer to immutable image entries; bases
// and PCs address the Machine-owned scalar register stack.
type machineContinuation struct {
	moduleID    programModuleID
	protoID     int32
	base        int32
	returnPC    int32
	returnBase  int32
	returnReg   int32
	returnCount int32
	stackLength int32
	closure     machineClosureHandle
	varargStart int32
	varargCount int32
	cellStart   int32
	resume      machineSemanticResume
}

// machineTransferValue carries one call result across stack truncation without
// retaining a register-relative boxed-number handle.
type machineTransferValue struct {
	value      slot
	numberBits uint64
	isNumber   uint8
}

var scalarMachinePool = sync.Pool{
	New: func() any { return new(scalarMachine) },
}

func executeCodeImage(image *codeImage, controller *executionController) ([]Value, error) {
	if image == nil || !image.eligible {
		return nil, fmt.Errorf("run: compact Machine received an ineligible image")
	}
	machine, err := bindScalarMachine(image, controller)
	if err != nil {
		return nil, err
	}
	defer releaseScalarMachine(machine)
	defer machine.window.commit()

	if controller != nil {
		if err := controller.enterCall(); err != nil {
			return nil, machine.wrapError(0, err)
		}
		defer controller.leaveCall()
	}
	errorPC, err := runGeneratedScalarMachineLoop(machine)
	if err != nil {
		return nil, machine.wrapError(errorPC, err)
	}
	return machine.exportResults()
}

func bindScalarMachine(image *codeImage, controller *executionController) (*scalarMachine, error) {
	if image == nil || !image.eligible {
		return nil, fmt.Errorf("bind compact Machine: ineligible image")
	}
	numberCells := image.registers + image.maxResults + 1
	if numberCells < 0 || uint64(numberCells+1) > slotIndexMask {
		return nil, fmt.Errorf("bind compact Machine: %d scalar cells exceed slot capacity", numberCells)
	}
	machine := scalarMachinePool.Get().(*scalarMachine)
	machine.image = image
	machine.persistentOwner = nil
	machine.activeModule = 0
	machine.registers = resizeSlots(machine.registers, image.registers)
	machine.results = resizeSlots(machine.results, image.maxResults)
	machine.continuations = machine.continuations[:0]
	machine.transfer = machine.transfer[:0]
	machine.captureScratch = machine.captureScratch[:0]
	machine.stringScratch = machine.stringScratch[:0]
	machine.operandScratch = machine.operandScratch[:0]
	machine.callScratch = machine.callScratch[:0]
	machine.generatedStrings = machine.generatedStrings[:0]
	machine.numberBits = resizeUint64s(machine.numberBits, numberCells)
	machine.tableNumbers = machine.tableNumbers[:0]
	machine.strings.reset()
	machine.tables.reset()
	if len(image.prototypes) > math.MaxUint32 {
		releaseScalarMachine(machine)
		return nil, fmt.Errorf("bind compact Machine: prototype inventory exceeds uint32")
	}
	if err := machine.closures.bindSingleModuleWithOwnerStopped(nextMachineClosureOwner(), uint32(len(image.prototypes))); err != nil {
		releaseScalarMachine(machine)
		return nil, fmt.Errorf("bind compact Machine closures: %w", err)
	}
	machine.bound = true
	machine.skipCharge = 0
	machine.resume = machineSemanticResume{}
	machine.restartPC = 0
	machine.activeCoroutine = machineCoroutineHandle{}
	machine.coroutineTransfer = machine.coroutineTransfer[:0]
	if err := machine.bindImageStrings(image); err != nil {
		releaseScalarMachine(machine)
		return nil, err
	}
	if machineNeedsSemanticStrings(image) {
		if err := machine.bindSemanticStringsStopped(); err != nil {
			releaseScalarMachine(machine)
			return nil, err
		}
	}
	machine.resultCount = 0
	machine.scratch = 0
	machine.activeProto = 0
	machine.activeBase = 0
	machine.activeClosure = machineClosureHandle{}
	machine.activeVarargStart = 0
	machine.activeVarargCount = 0
	machine.activeOpenStart = 0
	machine.activeOpenCount = 0
	machine.activeCellStart = 0
	machine.window = newExecutionWindow(controller)
	return machine, nil
}

func releaseScalarMachine(machine *scalarMachine) {
	if machine == nil || !machine.bound {
		return
	}
	machine.image = nil
	machine.persistentOwner = nil
	machine.activeModule = 0
	machine.registers = machine.registers[:0]
	machine.results = machine.results[:0]
	machine.continuations = machine.continuations[:0]
	machine.transfer = machine.transfer[:0]
	machine.captureScratch = machine.captureScratch[:0]
	machine.stringScratch = machine.stringScratch[:0]
	machine.operandScratch = machine.operandScratch[:0]
	machine.callScratch = machine.callScratch[:0]
	machine.generatedStrings = machine.generatedStrings[:0]
	machine.numberBits = machine.numberBits[:0]
	machine.tableNumbers = machine.tableNumbers[:0]
	machine.strings.reset()
	machine.tables.reset()
	machine.closures.reset()
	machine.resultCount = 0
	machine.scratch = 0
	machine.activeProto = 0
	machine.activeBase = 0
	machine.activeClosure = machineClosureHandle{}
	machine.activeVarargStart = 0
	machine.activeVarargCount = 0
	machine.activeOpenStart = 0
	machine.activeOpenCount = 0
	machine.activeCellStart = 0
	machine.window = executionWindow{}
	machine.bound = false
	machine.skipCharge = 0
	machine.resume = machineSemanticResume{}
	machine.restartPC = 0
	machine.activeCoroutine = machineCoroutineHandle{}
	clear(machine.coroutineTransfer)
	machine.coroutineTransfer = machine.coroutineTransfer[:0]
	machine.indexName = invalidMachineStringID
	machine.newIndexName = invalidMachineStringID
	machine.iterName = invalidMachineStringID
	machine.callName = invalidMachineStringID
	machine.metatableName = invalidMachineStringID
	scalarMachinePool.Put(machine)
}

func (machine *scalarMachine) bindImageStrings(image *codeImage) error {
	if image == nil || len(image.stringRecords) == 0 {
		return nil
	}
	if err := machine.strings.reserveStopped(len(image.stringData), len(image.stringRecords)); err != nil {
		return fmt.Errorf("bind compact Machine strings: %w", err)
	}
	for index, record := range image.stringRecords {
		start := uint64(record.offset)
		end := start + uint64(record.length)
		if end > uint64(len(image.stringData)) {
			return fmt.Errorf("bind compact Machine string %d has an invalid byte span", index+1)
		}
		id, err := machine.strings.internBytesStopped(image.stringData[int(start):int(end)])
		if err != nil {
			return fmt.Errorf("bind compact Machine string %d: %w", index+1, err)
		}
		if id != machineStringID(index+1) {
			return fmt.Errorf("bind compact Machine string %d received owner ID %d", index+1, id)
		}
	}
	return nil
}

func (machine *scalarMachine) bindSemanticStringsStopped() error {
	if machine == nil {
		return fmt.Errorf("bind compact Machine semantic strings: nil machine")
	}
	var err error
	if machine.indexName, err = machine.strings.internStringStopped("__index"); err != nil {
		return fmt.Errorf("bind compact Machine __index: %w", err)
	}
	if machine.newIndexName, err = machine.strings.internStringStopped("__newindex"); err != nil {
		return fmt.Errorf("bind compact Machine __newindex: %w", err)
	}
	if machine.iterName, err = machine.strings.internStringStopped("__iter"); err != nil {
		return fmt.Errorf("bind compact Machine __iter: %w", err)
	}
	if machine.callName, err = machine.strings.internStringStopped("__call"); err != nil {
		return fmt.Errorf("bind compact Machine __call: %w", err)
	}
	if machine.metatableName, err = machine.strings.internStringStopped("__metatable"); err != nil {
		return fmt.Errorf("bind compact Machine __metatable: %w", err)
	}
	return nil
}

func machineNeedsSemanticStrings(image *codeImage) bool {
	if image == nil {
		return false
	}
	for _, proto := range image.prototypes {
		for _, operation := range proto.operations {
			switch operation.op {
			case opPrepareIter, opSetField, opSetStringField, opSetStringFieldIndex,
				opGetStringField, opGetStringFieldIndex, opSetIndex, opGetIndex,
				opCall, opCallOne, opCallLocalOne, opCallUpvalueOne, opCallMethodOne:
				return true
			}
		}
	}
	return false
}

func resizeSlots(values []slot, length int) []slot {
	if cap(values) < length {
		values = make([]slot, length)
	} else {
		values = values[:length]
	}
	for index := range values {
		values[index] = slotNil
	}
	return values
}

func resizeUint64s(values []uint64, length int) []uint64 {
	if cap(values) < length {
		return make([]uint64, length)
	}
	values = values[:length]
	clear(values)
	return values
}

func (machine *scalarMachine) ensureStack(length int) error {
	if length < 0 || uint64(length+1) > slotIndexMask {
		return fmt.Errorf("compact Machine register stack exceeds slot capacity")
	}
	oldLength := len(machine.registers)
	if cap(machine.registers) < length {
		machine.registers = append(machine.registers, make([]slot, length-len(machine.registers))...)
	} else {
		machine.registers = machine.registers[:length]
	}
	for index := oldLength; index < length; index++ {
		machine.registers[index] = slotNil
	}
	if cap(machine.numberBits) < length+len(machine.results)+1 {
		machine.numberBits = append(machine.numberBits, make([]uint64, length+len(machine.results)+1-len(machine.numberBits))...)
	} else {
		machine.numberBits = machine.numberBits[:length+len(machine.results)+1]
	}
	return nil
}

func (machine *scalarMachine) currentProto() *machineProto {
	if machine == nil || machine.image == nil || machine.activeProto < 0 || int(machine.activeProto) >= len(machine.image.prototypes) {
		return nil
	}
	return &machine.image.prototypes[machine.activeProto]
}

func (machine *scalarMachine) charge(operation machineOperation) error {
	for count := uint8(0); count < operation.guestCharge+operation.tailCharge; count++ {
		if err := machine.window.stepInstruction(); err != nil {
			return err
		}
	}
	return nil
}

func (machine *scalarMachine) refundTailCharge(operation machineOperation) {
	if machine == nil || machine.window.controller == nil || machine.window.remaining < 0 || operation.tailCharge == 0 {
		return
	}
	machine.window.remaining += int64(operation.tailCharge)
	machine.window.pollLeft += uint32(operation.tailCharge)
}

func (machine *scalarMachine) wrapError(pc int, err error) error {
	if err == nil || runtimeErrorAlreadyWrapped(err) {
		return err
	}
	frame := ScriptFrame{}
	if machine != nil {
		if proto := machine.currentProto(); proto != nil && pc >= 0 && pc < len(proto.operations) {
			frame = ScriptFrame{Source: proto.sourceName, Function: proto.functionName, Line: int(proto.operations[pc].line)}
		}
	}
	return newRuntimeErrorWithController(err, []ScriptFrame{frame}, machine.window.controller)
}

func (machine *scalarMachine) loadConstant(destination, constant int) error {
	proto := machine.currentProto()
	if proto == nil || destination < 0 || destination >= len(machine.registers) || constant < 0 || constant >= len(proto.constants) {
		return fmt.Errorf("compact Machine LOAD_CONST operands are out of range")
	}
	descriptor := proto.constants[constant]
	switch descriptor.kind {
	case NilKind:
		machine.registers[destination] = slotNil
	case BoolKind:
		machine.registers[destination] = slotBool(descriptor.bits != 0)
	case NumberKind:
		machine.setNumber(destination, math.Float64frombits(descriptor.bits))
	case StringKind:
		value, err := machine.stringSlot(descriptor.bits)
		if err != nil {
			return err
		}
		machine.registers[destination] = value
	default:
		return fmt.Errorf("compact Machine LOAD_CONST kind %s is unsupported", descriptor.kind)
	}
	return nil
}

func (machine *scalarMachine) loadGlobal(destination int, imageIndex int32) error {
	if machine == nil || machine.persistentOwner == nil || destination < 0 || destination >= len(machine.registers) {
		return fmt.Errorf("compact Machine LOAD_GLOBAL requires an owner-bound destination")
	}
	dense, err := machine.persistentOwner.globalIndexStopped(machine.activeModule, imageIndex)
	if err != nil {
		return err
	}
	value, present, err := machine.persistentOwner.globals.getAt(dense)
	if err != nil {
		return err
	}
	if !present {
		name := "?"
		if dense >= 0 && dense < len(machine.persistentOwner.globals.names) {
			if bytes, ok := machine.strings.bytesFor(machine.persistentOwner.globals.names[dense]); ok {
				name = string(bytes)
			}
		}
		return fmt.Errorf("run: undefined global %q", name)
	}
	return machine.copySlot(destination, value)
}

func (machine *scalarMachine) storeGlobal(imageIndex int32, source int) error {
	if machine == nil || machine.persistentOwner == nil || source < 0 || source >= len(machine.registers) {
		return fmt.Errorf("compact Machine SET_GLOBAL requires an owner-bound source")
	}
	dense, err := machine.persistentOwner.globalIndexStopped(machine.activeModule, imageIndex)
	if err != nil {
		return err
	}
	value, err := machine.stableMachineValueStopped(machine.registers[source])
	if err != nil {
		return err
	}
	return machine.persistentOwner.globals.setAt(dense, value)
}

func (machine *scalarMachine) move(destination, source int) error {
	if destination < 0 || destination >= len(machine.registers) || source < 0 || source >= len(machine.registers) {
		return fmt.Errorf("compact Machine MOVE operands are out of range")
	}
	return machine.copySlot(destination, machine.registers[source])
}

func (machine *scalarMachine) newTableStopped(destination, arrayCapacity, fieldCapacity int) error {
	if machine == nil || destination < 0 || destination >= len(machine.registers) || arrayCapacity < 0 || fieldCapacity < 0 ||
		uint64(arrayCapacity) > math.MaxUint32 || uint64(fieldCapacity) > math.MaxUint32 {
		return fmt.Errorf("compact Machine NEW_TABLE operands are out of range")
	}
	if err := machine.window.controller.chargeRuntimeObject(); err != nil {
		return err
	}
	id, err := machine.tables.newTableStopped(uint32(arrayCapacity), uint32(fieldCapacity))
	if err != nil {
		return fmt.Errorf("compact Machine NEW_TABLE failed: %w", err)
	}
	if uint64(id) > slotIndexMask {
		return fmt.Errorf("compact Machine table ID %d exceeds slot capacity", id)
	}
	value, err := slotPackHandle(slotTagTable, uint32(id), 1)
	if err != nil {
		return err
	}
	machine.setCell(destination, value)
	return nil
}

func (machine *scalarMachine) setConstantFieldStopped(tableRegister, constant, valueRegister, nextPC int) (int32, int, error) {
	if machine == nil || tableRegister < 0 || tableRegister >= len(machine.registers) ||
		valueRegister < 0 || valueRegister >= len(machine.registers) {
		return 0, 0, fmt.Errorf("compact Machine SET_FIELD operands are out of range")
	}
	key, err := machine.constantSlot(constant, tableRegister)
	if err != nil {
		return 0, 0, err
	}
	return machine.setTableIndexSemanticStopped(machine.registers[tableRegister], key, machine.registers[valueRegister], tableRegister, nextPC)
}

func (machine *scalarMachine) setStringFieldStopped(tableRegister, constant, valueRegister, nextPC int) (int32, int, error) {
	if machine == nil || tableRegister < 0 || tableRegister >= len(machine.registers) ||
		valueRegister < 0 || valueRegister >= len(machine.registers) {
		return 0, 0, fmt.Errorf("compact Machine SET_STRING_FIELD operands are out of range")
	}
	if _, err := machine.tableID(machine.registers[tableRegister]); err != nil {
		return 0, 0, fmt.Errorf("run: set field target is %s, want table", slotValueKind(machine.registers[tableRegister]))
	}
	return machine.setConstantFieldStopped(tableRegister, constant, valueRegister, nextPC)
}

func (machine *scalarMachine) setIndexStopped(tableRegister, keyRegister, valueRegister, nextPC int) (int32, int, error) {
	if machine == nil || tableRegister < 0 || tableRegister >= len(machine.registers) ||
		keyRegister < 0 || keyRegister >= len(machine.registers) ||
		valueRegister < 0 || valueRegister >= len(machine.registers) {
		return 0, 0, fmt.Errorf("compact Machine SET_INDEX operands are out of range")
	}
	return machine.setTableIndexSemanticStopped(
		machine.registers[tableRegister],
		machine.registers[keyRegister],
		machine.registers[valueRegister],
		tableRegister,
		nextPC,
	)
}

func (machine *scalarMachine) getIndex(destination, tableRegister, keyRegister, nextPC int) (int32, int, error) {
	if machine == nil || destination < 0 || destination >= len(machine.registers) ||
		tableRegister < 0 || tableRegister >= len(machine.registers) ||
		keyRegister < 0 || keyRegister >= len(machine.registers) {
		return 0, 0, fmt.Errorf("compact Machine GET_INDEX operands are out of range")
	}
	return machine.getTableIndexSemantic(destination, machine.registers[tableRegister], machine.registers[keyRegister], nextPC)
}

func (machine *scalarMachine) getStringField(destination, tableRegister, constant, nextPC int) (int32, int, error) {
	if machine == nil || destination < 0 || destination >= len(machine.registers) ||
		tableRegister < 0 || tableRegister >= len(machine.registers) {
		return 0, 0, fmt.Errorf("compact Machine GET_STRING_FIELD operands are out of range")
	}
	if _, err := machine.tableID(machine.registers[tableRegister]); err != nil {
		return 0, 0, fmt.Errorf("run: get field target is %s, want table", slotValueKind(machine.registers[tableRegister]))
	}
	key, err := machine.constantSlot(constant, destination)
	if err != nil {
		return 0, 0, err
	}
	return machine.getTableIndexSemantic(destination, machine.registers[tableRegister], key, nextPC)
}

func (machine *scalarMachine) prepareIteratorRegisters(generator, state, control, nextPC int) (int32, int, error) {
	if machine == nil || generator < 0 || state < 0 || control < 0 ||
		generator >= len(machine.registers) || state >= len(machine.registers) || control >= len(machine.registers) {
		return 0, 0, fmt.Errorf("compact Machine PREPARE_ITER operands are out of range")
	}
	plan, err := machine.tables.prepareIterator(machine.registers[generator], machine.iterName)
	if err != nil {
		return 0, 0, fmt.Errorf("run: prepare iterator failed: %w", err)
	}
	if plan.action.kind == machineTableActionCall {
		arguments := [...]slot{plan.action.value}
		target, base, err := machine.enterExplicitCall(machineCallRequest{
			callable: plan.action.callable, arguments: arguments[:], destination: generator,
			resultCount: 3, returnPC: nextPC,
		})
		if err != nil {
			return 0, 0, fmt.Errorf("run: prepare iterator failed: %w", err)
		}
		return target, base, nil
	}
	if plan.ready == 0 {
		return -1, machine.activeBase, nil
	}
	machine.registers[generator] = plan.generator
	machine.registers[state] = plan.state
	machine.registers[control] = plan.control
	return -1, machine.activeBase, nil
}

func (machine *scalarMachine) iteratorStepRegisters(destination, generator, state, count int) (machineIteratorStep, error) {
	step, _, err := machine.iteratorStep(destination, generator, state, count, false)
	return step, err
}

func (machine *scalarMachine) arrayNextJump2Registers(destination, generator, state int) (machineIteratorStep, bool, error) {
	return machine.iteratorStep(destination, generator, state, 2, true)
}

func (machine *scalarMachine) iteratorStep(destination, generator, state, count int, fusedJump bool) (machineIteratorStep, bool, error) {
	if destination < 0 || generator < 0 || state < 0 || count <= 0 ||
		destination+count > len(machine.registers) || generator >= len(machine.registers) || state >= len(machine.registers) {
		return machineIteratorStep{}, false, fmt.Errorf("compact Machine iterator operands are out of range")
	}
	nativeID, err := slotNativeIDValue(machine.registers[generator])
	if err != nil {
		return machineIteratorStep{}, false, fmt.Errorf("run: call failed: compact Machine iterator callable is unsupported")
	}
	mode := machineIteratorInvalid
	switch nativeID {
	case nativeFuncArrayNext:
		mode = machineIteratorArray
	case nativeFuncTableNext, nativeFuncNext:
		mode = machineIteratorGeneric
	default:
		return machineIteratorStep{}, false, fmt.Errorf("run: call failed: compact Machine iterator native ID %d is unsupported", nativeID)
	}
	table, err := machine.tableID(machine.registers[state])
	if err != nil {
		name := "table iterator"
		if mode == machineIteratorArray {
			name = "array iterator"
		}
		return machineIteratorStep{}, false, fmt.Errorf("run: call failed: host function failed: %s: argument #1 is %s, want table", name, slotValueKind(machine.registers[state]))
	}
	cursor, err := machine.iteratorCursor(mode, machine.registers[destination])
	if err != nil {
		return machineIteratorStep{}, false, fmt.Errorf("run: call failed: host function failed: %w", err)
	}
	var step machineIteratorStep
	jump := false
	if fusedJump {
		step, jump, err = machine.tables.arrayNextJump2(mode, table, cursor)
	} else {
		step, err = machine.tables.iteratorNext(mode, table, cursor)
	}
	if err != nil {
		return machineIteratorStep{}, false, fmt.Errorf("run: call failed: host function failed: %w", err)
	}
	for index := 0; index < count; index++ {
		value := slotNil
		if index == 0 && step.count > 0 {
			value = step.key
		} else if index == 1 && step.count > 1 {
			value = step.value
		}
		if err := machine.copySlot(destination+index, value); err != nil {
			return machineIteratorStep{}, false, err
		}
	}
	return step, jump, nil
}

func (machine *scalarMachine) iteratorCursor(mode machineIteratorMode, control slot) (machineTableCursor, error) {
	if control == slotNil {
		return machineTableCursor{}, nil
	}
	key, err := machine.tableKey(control)
	if err != nil {
		return machineTableCursor{}, err
	}
	if mode == machineIteratorArray && key.kind != machineTableKeyArray {
		return machineTableCursor{}, fmt.Errorf("array iterator: index is %s, want number or nil", slotValueKind(control))
	}
	return machineTableCursor{key: key, set: 1}, nil
}

func (machine *scalarMachine) getStringFieldIndex(destination, base, constant, keyRegister, pc, nextPC int) (int32, int, error) {
	if destination < 0 || base < 0 || keyRegister < 0 || destination >= len(machine.registers) || base >= len(machine.registers) || keyRegister >= len(machine.registers) {
		return 0, 0, fmt.Errorf("compact Machine GET_STRING_FIELD_INDEX operands are out of range")
	}
	var action machineTableAction
	if machine.resume.kind == machineResumeGetStringFieldIndex {
		resume := machine.resume
		machine.resume = machineSemanticResume{}
		var err error
		action, err = machine.tables.resumeStringFieldIndex(resume.fieldIndex, machine.registers[destination])
		if err != nil {
			return 0, 0, fmt.Errorf("run: get index failed: %w", err)
		}
	} else {
		baseID, err := machine.tableID(machine.registers[base])
		if err != nil {
			return 0, 0, fmt.Errorf("run: get field target is %s, want table", slotValueKind(machine.registers[base]))
		}
		fieldValue, err := machine.constantSlot(constant, destination)
		if err != nil {
			return 0, 0, err
		}
		field, err := machine.stringID(fieldValue)
		if err != nil {
			return 0, 0, err
		}
		key, err := machine.tableKey(machine.registers[keyRegister])
		if err != nil {
			return 0, 0, fmt.Errorf("run: get index failed: %w", err)
		}
		continuation := machineStringFieldIndexContinuation{}
		action, continuation, err = machine.tables.decideGetStringFieldIndex(baseID, field, key, machine.indexName)
		if err != nil {
			return 0, 0, fmt.Errorf("run: get index failed: %w", err)
		}
		if continuation.active != 0 {
			resume := machineSemanticResume{kind: machineResumeGetStringFieldIndex, fieldIndex: continuation}
			targetID, targetBase, err := machine.enterTableAction(action, destination, 1, pc, resume)
			if err != nil || targetID >= 0 {
				return targetID, targetBase, err
			}
			action, err = machine.tables.resumeStringFieldIndex(continuation, machine.registers[destination])
			if err != nil {
				return 0, 0, fmt.Errorf("run: get index failed: %w", err)
			}
		}
	}
	switch action.kind {
	case machineTableActionReturn:
		return -1, machine.activeBase, machine.copySlot(destination, action.value)
	case machineTableActionCall:
		return machine.enterTableAction(action, destination, 1, nextPC, machineSemanticResume{})
	default:
		return 0, 0, fmt.Errorf("run: get index failed: invalid compact Machine table action")
	}
}

func (machine *scalarMachine) setStringFieldIndex(base, constant, keyRegister, valueRegister, pc, nextPC int) (int32, int, error) {
	if base < 0 || keyRegister < 0 || valueRegister < 0 || base >= len(machine.registers) || keyRegister >= len(machine.registers) || valueRegister >= len(machine.registers) {
		return 0, 0, fmt.Errorf("compact Machine SET_STRING_FIELD_INDEX operands are out of range")
	}
	var action machineTableAction
	if machine.resume.kind == machineResumeSetStringFieldIndex {
		resume := machine.resume
		machine.resume = machineSemanticResume{}
		if resume.temporaryReg < 0 || int(resume.temporaryReg) >= len(machine.registers) || resume.stackLength < 0 || int(resume.stackLength) > len(machine.registers) {
			return 0, 0, fmt.Errorf("compact Machine SET_STRING_FIELD_INDEX resume is invalid")
		}
		first := machine.registers[resume.temporaryReg]
		machine.registers = machine.registers[:resume.stackLength]
		var err error
		action, err = machine.tables.resumeStringFieldIndex(resume.fieldIndex, first)
		if err != nil {
			return 0, 0, fmt.Errorf("run: set index failed: %w", err)
		}
	} else {
		baseID, err := machine.tableID(machine.registers[base])
		if err != nil {
			return 0, 0, fmt.Errorf("run: set field target is %s, want table", slotValueKind(machine.registers[base]))
		}
		fieldValue, err := machine.constantSlot(constant, base)
		if err != nil {
			return 0, 0, err
		}
		field, err := machine.stringID(fieldValue)
		if err != nil {
			return 0, 0, err
		}
		key, err := machine.tableKey(machine.registers[keyRegister])
		if err != nil {
			return 0, 0, fmt.Errorf("run: set index failed: %w", err)
		}
		stable, err := machine.stableTableValueStopped(machine.registers[valueRegister])
		if err != nil {
			return 0, 0, err
		}
		continuation := machineStringFieldIndexContinuation{}
		action, continuation, err = machine.tables.decideSetStringFieldIndex(baseID, field, key, stable, machine.indexName, machine.newIndexName)
		if err != nil {
			return 0, 0, fmt.Errorf("run: set index failed: %w", err)
		}
		if continuation.active != 0 {
			stackLength := len(machine.registers)
			if err := machine.ensureStack(stackLength + 1); err != nil {
				return 0, 0, err
			}
			resume := machineSemanticResume{
				kind: machineResumeSetStringFieldIndex, fieldIndex: continuation,
				temporaryReg: int32(stackLength), stackLength: int32(stackLength),
			}
			targetID, targetBase, err := machine.enterTableAction(action, stackLength, 1, pc, resume)
			if err != nil || targetID >= 0 {
				return targetID, targetBase, err
			}
			first := machine.registers[stackLength]
			machine.registers = machine.registers[:stackLength]
			action, err = machine.tables.resumeStringFieldIndex(continuation, first)
			if err != nil {
				return 0, 0, fmt.Errorf("run: set index failed: %w", err)
			}
		}
	}
	switch action.kind {
	case machineTableActionStore:
		return -1, machine.activeBase, machine.applyTableStoreStopped(action)
	case machineTableActionCall:
		return machine.enterTableAction(action, base, 0, nextPC, machineSemanticResume{})
	default:
		return 0, 0, fmt.Errorf("run: set index failed: invalid compact Machine table action")
	}
}

func (machine *scalarMachine) enterTableAction(action machineTableAction, destination, resultCount, returnPC int, resume machineSemanticResume) (int32, int, error) {
	arguments, err := machineTableActionArguments(action)
	if err != nil {
		return 0, 0, err
	}
	values := [...]slot{arguments.first, arguments.second, arguments.third}
	return machine.enterExplicitCall(machineCallRequest{
		callable: action.callable, arguments: values[:arguments.count], destination: destination,
		resultCount: resultCount, returnPC: returnPC, resume: resume,
	})
}

func (machine *scalarMachine) getTableIndexSemantic(destination int, tableValue, keyValue slot, nextPC int) (int32, int, error) {
	id, err := machine.tableID(tableValue)
	if err != nil {
		return 0, 0, fmt.Errorf("run: get index target is %s, want table", slotValueKind(tableValue))
	}
	key, err := machine.tableKey(keyValue)
	if err != nil {
		return 0, 0, fmt.Errorf("run: get index failed: %w", err)
	}
	action, err := machine.tables.decideIndex(id, key, machine.indexName)
	if err != nil {
		return 0, 0, fmt.Errorf("run: get index failed: %w", err)
	}
	switch action.kind {
	case machineTableActionReturn:
		return -1, machine.activeBase, machine.copySlot(destination, action.value)
	case machineTableActionCall:
		return machine.enterTableAction(action, destination, 1, nextPC, machineSemanticResume{})
	default:
		return 0, 0, fmt.Errorf("run: get index failed: invalid compact Machine table action")
	}
}

func (machine *scalarMachine) setTableIndexSemanticStopped(tableValue, keyValue, value slot, destination, nextPC int) (int32, int, error) {
	id, err := machine.tableID(tableValue)
	if err != nil {
		return 0, 0, fmt.Errorf("run: set index target is %s, want table", slotValueKind(tableValue))
	}
	key, err := machine.tableKey(keyValue)
	if err != nil {
		return 0, 0, fmt.Errorf("run: set index failed: %w", err)
	}
	stable, err := machine.stableTableValueStopped(value)
	if err != nil {
		return 0, 0, fmt.Errorf("run: set index failed: %w", err)
	}
	action, err := machine.tables.decideNewIndex(id, key, stable, machine.newIndexName)
	if err != nil {
		return 0, 0, fmt.Errorf("run: set index failed: %w", err)
	}
	switch action.kind {
	case machineTableActionStore:
		return -1, machine.activeBase, machine.applyTableStoreStopped(action)
	case machineTableActionCall:
		return machine.enterTableAction(action, destination, 0, nextPC, machineSemanticResume{})
	default:
		return 0, 0, fmt.Errorf("run: set index failed: invalid compact Machine table action")
	}
}

func (machine *scalarMachine) applyTableStoreStopped(action machineTableAction) error {
	limit := uint64(0)
	if machine.window.controller != nil {
		limit = machine.window.controller.limits.MaxTableEntriesPerTable
	}
	key := action.key
	if key.kind == machineTableKeyArray {
		recordKey := machineTableArrayRecordKey(key.id)
		if _, exists := machine.tables.getSlot(action.table, recordKey.value); exists {
			action.key = recordKey
		} else {
			record, ok := machine.tables.lookup(action.table)
			if !ok {
				return errMachineTableInvalidID
			}
			if _, exists := machine.tables.getArray(action.table, key.id); !exists && key.id > record.arrayCapacity && key.id > record.arrayLength+1 {
				action.key = recordKey
			}
		}
	}
	return machine.tables.rawSetMetatableAwareStopped(action.table, action.key, action.value, machine.metatableName, limit)
}

// setTableIndexStopped is the raw import/build seam. Runtime opcode writes use
// setTableIndexSemanticStopped so __newindex remains observable.
func (machine *scalarMachine) setTableIndexStopped(tableValue, keyValue, value slot) error {
	id, err := machine.tableID(tableValue)
	if err != nil {
		return fmt.Errorf("run: set index target is %s, want table", slotValueKind(tableValue))
	}
	key, err := machine.tableKey(keyValue)
	if err != nil {
		return fmt.Errorf("run: set index failed: %w", err)
	}
	stable, err := machine.stableTableValueStopped(value)
	if err != nil {
		return fmt.Errorf("run: set index failed: %w", err)
	}
	return machine.applyTableStoreStopped(machineTableAction{kind: machineTableActionStore, table: id, key: key, value: stable})
}

func (machine *scalarMachine) tableValue(id machineTableID, key machineTableKey) (slot, bool) {
	if key.kind == machineTableKeyArray {
		if value, ok := machine.tables.getArray(id, key.id); ok {
			return value, true
		}
		return machine.tables.getSlot(id, machineTableArrayRecordKey(key.id).value)
	}
	switch key.kind {
	case machineTableKeySlot:
		return machine.tables.getSlot(id, key.value)
	case machineTableKeyString:
		return machine.tables.getString(id, machineStringID(key.id))
	default:
		return slotNil, false
	}
}

func (machine *scalarMachine) tableID(value slot) (machineTableID, error) {
	index, generation, err := slotValidateHandle(value, slotTagTable)
	if err != nil || generation != 1 {
		return invalidMachineTableID, fmt.Errorf("compact Machine value is not a valid table handle")
	}
	id := machineTableID(index)
	if _, ok := machine.tables.lookup(id); !ok {
		return invalidMachineTableID, fmt.Errorf("compact Machine table ID %d is invalid", id)
	}
	return id, nil
}

func (machine *scalarMachine) tableKey(value slot) (machineTableKey, error) {
	switch slotValueKind(value) {
	case NilKind:
		return machineTableKey{}, errors.New("table key is nil")
	case BoolKind:
		if _, err := slotBoolValue(value); err != nil {
			return machineTableKey{}, errMachineTableInvalidKey
		}
		return machineTableSlotKey(value), nil
	case NumberKind:
		number, err := machine.number(value)
		if err != nil || math.IsNaN(number) {
			return machineTableKey{}, errors.New("table key is NaN")
		}
		if number >= 1 && number <= math.MaxUint32 && math.Trunc(number) == number {
			return machineTableArrayKey(uint32(number)), nil
		}
		return machineTableSlotKey(machineTableNumberKey(number)), nil
	case StringKind:
		id, err := machine.stringID(value)
		if err != nil {
			return machineTableKey{}, err
		}
		return machineTableStringKey(id), nil
	case TableKind:
		if _, err := machine.tableID(value); err != nil {
			return machineTableKey{}, err
		}
		return machineTableSlotKey(value), nil
	case UserDataKind:
		if machine.persistentOwner == nil || slotTagOf(value) != slotTagCoroutine {
			return machineTableKey{}, fmt.Errorf("table key kind %s is unsupported", slotValueKind(value))
		}
		if _, err := machine.persistentOwner.coroutines.handleFromSlot(value); err != nil {
			return machineTableKey{}, err
		}
		return machineTableSlotKey(value), nil
	default:
		return machineTableKey{}, fmt.Errorf("table key kind %s is unsupported", slotValueKind(value))
	}
}

func (machine *scalarMachine) stableTableValueStopped(value slot) (slot, error) {
	switch slotValueKind(value) {
	case NilKind, BoolKind:
		return value, nil
	case FunctionKind:
		if _, _, _, err := machine.closureTarget(value); err != nil {
			return 0, err
		}
		return value, nil
	case UserDataKind:
		if machine.persistentOwner == nil || slotTagOf(value) != slotTagCoroutine {
			return slotNil, fmt.Errorf("compact Machine value kind %s cannot cross a call boundary", slotValueKind(value))
		}
		if _, err := machine.persistentOwner.coroutines.handleFromSlot(value); err != nil {
			return slotNil, err
		}
		return value, nil
	case NumberKind:
		if !slotIsTagged(value) {
			return value, nil
		}
		_, index, generation, err := slotUnpackHandle(value)
		if err == nil && generation == 2 && index > 0 && int(index) <= len(machine.tableNumbers) {
			return value, nil
		}
		number, err := machine.number(value)
		if err != nil {
			return 0, err
		}
		if len(machine.tableNumbers) == int(slotIndexMask) {
			return 0, errors.New("machine table number arena exceeds slot capacity")
		}
		machine.tableNumbers = append(machine.tableNumbers, math.Float64bits(number))
		return slotPackHandle(slotTagBoxedNumber, uint32(len(machine.tableNumbers)), 2)
	case StringKind:
		if _, err := machine.stringID(value); err != nil {
			return 0, err
		}
		return value, nil
	case TableKind:
		if _, err := machine.tableID(value); err != nil {
			return 0, err
		}
		return value, nil
	case HostFuncKind:
		switch slotTagOf(value) {
		case slotTagNativeID:
			if _, err := slotNativeIDValue(value); err != nil {
				return 0, err
			}
			return value, nil
		case slotTagHostCallable:
			if machine.persistentOwner == nil {
				return 0, errors.New("compact Machine host callable has no persistent owner")
			}
			if _, err := machine.persistentOwner.hosts.lookup(value); err != nil {
				return 0, err
			}
			return value, nil
		default:
			return 0, errors.New("compact Machine host callable is invalid")
		}
	default:
		return 0, fmt.Errorf("table value kind %s is unsupported", slotValueKind(value))
	}
}

func machineTableNumberKey(number float64) slot {
	if number == 0 {
		number = 0
	}
	return slot(math.Float64bits(number))
}

func machineTableArrayRecordKey(index uint32) machineTableKey {
	return machineTableSlotKey(machineTableNumberKey(float64(index)))
}

func (machine *scalarMachine) makeClosure(destination int, targetProto int32) error {
	if destination < 0 || destination >= len(machine.registers) || targetProto <= 0 || machine.image == nil || int(targetProto) >= len(machine.image.prototypes) {
		return fmt.Errorf("compact Machine CLOSURE operands are out of range")
	}
	target := &machine.image.prototypes[targetProto]
	machine.captureScratch = machine.captureScratch[:0]
	for _, descriptor := range target.upvalues {
		if descriptor.local != 0 {
			register := machine.activeBase + int(descriptor.index)
			if register < machine.activeBase || register >= machine.activeBase+machine.currentProto().registers {
				return fmt.Errorf("compact Machine closure local capture is out of range")
			}
			if descriptor.copy != 0 {
				value, err := machine.stableMachineValueStopped(machine.registers[register])
				if err != nil {
					return err
				}
				machine.captureScratch = append(machine.captureScratch, machineCaptureDescriptor{mode: machineCaptureByValue, value: value})
				continue
			}
			cell, err := machine.closures.openCellStopped(register, machine.registers[register])
			if err != nil {
				return err
			}
			machine.captureScratch = append(machine.captureScratch, machineCaptureDescriptor{mode: machineCaptureShared, cell: cell})
			continue
		}
		cell, err := machine.upvalueCell(int(descriptor.index))
		if err != nil {
			return err
		}
		if descriptor.copy == 0 {
			machine.captureScratch = append(machine.captureScratch, machineCaptureDescriptor{mode: machineCaptureShared, cell: cell})
			continue
		}
		value, err := machine.closures.cellGetOpen(cell, machine.registers)
		if err != nil {
			return err
		}
		value, err = machine.stableMachineValueStopped(value)
		if err != nil {
			return err
		}
		machine.captureScratch = append(machine.captureScratch, machineCaptureDescriptor{mode: machineCaptureByValue, value: value})
	}
	handle, err := machine.closures.createClosureStopped(machine.activeModule, machineProtoID(targetProto), machine.captureScratch)
	if err != nil {
		return err
	}
	value, err := slotPackHandle(slotTagClosure, handle.index, handle.generation)
	if err != nil {
		return err
	}
	machine.registers[destination] = value
	return nil
}

func (machine *scalarMachine) closureTarget(value slot) (machineClosureHandle, programModuleID, int32, error) {
	index, generation, err := slotValidateHandle(value, slotTagClosure)
	if err != nil || index == 0 {
		return machineClosureHandle{}, 0, 0, fmt.Errorf("compact Machine call target is not a valid closure")
	}
	handle := machineClosureHandle{owner: machine.closures.owner, index: index, generation: generation}
	record, err := machine.closures.closureRecord(handle)
	if err != nil {
		return machineClosureHandle{}, 0, 0, fmt.Errorf("compact Machine call target is not a valid closure: %w", err)
	}
	target := int32(record.proto)
	image := machine.image
	if machine.persistentOwner != nil {
		if machine.persistentOwner.image == nil || uint64(record.module) >= uint64(len(machine.persistentOwner.image.modules)) {
			return machineClosureHandle{}, 0, 0, fmt.Errorf("compact Machine closure module %d is invalid", record.module)
		}
		image = machine.persistentOwner.image.modules[record.module].code
	} else if record.module != 0 {
		return machineClosureHandle{}, 0, 0, fmt.Errorf("compact Machine closure module %d is invalid", record.module)
	}
	if image == nil || target <= 0 || int(target) >= len(image.prototypes) {
		return machineClosureHandle{}, 0, 0, fmt.Errorf("compact Machine closure target %d is invalid", target)
	}
	return handle, record.module, target, nil
}

func (machine *scalarMachine) enterCall(operation machineOperation, nextPC int) (int32, int, error) {
	if machine == nil {
		return 0, 0, fmt.Errorf("compact Machine call state is unavailable")
	}
	var callable slot
	var err error
	if operation.op == opCallUpvalueOne {
		cell, cellErr := machine.upvalueCell(int(operation.b))
		if cellErr != nil {
			return 0, 0, cellErr
		}
		callable, err = machine.closures.cellGetOpen(cell, machine.registers)
	} else {
		calleeIndex := machine.activeBase + int(operation.b)
		if calleeIndex < 0 || calleeIndex >= len(machine.registers) {
			return 0, 0, fmt.Errorf("compact Machine call target register is out of range")
		}
		callable = machine.registers[calleeIndex]
	}
	if err != nil {
		return 0, 0, err
	}
	arguments, err := machine.callArguments(operation)
	if err != nil {
		return 0, 0, err
	}
	targetID, targetBase, err := machine.enterExplicitCall(machineCallRequest{
		callable:    callable,
		arguments:   arguments,
		destination: machine.activeBase + int(operation.a),
		resultCount: int(operation.callResults),
		returnPC:    nextPC,
		tailCall:    operation.tailCall,
	})
	if err != nil && !strings.HasPrefix(err.Error(), "run: call failed:") {
		err = fmt.Errorf("run: call failed: %w", err)
	}
	return targetID, targetBase, err
}

func (machine *scalarMachine) enterMethodOne(operation machineOperation, pc, nextPC int) (int32, int, error) {
	destination := machine.activeBase + int(operation.a)
	receiverRegister := machine.activeBase + int(operation.b)
	argStart := machine.activeBase + int(operation.callArgStart)
	argCount := int(operation.callArgCount)
	if destination < machine.activeBase || destination+1 >= len(machine.registers) ||
		receiverRegister < machine.activeBase || receiverRegister >= len(machine.registers) ||
		argStart != destination+1 || argCount < 1 || argStart+argCount > len(machine.registers) {
		return 0, 0, fmt.Errorf("compact Machine CALL_METHOD_ONE operands are out of range")
	}
	var action machineMethodOneAction
	if machine.resume.kind == machineResumeMethodLookup {
		resume := machine.resume
		machine.resume = machineSemanticResume{}
		var err error
		action, err = machine.tables.resumeMethodOneLookup(resume.method, machine.registers[destination], 1)
		if err != nil {
			return 0, 0, err
		}
	} else {
		keyValue, err := machine.constantSlot(int(operation.c), destination)
		if err != nil {
			return 0, 0, err
		}
		key, err := machine.stringID(keyValue)
		if err != nil {
			return 0, 0, err
		}
		action, err = machine.tables.prepareMethodOne(machine.registers[receiverRegister], key, machine.indexName, machine.callName)
		if err != nil {
			return 0, 0, err
		}
		if action.kind == machineMethodOneLookup {
			resume := machineSemanticResume{kind: machineResumeMethodLookup, method: action}
			targetID, targetBase, err := machine.enterTableAction(action.lookup, destination, 1, pc, resume)
			if err != nil || targetID >= 0 {
				return targetID, targetBase, err
			}
			action, err = machine.tables.resumeMethodOneLookup(action, machine.registers[destination], 1)
			if err != nil {
				return 0, 0, err
			}
		}
	}
	if action.kind != machineMethodOneScript && action.kind != machineMethodOneHost && action.kind != machineMethodOneMetamethod {
		return 0, 0, fmt.Errorf("compact Machine CALL_METHOD_ONE action is invalid")
	}
	if err := machine.copySlot(destination+1, action.receiver); err != nil {
		return 0, 0, err
	}
	return machine.enterExplicitCall(machineCallRequest{
		callable: action.callee, arguments: machine.registers[argStart : argStart+argCount],
		destination: destination, resultCount: 1, returnPC: nextPC,
	})
}

type machineCallRequest struct {
	callable    slot
	arguments   []slot
	destination int
	resultCount int
	returnPC    int
	resume      machineSemanticResume
	tailCall    uint8
}

func (machine *scalarMachine) callArguments(operation machineOperation) ([]slot, error) {
	argStart := machine.activeBase + int(operation.callArgStart)
	argCount := int(operation.callArgCount)
	if argCount < 0 {
		openStart := argStart + int(operation.callPrefix)
		if openStart != machine.activeOpenStart || machine.activeOpenCount < 0 {
			return nil, fmt.Errorf("compact Machine open call argument window is unavailable")
		}
		argCount = int(operation.callPrefix) + machine.activeOpenCount
	}
	if argStart < machine.activeBase || argCount < 0 || argStart+argCount > len(machine.registers) {
		return nil, fmt.Errorf("compact Machine call argument window is out of range")
	}
	return machine.registers[argStart : argStart+argCount], nil
}

func (machine *scalarMachine) enterExplicitCall(request machineCallRequest) (int32, int, error) {
	callable, arguments, err := machine.resolveCallable(request.callable, request.arguments)
	if err != nil {
		return 0, 0, err
	}
	request.callable = callable
	request.arguments = arguments
	if slotTagOf(callable) == slotTagNativeID {
		if err := machine.callNativeArgumentsStopped(callable, arguments, request.destination, request.resultCount, request.returnPC); err != nil {
			return 0, 0, err
		}
		if request.tailCall != 0 {
			machine.skipCharge = 1
		}
		return -1, machine.activeBase, nil
	}
	if slotTagOf(callable) == slotTagHostCallable {
		if err := machine.callFastHostStopped(callable, arguments, request.destination, request.resultCount); err != nil {
			return 0, 0, err
		}
		if request.tailCall != 0 {
			machine.skipCharge = 1
		}
		return -1, machine.activeBase, nil
	}
	closure, targetModule, targetID, err := machine.closureTarget(callable)
	if err != nil {
		return 0, 0, err
	}
	targetImage := machine.image
	if machine.persistentOwner != nil {
		targetImage = machine.persistentOwner.image.modules[targetModule].code
	}
	target := &targetImage.prototypes[targetID]
	if len(target.operations) == 0 {
		return 0, 0, fmt.Errorf("compact Machine call target has no operations")
	}
	callerBase := machine.activeBase
	argCount := len(arguments)
	calleeBase := len(machine.registers)
	varargCount := 0
	if target.variadic && argCount > target.params {
		varargCount = argCount - target.params
	}
	if request.tailCall != 0 && request.resume.kind == machineResumeNone {
		return machine.enterTailCall(targetModule, targetID, closure, target, arguments, varargCount)
	}
	if err := machine.ensureStack(calleeBase + target.registers + varargCount); err != nil {
		return 0, 0, err
	}
	for index := calleeBase; index < calleeBase+target.registers; index++ {
		machine.registers[index] = slotNil
	}
	for index := 0; index < target.params && index < argCount; index++ {
		if err := machine.copySlot(calleeBase+index, arguments[index]); err != nil {
			return 0, 0, err
		}
	}
	varargStart := calleeBase + target.registers
	for index := 0; index < varargCount; index++ {
		if err := machine.copySlot(varargStart+index, arguments[target.params+index]); err != nil {
			return 0, 0, err
		}
	}
	if machine.window.controller != nil {
		if err := machine.window.controller.enterCall(); err != nil {
			return 0, 0, err
		}
	}
	machine.continuations = append(machine.continuations, machineContinuation{
		moduleID:    machine.activeModule,
		protoID:     machine.activeProto,
		base:        int32(callerBase),
		returnPC:    int32(request.returnPC),
		returnBase:  int32(callerBase),
		returnReg:   int32(request.destination),
		returnCount: int32(request.resultCount),
		stackLength: int32(calleeBase),
		closure:     machine.activeClosure,
		varargStart: int32(machine.activeVarargStart),
		varargCount: int32(machine.activeVarargCount),
		cellStart:   int32(machine.activeCellStart),
		resume:      request.resume,
	})
	machine.activeModule = targetModule
	machine.image = targetImage
	machine.activeProto = targetID
	machine.activeBase = calleeBase
	machine.activeClosure = closure
	machine.activeVarargStart = varargStart
	machine.activeVarargCount = varargCount
	machine.activeOpenStart = 0
	machine.activeOpenCount = 0
	machine.activeCellStart = len(machine.closures.openCells)
	return targetID, calleeBase, nil
}

func (machine *scalarMachine) resolveCallable(callable slot, arguments []slot) (slot, []slot, error) {
	if slotValueKind(callable) != TableKind {
		if !machineTableCallable(callable) {
			return slotNil, nil, fmt.Errorf("call target is %s, want function", slotValueKind(callable))
		}
		return callable, arguments, nil
	}
	machine.callScratch = machine.callScratch[:0]
	for slotValueKind(callable) == TableKind {
		for _, seen := range machine.callScratch {
			if seen == callable {
				return slotNil, nil, fmt.Errorf("cyclic __call chain")
			}
		}
		if len(machine.callScratch) >= len(machine.tables.tables) {
			return slotNil, nil, fmt.Errorf("cyclic __call chain")
		}
		machine.callScratch = append(machine.callScratch, callable)
		table, err := machine.tableID(callable)
		if err != nil {
			return slotNil, nil, err
		}
		metamethod, found, err := machine.tables.metamethod(table, machine.callName)
		if err != nil {
			return slotNil, nil, err
		}
		if !found {
			return slotNil, nil, fmt.Errorf("call target is table, want function")
		}
		if !machineTableCallable(metamethod) && slotValueKind(metamethod) != TableKind {
			return slotNil, nil, fmt.Errorf("__call is %s, want function", slotValueKind(metamethod))
		}
		callable = metamethod
	}
	if !machineTableCallable(callable) {
		return slotNil, nil, fmt.Errorf("call target is %s, want function", slotValueKind(callable))
	}
	for left, right := 0, len(machine.callScratch)-1; left < right; left, right = left+1, right-1 {
		machine.callScratch[left], machine.callScratch[right] = machine.callScratch[right], machine.callScratch[left]
	}
	machine.callScratch = append(machine.callScratch, arguments...)
	return callable, machine.callScratch, nil
}

func (machine *scalarMachine) callNativeArgumentsStopped(callable slot, arguments []slot, destination, resultCount, returnPC int) error {
	nativeID, err := slotNativeIDValue(callable)
	if err != nil {
		return err
	}
	if nativeID == nativeFuncToString {
		value := slotNil
		if len(arguments) > 0 {
			value = arguments[0]
		}
		if err := machine.toString(destination, value); err != nil {
			return fmt.Errorf("run: call failed: host function failed: %w", err)
		}
		return machine.fillFixedCallResults(destination, resultCount, 1)
	}
	if nativeID == nativeFuncSelect {
		return machine.callNativeAdapterArgumentsStopped(nativeID, arguments, destination, resultCount)
	}
	if nativeID == nativeFuncCoroutineCreate || nativeID == nativeFuncCoroutineStatus || nativeID == nativeFuncCoroutineResume || nativeID == nativeFuncCoroutineYield {
		return machine.callCoroutineNativeStopped(nativeID, arguments, destination, resultCount, returnPC)
	}
	if nativeID == nativeFuncSetMetatable || nativeID == nativeFuncGetMetatable {
		first, second := slotNil, slotNil
		if len(arguments) > 0 {
			first = arguments[0]
		}
		if len(arguments) > 1 {
			second = arguments[1]
		}
		outcome, err := runMachineGuardedMetatableIntrinsicStopped(&machine.tables, machineMetatableIntrinsicRequest{
			callee: callable, first: first, second: second, argumentCount: uint32(len(arguments)), nativeID: nativeID,
		})
		if err != nil {
			return fmt.Errorf("run: call failed: host function failed: %w", err)
		}
		if !outcome.matched {
			return fmt.Errorf("run: call failed: compact Machine native ID %d is unsupported", nativeID)
		}
		return machine.applyIntrinsicOutcome(destination, resultCount, machineIntrinsicOutcome{
			value: outcome.value, resultCount: outcome.resultCount, matched: true,
		})
	}
	request := machineIntrinsicRequest{
		nativeID: nativeID,
		callee:   callable,
		args:     arguments,
	}
	if machine.window.controller != nil {
		request.tableEntryLimit = machine.window.controller.limits.MaxTableEntriesPerTable
	}
	outcome, err := runMachineGuardedIntrinsicStopped(machine, request)
	if err != nil {
		return fmt.Errorf("run: call failed: host function failed: %w", err)
	}
	if !outcome.matched {
		return fmt.Errorf("run: call failed: compact Machine native ID %d is unsupported", nativeID)
	}
	return machine.applyIntrinsicOutcome(destination, resultCount, outcome)
}

func (machine *scalarMachine) fastCall(operation machineOperation, returnPC int) error {
	if machine == nil || machine.persistentOwner == nil {
		return fmt.Errorf("compact Machine FAST_CALL requires a persistent owner")
	}
	dense, err := machine.persistentOwner.globalIndexStopped(machine.activeModule, operation.globalIndex)
	if err != nil {
		return err
	}
	callee, present, err := machine.persistentOwner.globals.getAt(dense)
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("run: undefined intrinsic global")
	}
	if operation.guardField != invalidMachineStringID {
		globalTable, tableErr := machine.tableID(callee)
		if tableErr != nil {
			return fmt.Errorf("run: intrinsic guard target is %s, want table", slotValueKind(callee))
		}
		field, translateErr := machine.persistentOwner.translateImageStringIDStopped(machine.activeModule, operation.guardField)
		if translateErr != nil {
			return translateErr
		}
		callee, err = machine.tables.rawGet(globalTable, machineTableStringKey(field))
		if err != nil {
			return err
		}
	}
	start := machine.activeBase + int(operation.a)
	argCount := int(operation.c)
	if start < machine.activeBase || argCount < 0 || start+argCount > len(machine.registers) {
		return fmt.Errorf("compact Machine FAST_CALL argument window is out of range")
	}
	nativeID := nativeFuncID(operation.nativeID)
	if nativeID == nativeFuncCoroutineResume && slotTagOf(callee) == slotTagNativeID {
		actual, nativeErr := slotNativeIDValue(callee)
		if nativeErr != nil {
			return nativeErr
		}
		if actual == nativeID {
			return machine.callCoroutineNativeStopped(nativeID, machine.registers[start:start+argCount], start, int(operation.d), returnPC)
		}
	}
	request := machineIntrinsicRequest{
		nativeID:          nativeID,
		callee:            callee,
		args:              machine.registers[start : start+argCount],
		selectVarargCount: machine.activeVarargCount,
	}
	if machine.window.controller != nil {
		request.tableEntryLimit = machine.window.controller.limits.MaxTableEntriesPerTable
	}
	outcome, err := runMachineGuardedIntrinsicStopped(machine, request)
	if err != nil {
		return fmt.Errorf("run: call failed: host function failed: %w", err)
	}
	if outcome.matched {
		if outcome.additionalGuestCharge != 0 && machine.window.controller != nil {
			if err := machine.window.controller.chargeInstructions(uint64(outcome.additionalGuestCharge)); err != nil {
				return err
			}
		}
		return machine.applyIntrinsicOutcome(start, int(operation.d), outcome)
	}
	if slotTagOf(callee) != slotTagHostCallable {
		return fmt.Errorf("run: call failed: intrinsic guard replacement is %s, want function", slotValueKind(callee))
	}
	args := machine.registers[start : start+argCount]
	if nativeID == nativeFuncSelect {
		machine.callScratch = machine.callScratch[:0]
		hashID, internErr := machine.strings.internStringStopped("#")
		if internErr != nil {
			return internErr
		}
		hashSlot, internErr := slotPackHandle(slotTagString, uint32(hashID), 1)
		if internErr != nil {
			return internErr
		}
		machine.callScratch = append(machine.callScratch, hashSlot)
		varargStart := machine.activeVarargStart
		if varargStart < 0 || varargStart+machine.activeVarargCount > len(machine.registers) {
			return fmt.Errorf("compact Machine select vararg window is unavailable")
		}
		machine.callScratch = append(machine.callScratch, machine.registers[varargStart:varargStart+machine.activeVarargCount]...)
		args = machine.callScratch
	}
	return machine.callFastHostStopped(callee, args, start, int(operation.d))
}

func (machine *scalarMachine) applyIntrinsicOutcome(destination, requested int, outcome machineIntrinsicOutcome) error {
	if outcome.resultCount > 1 {
		return fmt.Errorf("compact Machine intrinsic returned unsupported result count %d", outcome.resultCount)
	}
	actual := int(outcome.resultCount)
	if requested < 0 {
		requested = actual
		machine.activeOpenStart = destination
		machine.activeOpenCount = actual
	} else {
		machine.activeOpenStart = 0
		machine.activeOpenCount = 0
	}
	if requested < 0 || destination < 0 || destination+requested > len(machine.registers) {
		return fmt.Errorf("compact Machine intrinsic result window is out of range")
	}
	if actual > 0 && requested > 0 {
		if err := machine.copySlot(destination, outcome.value); err != nil {
			return err
		}
	}
	for index := actual; index < requested; index++ {
		machine.registers[destination+index] = slotNil
	}
	return nil
}

func (machine *scalarMachine) fillFixedCallResults(destination, requested, actual int) error {
	if requested < 0 {
		machine.activeOpenStart = destination
		machine.activeOpenCount = actual
		return nil
	}
	for index := actual; index < requested; index++ {
		if destination+index < 0 || destination+index >= len(machine.registers) {
			return fmt.Errorf("compact Machine call result window is out of range")
		}
		machine.registers[destination+index] = slotNil
	}
	return nil
}

func (machine *scalarMachine) enterTailCall(targetModule programModuleID, targetID int32, closure machineClosureHandle, target *machineProto, arguments []slot, varargCount int) (int32, int, error) {
	machine.transfer = machine.transfer[:0]
	for _, value := range arguments {
		transfer := machineTransferValue{value: value}
		if slotValueKind(value) == NumberKind {
			number, err := machine.number(value)
			if err != nil {
				return 0, 0, err
			}
			transfer.numberBits = math.Float64bits(number)
			transfer.isNumber = 1
		}
		machine.transfer = append(machine.transfer, transfer)
	}
	if err := machine.closeActiveFrameCaptures(); err != nil {
		return 0, 0, err
	}
	calleeBase := machine.activeBase
	machine.registers = machine.registers[:calleeBase]
	if err := machine.ensureStack(calleeBase + target.registers + varargCount); err != nil {
		return 0, 0, err
	}
	for index := calleeBase; index < calleeBase+target.registers; index++ {
		machine.registers[index] = slotNil
	}
	for index := 0; index < target.params && index < len(arguments); index++ {
		if err := machine.copyTransfer(calleeBase+index, machine.transfer[index]); err != nil {
			return 0, 0, err
		}
	}
	varargStart := calleeBase + target.registers
	for index := 0; index < varargCount; index++ {
		if err := machine.copyTransfer(varargStart+index, machine.transfer[target.params+index]); err != nil {
			return 0, 0, err
		}
	}
	machine.activeModule = targetModule
	if machine.persistentOwner != nil {
		machine.image = machine.persistentOwner.image.modules[targetModule].code
	}
	machine.activeProto = targetID
	machine.activeBase = calleeBase
	machine.activeClosure = closure
	machine.activeVarargStart = varargStart
	machine.activeVarargCount = varargCount
	machine.activeOpenStart = 0
	machine.activeOpenCount = 0
	machine.activeCellStart = len(machine.closures.openCells)
	return targetID, calleeBase, nil
}

func (machine *scalarMachine) returnFromCall(start, count int) (done bool, protoID int32, base, pc int, err error) {
	openReturn := count < 0
	if count < 0 {
		prefix := -count - 1
		if machine.activeBase+start+prefix != machine.activeOpenStart || machine.activeOpenCount < 0 {
			return false, 0, 0, 0, fmt.Errorf("compact Machine open return window is unavailable")
		}
		count = prefix + machine.activeOpenCount
	}
	if openReturn && count == 0 {
		count = 1
		absolute := machine.activeBase + start
		if absolute >= len(machine.registers) {
			if err := machine.ensureStack(absolute + 1); err != nil {
				return false, 0, 0, 0, err
			}
		}
		machine.registers[absolute] = slotNil
	}
	if count < 0 || start < 0 || machine.activeBase+start+count > len(machine.registers) {
		return false, 0, 0, 0, fmt.Errorf("compact Machine return window is out of range")
	}
	start += machine.activeBase
	machine.transfer = machine.transfer[:0]
	for index := 0; index < count; index++ {
		value := machine.registers[start+index]
		transfer := machineTransferValue{value: value}
		if slotValueKind(value) == NumberKind {
			number, numberErr := machine.number(value)
			if numberErr != nil {
				return false, 0, 0, 0, numberErr
			}
			transfer.numberBits = math.Float64bits(number)
			transfer.isNumber = 1
		}
		machine.transfer = append(machine.transfer, transfer)
	}
	if err := machine.closeActiveFrameCaptures(); err != nil {
		return false, 0, 0, 0, err
	}
	if len(machine.continuations) == 0 {
		if err := machine.returnTransfer(); err != nil {
			return false, 0, 0, 0, err
		}
		return true, 0, 0, 0, nil
	}
	continuation := machine.continuations[len(machine.continuations)-1]
	machine.continuations = machine.continuations[:len(machine.continuations)-1]
	machine.registers = machine.registers[:continuation.stackLength]
	machine.activeModule = continuation.moduleID
	if machine.persistentOwner != nil {
		machine.image = machine.persistentOwner.image.modules[continuation.moduleID].code
	}
	machine.activeProto = continuation.protoID
	machine.activeBase = int(continuation.returnBase)
	machine.activeClosure = continuation.closure
	machine.activeVarargStart = int(continuation.varargStart)
	machine.activeVarargCount = int(continuation.varargCount)
	machine.activeOpenStart = 0
	machine.activeOpenCount = 0
	machine.activeCellStart = int(continuation.cellStart)
	if machine.window.controller != nil {
		machine.window.controller.leaveCall()
	}
	returnCount := int(continuation.returnCount)
	if returnCount < 0 {
		returnCount = count
		end := int(continuation.returnReg) + returnCount
		if end > len(machine.registers) {
			if err := machine.ensureStack(end); err != nil {
				return false, 0, 0, 0, err
			}
		}
		machine.activeOpenStart = int(continuation.returnReg)
		machine.activeOpenCount = returnCount
	}
	for index := 0; index < returnCount; index++ {
		value := machineTransferValue{value: slotNil}
		if index < len(machine.transfer) {
			value = machine.transfer[index]
		}
		if err := machine.copyTransfer(int(continuation.returnReg)+index, value); err != nil {
			return false, 0, 0, 0, err
		}
	}
	if continuation.resume.kind != machineResumeNone {
		machine.resume = continuation.resume
		machine.skipCharge = 1
	}
	return false, continuation.protoID, int(continuation.returnBase), int(continuation.returnPC), nil
}

func (machine *scalarMachine) upvalueCell(index int) (machineCellHandle, error) {
	if machine.activeClosure.index == 0 {
		return machineCellHandle{}, fmt.Errorf("compact Machine has no active closure")
	}
	return machine.closures.captureCell(machine.activeClosure, index)
}

func (machine *scalarMachine) loadUpvalue(destination, index int) error {
	cell, err := machine.upvalueCell(index)
	if err != nil {
		return err
	}
	value, err := machine.closures.cellGetOpen(cell, machine.registers)
	if err != nil {
		return err
	}
	return machine.copySlot(destination, value)
}

func (machine *scalarMachine) storeUpvalue(index, source int) error {
	if source < 0 || source >= len(machine.registers) {
		return fmt.Errorf("compact Machine SET_UPVALUE source is out of range")
	}
	cell, err := machine.upvalueCell(index)
	if err != nil {
		return err
	}
	record, err := machine.closures.cellRecord(cell)
	if err != nil {
		return err
	}
	if record.openRegister != 0 {
		return machine.copySlot(int(record.openRegister-1), machine.registers[source])
	}
	value, err := machine.stableMachineValueStopped(machine.registers[source])
	if err != nil {
		return err
	}
	return machine.closures.cellSet(cell, value)
}

func (machine *scalarMachine) loadVarargs(destination, count int) error {
	if destination < 0 || machine.activeVarargCount < 0 {
		return fmt.Errorf("compact Machine VARARG operands are out of range")
	}
	resultCount := count
	if resultCount == 0 {
		resultCount = 1
	}
	if resultCount < 0 {
		resultCount = machine.activeVarargCount
		end := destination + resultCount
		if end > len(machine.registers) {
			if err := machine.ensureStack(end); err != nil {
				return err
			}
		}
		machine.activeOpenStart = destination
		machine.activeOpenCount = resultCount
	}
	if destination+resultCount > len(machine.registers) {
		return fmt.Errorf("compact Machine VARARG result window is out of range")
	}
	for index := 0; index < resultCount; index++ {
		value := slotNil
		if index < machine.activeVarargCount {
			value = machine.registers[machine.activeVarargStart+index]
		}
		if err := machine.copySlot(destination+index, value); err != nil {
			return err
		}
	}
	return nil
}

func (machine *scalarMachine) closeActiveFrameCaptures() error {
	proto := machine.currentProto()
	if proto == nil {
		return fmt.Errorf("compact Machine active prototype is unavailable")
	}
	start, end := machine.activeBase, machine.activeBase+proto.registers
	if machine.activeCellStart < 0 || machine.activeCellStart > len(machine.closures.openCells) {
		return fmt.Errorf("compact Machine open-cell frame index is invalid")
	}
	for _, open := range machine.closures.openCells[machine.activeCellStart:] {
		if open.cell == 0 || uint64(open.cell) > uint64(len(machine.closures.cells)) {
			return fmt.Errorf("compact Machine open-cell index is invalid")
		}
		record := &machine.closures.cells[open.cell-1]
		if record.live == 0 || record.openRegister == 0 {
			continue
		}
		register := int(record.openRegister - 1)
		if register < start || register >= end {
			continue
		}
		value, err := machine.stableMachineValueStopped(machine.registers[register])
		if err != nil {
			return err
		}
		record.value = value
		record.openRegister = 0
	}
	clear(machine.closures.openCells[machine.activeCellStart:])
	machine.closures.openCells = machine.closures.openCells[:machine.activeCellStart]
	return nil
}

func (machine *scalarMachine) closeAllCapturesStopped() error {
	if machine == nil {
		return nil
	}
	for _, open := range machine.closures.openCells {
		if open.cell == 0 || uint64(open.cell) > uint64(len(machine.closures.cells)) {
			return fmt.Errorf("compact Machine open-cell index is invalid")
		}
		record := &machine.closures.cells[open.cell-1]
		if record.live == 0 || record.openRegister == 0 {
			continue
		}
		register := int(record.openRegister - 1)
		if register < 0 || register >= len(machine.registers) {
			return fmt.Errorf("compact Machine open-cell register is invalid")
		}
		value, err := machine.stableMachineValueStopped(machine.registers[register])
		if err != nil {
			return err
		}
		record.value = value
		record.openRegister = 0
	}
	clear(machine.closures.openCells)
	machine.closures.openCells = machine.closures.openCells[:0]
	machine.activeCellStart = 0
	return nil
}

func (machine *scalarMachine) stableMachineValueStopped(value slot) (slot, error) {
	switch slotValueKind(value) {
	case NilKind, BoolKind:
		return value, nil
	case NumberKind, StringKind, TableKind, HostFuncKind, UserDataKind:
		return machine.stableTableValueStopped(value)
	case FunctionKind:
		if _, _, _, err := machine.closureTarget(value); err != nil {
			return slotNil, err
		}
		return value, nil
	default:
		return slotNil, fmt.Errorf("compact Machine value kind %s cannot cross a call boundary", slotValueKind(value))
	}
}

func (machine *scalarMachine) returnTransfer() error {
	count := len(machine.transfer)
	machine.results = resizeSlots(machine.results, count)
	if cap(machine.numberBits) < len(machine.registers)+count+1 {
		machine.numberBits = append(machine.numberBits, make([]uint64, len(machine.registers)+count+1-len(machine.numberBits))...)
	} else {
		machine.numberBits = machine.numberBits[:len(machine.registers)+count+1]
	}
	for index, value := range machine.transfer {
		if err := machine.copyTransfer(len(machine.registers)+index, value); err != nil {
			return err
		}
	}
	machine.resultCount = count
	return nil
}

func (machine *scalarMachine) copyTransfer(destination int, value machineTransferValue) error {
	if value.isNumber != 0 {
		machine.setNumber(destination, math.Float64frombits(value.numberBits))
		return nil
	}
	return machine.copySlot(destination, value.value)
}

func (machine *scalarMachine) copySlot(destination int, value slot) error {
	if slotValueKind(value) == NumberKind {
		number, err := machine.number(value)
		if err != nil {
			return err
		}
		machine.setNumber(destination, number)
		return nil
	}
	machine.setCell(destination, value)
	return nil
}

func (machine *scalarMachine) setNumber(cell int, number float64) {
	if cell < 0 {
		return
	}
	if cell >= len(machine.numberBits) {
		if cap(machine.numberBits) < cell+1 {
			machine.numberBits = append(machine.numberBits, make([]uint64, cell+1-len(machine.numberBits))...)
		} else {
			machine.numberBits = machine.numberBits[:cell+1]
		}
	}
	bits := math.Float64bits(number)
	if bits&slotTaggedMask != slotTaggedPrefix {
		machine.setCell(cell, slot(bits))
		return
	}
	machine.numberBits[cell] = bits
	machine.setCell(cell, slot(slotTaggedPrefix|
		uint64(slotTagBoxedNumber)<<slotTagShift|
		uint64(1)<<slotGenerationShift|
		uint64(cell+1)))
}

func (machine *scalarMachine) setCell(cell int, value slot) {
	if cell < len(machine.registers) {
		machine.registers[cell] = value
		return
	}
	result := cell - len(machine.registers)
	if result < len(machine.results) {
		machine.results[result] = value
		return
	}
	machine.scratch = value
}

func (machine *scalarMachine) number(value slot) (float64, error) {
	if !slotIsTagged(value) {
		return math.Float64frombits(uint64(value)), nil
	}
	if slotTagOf(value) != slotTagBoxedNumber {
		return 0, fmt.Errorf("value is %s, want number", slotValueKind(value))
	}
	_, index, generation, err := slotUnpackHandle(value)
	if err != nil || index == 0 {
		return 0, fmt.Errorf("compact Machine boxed number handle is invalid")
	}
	switch generation {
	case 1:
		if int(index) > len(machine.numberBits) {
			return 0, fmt.Errorf("compact Machine boxed number handle is invalid")
		}
		return math.Float64frombits(machine.numberBits[index-1]), nil
	case 2:
		if int(index) > len(machine.tableNumbers) {
			return 0, fmt.Errorf("compact Machine boxed table number handle is invalid")
		}
		return math.Float64frombits(machine.tableNumbers[index-1]), nil
	default:
		return 0, fmt.Errorf("compact Machine boxed number generation is invalid")
	}
}

func (machine *scalarMachine) binary(op opcode, destination int, left, right slot) error {
	operator, prefix := machineArithmeticNames(op)
	leftNumber, err := machine.numericOperand(left, "left", operator)
	if err != nil {
		return fmt.Errorf("run: %s failed: %w", prefix, err)
	}
	rightNumber, err := machine.numericOperand(right, "right", operator)
	if err != nil {
		return fmt.Errorf("run: %s failed: %w", prefix, err)
	}
	return machine.binaryNumbers(op, destination, leftNumber, rightNumber)
}

func (machine *scalarMachine) binaryNumbers(op opcode, destination int, leftNumber, rightNumber float64) error {
	var result float64
	switch op {
	case opAdd, opAddK:
		result = leftNumber + rightNumber
	case opSub, opSubK:
		result = leftNumber - rightNumber
	case opMul, opMulK:
		result = leftNumber * rightNumber
	case opDiv, opDivK:
		result = leftNumber / rightNumber
	case opMod, opModK:
		result = leftNumber - math.Floor(leftNumber/rightNumber)*rightNumber
	case opIDiv, opIDivK:
		result = math.Floor(leftNumber / rightNumber)
	case opPow:
		result = math.Pow(leftNumber, rightNumber)
	default:
		return fmt.Errorf("compact Machine arithmetic opcode %s is unsupported", opcodeName(op))
	}
	machine.setNumber(destination, result)
	return nil
}

func machineArithmeticNames(op opcode) (operator, prefix string) {
	switch op {
	case opAdd, opAddK:
		return "add", "add"
	case opSub, opSubK:
		return "subtract", "subtract"
	case opMul, opMulK:
		return "multiply", "multiply"
	case opDiv, opDivK:
		return "divide", "divide"
	case opMod, opModK:
		return "modulo", "modulo"
	case opIDiv, opIDivK:
		return "floor divide", "floor divide"
	case opPow:
		return "power", "power"
	default:
		return opcodeName(op), opcodeName(op)
	}
}

func (machine *scalarMachine) binaryRegisters(op opcode, destination, left, right int) error {
	leftValue, rightValue := machine.registers[left], machine.registers[right]
	if !slotIsTagged(leftValue) && !slotIsTagged(rightValue) {
		return machine.binaryNumbers(op, destination,
			math.Float64frombits(uint64(leftValue)), math.Float64frombits(uint64(rightValue)))
	}
	return machine.binary(op, destination, leftValue, rightValue)
}

func (machine *scalarMachine) binaryConstant(op opcode, destination, left, constant int) error {
	proto := machine.currentProto()
	leftValue := machine.registers[left]
	if proto != nil && constant >= 0 && constant < len(proto.constants) && !slotIsTagged(leftValue) {
		descriptor := proto.constants[constant]
		if descriptor.kind == NumberKind {
			return machine.binaryNumbers(op, destination,
				math.Float64frombits(uint64(leftValue)), math.Float64frombits(descriptor.bits))
		}
	}
	right, err := machine.constantSlot(constant, destination)
	if err != nil {
		return err
	}
	return machine.binary(op, destination, machine.registers[left], right)
}

func (machine *scalarMachine) constantSlot(index, _ int) (slot, error) {
	proto := machine.currentProto()
	if proto == nil || index < 0 || index >= len(proto.constants) {
		return 0, fmt.Errorf("compact Machine constant index %d is out of range", index)
	}
	descriptor := proto.constants[index]
	switch descriptor.kind {
	case NilKind:
		return slotNil, nil
	case BoolKind:
		return slotBool(descriptor.bits != 0), nil
	case NumberKind:
		scratchCell := len(machine.registers) + len(machine.results)
		machine.setNumber(scratchCell, math.Float64frombits(descriptor.bits))
		return machine.scratch, nil
	case StringKind:
		return machine.stringSlot(descriptor.bits)
	default:
		return 0, fmt.Errorf("compact Machine constant kind %s is unsupported", descriptor.kind)
	}
}

func (machine *scalarMachine) stringSlot(bits uint64) (slot, error) {
	if bits > math.MaxUint32 {
		return 0, fmt.Errorf("compact Machine string constant ID overflows uint32")
	}
	id := machineStringID(bits)
	if id == invalidMachineStringID {
		return 0, fmt.Errorf("compact Machine string constant has an invalid ID")
	}
	if machine.persistentOwner != nil {
		var err error
		id, err = machine.persistentOwner.translateImageStringIDStopped(machine.activeModule, id)
		if err != nil {
			return 0, err
		}
	}
	if _, ok := machine.strings.lookup(id); !ok {
		return 0, fmt.Errorf("compact Machine string constant ID %d is not bound", id)
	}
	return slotPackHandle(slotTagString, uint32(id), 1)
}

func (machine *scalarMachine) stringID(value slot) (machineStringID, error) {
	index, generation, err := slotValidateHandle(value, slotTagString)
	if err != nil || generation != 1 {
		return invalidMachineStringID, fmt.Errorf("compact Machine value is not a valid string handle")
	}
	id := machineStringID(index)
	if _, ok := machine.strings.lookup(id); !ok {
		return invalidMachineStringID, fmt.Errorf("compact Machine string ID %d is invalid", id)
	}
	return id, nil
}

func (machine *scalarMachine) numericOperand(value slot, side, operator string) (float64, error) {
	operand, err := machine.scalarOperand(value)
	if err != nil {
		return 0, err
	}
	action, err := machinePrepareNumericOperand(operand, &machine.strings)
	if err != nil {
		return 0, err
	}
	return machineNumericOperandResult(action, side, operator)
}

func (machine *scalarMachine) scalarOperand(value slot) (machineScalarOperand, error) {
	operand := machineScalarOperand{value: value}
	if slotValueKind(value) != NumberKind || !slotIsTagged(value) {
		return operand, nil
	}
	number, err := machine.number(value)
	if err != nil {
		return machineScalarOperand{}, err
	}
	operand.numberBits = math.Float64bits(number)
	return operand, nil
}

func (machine *scalarMachine) negate(destination, operand int) error {
	value := machine.registers[operand]
	if !slotIsTagged(value) {
		machine.setNumber(destination, -math.Float64frombits(uint64(value)))
		return nil
	}
	number, err := machine.numericOperand(value, "", "negate")
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}
	machine.setNumber(destination, -number)
	return nil
}

func (machine *scalarMachine) concat(destination int, values ...slot) error {
	machine.stringScratch = machine.stringScratch[:0]
	machine.operandScratch = machine.operandScratch[:0]
	for _, value := range values {
		operand, err := machine.scalarOperand(value)
		if err != nil {
			return err
		}
		machine.operandScratch = append(machine.operandScratch, operand)
	}
	var action machineStringAction
	var err error
	machine.stringScratch, action, err = machinePrepareConcatChain(machine.stringScratch, machine.operandScratch, &machine.strings)
	if err != nil {
		return fmt.Errorf("run: concat failed: %w", err)
	}
	if action.kind == machineStringActionEffect {
		index := int(action.operand)
		side := "right"
		if index == 0 {
			side = "left"
		}
		kind := slotInvalidValueKind
		if index >= 0 && index < len(values) {
			kind = slotValueKind(values[index])
		}
		return fmt.Errorf("run: concat failed: concat %s operand is %s, want string or number", side, kind)
	}
	return machine.completeStringAction(destination, action)
}

func (machine *scalarMachine) concatRegisters(destination, start, count int) error {
	if count < 0 || start < 0 || start+count > len(machine.registers) {
		return fmt.Errorf("compact Machine CONCAT_CHAIN operands are out of range")
	}
	return machine.concat(destination, machine.registers[start:start+count]...)
}

func (machine *scalarMachine) toString(destination int, value slot) error {
	operand, err := machine.scalarOperand(value)
	if err != nil {
		return err
	}
	machine.stringScratch = machine.stringScratch[:0]
	var action machineStringAction
	machine.stringScratch, action, err = machinePrepareToString(machine.stringScratch, operand, &machine.strings)
	if err != nil {
		return err
	}
	if action.kind == machineStringActionEffect {
		text := slotValueKind(value).String()
		if slotValueKind(value) == HostFuncKind {
			text = FunctionKind.String()
		}
		start := len(machine.stringScratch)
		machine.stringScratch = append(machine.stringScratch, text...)
		machine.stringScratch, action, err = machineGeneratedStringAction(machine.stringScratch, start, false)
		if err != nil {
			return err
		}
	}
	return machine.completeStringAction(destination, action)
}

func (machine *scalarMachine) completeStringAction(destination int, action machineStringAction) error {
	if destination < 0 || destination >= len(machine.registers) {
		return fmt.Errorf("compact Machine string destination is out of range")
	}
	if action.kind == machineStringActionReuse {
		value, err := slotPackHandle(slotTagString, uint32(action.stringID), 1)
		if err != nil {
			return err
		}
		machine.registers[destination] = value
		return nil
	}
	text, err := machineStringActionBytes(action, machine.stringScratch, &machine.strings)
	if err != nil {
		return err
	}
	hash := machineStringHash(text)
	existing := machine.strings.find(text, hash)
	alreadyGenerated := false
	if action.generatedBytes != 0 && existing != invalidMachineStringID {
		for _, generated := range machine.generatedStrings {
			if generated == existing {
				alreadyGenerated = true
				break
			}
		}
	}
	if err := machineChargeGeneratedStringStopped(machine.window.controller, action, alreadyGenerated); err != nil {
		return err
	}
	id := existing
	if id == invalidMachineStringID {
		id, err = machine.strings.internBytesStopped(text)
		if err != nil {
			return err
		}
	}
	if action.generatedBytes != 0 && !alreadyGenerated {
		machine.generatedStrings = append(machine.generatedStrings, id)
	}
	value, err := slotPackHandle(slotTagString, uint32(id), 1)
	if err != nil {
		return err
	}
	machine.registers[destination] = value
	return nil
}

func (machine *scalarMachine) equal(left, right slot) (bool, error) {
	if !slotIsTagged(left) && !slotIsTagged(right) {
		leftNumber := math.Float64frombits(uint64(left))
		rightNumber := math.Float64frombits(uint64(right))
		return !math.IsNaN(leftNumber) && !math.IsNaN(rightNumber) && leftNumber == rightNumber, nil
	}
	leftKind, rightKind := slotValueKind(left), slotValueKind(right)
	if leftKind != rightKind {
		return false, nil
	}
	switch leftKind {
	case NilKind:
		return true, nil
	case BoolKind:
		leftBool, err := slotBoolValue(left)
		if err != nil {
			return false, err
		}
		rightBool, err := slotBoolValue(right)
		return leftBool == rightBool, err
	case NumberKind:
		leftNumber, err := machine.number(left)
		if err != nil {
			return false, err
		}
		rightNumber, err := machine.number(right)
		if err != nil {
			return false, err
		}
		return !math.IsNaN(leftNumber) && !math.IsNaN(rightNumber) && leftNumber == rightNumber, nil
	case StringKind:
		leftID, err := machine.stringID(left)
		if err != nil {
			return false, err
		}
		rightID, err := machine.stringID(right)
		if err != nil {
			return false, err
		}
		return machineStringIDsEqual(&machine.strings, leftID, rightID), nil
	case TableKind:
		leftID, err := machine.tableID(left)
		if err != nil {
			return false, err
		}
		rightID, err := machine.tableID(right)
		if err != nil {
			return false, err
		}
		return leftID == rightID, nil
	case FunctionKind:
		leftHandle, _, _, err := machine.closureTarget(left)
		if err != nil {
			return false, err
		}
		rightHandle, _, _, err := machine.closureTarget(right)
		if err != nil {
			return false, err
		}
		return leftHandle.index == rightHandle.index && leftHandle.generation == rightHandle.generation, nil
	case UserDataKind:
		if machine.persistentOwner == nil || slotTagOf(left) != slotTagCoroutine || slotTagOf(right) != slotTagCoroutine {
			return false, fmt.Errorf("compact Machine equality kind %s is unsupported", leftKind)
		}
		leftHandle, err := machine.persistentOwner.coroutines.handleFromSlot(left)
		if err != nil {
			return false, err
		}
		rightHandle, err := machine.persistentOwner.coroutines.handleFromSlot(right)
		if err != nil {
			return false, err
		}
		return leftHandle.index == rightHandle.index && leftHandle.generation == rightHandle.generation, nil
	default:
		return false, fmt.Errorf("compact Machine equality kind %s is unsupported", leftKind)
	}
}

func (machine *scalarMachine) compare(op opcode, left, right slot) (bool, error) {
	if !slotIsTagged(left) && !slotIsTagged(right) {
		return machine.compareNumbers(op, math.Float64frombits(uint64(left)), math.Float64frombits(uint64(right)))
	}
	prefix := machineComparisonName(op)
	if slotValueKind(left) != slotValueKind(right) {
		return false, fmt.Errorf("run: %s failed: compare operands are %s and %s", prefix, slotValueKind(left), slotValueKind(right))
	}
	if slotValueKind(left) == StringKind {
		leftID, err := machine.stringID(left)
		if err != nil {
			return false, err
		}
		rightID, err := machine.stringID(right)
		if err != nil {
			return false, err
		}
		leftBytes, leftOK := machine.strings.bytesFor(leftID)
		rightBytes, rightOK := machine.strings.bytesFor(rightID)
		if !leftOK || !rightOK {
			return false, fmt.Errorf("compact Machine string comparison ID is invalid")
		}
		comparison := bytesCompare(leftBytes, rightBytes)
		switch op {
		case opLess, opJumpIfNotLess, opJumpIfLess, opJumpIfNotLessK, opJumpIfLessK:
			return comparison < 0, nil
		case opLessEqual:
			return comparison <= 0, nil
		case opGreater, opJumpIfNotGreater, opJumpIfGreater, opJumpIfNotGreaterK, opJumpIfGreaterK:
			return comparison > 0, nil
		case opGreaterEqual:
			return comparison >= 0, nil
		default:
			return false, fmt.Errorf("compact Machine comparison opcode %s is unsupported", opcodeName(op))
		}
	}
	if slotValueKind(left) != NumberKind {
		return false, fmt.Errorf("run: %s failed: compare operands are %s, want number or string", prefix, slotValueKind(left))
	}
	leftNumber, err := machine.number(left)
	if err != nil {
		return false, err
	}
	rightNumber, err := machine.number(right)
	if err != nil {
		return false, err
	}
	return machine.compareNumbers(op, leftNumber, rightNumber)
}

func (machine *scalarMachine) compareNumbers(op opcode, leftNumber, rightNumber float64) (bool, error) {
	if math.IsNaN(leftNumber) || math.IsNaN(rightNumber) {
		return false, fmt.Errorf("run: %s failed: compare operand is NaN", machineComparisonName(op))
	}
	switch op {
	case opLess, opJumpIfNotLess, opJumpIfLess, opJumpIfNotLessK, opJumpIfLessK:
		return leftNumber < rightNumber, nil
	case opLessEqual:
		return leftNumber <= rightNumber, nil
	case opGreater, opJumpIfNotGreater, opJumpIfGreater, opJumpIfNotGreaterK, opJumpIfGreaterK:
		return leftNumber > rightNumber, nil
	case opGreaterEqual:
		return leftNumber >= rightNumber, nil
	default:
		return false, fmt.Errorf("compact Machine comparison opcode %s is unsupported", opcodeName(op))
	}
}

func machineStringIDsEqual(arena *machineStringArena, left, right machineStringID) bool {
	return arena != nil && arena.equal(left, right)
}

func machineComparisonName(op opcode) string {
	switch op {
	case opLess, opJumpIfNotLess, opJumpIfLess, opJumpIfNotLessK, opJumpIfLessK:
		return "less"
	case opLessEqual:
		return "less equal"
	case opGreater, opJumpIfNotGreater, opJumpIfGreater, opJumpIfNotGreaterK, opJumpIfGreaterK:
		return "greater"
	case opGreaterEqual:
		return "greater equal"
	default:
		return opcodeName(op)
	}
}

func (machine *scalarMachine) storeComparison(op opcode, destination, left, right int) error {
	var result bool
	var err error
	if op == opEqual || op == opNotEqual {
		result, err = machine.equal(machine.registers[left], machine.registers[right])
		if op == opNotEqual {
			result = !result
		}
	} else {
		result, err = machine.compare(op, machine.registers[left], machine.registers[right])
	}
	if err != nil {
		return err
	}
	machine.registers[destination] = slotBool(result)
	return nil
}

func (machine *scalarMachine) equalityConstant(register, constant, scratch int) (bool, error) {
	right, err := machine.constantSlot(constant, scratch)
	if err != nil {
		return false, err
	}
	return machine.equal(machine.registers[register], right)
}

func (machine *scalarMachine) compareConstant(op opcode, register, constant, scratch int) (bool, error) {
	proto := machine.currentProto()
	if proto != nil && constant >= 0 && constant < len(proto.constants) {
		descriptor := proto.constants[constant]
		left := machine.registers[register]
		if descriptor.kind == NumberKind && slotValueKind(left) == NumberKind {
			leftNumber := 0.0
			if !slotIsTagged(left) {
				leftNumber = math.Float64frombits(uint64(left))
			} else {
				var err error
				leftNumber, err = machine.number(left)
				if err != nil {
					return false, err
				}
			}
			rightNumber := math.Float64frombits(descriptor.bits)
			if math.IsNaN(leftNumber) || math.IsNaN(rightNumber) {
				return false, fmt.Errorf("run: %s failed: compare operand is NaN", machineComparisonName(op))
			}
			switch op {
			case opJumpIfNotLessK, opJumpIfLessK:
				return leftNumber < rightNumber, nil
			case opJumpIfNotGreaterK, opJumpIfGreaterK:
				return leftNumber > rightNumber, nil
			}
		}
	}
	right, err := machine.constantSlot(constant, scratch)
	if err != nil {
		return false, err
	}
	return machine.compare(op, machine.registers[register], right)
}

func (machine *scalarMachine) numericForCheck(loop, limit, step int) (bool, error) {
	loopValue, limitValue, stepValue := machine.registers[loop], machine.registers[limit], machine.registers[step]
	if !slotIsTagged(loopValue) && !slotIsTagged(limitValue) && !slotIsTagged(stepValue) {
		return machine.numericForNumbers(
			math.Float64frombits(uint64(loopValue)),
			math.Float64frombits(uint64(limitValue)),
			math.Float64frombits(uint64(stepValue)),
		)
	}
	values := [...]struct {
		name     string
		register int
	}{
		{name: "loop value", register: loop},
		{name: "limit", register: limit},
		{name: "step", register: step},
	}
	numbers := [3]float64{}
	for index, value := range values {
		slotValue := machine.registers[value.register]
		number, err := machine.numericOperand(slotValue, "", "numeric for")
		if err != nil {
			return false, fmt.Errorf("run: numeric for %s is %s, want number", value.name, slotValueKind(slotValue))
		}
		numbers[index] = number
	}
	return machine.numericForNumbers(numbers[0], numbers[1], numbers[2])
}

func (machine *scalarMachine) numericForNumbers(loop, limit, step float64) (bool, error) {
	if math.IsNaN(loop) || math.IsNaN(limit) || math.IsNaN(step) {
		return false, fmt.Errorf("run: numeric for operand is NaN")
	}
	if step > 0 {
		return loop > limit, nil
	}
	return loop < limit, nil
}

func (machine *scalarMachine) numericForLoop(loop, step int) error {
	loopValue := machine.registers[loop]
	stepValue := machine.registers[step]
	if !slotIsTagged(loopValue) && !slotIsTagged(stepValue) {
		machine.setNumber(loop, math.Float64frombits(uint64(loopValue))+math.Float64frombits(uint64(stepValue)))
		return nil
	}
	loopNumber, err := machine.numericOperand(loopValue, "", "numeric for")
	if err != nil {
		return fmt.Errorf("run: numeric for loop value is %s, want number", slotValueKind(loopValue))
	}
	stepNumber, err := machine.numericOperand(stepValue, "", "numeric for")
	if err != nil {
		return fmt.Errorf("run: numeric for step is %s, want number", slotValueKind(stepValue))
	}
	machine.setNumber(loop, loopNumber+stepNumber)
	return nil
}

func machineTruthy(value slot) bool {
	switch slotValueKind(value) {
	case NilKind:
		return false
	case BoolKind:
		result, err := slotBoolValue(value)
		return err == nil && result
	default:
		return true
	}
}

func (machine *scalarMachine) returnValues(start, count int) error {
	if count == 0 {
		machine.resultCount = 0
		return nil
	}
	if count < 0 || count > len(machine.results) || start < 0 || start+count > len(machine.registers) {
		return fmt.Errorf("compact Machine return window is out of range")
	}
	for index := 0; index < count; index++ {
		value := machine.registers[start+index]
		cell := len(machine.registers) + index
		if slotValueKind(value) == NumberKind {
			number, err := machine.number(value)
			if err != nil {
				return err
			}
			machine.setNumber(cell, number)
		} else {
			machine.results[index] = value
		}
	}
	machine.resultCount = count
	return nil
}

func (machine *scalarMachine) exportResults() ([]Value, error) {
	if machine.resultCount == 0 {
		return nil, nil
	}
	exporter := machineTableExporter{
		machine: machine,
		tables:  make(map[machineTableID]machineExportedTable),
	}
	values := make([]Value, machine.resultCount)
	for index, value := range machine.results[:machine.resultCount] {
		exported, err := exporter.value(value)
		if err != nil {
			return nil, err
		}
		values[index] = exported
	}
	return values, nil
}

type machineExportedTable struct {
	table    *Table
	visiting bool
	complete bool
}

// machineTableExporter is a stopped-boundary adapter from scalar owner-local
// tables to detached public Values. The map is deliberately confined to this
// effect boundary; Machine execution and table storage remain map-free.
type machineTableExporter struct {
	machine   *scalarMachine
	tables    map[machineTableID]machineExportedTable
	originals map[*Table]slot
	values    map[slot]Value
	reverse   map[Value]slot
}

func (exporter *machineTableExporter) value(value slot) (Value, error) {
	if exporter == nil || exporter.machine == nil {
		return NilValue(), errors.New("compact Machine result exporter is unavailable")
	}
	machine := exporter.machine
	switch slotValueKind(value) {
	case NilKind:
		return NilValue(), nil
	case BoolKind:
		boolean, err := slotBoolValue(value)
		if err != nil {
			return NilValue(), err
		}
		return BoolValue(boolean), nil
	case NumberKind:
		number, err := machine.number(value)
		if err != nil {
			return NilValue(), err
		}
		return NumberValue(number), nil
	case StringKind:
		id, err := machine.stringID(value)
		if err != nil {
			return NilValue(), err
		}
		text, ok := machine.strings.bytesFor(id)
		if !ok {
			return NilValue(), fmt.Errorf("compact Machine result string ID %d is invalid", id)
		}
		return StringValue(string(text)), nil
	case TableKind:
		return exporter.table(value)
	case FunctionKind:
		if machine.persistentOwner == nil {
			return NilValue(), errors.New("compact Machine cannot detach a function")
		}
		handle, _, _, err := machine.closureTarget(value)
		if err != nil {
			return NilValue(), err
		}
		return transientScriptCallableValue(scriptCallableHandle{
			owner:      handle.owner,
			index:      handle.index,
			generation: uint32(handle.generation),
		})
	case HostFuncKind:
		if machine.persistentOwner == nil {
			return NilValue(), errors.New("compact Machine cannot detach a host function")
		}
		if cached, ok := exporter.values[value]; ok {
			return cached, nil
		}
		if slotTagOf(value) == slotTagNativeID {
			id, err := slotNativeIDValue(value)
			if err != nil {
				return NilValue(), err
			}
			fn, ok := nativeFuncByID(id)
			if !ok {
				return NilValue(), fmt.Errorf("compact Machine native ID %d is unsupported", id)
			}
			return nativeFuncValueWithID(fn, id), nil
		}
		fn, err := machine.persistentOwner.hosts.lookup(value)
		if err != nil {
			return NilValue(), err
		}
		exported := ContextHostFuncValue(fn)
		exporter.rememberValue(value, exported)
		return exported, nil
	case UserDataKind:
		if machine.persistentOwner != nil && slotTagOf(value) == slotTagCoroutine {
			if cached, ok := exporter.values[value]; ok {
				return cached, nil
			}
			exported, err := machine.persistentOwner.coroutines.exportValueStopped(value)
			if err != nil {
				return NilValue(), err
			}
			exporter.rememberValue(value, exported)
			return exported, nil
		}
		return NilValue(), fmt.Errorf("compact Machine result userdata is unsupported")
	default:
		return NilValue(), fmt.Errorf("compact Machine result kind %s is unsupported", slotValueKind(value))
	}
}

func (exporter *machineTableExporter) rememberValue(original slot, exported Value) {
	if exporter.values == nil {
		exporter.values = make(map[slot]Value)
	}
	if exporter.reverse == nil {
		exporter.reverse = make(map[Value]slot)
	}
	exporter.values[original] = exported
	exporter.reverse[exported] = original
}

func (exporter *machineTableExporter) table(value slot) (Value, error) {
	id, err := exporter.machine.tableID(value)
	if err != nil {
		return NilValue(), err
	}
	if existing, ok := exporter.tables[id]; ok {
		if existing.visiting || existing.complete {
			return TableValue(existing.table), nil
		}
	}
	record, ok := exporter.machine.tables.lookup(id)
	if !ok {
		return NilValue(), fmt.Errorf("compact Machine table ID %d is invalid", id)
	}
	table := newTableWithCapacity(int(record.arrayLength), int(record.fieldCount))
	if exporter.originals == nil {
		exporter.originals = make(map[*Table]slot)
	}
	exporter.originals[table] = value
	exporter.tables[id] = machineExportedTable{table: table, visiting: true}
	var cursor machineTableCursor
	for {
		key, stored, next, present, err := exporter.machine.tables.next(id, cursor)
		if err != nil {
			return NilValue(), err
		}
		if !present {
			break
		}
		exportedKey, err := exporter.key(key)
		if err != nil {
			return NilValue(), err
		}
		exportedValue, err := exporter.value(stored)
		if err != nil {
			return NilValue(), err
		}
		if err := table.rawSet(exportedKey, exportedValue); err != nil {
			return NilValue(), fmt.Errorf("compact Machine detach table %d: %w", id, err)
		}
		cursor = next
	}
	if record.metatable != invalidMachineTableID {
		metatableSlot, err := slotPackHandle(slotTagTable, uint32(record.metatable), 1)
		if err != nil {
			return NilValue(), err
		}
		exported, err := exporter.table(metatableSlot)
		if err != nil {
			return NilValue(), err
		}
		metatable, _ := exported.Table()
		table.setMetatable(metatable)
	}
	exporter.tables[id] = machineExportedTable{table: table, complete: true}
	return TableValue(table), nil
}

type machineTableReconciler struct {
	machine  *scalarMachine
	imported map[*Table]slot
	visiting map[*Table]bool
	complete map[*Table]bool
	limit    uint64
}

type machineTableReconcileCheckpoint struct {
	tables       machineTableArena
	strings      machineStringArena
	tableNumbers []uint64
	hosts        machineHostCallableArena
	originals    map[*Table]slot
	registers    []slot
	numberBits   []uint64
	openStart    int
	openCount    int
}

func captureMachineTableReconcileCheckpoint(exporter *machineTableExporter) machineTableReconcileCheckpoint {
	machine := exporter.machine
	checkpoint := machineTableReconcileCheckpoint{
		tables: machine.tables, strings: machine.strings, hosts: machine.persistentOwner.hosts,
		tableNumbers: append([]uint64(nil), machine.tableNumbers...),
		originals:    make(map[*Table]slot, len(exporter.originals)),
		registers:    append([]slot(nil), machine.registers...),
		numberBits:   append([]uint64(nil), machine.numberBits...),
		openStart:    machine.activeOpenStart,
		openCount:    machine.activeOpenCount,
	}
	checkpoint.tables.tables = append([]machineTableRecord(nil), machine.tables.tables...)
	checkpoint.tables.arrays = append([]slot(nil), machine.tables.arrays...)
	checkpoint.tables.fields = append([]machineTableField(nil), machine.tables.fields...)
	checkpoint.tables.orders = append([]machineTableOrderEntry(nil), machine.tables.orders...)
	checkpoint.strings.records = append([]machineStringRecord(nil), machine.strings.records...)
	checkpoint.strings.data = append([]byte(nil), machine.strings.data...)
	checkpoint.strings.index = append([]machineStringID(nil), machine.strings.index...)
	checkpoint.hosts.values = append([]ContextHostFunc(nil), machine.persistentOwner.hosts.values...)
	for table, value := range exporter.originals {
		checkpoint.originals[table] = value
	}
	return checkpoint
}

func (checkpoint machineTableReconcileCheckpoint) restore(exporter *machineTableExporter) {
	machine := exporter.machine
	machine.tables = checkpoint.tables
	machine.strings = checkpoint.strings
	machine.tableNumbers = checkpoint.tableNumbers
	machine.persistentOwner.hosts = checkpoint.hosts
	exporter.originals = checkpoint.originals
	machine.registers = checkpoint.registers
	machine.numberBits = checkpoint.numberBits
	machine.activeOpenStart = checkpoint.openStart
	machine.activeOpenCount = checkpoint.openCount
}

func (exporter *machineTableExporter) reconcileStopped(limit uint64) error {
	if exporter == nil || exporter.machine == nil || len(exporter.originals) == 0 {
		return nil
	}
	checkpoint := captureMachineTableReconcileCheckpoint(exporter)
	reconciler := machineTableReconciler{
		machine: exporter.machine, imported: exporter.originals,
		visiting: make(map[*Table]bool), complete: make(map[*Table]bool), limit: limit,
	}
	tables := make([]*Table, 0, len(exporter.originals))
	for table := range exporter.originals {
		tables = append(tables, table)
	}
	sort.Slice(tables, func(left, right int) bool {
		leftSlot := exporter.originals[tables[left]]
		rightSlot := exporter.originals[tables[right]]
		leftIndex, _, _ := slotValidateHandle(leftSlot, slotTagTable)
		rightIndex, _, _ := slotValidateHandle(rightSlot, slotTagTable)
		return leftIndex < rightIndex
	})
	for _, table := range tables {
		if _, err := reconciler.syncTableStopped(table); err != nil {
			checkpoint.restore(exporter)
			return err
		}
	}
	return nil
}

func (exporter *machineTableExporter) importValueStopped(value Value) (slot, error) {
	if original, ok := exporter.reverse[value]; ok {
		return original, nil
	}
	if exporter.originals == nil {
		exporter.originals = make(map[*Table]slot)
	}
	return exporter.machine.persistentOwner.importValueWithTablesStopped(value, exporter.originals)
}

func (reconciler *machineTableReconciler) valueStopped(value Value) (slot, error) {
	if value.Kind() != TableKind {
		return reconciler.machine.persistentOwner.importValueWithTablesStopped(value, reconciler.imported)
	}
	table, _ := value.Table()
	return reconciler.syncTableStopped(table)
}

func (reconciler *machineTableReconciler) syncTableStopped(table *Table) (slot, error) {
	if table == nil {
		return slotNil, fmt.Errorf("compact Machine callback returned a nil table")
	}
	value, ok := reconciler.imported[table]
	if !ok {
		id, err := reconciler.machine.tables.newTableStopped(0, 0)
		if err != nil {
			return slotNil, err
		}
		value, err = slotPackHandle(slotTagTable, uint32(id), 1)
		if err != nil {
			return slotNil, err
		}
		reconciler.imported[table] = value
	}
	if reconciler.complete[table] || reconciler.visiting[table] {
		return value, nil
	}
	reconciler.visiting[table] = true
	defer delete(reconciler.visiting, table)

	type publicEntry struct{ key, value Value }
	entries := make([]publicEntry, 0)
	key := NilValue()
	for {
		next, item, err := table.rawNext(key)
		if err != nil {
			return slotNil, err
		}
		if next.IsNil() {
			break
		}
		entries = append(entries, publicEntry{key: next, value: item})
		key = next
	}
	id, err := reconciler.machine.tableID(value)
	if err != nil {
		return slotNil, err
	}
	tableIndex, ok := reconciler.machine.tables.tableIndex(id)
	if !ok {
		return slotNil, errMachineTableInvalidID
	}
	record := &reconciler.machine.tables.tables[tableIndex]
	if record.protection != slotNil {
		record.protection = slotNil
		record.metaVersion = machineTableNextVersion(record.metaVersion)
	}
	for {
		storedKey, _, _, found, err := reconciler.machine.tables.next(id, machineTableCursor{})
		if err != nil {
			return slotNil, err
		}
		if !found {
			break
		}
		if err := reconciler.machine.tables.rawSetMetatableAwareStopped(id, storedKey, slotNil, reconciler.machine.metatableName, 0); err != nil {
			return slotNil, err
		}
	}
	for _, entry := range entries {
		importedKey, err := reconciler.valueStopped(entry.key)
		if err != nil {
			return slotNil, err
		}
		importedValue, err := reconciler.valueStopped(entry.value)
		if err != nil {
			return slotNil, err
		}
		machineKey, err := reconciler.machine.tableKey(importedKey)
		if err != nil {
			return slotNil, err
		}
		stable, err := reconciler.machine.stableTableValueStopped(importedValue)
		if err != nil {
			return slotNil, err
		}
		if err := reconciler.machine.tables.rawSetMetatableAwareStopped(id, machineKey, stable, reconciler.machine.metatableName, reconciler.limit); err != nil {
			return slotNil, err
		}
	}
	metatableID := invalidMachineTableID
	if table.metatable != nil {
		metatableValue, err := reconciler.syncTableStopped(table.metatable)
		if err != nil {
			return slotNil, err
		}
		metatableID, err = reconciler.machine.tableID(metatableValue)
		if err != nil {
			return slotNil, err
		}
	}
	record = &reconciler.machine.tables.tables[tableIndex]
	if record.metatable != metatableID {
		record.metatable = metatableID
		record.metaVersion = machineTableNextVersion(record.metaVersion)
	}
	reconciler.complete[table] = true
	return value, nil
}

func (exporter *machineTableExporter) key(key machineTableKey) (Value, error) {
	switch key.kind {
	case machineTableKeyArray:
		return NumberValue(float64(key.id)), nil
	case machineTableKeyString:
		id := machineStringID(key.id)
		text, ok := exporter.machine.strings.bytesFor(id)
		if !ok {
			return NilValue(), fmt.Errorf("compact Machine table string key ID %d is invalid", id)
		}
		return StringValue(string(text)), nil
	case machineTableKeySlot:
		return exporter.value(key.value)
	default:
		return NilValue(), errMachineTableInvalidKey
	}
}
