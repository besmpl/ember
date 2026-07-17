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

type machineHostFunc func(context.Context, []Value) HostResult

func reconcileMachineExportedTablesStopped(machine *scalarMachine, exporter *machineTableExporter) error {
	limit := uint64(0)
	if machine != nil && machine.window.controller != nil {
		limit = machine.window.controller.limits.MaxTableEntriesPerTable
	}
	if err := exporter.reconcileStopped(limit); err != nil {
		return fmt.Errorf("compact Machine reconcile callback tables: %w", err)
	}
	return nil
}

type machineHostCallableArena struct {
	owner  uint64
	values []machineHostFunc
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

func (arena *machineHostCallableArena) importStopped(fn machineHostFunc) (slot, error) {
	if arena == nil || arena.closed || arena.owner == 0 || fn == nil {
		return slotNil, errors.New("machine host callable is invalid")
	}
	if uint64(len(arena.values)+1) > slotIndexMask {
		return slotNil, errors.New("machine host callable arena exceeds slot capacity")
	}
	arena.values = append(arena.values, fn)
	return slotPackHandle(slotTagHostCallable, uint32(len(arena.values)), 1)
}

func (arena *machineHostCallableArena) replaceStopped(value slot, fn machineHostFunc) error {
	index, err := arena.index(value)
	if err != nil || fn == nil {
		return errors.New("machine host callable is invalid")
	}
	arena.values[index] = fn
	return nil
}

func (arena *machineHostCallableArena) lookup(value slot) (machineHostFunc, error) {
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
	if len(values) == 0 {
		return owner.restoreBaseGlobalsStopped()
	}
	for dense, nameID := range owner.globals.names {
		nameBytes, ok := owner.strings.bytesFor(nameID)
		if !ok {
			return fmt.Errorf("import machine globals: name ID %d is stale", nameID)
		}
		value, ok := values[string(nameBytes)]
		if !ok {
			if err := owner.restoreBaseGlobalAtStopped(dense); err != nil {
				return fmt.Errorf("restore machine base global %q: %w", nameBytes, err)
			}
			continue
		}
		var imported slot
		var err error
		if fn, isHost := machineHostFunction(value, owner); isHost && owner.globals.present[dense] != 0 && slotTagOf(owner.globals.values[dense]) == slotTagHostCallable {
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
	if value.Kind() == TableKind {
		return owner.importValueWithTablesStopped(value, make(map[*Table]slot))
	}
	return owner.importValueWithTablesStopped(value, nil)
}

func (owner *machineOwner) bindBaseGlobalsStopped() error {
	if owner == nil || owner.image == nil {
		return errMachineOwnerInvalid
	}
	owner.baseGlobals = make([]slot, len(owner.globals.names))
	owner.basePresent = make([]uint8, len(owner.globals.names))
	owner.baseTableIndexes = make([]uint32, len(owner.globals.names))
	for dense, nameID := range owner.globals.names {
		nameBytes, ok := owner.strings.bytesFor(nameID)
		if !ok {
			return fmt.Errorf("bind machine base globals: name ID %d is stale", nameID)
		}
		value, ok, err := machineBaseGlobalValue(string(nameBytes))
		if err != nil {
			return fmt.Errorf("bind machine base global %q: %w", nameBytes, err)
		}
		if !ok {
			continue
		}
		imported, err := owner.importValueStopped(value)
		if err != nil {
			return fmt.Errorf("bind machine base global %q: %w", nameBytes, err)
		}
		owner.baseGlobals[dense] = imported
		owner.basePresent[dense] = 1
		if slotValueKind(imported) == TableKind {
			snapshot, err := owner.captureBaseTableStopped(imported)
			if err != nil {
				return fmt.Errorf("bind machine base global %q table: %w", nameBytes, err)
			}
			owner.baseTables = append(owner.baseTables, snapshot)
			owner.baseTableIndexes[dense] = uint32(len(owner.baseTables))
		}
	}
	return owner.restoreBaseGlobalsStopped()
}

func (owner *machineOwner) restoreBaseGlobalsStopped() error {
	if owner == nil || len(owner.baseGlobals) != len(owner.globals.values) || len(owner.basePresent) != len(owner.globals.present) {
		return errMachineOwnerInvalid
	}
	for dense := range owner.globals.values {
		if err := owner.restoreBaseGlobalAtStopped(dense); err != nil {
			return err
		}
	}
	return nil
}

func (owner *machineOwner) restoreBaseGlobalAtStopped(dense int) error {
	if dense < 0 || dense >= len(owner.baseGlobals) || dense >= len(owner.baseTableIndexes) {
		return errMachineOwnerInvalid
	}
	if snapshotIndex := owner.baseTableIndexes[dense]; snapshotIndex != 0 {
		if int(snapshotIndex) > len(owner.baseTables) {
			return errMachineOwnerInvalid
		}
		if err := owner.restoreBaseTableStopped(owner.baseTables[snapshotIndex-1]); err != nil {
			return err
		}
	}
	owner.globals.epoch = nextMachineGlobalVersion(owner.globals.epoch)
	owner.globals.values[dense] = owner.baseGlobals[dense]
	owner.globals.versions[dense] = owner.globals.epoch
	owner.globals.present[dense] = owner.basePresent[dense]
	return nil
}

func (owner *machineOwner) captureBaseTableStopped(value slot) (machineBaseTableSnapshot, error) {
	id, err := owner.tableID(value)
	if err != nil {
		return machineBaseTableSnapshot{}, err
	}
	record, ok := owner.tables.lookup(id)
	if !ok {
		return machineBaseTableSnapshot{}, errMachineTableInvalidID
	}
	snapshot := machineBaseTableSnapshot{id: id, metatable: record.metatable, protection: record.protection}
	var cursor machineTableCursor
	for {
		key, value, next, found, err := owner.tables.next(id, cursor)
		if err != nil {
			return machineBaseTableSnapshot{}, err
		}
		if !found {
			break
		}
		snapshot.entries = append(snapshot.entries, machineBaseTableEntry{key: key, value: value})
		cursor = next
	}
	return snapshot, nil
}

func (owner *machineOwner) restoreBaseTableStopped(snapshot machineBaseTableSnapshot) error {
	for {
		key, _, _, found, err := owner.tables.next(snapshot.id, machineTableCursor{})
		if err != nil {
			return err
		}
		if !found {
			break
		}
		if err := owner.tables.rawSetStopped(snapshot.id, key, slotNil, 0); err != nil {
			return err
		}
	}
	for _, entry := range snapshot.entries {
		if err := owner.tables.rawSetStopped(snapshot.id, entry.key, entry.value, 0); err != nil {
			return err
		}
	}
	tableIndex, ok := owner.tables.tableIndex(snapshot.id)
	if !ok {
		return errMachineTableInvalidID
	}
	owner.tables.tables[tableIndex].metatable = snapshot.metatable
	owner.tables.tables[tableIndex].protection = snapshot.protection
	owner.tables.tables[tableIndex].metaVersion = machineTableNextVersion(owner.tables.tables[tableIndex].metaVersion)
	return nil
}

func (owner *machineOwner) importValueWithTablesStopped(value Value, importedTables map[*Table]slot) (slot, error) {
	if value.Kind() == UserDataKind && value.bits == uint64(UserDataKind)|valueMachineCoroutineTag {
		return owner.coroutines.importValueStopped(value)
	}
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
		if nativeID := valueNativeID(value); nativeID != nativeFuncUnknown {
			return slotNativeID(nativeID), nil
		}
		fn, ok := machineHostFunction(value, owner)
		if !ok {
			return slotNil, errors.New("host function is not supported by compact Machine")
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
		if table.metatable != nil {
			importedMetatable, err := owner.importValueWithTablesStopped(TableValue(table.metatable), importedTables)
			if err != nil {
				return slotNil, err
			}
			metatableID, err := owner.tableID(importedMetatable)
			if err != nil {
				return slotNil, err
			}
			if err := owner.tables.setMetatableStopped(id, metatableID); err != nil {
				return slotNil, err
			}
		}
		return result, nil
	default:
		return slotNil, fmt.Errorf("value kind %s is unsupported by Machine", value.Kind())
	}
}

func machineHostFunction(value Value, owner *machineOwner) (machineHostFunc, bool) {
	if host, ok := value.resumableHostFunction(); ok {
		return func(ctx context.Context, args []Value) HostResult {
			return host(ctx, args)
		}, true
	}
	if host, ok := value.contextHostFunction(); ok {
		return func(ctx context.Context, args []Value) HostResult {
			values, err := host(ctx, args)
			if err != nil {
				return HostError(err)
			}
			return HostReturn(values...)
		}, true
	}
	if host, ok := value.hostFunction(); ok {
		return func(_ context.Context, args []Value) HostResult {
			values, err := host(args)
			if err != nil {
				return HostError(err)
			}
			return HostReturn(values...)
		}, true
	}
	if native, ok := value.nativeFunction(); ok {
		return func(_ context.Context, args []Value) HostResult {
			env := globalEnv{}
			if owner != nil {
				env.controller = owner.scalarMachine.window.controller
			}
			values, err := native(&env, args)
			if err != nil {
				return HostError(err)
			}
			return HostReturn(values...)
		}, true
	}
	return nil, false
}

func machineBaseGlobalValue(name string) (Value, bool, error) {
	value, ok := baseGlobalValue(name)
	return value, ok, nil
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
	ctx = machine.contextWithHostScriptFrames(ctx, int(operation.line))
	if machine.window.controller != nil {
		if err := machine.window.controller.enterCall(); err != nil {
			return err
		}
		defer machine.window.controller.leaveCall()
	}
	result := fn(ctx, args)
	checkpoint := captureMachineTableReconcileCheckpoint(&exporter)
	reconcileErr := reconcileMachineExportedTablesStopped(machine, &exporter)
	if result.err != nil {
		return result.err
	}
	if reconcileErr != nil {
		return reconcileErr
	}
	if result.suspended {
		return fmt.Errorf("host suspension is unavailable on the legacy Machine host-call path")
	}
	returned := result.values
	resultCount := int(operation.callResults)
	if resultCount < 0 {
		resultCount = len(returned)
		end := machine.activeBase + int(operation.a) + resultCount
		if end > len(machine.registers) {
			if err := machine.ensureStack(end); err != nil {
				checkpoint.restore(&exporter)
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
		checkpoint.restore(&exporter)
		return errors.New("compact Machine host-call result window is out of range")
	}
	for index := 0; index < resultCount; index++ {
		value := slotNil
		if index < len(returned) {
			value, err = exporter.importValueStopped(returned[index])
			if err != nil {
				checkpoint.restore(&exporter)
				return fmt.Errorf("compact Machine host result %d: %w", index, err)
			}
		}
		if err := machine.copySlot(destination+index, value); err != nil {
			checkpoint.restore(&exporter)
			return err
		}
	}
	if operation.tailCall != 0 {
		machine.skipCharge = 1
	}
	return nil
}

func (machine *scalarMachine) callNativeAdapterStopped(operation machineOperation, nativeID nativeFuncID) error {
	if machine == nil || machine.persistentOwner == nil {
		return errors.New("compact Machine native adapter requires a persistent owner")
	}
	native, ok := nativeFuncByID(nativeID)
	if !ok {
		return fmt.Errorf("compact Machine native ID %d is unsupported", nativeID)
	}
	argStart := machine.activeBase + int(operation.callArgStart)
	argCount := int(operation.callArgCount)
	if argCount < 0 {
		openStart := argStart + int(operation.callPrefix)
		if openStart != machine.activeOpenStart || machine.activeOpenCount < 0 {
			return errors.New("compact Machine open native-call argument window is unavailable")
		}
		argCount = int(operation.callPrefix) + machine.activeOpenCount
	}
	if argStart < machine.activeBase || argCount < 0 || argStart+argCount > len(machine.registers) {
		return errors.New("compact Machine native-call argument window is out of range")
	}
	exporter := machineTableExporter{machine: machine, tables: make(map[machineTableID]machineExportedTable)}
	args := make([]Value, argCount)
	for index := range args {
		value, err := exporter.value(machine.registers[argStart+index])
		if err != nil {
			return fmt.Errorf("compact Machine native argument %d: %w", index, err)
		}
		args[index] = value
	}
	if machine.window.controller != nil {
		if err := machine.window.controller.enterCall(); err != nil {
			return err
		}
		defer machine.window.controller.leaveCall()
	}
	env := globalEnv{controller: machine.window.controller}
	returned, callErr := native(&env, args)
	checkpoint := captureMachineTableReconcileCheckpoint(&exporter)
	reconcileErr := reconcileMachineExportedTablesStopped(machine, &exporter)
	if callErr != nil {
		return callErr
	}
	if reconcileErr != nil {
		return reconcileErr
	}
	var err error
	destination := machine.activeBase + int(operation.a)
	resultCount := int(operation.callResults)
	if resultCount < 0 {
		resultCount = len(returned)
		if end := destination + resultCount; end > len(machine.registers) {
			if err := machine.ensureStack(end); err != nil {
				checkpoint.restore(&exporter)
				return err
			}
		}
		machine.activeOpenStart = destination
		machine.activeOpenCount = resultCount
	} else {
		machine.activeOpenStart = 0
		machine.activeOpenCount = 0
	}
	if destination < machine.activeBase || destination+resultCount > len(machine.registers) {
		checkpoint.restore(&exporter)
		return errors.New("compact Machine native-call result window is out of range")
	}
	for index := 0; index < resultCount; index++ {
		value := slotNil
		if index < len(returned) {
			value, err = exporter.importValueStopped(returned[index])
			if err != nil {
				checkpoint.restore(&exporter)
				return fmt.Errorf("compact Machine native result %d: %w", index, err)
			}
		}
		if err := machine.copySlot(destination+index, value); err != nil {
			checkpoint.restore(&exporter)
			return err
		}
	}
	return nil
}

func (machine *scalarMachine) callNativeAdapterArgumentsStopped(nativeID nativeFuncID, arguments []slot, destination, resultCount int) error {
	if machine == nil || machine.persistentOwner == nil {
		return errors.New("compact Machine native adapter requires a persistent owner")
	}
	native, ok := nativeFuncByID(nativeID)
	if !ok {
		return fmt.Errorf("compact Machine native ID %d is unsupported", nativeID)
	}
	exporter := machineTableExporter{machine: machine, tables: make(map[machineTableID]machineExportedTable)}
	args := make([]Value, len(arguments))
	for index, argument := range arguments {
		value, err := exporter.value(argument)
		if err != nil {
			return fmt.Errorf("compact Machine native argument %d: %w", index, err)
		}
		args[index] = value
	}
	if machine.window.controller != nil {
		if err := machine.window.controller.enterCall(); err != nil {
			return err
		}
		defer machine.window.controller.leaveCall()
	}
	env := globalEnv{controller: machine.window.controller}
	returned, callErr := native(&env, args)
	checkpoint := captureMachineTableReconcileCheckpoint(&exporter)
	reconcileErr := reconcileMachineExportedTablesStopped(machine, &exporter)
	if callErr != nil {
		return callErr
	}
	if reconcileErr != nil {
		return reconcileErr
	}
	var err error
	if resultCount < 0 {
		resultCount = len(returned)
		if end := destination + resultCount; end > len(machine.registers) {
			if err := machine.ensureStack(end); err != nil {
				checkpoint.restore(&exporter)
				return err
			}
		}
		machine.activeOpenStart = destination
		machine.activeOpenCount = resultCount
	} else {
		machine.activeOpenStart = 0
		machine.activeOpenCount = 0
	}
	if destination < machine.activeBase || resultCount < 0 || destination+resultCount > len(machine.registers) {
		checkpoint.restore(&exporter)
		return errors.New("compact Machine native-call result window is out of range")
	}
	for index := 0; index < resultCount; index++ {
		value := slotNil
		if index < len(returned) {
			value, err = exporter.importValueStopped(returned[index])
			if err != nil {
				checkpoint.restore(&exporter)
				return fmt.Errorf("compact Machine native result %d: %w", index, err)
			}
		}
		if err := machine.copySlot(destination+index, value); err != nil {
			checkpoint.restore(&exporter)
			return err
		}
	}
	return nil
}

func (machine *scalarMachine) callFastHostStopped(callable slot, arguments []slot, destination, resultCount, returnPC int) error {
	if machine == nil || machine.persistentOwner == nil {
		return errors.New("compact Machine fast host call requires a persistent owner")
	}
	fn, err := machine.persistentOwner.hosts.lookup(callable)
	if err != nil {
		return err
	}
	exporter := machineTableExporter{machine: machine, tables: make(map[machineTableID]machineExportedTable)}
	args := make([]Value, len(arguments))
	for index, value := range arguments {
		args[index], err = exporter.value(value)
		if err != nil {
			return fmt.Errorf("compact Machine host argument %d: %w", index, err)
		}
	}
	ctx := machine.effects.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	line := 0
	if proto := machine.currentProto(); proto != nil && returnPC > 0 && returnPC-1 < len(proto.operations) {
		line = int(proto.operations[returnPC-1].line)
	}
	ctx = machine.contextWithHostScriptFrames(ctx, line)
	if machine.window.controller != nil {
		if err := machine.window.controller.enterCall(); err != nil {
			return err
		}
		defer machine.window.controller.leaveCall()
	}
	result := fn(ctx, args)
	checkpoint := captureMachineTableReconcileCheckpoint(&exporter)
	reconcileErr := reconcileMachineExportedTablesStopped(machine, &exporter)
	if result.err != nil {
		return fmt.Errorf("run: call failed: %w", result.err)
	}
	if reconcileErr != nil {
		return reconcileErr
	}
	if result.suspended {
		return machine.suspendHostCallStopped(result.token, destination, resultCount, returnPC)
	}
	returned := result.values
	if resultCount < 0 {
		resultCount = len(returned)
		if destination+resultCount > len(machine.registers) {
			if err := machine.ensureStack(destination + resultCount); err != nil {
				checkpoint.restore(&exporter)
				return err
			}
		}
		machine.activeOpenStart = destination
		machine.activeOpenCount = resultCount
	} else {
		machine.activeOpenStart = 0
		machine.activeOpenCount = 0
	}
	if destination < 0 || resultCount < 0 || destination+resultCount > len(machine.registers) {
		checkpoint.restore(&exporter)
		return errors.New("compact Machine fast host-call result window is out of range")
	}
	for index := 0; index < resultCount; index++ {
		value := slotNil
		if index < len(returned) {
			value, err = exporter.importValueStopped(returned[index])
			if err != nil {
				checkpoint.restore(&exporter)
				return fmt.Errorf("compact Machine host result %d: %w", index, err)
			}
		}
		if err := machine.copySlot(destination+index, value); err != nil {
			checkpoint.restore(&exporter)
			return err
		}
	}
	return nil
}

func (machine *scalarMachine) contextWithHostScriptFrames(ctx context.Context, line int) context.Context {
	scope, ok := invocationScopeFromContext(ctx)
	if !ok || !scope.resumable {
		return ctx
	}
	frame := ScriptFrame{Line: line}
	if proto := machine.currentProto(); proto != nil {
		frame.Source = proto.sourceName
		frame.Function = proto.functionName
	}
	frames := []ScriptFrame{frame}
	if machine.window.controller != nil {
		frames = append(frames, machine.window.controller.inheritedScriptFrames...)
	}
	scope.inheritedScriptFrames = frames
	return contextWithInvocationScope(ctx, scope)
}

func (machine *scalarMachine) suspendHostCallStopped(token any, destination, resultCount, returnPC int) error {
	if machine == nil || machine.persistentOwner == nil || machine.activeCoroutine.index == 0 {
		return fmt.Errorf("host suspension requires a resumable runtime operation")
	}
	var snapshot machineCoroutineSnapshot
	if err := captureMachineCoroutineStopped(machine, machineCoroutineFrameState{
		pc:             int32(returnPC),
		resumeRegister: int32(destination),
		resumeCount:    int32(resultCount),
	}, &snapshot); err != nil {
		return err
	}
	if err := machine.persistentOwner.coroutines.setActiveDepthStopped(machine.activeCoroutine, snapshot.frame.callDepth); err != nil {
		return err
	}
	exit, err := machine.persistentOwner.coroutines.yieldStopped(machine.activeCoroutine, snapshot, nil)
	if err != nil {
		return err
	}
	return &machineCoroutineLoopSignal{exit: exit, hostToken: token}
}
