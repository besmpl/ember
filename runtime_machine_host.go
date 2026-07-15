package ember

import (
	"context"
	"errors"
	"fmt"
	"math"
)

type machineRunEffects struct {
	ctx context.Context
}

type machineHostCallableArena struct {
	owner  uint64
	values []ContextHostFunc
	closed bool
}

func (arena *machineHostCallableArena) bindStopped(owner uint64) error {
	if arena == nil || arena.closed || owner == 0 {
		return errors.New("machine host callable arena cannot bind")
	}
	arena.owner = owner
	clear(arena.values)
	arena.values = arena.values[:0]
	return nil
}

func (arena *machineHostCallableArena) importStopped(fn ContextHostFunc) (slot, error) {
	if arena == nil || arena.closed || arena.owner == 0 || fn == nil {
		return slotNil, errors.New("machine host callable is invalid")
	}
	if uint64(len(arena.values)+1) > slotIndexMask {
		return slotNil, errors.New("machine host callable arena exceeds slot capacity")
	}
	arena.values = append(arena.values, fn)
	return slotPackHandle(slotTagHostCallable, uint32(len(arena.values)), 1)
}

func (arena *machineHostCallableArena) replaceStopped(value slot, fn ContextHostFunc) error {
	index, err := arena.index(value)
	if err != nil || fn == nil {
		return errors.New("machine host callable is invalid")
	}
	arena.values[index] = fn
	return nil
}

func (arena *machineHostCallableArena) lookup(value slot) (ContextHostFunc, error) {
	index, err := arena.index(value)
	if err != nil {
		return nil, err
	}
	fn := arena.values[index]
	if fn == nil {
		return nil, errors.New("machine host callable is stale")
	}
	return fn, nil
}

func (arena *machineHostCallableArena) index(value slot) (int, error) {
	if arena == nil || arena.closed || arena.owner == 0 {
		return 0, errors.New("machine host callable arena is closed")
	}
	index, generation, err := slotValidateHandle(value, slotTagHostCallable)
	if err != nil || generation != 1 || index == 0 || uint64(index) > uint64(len(arena.values)) {
		return 0, fmt.Errorf("machine host callable handle is invalid")
	}
	return int(index - 1), nil
}

func (arena *machineHostCallableArena) close() {
	if arena == nil {
		return
	}
	arena.owner = 0
	arena.values = nil
	arena.closed = true
}

func (owner *machineOwner) importGlobalsStopped(values map[string]Value) error {
	if owner == nil || owner.image == nil {
		return errMachineOwnerInvalid
	}
	for dense, nameID := range owner.globals.names {
		nameBytes, ok := owner.strings.bytesFor(nameID)
		if !ok {
			return fmt.Errorf("import machine globals: name ID %d is stale", nameID)
		}
		value, ok := values[string(nameBytes)]
		if !ok {
			owner.globals.values[dense] = slotNil
			owner.globals.versions[dense] = 0
			owner.globals.present[dense] = 0
			continue
		}
		var imported slot
		var err error
		if fn, isHost := value.contextHostFunction(); isHost && owner.globals.present[dense] != 0 && slotTagOf(owner.globals.values[dense]) == slotTagHostCallable {
			imported = owner.globals.values[dense]
			err = owner.hosts.replaceStopped(imported, fn)
		} else {
			imported, err = owner.importValueStopped(value)
		}
		if err != nil {
			return fmt.Errorf("import machine global %q: %w", nameBytes, err)
		}
		if err := owner.globals.setAt(dense, imported); err != nil {
			return err
		}
	}
	return nil
}

func (owner *machineOwner) importValueStopped(value Value) (slot, error) {
	return owner.importValueWithTablesStopped(value, make(map[*Table]slot))
}

