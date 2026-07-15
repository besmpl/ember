package ember

import (
	"fmt"
	"math"
)

// machineIntrinsicRequest is the stopped-boundary input for one guarded
// FAST_CALL. Dispatch resolves the current callee before entering this helper;
// only the exact native immediate may take the intrinsic path.
//
// tableEntryLimit is copied from the active execution policy. Keeping the
// limit explicit avoids hiding controller access inside scalar table logic.
type machineIntrinsicRequest struct {
	nativeID          nativeFuncID
	callee            slot
	args              []slot
	selectVarargCount int
	tableEntryLimit   uint64
}

// machineIntrinsicOutcome carries raw Luau call results. Dispatch remains the
// owner of fixed-result adjustment and open-result publication. The helper
// adds no guest instruction charge: the generated loop has already charged
// the FAST_CALL operation's metadata before reaching this stopped boundary.
type machineIntrinsicOutcome struct {
	value                 slot
	resultCount           uint8
	additionalGuestCharge uint8
	matched               bool
}

// runMachineGuardedIntrinsicStopped executes the bounded intrinsic families
// needed by the Machine corpus. A guard miss has no effects and asks dispatch
// to use the ordinary call path.
func runMachineGuardedIntrinsicStopped(machine *scalarMachine, request machineIntrinsicRequest) (machineIntrinsicOutcome, error) {
	if request.callee != slotNativeID(request.nativeID) {
		return machineIntrinsicOutcome{}, nil
	}
	outcome := machineIntrinsicOutcome{matched: true, value: slotNil}
	if machine == nil {
		return outcome, fmt.Errorf("compact Machine guarded intrinsic is unavailable")
	}

	switch request.nativeID {
	case nativeFuncMathMin:
		value, err := machineMathMinIntrinsic(machine, request.args)
		outcome.value = value
		outcome.resultCount = 1
		return outcome, err
	case nativeFuncRawLen:
		value, err := machineRawLenIntrinsic(machine, request.args)
		outcome.value = value
		outcome.resultCount = 1
		return outcome, err
	case nativeFuncSelect:
		if request.selectVarargCount < 0 {
			return outcome, fmt.Errorf("compact Machine select vararg count is negative")
		}
		outcome.value = machineIntrinsicNumber(machine, float64(request.selectVarargCount))
		outcome.resultCount = 1
		return outcome, nil
	case nativeFuncTableInsert:
		return outcome, machineTableInsertIntrinsic(machine, request.args, request.tableEntryLimit)
	case nativeFuncTableRemove:
		value, err := machineTableRemoveIntrinsic(machine, request.args, request.tableEntryLimit)
		outcome.value = value
		outcome.resultCount = 1
		return outcome, err
	default:
		return outcome, fmt.Errorf("compact Machine guarded intrinsic ID %d is unsupported", request.nativeID)
	}
}

func machineMathMinIntrinsic(machine *scalarMachine, args []slot) (slot, error) {
	if len(args) == 0 {
		return slotNil, fmt.Errorf("math.min: argument #1 is nil, want number")
	}
	minimum, err := machineIntrinsicNumberArg(machine, "math.min", args, 0)
	if err != nil {
		return slotNil, err
	}
	for index := 1; index < len(args); index++ {
		value, err := machineIntrinsicNumberArg(machine, "math.min", args, index)
		if err != nil {
			return slotNil, err
		}
		minimum = math.Min(minimum, value)
	}
	return machineIntrinsicNumber(machine, minimum), nil
}

func machineRawLenIntrinsic(machine *scalarMachine, args []slot) (slot, error) {
	value := slotNil
	if len(args) != 0 {
		value = args[0]
	}
	var length uint32
	switch slotValueKind(value) {
	case StringKind:
		id, err := machine.stringID(value)
		if err != nil {
			return slotNil, fmt.Errorf("rawlen: %w", err)
		}
		record, ok := machine.strings.lookup(id)
		if !ok {
			return slotNil, fmt.Errorf("rawlen: compact Machine string ID %d is invalid", id)
		}
		length = record.length
	case TableKind:
		id, err := machine.tableID(value)
		if err != nil {
			return slotNil, fmt.Errorf("rawlen: %w", err)
		}
		length, err = machine.tables.rawLen(id)
		if err != nil {
			return slotNil, fmt.Errorf("rawlen: %w", err)
		}
	default:
		return slotNil, fmt.Errorf("rawlen: length operand is %s, want string or table", slotValueKind(value))
	}
	return machineIntrinsicNumber(machine, float64(length)), nil
}

