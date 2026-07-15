package ember

import (
	"errors"
	"fmt"
	"math"
)

var (
	errMachineTableNilKey             = errors.New("table: key is nil")
	errMachineTableNaNKey             = errors.New("table: key is NaN")
	errMachineTableProtectedMetatable = errors.New("setmetatable: cannot change protected metatable")
	errMachineTableIndexCycle         = errors.New("table: cyclic __index chain")
	errMachineTableNewIndexCycle      = errors.New("table: cyclic __newindex chain")
)

type machineTableActionKind uint8

const (
	machineTableActionInvalid machineTableActionKind = iota
	machineTableActionReturn
	machineTableActionStore
	machineTableActionCall
)

type machineTableEvent uint8

const (
	machineTableEventInvalid machineTableEvent = iota
	machineTableEventIndex
	machineTableEventNewIndex
	machineTableEventIter
)

// machineTableAction is the pointer-free outcome of semantic table access.
// Return and store actions can be completed by the scalar kernel. Call actions
// describe the exact function invocation for an outer stopped/effect layer.
type machineTableAction struct {
	key      machineTableKey
	value    slot
	callable slot
	table    machineTableID
	kind     machineTableActionKind
	event    machineTableEvent
	_        [2]byte
}

// machineTableKeyFromScalar converts a scalar Machine value into its canonical
// raw table identity. boxedNumberBits supplies the resolved IEEE-754 payload
// when value is a boxed number handle; unboxed numbers carry their own bits.
func machineTableKeyFromScalar(value slot, boxedNumberBits uint64) (machineTableKey, error) {
	switch slotValueKind(value) {
	case NilKind:
		return machineTableKey{}, errMachineTableNilKey
	case BoolKind:
		if _, err := slotBoolValue(value); err != nil {
			return machineTableKey{}, errMachineTableInvalidKey
		}
		return machineTableSlotKey(value), nil
	case NumberKind:
		bits := uint64(value)
		if slotIsTagged(value) {
			if _, _, err := slotValidateHandle(value, slotTagBoxedNumber); err != nil {
				return machineTableKey{}, errMachineTableInvalidKey
			}
			bits = boxedNumberBits
		}
		number := math.Float64frombits(bits)
		if math.IsNaN(number) {
			return machineTableKey{}, errMachineTableNaNKey
		}
		if number == 0 {
			number = 0
		}
		if number >= 1 && number <= math.MaxUint32 && math.Trunc(number) == number {
			return machineTableArrayKey(uint32(number)), nil
		}
		return machineTableSlotKey(machineTableNumberKey(number)), nil
	case StringKind:
		index, generation, err := slotValidateHandle(value, slotTagString)
		if err != nil || generation != 1 {
			return machineTableKey{}, errMachineTableInvalidKey
		}
		return machineTableStringKey(machineStringID(index)), nil
	case TableKind:
		_, generation, err := slotValidateHandle(value, slotTagTable)
		if err != nil || generation != 1 {
			return machineTableKey{}, errMachineTableInvalidKey
		}
		return machineTableSlotKey(value), nil
	case UserDataKind:
		if slotTagOf(value) != slotTagCoroutine {
			return machineTableKey{}, fmt.Errorf("table: key is %s, want boolean, string, number, table, or coroutine", slotValueKind(value))
		}
		if _, _, err := slotValidateHandle(value, slotTagCoroutine); err != nil {
			return machineTableKey{}, errMachineTableInvalidKey
		}
		return machineTableSlotKey(value), nil
	default:
		return machineTableKey{}, fmt.Errorf("table: key is %s, want boolean, string, number, table, or coroutine", slotValueKind(value))
	}
}

func machineTableKeysEqual(left, right machineTableKey) bool {
	return left == right
}

func machineTableNextVersion(version uint32) uint32 {
	version++
	if version == 0 {
		return 1
	}
	return version
}

// rawGet performs a checked, metamethod-free lookup through one normalized
// scalar key. Missing entries are represented by slotNil.
func (arena *machineTableArena) rawGet(id machineTableID, key machineTableKey) (slot, error) {
	if _, ok := arena.lookup(id); !ok {
		return slotNil, arena.tableError()
	}
	switch key.kind {
	case machineTableKeyArray:
		if key.id == 0 {
			return slotNil, errMachineTableInvalidKey
		}
		value, _ := arena.getArray(id, key.id)
		return value, nil
	case machineTableKeySlot:
		if key.value == slotNil {
			return slotNil, errMachineTableInvalidKey
		}
		value, _ := arena.getSlot(id, key.value)
		return value, nil
	case machineTableKeyString:
		if key.id == 0 {
			return slotNil, errMachineTableInvalidKey
		}
		value, _ := arena.getString(id, machineStringID(key.id))
		return value, nil
	default:
		return slotNil, errMachineTableInvalidKey
	}
}