func (owner *machineOwner) importValueWithTablesStopped(value Value, importedTables map[*Table]slot) (slot, error) {
	switch value.Kind() {
	case NilKind:
		return slotNil, nil
	case BoolKind:
		boolean, _ := value.Bool()
		return slotBool(boolean), nil
	case NumberKind:
		number, _ := value.Number()
		bits := math.Float64bits(number)
		if bits&slotTaggedMask != slotTaggedPrefix {
			return slot(bits), nil
		}
		if uint64(len(owner.tableNumbers)+1) > slotIndexMask {
			return slotNil, errors.New("machine number arena exceeds slot capacity")
		}
		owner.tableNumbers = append(owner.tableNumbers, bits)
		return slotPackHandle(slotTagBoxedNumber, uint32(len(owner.tableNumbers)), 2)
	case StringKind:
		text, _ := value.String()
		id, err := owner.strings.internStringStopped(text)
		if err != nil {
			return slotNil, err
		}
		return slotPackHandle(slotTagString, uint32(id), 1)
	case HostFuncKind:
		fn, ok := value.contextHostFunction()
		if !ok {
			return slotNil, errors.New("host function is not a ContextHostFunc")
		}
		return owner.hosts.importStopped(fn)
	case FunctionKind:
		handle, err := decodeTransientScriptCallableValue(value, owner.closures.owner, owner.validateScriptCallableHandle)
		if err != nil {
			return slotNil, err
		}
		return slotPackHandle(slotTagClosure, handle.index, uint16(handle.generation))
	case TableKind:
		table, _ := value.Table()
		if existing, ok := importedTables[table]; ok {
			return existing, nil
		}
		id, err := owner.tables.newTableStopped(0, 0)
		if err != nil {
			return slotNil, err
		}
		result, err := slotPackHandle(slotTagTable, uint32(id), 1)
		if err != nil {
			return slotNil, err
		}
		importedTables[table] = result
		key := NilValue()
		for {
			next, item, err := table.rawNext(key)
			if err != nil {
				return slotNil, err
			}
			if next.IsNil() {
				break
			}
			importedKey, err := owner.importValueWithTablesStopped(next, importedTables)
			if err != nil {
				return slotNil, err
			}
			importedValue, err := owner.importValueWithTablesStopped(item, importedTables)
			if err != nil {
				return slotNil, err
			}
			if err := owner.setTableIndexStopped(result, importedKey, importedValue); err != nil {
				return slotNil, err
			}
			key = next
		}
		return result, nil
	default:
		return slotNil, fmt.Errorf("value kind %s is unsupported by Machine", value.Kind())
	}
}

func (owner *machineOwner) validateScriptCallableHandle(handle scriptCallableHandle) bool {
	if owner == nil || handle.owner != owner.closures.owner || handle.index == 0 || handle.generation == 0 || handle.generation > math.MaxUint16 {
		return false
	}
	_, err := owner.closures.closureRecord(machineClosureHandle{
		owner:      handle.owner,
		index:      handle.index,
		generation: uint16(handle.generation),
	})
	return err == nil
}

func (machine *scalarMachine) callHostStopped(operation machineOperation, callable slot) error {
	if machine == nil || machine.persistentOwner == nil {
		return errors.New("compact Machine host call requires a persistent owner")
	}
	fn, err := machine.persistentOwner.hosts.lookup(callable)
	if err != nil {
		return err
	}
	argStart := machine.activeBase + int(operation.callArgStart)
	argCount := int(operation.callArgCount)
	if argCount < 0 {
		openStart := argStart + int(operation.callPrefix)
		if openStart != machine.activeOpenStart || machine.activeOpenCount < 0 {
			return errors.New("compact Machine open host-call argument window is unavailable")
		}
		argCount = int(operation.callPrefix) + machine.activeOpenCount
	}
	if argStart < machine.activeBase || argCount < 0 || argStart+argCount > len(machine.registers) {
		return errors.New("compact Machine host-call argument window is out of range")
	}
	exporter := machineTableExporter{machine: machine, tables: make(map[machineTableID]machineExportedTable)}
	args := make([]Value, argCount)
	for index := range args {
		value, err := exporter.value(machine.registers[argStart+index])
		if err != nil {
			return fmt.Errorf("compact Machine host argument %d: %w", index, err)
		}
		args[index] = value
	}
	ctx := machine.effects.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if machine.window.controller != nil {
		if err := machine.window.controller.enterCall(); err != nil {
			return err
		}
		defer machine.window.controller.leaveCall()
	}
	returned, err := fn(ctx, args)
	if err != nil {
		return err
	}
	resultCount := int(operation.callResults)
	if resultCount < 0 {
		resultCount = len(returned)
		end := machine.activeBase + int(operation.a) + resultCount
		if end > len(machine.registers) {
			if err := machine.ensureStack(end); err != nil {
				return err
			}
		}
		machine.activeOpenStart = machine.activeBase + int(operation.a)
		machine.activeOpenCount = resultCount
	} else {
		machine.activeOpenStart = 0
		machine.activeOpenCount = 0
	}
	destination := machine.activeBase + int(operation.a)
	if destination < machine.activeBase || destination+resultCount > len(machine.registers) {
		return errors.New("compact Machine host-call result window is out of range")
	}
	for index := 0; index < resultCount; index++ {
		value := slotNil
		if index < len(returned) {
			value, err = machine.persistentOwner.importValueStopped(returned[index])
			if err != nil {
				return fmt.Errorf("compact Machine host result %d: %w", index, err)
			}
		}
		if err := machine.copySlot(destination+index, value); err != nil {
			return err
		}
	}
	if operation.tailCall != 0 {
		machine.skipCharge = 1
	}
	return nil
}