func machineTableInsertIntrinsic(machine *scalarMachine, args []slot, entryLimit uint64) error {
	table, err := machineIntrinsicTableArg(machine, "table.insert", args, 0)
	if err != nil {
		return err
	}
	if len(args) < 2 {
		return fmt.Errorf("table.insert: argument #2 is nil, want value")
	}
	length, err := machine.tables.rawLen(table)
	if err != nil {
		return fmt.Errorf("table.insert: %w", err)
	}
	position := int(length) + 1
	value := args[1]
	if len(args) > 2 {
		position, err = machineIntrinsicIntegerArg(machine, "table.insert", args, 1)
		if err != nil {
			return err
		}
		value = args[2]
	}
	if position < 1 || uint64(position) > uint64(length)+1 {
		return fmt.Errorf("table.insert: position %d out of range", position)
	}
	stable, err := machine.stableTableValueStopped(value)
	if err != nil {
		return fmt.Errorf("table.insert: %w", err)
	}
	if err := machineIntrinsicCheckEntryLimit(machine, table, machineTableArrayKey(length+1), stable, entryLimit); err != nil {
		return err
	}
	for index := int(length); index >= position; index-- {
		shifted, err := machine.tables.rawGet(table, machineTableArrayKey(uint32(index)))
		if err != nil {
			return fmt.Errorf("table.insert: %w", err)
		}
		if err := machine.tables.rawSetStopped(table, machineTableArrayKey(uint32(index+1)), shifted, 0); err != nil {
			return fmt.Errorf("table.insert: %w", err)
		}
	}
	if err := machine.tables.rawSetStopped(table, machineTableArrayKey(uint32(position)), stable, 0); err != nil {
		return fmt.Errorf("table.insert: %w", err)
	}
	return nil
}

func machineIntrinsicCheckEntryLimit(machine *scalarMachine, table machineTableID, key machineTableKey, value slot, limit uint64) error {
	if limit == 0 || value == slotNil {
		return nil
	}
	current, err := machine.tables.rawGet(table, key)
	if err != nil {
		return fmt.Errorf("table.insert: %w", err)
	}
	if current != slotNil {
		return nil
	}
	record, ok := machine.tables.lookup(table)
	if !ok {
		return fmt.Errorf("table.insert: %w", errMachineTableInvalidID)
	}
	if uint64(record.entryCount) < limit {
		return nil
	}
	return &LimitError{Kind: LimitTableEntriesPerTable, Limit: limit, Used: uint64(record.entryCount) + 1}
}

func machineTableRemoveIntrinsic(machine *scalarMachine, args []slot, entryLimit uint64) (slot, error) {
	table, err := machineIntrinsicTableArg(machine, "table.remove", args, 0)
	if err != nil {
		return slotNil, err
	}
	length, err := machine.tables.rawLen(table)
	if err != nil {
		return slotNil, fmt.Errorf("table.remove: %w", err)
	}
	if length == 0 {
		return slotNil, nil
	}
	position := int(length)
	if len(args) > 1 && args[1] != slotNil {
		position, err = machineIntrinsicIntegerArg(machine, "table.remove", args, 1)
		if err != nil {
			return slotNil, err
		}
	}
	if position < 1 || uint64(position) > uint64(length) {
		return slotNil, nil
	}
	removed, err := machine.tables.rawGet(table, machineTableArrayKey(uint32(position)))
	if err != nil {
		return slotNil, fmt.Errorf("table.remove: %w", err)
	}
	for index := position; uint64(index) < uint64(length); index++ {
		shifted, err := machine.tables.rawGet(table, machineTableArrayKey(uint32(index+1)))
		if err != nil {
			return slotNil, fmt.Errorf("table.remove: %w", err)
		}
		if err := machine.tables.rawSetStopped(table, machineTableArrayKey(uint32(index)), shifted, entryLimit); err != nil {
			return slotNil, fmt.Errorf("table.remove: %w", err)
		}
	}
	if err := machine.tables.rawSetStopped(table, machineTableArrayKey(length), slotNil, entryLimit); err != nil {
		return slotNil, fmt.Errorf("table.remove: %w", err)
	}
	return removed, nil
}

func machineIntrinsicTableArg(machine *scalarMachine, name string, args []slot, index int) (machineTableID, error) {
	if index >= len(args) {
		return invalidMachineTableID, fmt.Errorf("%s: argument #%d is nil, want table", name, index+1)
	}
	if slotValueKind(args[index]) != TableKind {
		return invalidMachineTableID, fmt.Errorf("%s: argument #%d is %s, want table", name, index+1, slotValueKind(args[index]))
	}
	table, err := machine.tableID(args[index])
	if err != nil {
		return invalidMachineTableID, fmt.Errorf("%s: argument #%d is table, want table", name, index+1)
	}
	return table, nil
}

func machineIntrinsicNumberArg(machine *scalarMachine, name string, args []slot, index int) (float64, error) {
	if index >= len(args) {
		return 0, fmt.Errorf("%s: argument #%d is nil, want number", name, index+1)
	}
	if slotValueKind(args[index]) != NumberKind {
		return 0, fmt.Errorf("%s: argument #%d is %s, want number", name, index+1, slotValueKind(args[index]))
	}
	value, err := machine.number(args[index])
	if err != nil {
		return 0, fmt.Errorf("%s: argument #%d is number, want number", name, index+1)
	}
	return value, nil
}

func machineIntrinsicIntegerArg(machine *scalarMachine, name string, args []slot, index int) (int, error) {
	value, err := machineIntrinsicNumberArg(machine, name, args, index)
	if err != nil {
		return 0, err
	}
	if value != math.Trunc(value) {
		return 0, fmt.Errorf("%s: argument #%d is %v, want integer", name, index+1, value)
	}
	return int(value), nil
}

func machineIntrinsicNumber(machine *scalarMachine, value float64) slot {
	cell := len(machine.registers) + len(machine.results)
	machine.setNumber(cell, value)
	return machine.scratch
}
