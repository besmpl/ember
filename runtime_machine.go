package ember

import (
	"errors"
	"fmt"
	"math"
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
	if err := machine.bindImageStrings(image); err != nil {
		releaseScalarMachine(machine)
		return nil, err
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

func (machine *scalarMachine) setConstantFieldStopped(tableRegister, constant, valueRegister int) error {
	if machine == nil || tableRegister < 0 || tableRegister >= len(machine.registers) ||
		valueRegister < 0 || valueRegister >= len(machine.registers) {
		return fmt.Errorf("compact Machine SET_FIELD operands are out of range")
	}
	key, err := machine.constantSlot(constant, tableRegister)
	if err != nil {
		return err
	}
	return machine.setTableIndexStopped(machine.registers[tableRegister], key, machine.registers[valueRegister])
}

func (machine *scalarMachine) setStringFieldStopped(tableRegister, constant, valueRegister int) error {
	if machine == nil || tableRegister < 0 || tableRegister >= len(machine.registers) ||
		valueRegister < 0 || valueRegister >= len(machine.registers) {
		return fmt.Errorf("compact Machine SET_STRING_FIELD operands are out of range")
	}
	if _, err := machine.tableID(machine.registers[tableRegister]); err != nil {
		return fmt.Errorf("run: set field target is %s, want table", slotValueKind(machine.registers[tableRegister]))
	}
	return machine.setConstantFieldStopped(tableRegister, constant, valueRegister)
}

func (machine *scalarMachine) setIndexStopped(tableRegister, keyRegister, valueRegister int) error {
	if machine == nil || tableRegister < 0 || tableRegister >= len(machine.registers) ||
		keyRegister < 0 || keyRegister >= len(machine.registers) ||
		valueRegister < 0 || valueRegister >= len(machine.registers) {
		return fmt.Errorf("compact Machine SET_INDEX operands are out of range")
	}
	return machine.setTableIndexStopped(
		machine.registers[tableRegister],
		machine.registers[keyRegister],
		machine.registers[valueRegister],
	)
}

func (machine *scalarMachine) getIndex(destination, tableRegister, keyRegister int) error {
	if machine == nil || destination < 0 || destination >= len(machine.registers) ||
		tableRegister < 0 || tableRegister >= len(machine.registers) ||
		keyRegister < 0 || keyRegister >= len(machine.registers) {
		return fmt.Errorf("compact Machine GET_INDEX operands are out of range")
	}
	return machine.getTableIndex(destination, machine.registers[tableRegister], machine.registers[keyRegister])
}

func (machine *scalarMachine) getStringField(destination, tableRegister, constant int) error {
	if machine == nil || destination < 0 || destination >= len(machine.registers) ||
		tableRegister < 0 || tableRegister >= len(machine.registers) {
		return fmt.Errorf("compact Machine GET_STRING_FIELD operands are out of range")
	}
	if _, err := machine.tableID(machine.registers[tableRegister]); err != nil {
		return fmt.Errorf("run: get field target is %s, want table", slotValueKind(machine.registers[tableRegister]))
	}
	key, err := machine.constantSlot(constant, destination)
	if err != nil {
		return err
	}
	return machine.getTableIndex(destination, machine.registers[tableRegister], key)
}

func (machine *scalarMachine) getTableIndex(destination int, tableValue, keyValue slot) error {
	id, err := machine.tableID(tableValue)
	if err != nil {
		return fmt.Errorf("run: get index target is %s, want table", slotValueKind(tableValue))
	}
	key, err := machine.tableKey(keyValue)
	if err != nil {
		return fmt.Errorf("run: get index failed: %w", err)
	}
	value, ok := machine.tableValue(id, key)
	if !ok {
		machine.registers[destination] = slotNil
		return nil
	}
	return machine.copySlot(destination, value)
}

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
	if key.kind == machineTableKeyArray {
		recordKey := machineTableArrayRecordKey(key.id)
		if _, exists := machine.tables.getSlot(id, recordKey.value); exists {
			return machine.tables.setSlotStopped(id, recordKey.value, stable)
		}
		record, ok := machine.tables.lookup(id)
		if !ok {
			return errMachineTableInvalidID
		}
		if _, exists := machine.tables.getArray(id, key.id); exists || key.id <= record.arrayCapacity || key.id <= record.arrayLength+1 {
			return machine.tables.setArrayStopped(id, key.id, stable)
		}
		return machine.tables.setSlotStopped(id, recordKey.value, stable)
	}
	switch key.kind {
	case machineTableKeySlot:
		return machine.tables.setSlotStopped(id, key.value, stable)
	case machineTableKeyString:
		return machine.tables.setStringStopped(id, machineStringID(key.id), stable)
	default:
		return errMachineTableInvalidKey
	}
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
	if slotTagOf(callable) == slotTagHostCallable {
		if err := machine.callHostStopped(operation, callable); err != nil {
			return 0, 0, err
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
	argStart := callerBase + int(operation.callArgStart)
	argCount := int(operation.callArgCount)
	if argCount < 0 {
		openStart := argStart + int(operation.callPrefix)
		if openStart != machine.activeOpenStart || machine.activeOpenCount < 0 {
			return 0, 0, fmt.Errorf("compact Machine open call argument window is unavailable")
		}
		argCount = int(operation.callPrefix) + machine.activeOpenCount
	}
	if argStart < callerBase || argCount < 0 || argStart+argCount > len(machine.registers) {
		return 0, 0, fmt.Errorf("compact Machine call argument window is out of range")
	}
	calleeBase := len(machine.registers)
	varargCount := 0
	if target.variadic && argCount > target.params {
		varargCount = argCount - target.params
	}
	if operation.tailCall != 0 {
		return machine.enterTailCall(targetModule, targetID, closure, target, argStart, argCount, varargCount)
	}
	if err := machine.ensureStack(calleeBase + target.registers + varargCount); err != nil {
		return 0, 0, err
	}
	for index := calleeBase; index < calleeBase+target.registers; index++ {
		machine.registers[index] = slotNil
	}
	for index := 0; index < target.params && index < argCount; index++ {
		if err := machine.copySlot(calleeBase+index, machine.registers[argStart+index]); err != nil {
			return 0, 0, err
		}
	}
	varargStart := calleeBase + target.registers
	for index := 0; index < varargCount; index++ {
		if err := machine.copySlot(varargStart+index, machine.registers[argStart+target.params+index]); err != nil {
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
		returnPC:    int32(nextPC),
		returnBase:  int32(callerBase),
		returnReg:   int32(callerBase + int(operation.a)),
		returnCount: operation.callResults,
		stackLength: int32(calleeBase),
		closure:     machine.activeClosure,
		varargStart: int32(machine.activeVarargStart),
		varargCount: int32(machine.activeVarargCount),
		cellStart:   int32(machine.activeCellStart),
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

func (machine *scalarMachine) enterTailCall(targetModule programModuleID, targetID int32, closure machineClosureHandle, target *machineProto, argStart, argCount, varargCount int) (int32, int, error) {
	machine.transfer = machine.transfer[:0]
	for index := 0; index < argCount; index++ {
		value := machine.registers[argStart+index]
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
	for index := 0; index < target.params && index < argCount; index++ {
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
	case NumberKind, StringKind, TableKind:
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
	return machine.binary(op, destination, machine.registers[left], machine.registers[right])
}

func (machine *scalarMachine) binaryConstant(op opcode, destination, left, constant int) error {
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
	if slotValueKind(value) == NumberKind {
		return machine.number(value)
	}
	return 0, fmt.Errorf("%s %s operand is %s, want number", operator, side, slotValueKind(value))
}

func (machine *scalarMachine) negate(destination, operand int) error {
	value := machine.registers[operand]
	if slotValueKind(value) != NumberKind {
		return fmt.Errorf("run: negate operand is %s, want number", slotValueKind(value))
	}
	number, err := machine.number(value)
	if err != nil {
		return err
	}
	machine.setNumber(destination, -number)
	return nil
}

func (machine *scalarMachine) equal(left, right slot) (bool, error) {
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
	default:
		return false, fmt.Errorf("compact Machine equality kind %s is unsupported", leftKind)
	}
}

func (machine *scalarMachine) compare(op opcode, left, right slot) (bool, error) {
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
	if math.IsNaN(leftNumber) || math.IsNaN(rightNumber) {
		return false, fmt.Errorf("run: %s failed: compare operand is NaN", prefix)
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
			leftNumber, err := machine.number(left)
			if err != nil {
				return false, err
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
		if slotValueKind(slotValue) != NumberKind {
			return false, fmt.Errorf("run: numeric for %s is %s, want number", value.name, slotValueKind(slotValue))
		}
		number, err := machine.number(slotValue)
		if err != nil {
			return false, err
		}
		numbers[index] = number
	}
	if math.IsNaN(numbers[0]) || math.IsNaN(numbers[1]) || math.IsNaN(numbers[2]) {
		return false, fmt.Errorf("run: numeric for operand is NaN")
	}
	if numbers[2] > 0 {
		return numbers[0] > numbers[1], nil
	}
	return numbers[0] < numbers[1], nil
}

func (machine *scalarMachine) numericForLoop(loop, step int) error {
	loopValue := machine.registers[loop]
	if slotValueKind(loopValue) != NumberKind {
		return fmt.Errorf("run: numeric for loop value is %s, want number", slotValueKind(loopValue))
	}
	stepValue := machine.registers[step]
	if slotValueKind(stepValue) != NumberKind {
		return fmt.Errorf("run: numeric for step is %s, want number", slotValueKind(stepValue))
	}
	loopNumber, err := machine.number(loopValue)
	if err != nil {
		return err
	}
	stepNumber, err := machine.number(stepValue)
	if err != nil {
		return err
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
	machine *scalarMachine
	tables  map[machineTableID]machineExportedTable
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
		fn, err := machine.persistentOwner.hosts.lookup(value)
		if err != nil {
			return NilValue(), err
		}
		return ContextHostFuncValue(fn), nil
	default:
		return NilValue(), fmt.Errorf("compact Machine result kind %s is unsupported", slotValueKind(value))
	}
}

func (exporter *machineTableExporter) table(value slot) (Value, error) {
	id, err := exporter.machine.tableID(value)
	if err != nil {
		return NilValue(), err
	}
	if existing, ok := exporter.tables[id]; ok {
		if existing.visiting {
			return NilValue(), fmt.Errorf("compact Machine cannot export cyclic table %d", id)
		}
		if existing.complete {
			return TableValue(existing.table), nil
		}
	}
	record, ok := exporter.machine.tables.lookup(id)
	if !ok {
		return NilValue(), fmt.Errorf("compact Machine table ID %d is invalid", id)
	}
	table := newTableWithCapacity(int(record.arrayLength), int(record.fieldCount))
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
	exporter.tables[id] = machineExportedTable{table: table, complete: true}
	return TableValue(table), nil
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