// rawSetStopped applies one metamethod-free mutation. A non-zero entryLimit is
// checked only for new live entries; updates and deletions remain permitted.
// The existing storage methods keep all growth and compaction at this stopped
// boundary.
func (arena *machineTableArena) rawSetStopped(id machineTableID, key machineTableKey, value slot, entryLimit uint64) error {
	record, ok := arena.lookup(id)
	if !ok {
		return arena.tableError()
	}
	current, err := arena.rawGet(id, key)
	if err != nil {
		return err
	}
	if value != slotNil && current == slotNil && entryLimit != 0 && uint64(record.entryCount) >= entryLimit {
		return &LimitError{
			Kind:  LimitTableEntriesPerTable,
			Limit: entryLimit,
			Used:  uint64(record.entryCount) + 1,
		}
	}
	switch key.kind {
	case machineTableKeyArray:
		return arena.setArrayStopped(id, key.id, value)
	case machineTableKeySlot:
		return arena.setSlotStopped(id, key.value, value)
	case machineTableKeyString:
		return arena.setStringStopped(id, machineStringID(key.id), value)
	default:
		return errMachineTableInvalidKey
	}
}

// rawLen returns the prefix before the first missing positive integer key.
// Entries beyond a hole remain stored but do not contribute to the result.
func (arena *machineTableArena) rawLen(id machineTableID) (uint32, error) {
	record, ok := arena.lookup(id)
	if !ok {
		return 0, arena.tableError()
	}
	for index := uint32(0); index < record.arrayLength; index++ {
		if arena.arrays[int(record.arrayOffset+index)] == slotNil {
			return index, nil
		}
	}
	return record.arrayLength, nil
}

func (arena *machineTableArena) tableVersions(id machineTableID) (uint32, uint32, error) {
	record, ok := arena.lookup(id)
	if !ok {
		return 0, 0, arena.tableError()
	}
	return record.rawVersion, record.metaVersion, nil
}

func (arena *machineTableArena) metatable(id machineTableID) (machineTableID, error) {
	record, ok := arena.lookup(id)
	if !ok {
		return invalidMachineTableID, arena.tableError()
	}
	return record.metatable, nil
}

// protectedMetatable reports the cached __metatable state of id's current
// metatable. The outer raw-string dispatcher is responsible for synchronizing
// that scalar state when the __metatable field changes.
func (arena *machineTableArena) protectedMetatable(id machineTableID) (slot, bool, error) {
	record, ok := arena.lookup(id)
	if !ok {
		return slotNil, false, arena.tableError()
	}
	if record.metatable == invalidMachineTableID {
		return slotNil, false, nil
	}
	metatable, ok := arena.lookup(record.metatable)
	if !ok {
		return slotNil, false, errMachineTableInvalidID
	}
	return metatable.protection, metatable.protection != slotNil, nil
}

func (arena *machineTableArena) setMetatableStopped(id, metatable machineTableID) error {
	tableIndex, ok := arena.tableIndex(id)
	if !ok {
		return arena.tableError()
	}
	if metatable != invalidMachineTableID {
		if _, ok := arena.lookup(metatable); !ok {
			return errMachineTableInvalidID
		}
	}
	if _, protected, err := arena.protectedMetatable(id); err != nil {
		return err
	} else if protected {
		return errMachineTableProtectedMetatable
	}
	record := &arena.tables[tableIndex]
	if record.metatable == metatable {
		return nil
	}
	record.metatable = metatable
	record.metaVersion = machineTableNextVersion(record.metaVersion)
	return nil
}

func (arena *machineTableArena) setMetatableProtectionStopped(id machineTableID, protection slot) error {
	tableIndex, ok := arena.tableIndex(id)
	if !ok {
		return arena.tableError()
	}
	record := &arena.tables[tableIndex]
	if record.protection == protection {
		return nil
	}
	record.protection = protection
	record.metaVersion = machineTableNextVersion(record.metaVersion)
	return nil
}

