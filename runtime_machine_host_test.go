package ember

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestMachineOwnerHostCallExportsAndReimportsTransientClosure(t *testing.T) {
	image := machineOwnerProgramImage(t, []string{`local callback = function()
	return 42
end
local returned = host(callback)
return returned()`})
	owner, err := newMachineOwner(image)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "visible")
	host := ContextHostFuncValue(func(got context.Context, args []Value) ([]Value, error) {
		if got.Value(contextKey{}) != "visible" {
			return nil, errors.New("host did not receive run context")
		}
		if len(args) != 1 || args[0].Kind() != FunctionKind {
			return nil, errors.New("host did not receive transient closure")
		}
		if _, err := decodeTransientScriptCallableValue(args[0], owner.closures.owner, owner.validateScriptCallableHandle); err != nil {
			return nil, err
		}
		return args, nil
	})

	lease, err := owner.beginRun()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.end()
	if err := owner.importGlobalsStopped(map[string]Value{"host": host}); err != nil {
		t.Fatal(err)
	}
	if err := owner.executeRootStopped(0, nil, machineRunEffects{ctx: ctx}); err != nil {
		t.Fatal(err)
	}
	value := owner.results[0]
	number, err := owner.number(value)
	if err != nil || number != 42 {
		t.Fatalf("result = (%v, %v), want 42", number, err)
	}
}

