package ember

import "fmt"

type machineTableCallArguments struct {
	first  slot
	second slot
	third  slot
	count  uint8
	_      [7]byte
}

// machineSetMetatablePlan stages setmetatable validation before the stopped
// mutation boundary. It contains only owner-local IDs and the returned scalar.
type machineSetMetatablePlan struct {
	table     machineTableID
	metatable machineTableID
	result    slot
	ready     uint8
	_         [7]byte
}

// machineMetatableGuard captures every scalar version that can change table
// lookup semantics without retaining arena storage.
type machineMetatableGuard struct {
	table                machineTableID
	metatable            machineTableID
	tableMetaVersion     uint32
	metatableRawVersion  uint32
	metatableMetaVersion uint32
}

func (arena *machineTableArena) prepareSetMetatable(tableValue, metatableValue slot, argumentCount uint32) (machineSetMetatablePlan, error) {
	if argumentCount == 0 {
		return machineSetMetatablePlan{}, fmt.Errorf("setmetatable: argument #1 is nil, want table")
	}
	if slotValueKind(tableValue) != TableKind {
		return machineSetMetatablePlan{}, fmt.Errorf("setmetatable: argument #1 is %s, want table", slotValueKind(tableValue))
	}
	table, err := arena.tableIDFromSlot(tableValue)
	if err != nil {
		return machineSetMetatablePlan{}, err
	}
	if _, protected, err := arena.protectedMetatable(table); err != nil {
		return machineSetMetatablePlan{}, err
	} else if protected {
		return machineSetMetatablePlan{}, errMachineTableProtectedMetatable
	}

	metatable := invalidMachineTableID
	if argumentCount > 1 && metatableValue != slotNil {
		if slotValueKind(metatableValue) != TableKind {
			return machineSetMetatablePlan{}, fmt.Errorf("setmetatable: argument #2 is %s, want table or nil", slotValueKind(metatableValue))
		}
		metatable, err = arena.tableIDFromSlot(metatableValue)
		if err != nil {
			return machineSetMetatablePlan{}, err
		}
	}
	return machineSetMetatablePlan{table: table, metatable: metatable, result: tableValue, ready: 1}, nil
}

func (arena *machineTableArena) applySetMetatableStopped(plan machineSetMetatablePlan) (slot, error) {
	if plan.ready == 0 {
		return slotNil, errMachineTableInvalidID
	}
	if err := arena.setMetatableStopped(plan.table, plan.metatable); err != nil {
		return slotNil, err
	}
	return plan.result, nil
}

func (arena *machineTableArena) getMetatableValue(tableValue slot, argumentCount uint32) (slot, error) {
	if argumentCount == 0 {
		return slotNil, fmt.Errorf("getmetatable: argument #1 is nil, want table")
	}
	if slotValueKind(tableValue) != TableKind {
		return slotNil, fmt.Errorf("getmetatable: argument #1 is %s, want table", slotValueKind(tableValue))
	}
	table, err := arena.tableIDFromSlot(tableValue)
	if err != nil {
		return slotNil, err
	}
	if protection, protected, err := arena.protectedMetatable(table); err != nil {
		return slotNil, err
	} else if protected {
		return protection, nil
	}
	metatable, err := arena.metatable(table)
	if err != nil || metatable == invalidMachineTableID {
		return slotNil, err
	}
	return slotPackHandle(slotTagTable, uint32(metatable), 1)
}

func (arena *machineTableArena) jumpIfTableHasMetatable(value slot) (bool, error) {
	if slotValueKind(value) != TableKind {
		return false, nil
	}
	table, err := arena.tableIDFromSlot(value)
	if err != nil {
		return false, err
	}
	metatable, err := arena.metatable(table)
	return metatable != invalidMachineTableID, err
}

func (arena *machineTableArena) metatableGuard(table machineTableID) (machineMetatableGuard, error) {
	record, ok := arena.lookup(table)
	if !ok {
		return machineMetatableGuard{}, arena.tableError()
	}
	guard := machineMetatableGuard{
		table:            table,
		metatable:        record.metatable,
		tableMetaVersion: record.metaVersion,
	}
	if record.metatable == invalidMachineTableID {
		return guard, nil
	}
	metatable, ok := arena.lookup(record.metatable)
	if !ok {
		return machineMetatableGuard{}, errMachineTableInvalidID
	}
	guard.metatableRawVersion = metatable.rawVersion
	guard.metatableMetaVersion = metatable.metaVersion
	return guard, nil
}

func (arena *machineTableArena) metatableGuardValid(guard machineMetatableGuard) (bool, error) {
	current, err := arena.metatableGuard(guard.table)
	if err != nil {
		return false, err
	}
	return current == guard, nil
}

// rawSetMetatableAwareStopped keeps the cached __metatable scalar synchronized
// with arbitrary raw or semantic writes to a table used as a metatable.
func (arena *machineTableArena) rawSetMetatableAwareStopped(id machineTableID, key machineTableKey, value slot, protectionName machineStringID, entryLimit uint64) error {
	if err := arena.rawSetStopped(id, key, value, entryLimit); err != nil {
		return err
	}
	if key == machineTableStringKey(protectionName) {
		return arena.setMetatableProtectionStopped(id, value)
	}
	return nil
}

func machineTableActionArguments(action machineTableAction) (machineTableCallArguments, error) {
	if action.kind != machineTableActionCall {
		return machineTableCallArguments{}, errMachineTableInvalidKey
	}
	table, err := slotPackHandle(slotTagTable, uint32(action.table), 1)
	if err != nil {
		return machineTableCallArguments{}, err
	}
	key, err := machineTableKeyValue(action.key)
	if err != nil {
		return machineTableCallArguments{}, err
	}
	arguments := machineTableCallArguments{first: table, second: key, count: 2}
	if action.event == machineTableEventNewIndex {
		arguments.third = action.value
		arguments.count = 3
	} else if action.event != machineTableEventIndex {
		return machineTableCallArguments{}, errMachineTableInvalidKey
	}
	return arguments, nil
}

// resumeMachineTableAction applies the old VM's result adjustment after a
// deferred metamethod call. __index consumes exactly one result; __newindex
// ignores every result and completes without a store.
func resumeMachineTableAction(action machineTableAction, first slot, resultCount uint32) (machineTableAction, error) {
	if action.kind != machineTableActionCall {
		return machineTableAction{}, errMachineTableInvalidKey
	}
	switch action.event {
	case machineTableEventIndex:
		if resultCount == 0 {
			first = slotNil
		}
		return machineTableAction{kind: machineTableActionReturn, value: first}, nil
	case machineTableEventNewIndex:
		return machineTableAction{kind: machineTableActionReturn, value: slotNil}, nil
	default:
		return machineTableAction{}, errMachineTableInvalidKey
	}
}