// decideIndex follows only scalar table handlers. It performs no mutation and
// never invokes guest or host code; a callable handler is returned as an
// explicit action for the outer execution layer.
func (arena *machineTableArena) decideIndex(id machineTableID, key machineTableKey, indexName machineStringID) (machineTableAction, error) {
	if indexName == invalidMachineStringID {
		return machineTableAction{}, errMachineTableInvalidKey
	}
	current := id
	for followed := 0; ; {
		value, err := arena.rawGet(current, key)
		if err != nil {
			return machineTableAction{}, err
		}
		if value != slotNil {
			return machineTableAction{kind: machineTableActionReturn, value: value}, nil
		}
		handler, found, err := arena.metamethod(current, indexName)
		if err != nil {
			return machineTableAction{}, err
		}
		if !found {
			return machineTableAction{kind: machineTableActionReturn, value: slotNil}, nil
		}
		if slotValueKind(handler) == TableKind {
			next, err := arena.tableIDFromSlot(handler)
			if err != nil {
				return machineTableAction{}, err
			}
			if followed >= len(arena.tables) {
				return machineTableAction{}, errMachineTableIndexCycle
			}
			followed++
			current = next
			continue
		}
		if machineTableCallable(handler) {
			return machineTableAction{
				kind:     machineTableActionCall,
				event:    machineTableEventIndex,
				table:    current,
				key:      key,
				value:    slotNil,
				callable: handler,
			}, nil
		}
		return machineTableAction{}, fmt.Errorf("table: __index is %s, want table or function", slotValueKind(handler))
	}
}

// decideNewIndex mirrors decideIndex but returns the terminal raw store rather
// than applying it. This keeps limit accounting and stopped growth explicit at
// the caller's chosen mutation boundary.
func (arena *machineTableArena) decideNewIndex(id machineTableID, key machineTableKey, value slot, newIndexName machineStringID) (machineTableAction, error) {
	if newIndexName == invalidMachineStringID {
		return machineTableAction{}, errMachineTableInvalidKey
	}
	current := id
	for followed := 0; ; {
		stored, err := arena.rawGet(current, key)
		if err != nil {
			return machineTableAction{}, err
		}
		if stored != slotNil {
			return machineTableAction{kind: machineTableActionStore, table: current, key: key, value: value}, nil
		}
		handler, found, err := arena.metamethod(current, newIndexName)
		if err != nil {
			return machineTableAction{}, err
		}
		if !found {
			return machineTableAction{kind: machineTableActionStore, table: current, key: key, value: value}, nil
		}
		if slotValueKind(handler) == TableKind {
			next, err := arena.tableIDFromSlot(handler)
			if err != nil {
				return machineTableAction{}, err
			}
			if followed >= len(arena.tables) {
				return machineTableAction{}, errMachineTableNewIndexCycle
			}
			followed++
			current = next
			continue
		}
		if machineTableCallable(handler) {
			return machineTableAction{
				kind:     machineTableActionCall,
				event:    machineTableEventNewIndex,
				table:    current,
				key:      key,
				value:    value,
				callable: handler,
			}, nil
		}
		return machineTableAction{}, fmt.Errorf("table: __newindex is %s, want table or function", slotValueKind(handler))
	}
}

func (arena *machineTableArena) metamethod(id machineTableID, name machineStringID) (slot, bool, error) {
	record, ok := arena.lookup(id)
	if !ok {
		return slotNil, false, arena.tableError()
	}
	if record.metatable == invalidMachineTableID {
		return slotNil, false, nil
	}
	value, err := arena.rawGet(record.metatable, machineTableStringKey(name))
	if err != nil {
		return slotNil, false, err
	}
	return value, value != slotNil, nil
}

func (arena *machineTableArena) tableIDFromSlot(value slot) (machineTableID, error) {
	index, generation, err := slotValidateHandle(value, slotTagTable)
	if err != nil || generation != 1 {
		return invalidMachineTableID, errMachineTableInvalidID
	}
	id := machineTableID(index)
	if _, ok := arena.lookup(id); !ok {
		return invalidMachineTableID, errMachineTableInvalidID
	}
	return id, nil
}

func machineTableCallable(value slot) bool {
	switch slotValueKind(value) {
	case FunctionKind:
		_, _, err := slotValidateHandle(value, slotTagClosure)
		return err == nil
	case HostFuncKind:
		switch slotTagOf(value) {
		case slotTagHostCallable:
			_, _, err := slotValidateHandle(value, slotTagHostCallable)
			return err == nil
		case slotTagNativeID:
			_, err := slotNativeIDValue(value)
			return err == nil
		}
	}
	return false
}