func TestMachineOwnerGlobalImportReusesHostSidecar(t *testing.T) {
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`return host()`}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	for index := 0; index < 10; index++ {
		want := float64(index)
		fn := ContextHostFuncValue(func(context.Context, []Value) ([]Value, error) {
			return []Value{NumberValue(want)}, nil
		})
		if err := owner.importGlobalsStopped(map[string]Value{"host": fn}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(owner.hosts.values); got != 1 {
		t.Fatalf("host sidecar records = %d, want 1 reused record", got)
	}
}

func TestMachineHostTableMutationIdentityCyclesAndMetatableMatchVM(t *testing.T) {
	mutate := HostFuncValue(func(args []Value) ([]Value, error) {
		table, ok := args[0].Table()
		if !ok {
			return nil, errors.New("mutate argument is not a table")
		}
		if table.metatable == nil {
			return nil, errors.New("mutate argument lost its metatable")
		}
		if err := table.rawSet(StringValue("changed"), NumberValue(42)); err != nil {
			return nil, err
		}
		if err := table.rawSet(StringValue("self"), TableValue(table)); err != nil {
			return nil, err
		}
		return []Value{TableValue(table)}, nil
	})
	assertMachineOwnerDispatchMatchesVM(t, `
local value = {original = 1}
setmetatable(value, {__index = {fallback = 7}})
local returned = mutate(value)
return returned == value, value.changed, value.self == value, value.fallback
`, map[string]Value{"mutate": mutate})
}

func TestMachineNativeRawSetMutatesOriginalTable(t *testing.T) {
	assertMachineOwnerDispatchMatchesVM(t, `
local value = {answer = 1}
local returned = rawset(value, "answer", 42)
return returned == value, value.answer
`, nil)
}

func TestMachineHostReturnedTablePreservesMetatableGraph(t *testing.T) {
	makeValue := HostFuncValue(func([]Value) ([]Value, error) {
		fallback := newTableWithCapacity(0, 1)
		_ = fallback.rawSet(StringValue("answer"), NumberValue(42))
		metatable := newTableWithCapacity(0, 1)
		_ = metatable.rawSet(StringValue("__index"), TableValue(fallback))
		value := newTableWithCapacity(0, 1)
		value.setMetatable(metatable)
		_ = value.rawSet(StringValue("self"), TableValue(value))
		return []Value{TableValue(value)}, nil
	})
	assertMachineOwnerDispatchMatchesVM(t, `
local value = makeValue()
return value.answer, value.self == value
`, map[string]Value{"makeValue": makeValue})
}

func TestMachineHostResultImportFailureRollsBackArgumentMutationAndAllocations(t *testing.T) {
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`
local input = {answer = 1}
local first, second = host(input)
return input.answer, first, second
`}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	var before machineTableReconcileCheckpoint
	host := ContextHostFuncValue(func(_ context.Context, args []Value) ([]Value, error) {
		before = captureMachineTableReconcileCheckpoint(&machineTableExporter{machine: &owner.scalarMachine})
		input, _ := args[0].Table()
		if err := input.rawSet(StringValue("answer"), NumberValue(2)); err != nil {
			return nil, err
		}
		valid := newTableWithCapacity(0, 1)
		_ = valid.rawSet(StringValue("accepted"), NumberValue(3))
		return []Value{TableValue(valid), UserDataValue(NewUserData("unsupported"))}, nil
	})
	if err := owner.importGlobalsStopped(map[string]Value{"host": host}); err != nil {
		t.Fatal(err)
	}
	err = owner.executeRoot(0, nil)
	if err == nil {
		t.Fatal("host result import unexpectedly succeeded")
	}
	after := captureMachineTableReconcileCheckpoint(&machineTableExporter{machine: &owner.scalarMachine})
	if !reflect.DeepEqual(after.tables, before.tables) ||
		!reflect.DeepEqual(after.strings, before.strings) ||
		!reflect.DeepEqual(after.tableNumbers, before.tableNumbers) ||
		after.hosts.owner != before.hosts.owner || after.hosts.closed != before.hosts.closed || len(after.hosts.values) != len(before.hosts.values) ||
		!reflect.DeepEqual(after.registers, before.registers) ||
		!reflect.DeepEqual(after.numberBits, before.numberBits) ||
		after.openStart != before.openStart || after.openCount != before.openCount {
		t.Fatalf("Machine state changed after failed host result import:\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestMachineHostErrorKeepsRepresentableArgumentMutation(t *testing.T) {
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`
saved = {answer = 1}
host(saved)
return saved.answer
`}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	hostErr := errors.New("host rejected call")
	host := ContextHostFuncValue(func(_ context.Context, args []Value) ([]Value, error) {
		input, _ := args[0].Table()
		if err := input.rawSet(StringValue("answer"), NumberValue(2)); err != nil {
			return nil, err
		}
		return nil, hostErr
	})
	if err := owner.importGlobalsStopped(map[string]Value{"host": host}); err != nil {
		t.Fatal(err)
	}
	if err := owner.executeRoot(0, nil); !errors.Is(err, hostErr) {
		t.Fatalf("execution error = %v, want host error", err)
	}
	name, err := owner.strings.internStringStopped("saved")
	if err != nil {
		t.Fatal(err)
	}
	value, present, err := owner.globals.get(name)
	if err != nil || !present {
		t.Fatalf("saved global = %#x present=%t err=%v", value, present, err)
	}
	tableID, err := owner.tableID(value)
	if err != nil {
		t.Fatal(err)
	}
	answerName, err := owner.strings.internStringStopped("answer")
	if err != nil {
		t.Fatal(err)
	}
	answer, found := owner.tables.getString(tableID, answerName)
	if !found {
		t.Fatal("representable host mutation was not reconciled")
	}
	number, err := owner.number(answer)
	if err != nil || number != 2 {
		t.Fatalf("saved.answer = %v err=%v, want 2", number, err)
	}
}

func TestMachineTableReconcileFailureIsAtomic(t *testing.T) {
	tests := []struct {
		name   string
		limit  uint64
		mutate func(*Table)
	}{
		{
			name: "unsupported value",
			mutate: func(table *Table) {
				table.clearRawStorage()
				_ = table.rawSet(StringValue("accepted"), NumberValue(2))
				_ = table.rawSet(StringValue("rejected"), UserDataValue(NewUserData("foreign")))
			},
		},
		{
			name:  "quota",
			limit: 1,
			mutate: func(table *Table) {
				table.clearRawStorage()
				_ = table.rawSet(StringValue("first"), NumberValue(1))
				_ = table.rawSet(StringValue("second"), NumberValue(2))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			owner := newMachineIntrinsicTestOwner(t)
			defer owner.close()
			original := newTableWithCapacity(0, 1)
			_ = original.rawSet(StringValue("original"), NumberValue(1))
			encoded, err := owner.importValueStopped(TableValue(original))
			if err != nil {
				t.Fatal(err)
			}
			before, err := owner.captureBaseTableStopped(encoded)
			if err != nil {
				t.Fatal(err)
			}
			exporter := machineTableExporter{machine: &owner.scalarMachine, tables: make(map[machineTableID]machineExportedTable)}
			exported, err := exporter.value(encoded)
			if err != nil {
				t.Fatal(err)
			}
			public, _ := exported.Table()
			test.mutate(public)
			if err := exporter.reconcileStopped(test.limit); err == nil {
				t.Fatal("reconcile unexpectedly succeeded")
			}
			after, err := owner.captureBaseTableStopped(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("table changed after failed reconcile:\nbefore=%#v\nafter=%#v", before, after)
			}
		})
	}
}

func TestMachineTableReconcileDeletingMetatableProtectionRefreshesCache(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	metatable := newTableWithCapacity(0, 1)
	_ = metatable.rawSet(StringValue("__metatable"), StringValue("locked"))
	object := newTableWithCapacity(0, 0)
	object.setMetatable(metatable)
	encoded, err := owner.importValueStopped(TableValue(object))
	if err != nil {
		t.Fatal(err)
	}
	exporter := machineTableExporter{machine: &owner.scalarMachine, tables: make(map[machineTableID]machineExportedTable)}
	exported, err := exporter.value(encoded)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := exported.Table()
	if err := public.metatable.rawSet(StringValue("__metatable"), NilValue()); err != nil {
		t.Fatal(err)
	}
	if err := exporter.reconcileStopped(0); err != nil {
		t.Fatal(err)
	}
	objectID, _ := owner.tableID(encoded)
	if _, protected, err := owner.tables.protectedMetatable(objectID); err != nil || protected {
		t.Fatalf("protection after deletion = protected:%t err:%v", protected, err)
	}
}

func TestMachineExporterCrossesNativeValuesDirectAndNested(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	exporter := machineTableExporter{machine: &owner.scalarMachine, tables: make(map[machineTableID]machineExportedTable)}
	direct, err := exporter.value(slotNativeID(nativeFuncToString))
	if err != nil {
		t.Fatal(err)
	}
	if got := valueNativeID(direct); got != nativeFuncToString {
		t.Fatalf("direct native ID = %d, want %d", got, nativeFuncToString)
	}
	tableID, err := owner.tables.newTableStopped(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	tableSlot, _ := slotPackHandle(slotTagTable, uint32(tableID), 1)
	keyID, err := owner.strings.internStringStopped("native")
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.tables.rawSetStopped(tableID, machineTableStringKey(keyID), slotNativeID(nativeFuncToString), 0); err != nil {
		t.Fatal(err)
	}
	nested, err := exporter.value(tableSlot)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := nested.Table()
	value, err := public.Get(StringValue("native"))
	if err != nil {
		t.Fatal(err)
	}
	if got := valueNativeID(value); got != nativeFuncToString {
		t.Fatalf("nested native ID = %d, want %d", got, nativeFuncToString)
	}
}

func TestMachineTableReconcileFailureOrderIsDeterministic(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	first := newTableWithCapacity(0, 1)
	_ = first.rawSet(StringValue("original"), NumberValue(1))
	second := newTableWithCapacity(0, 1)
	_ = second.rawSet(StringValue("original"), NumberValue(2))
	firstSlot, err := owner.importValueStopped(TableValue(first))
	if err != nil {
		t.Fatal(err)
	}
	secondSlot, err := owner.importValueStopped(TableValue(second))
	if err != nil {
		t.Fatal(err)
	}
	exporter := machineTableExporter{machine: &owner.scalarMachine, tables: make(map[machineTableID]machineExportedTable)}
	firstValue, err := exporter.value(firstSlot)
	if err != nil {
		t.Fatal(err)
	}
	secondValue, err := exporter.value(secondSlot)
	if err != nil {
		t.Fatal(err)
	}
	firstPublic, _ := firstValue.Table()
	firstPublic.clearRawStorage()
	_ = firstPublic.rawSet(StringValue("unsupported"), UserDataValue(NewUserData("foreign")))
	secondPublic, _ := secondValue.Table()
	secondPublic.clearRawStorage()
	_ = secondPublic.rawSet(StringValue("one"), NumberValue(1))
	_ = secondPublic.rawSet(StringValue("two"), NumberValue(2))
	for iteration := 0; iteration < 128; iteration++ {
		err := exporter.reconcileStopped(1)
		if err == nil {
			t.Fatal("reconcile unexpectedly succeeded")
		}
		var limit *LimitError
		if errors.As(err, &limit) {
			t.Fatalf("iteration %d visited higher table first: %v", iteration, err)
		}
	}
}

func TestMachineExporterReusesHostAndCoroutineWrappersAndReturnedSlots(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	hostSlot, err := owner.importValueStopped(ContextHostFuncValue(func(context.Context, []Value) ([]Value, error) { return nil, nil }))
	if err != nil {
		t.Fatal(err)
	}
	closure, err := owner.scalarMachine.closures.createClosureStopped(0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := owner.coroutines.createStopped(machineCoroutineRoot{module: 0, proto: 0, closure: closure})
	if err != nil {
		t.Fatal(err)
	}
	coroutineSlot, err := slotPackHandle(slotTagCoroutine, handle.index, handle.generation)
	if err != nil {
		t.Fatal(err)
	}
	exporter := machineTableExporter{machine: &owner.scalarMachine, tables: make(map[machineTableID]machineExportedTable)}
	for name, original := range map[string]slot{"host": hostSlot, "coroutine": coroutineSlot} {
		first, err := exporter.value(original)
		if err != nil {
			t.Fatalf("%s first export: %v", name, err)
		}
		second, err := exporter.value(original)
		if err != nil {
			t.Fatalf("%s second export: %v", name, err)
		}
		if first != second {
			t.Fatalf("%s duplicate exports use different wrappers", name)
		}
		returned, err := exporter.importValueStopped(first)
		if err != nil {
			t.Fatalf("%s returned import: %v", name, err)
		}
		if returned != original {
			t.Fatalf("%s returned slot = %#x, want original %#x", name, returned, original)
		}
	}
}
