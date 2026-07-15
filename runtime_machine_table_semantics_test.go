package ember

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestMachineTableScalarKeysMatchPublicRawIdentity(t *testing.T) {
	positiveZero, err := machineTableKeyFromScalar(slot(math.Float64bits(0)), 0)
	if err != nil {
		t.Fatal(err)
	}
	negativeZero, err := machineTableKeyFromScalar(slot(math.Float64bits(math.Copysign(0, -1))), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !machineTableKeysEqual(positiveZero, negativeZero) {
		t.Fatal("Machine scalar keys distinguish positive and negative zero")
	}

	public := NewTable()
	if err := public.rawSet(NumberValue(0), StringValue("zero")); err != nil {
		t.Fatal(err)
	}
	got, err := public.rawGet(NumberValue(math.Copysign(0, -1)))
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := got.String(); !ok || text != "zero" {
		t.Fatalf("public signed-zero lookup = %q (%t), want zero", text, ok)
	}

	var strings machineStringArena
	firstString, err := strings.internStringStopped("field")
	if err != nil {
		t.Fatal(err)
	}
	secondString, err := strings.internBytesStopped([]byte("field"))
	if err != nil {
		t.Fatal(err)
	}
	firstStringSlot, err := slotPackHandle(slotTagString, uint32(firstString), 1)
	if err != nil {
		t.Fatal(err)
	}
	secondStringSlot, err := slotPackHandle(slotTagString, uint32(secondString), 1)
	if err != nil {
		t.Fatal(err)
	}
	firstKey, err := machineTableKeyFromScalar(firstStringSlot, 0)
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := machineTableKeyFromScalar(secondStringSlot, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !machineTableKeysEqual(firstKey, secondKey) {
		t.Fatal("equal interned strings do not share Machine key identity")
	}

	firstTableSlot, err := slotPackHandle(slotTagTable, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	secondTableSlot, err := slotPackHandle(slotTagTable, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	firstTableKey, err := machineTableKeyFromScalar(firstTableSlot, 0)
	if err != nil {
		t.Fatal(err)
	}
	secondTableKey, err := machineTableKeyFromScalar(secondTableSlot, 0)
	if err != nil {
		t.Fatal(err)
	}
	if machineTableKeysEqual(firstTableKey, secondTableKey) {
		t.Fatal("distinct owner-local table IDs share Machine key identity")
	}
	firstPublicKey, secondPublicKey := NewTable(), NewTable()
	if err := public.rawSet(TableValue(firstPublicKey), BoolValue(true)); err != nil {
		t.Fatal(err)
	}
	if err := public.rawSet(TableValue(secondPublicKey), BoolValue(false)); err != nil {
		t.Fatal(err)
	}
	if got, err := public.rawGet(TableValue(firstPublicKey)); err != nil || got != BoolValue(true) {
		t.Fatalf("first public table-key lookup = %#v, err %v", got, err)
	}
	if got, err := public.rawGet(TableValue(secondPublicKey)); err != nil || got != BoolValue(false) {
		t.Fatalf("second public table-key lookup = %#v, err %v", got, err)
	}

	if _, err := machineTableKeyFromScalar(slotNil, 0); !errors.Is(err, errMachineTableNilKey) {
		t.Fatalf("nil key error = %v", err)
	}
	if err := public.rawSet(NilValue(), BoolValue(true)); err == nil {
		t.Fatal("public nil key was accepted")
	}
	boxedNaN, err := slotPackHandle(slotTagBoxedNumber, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machineTableKeyFromScalar(boxedNaN, math.Float64bits(math.NaN())); !errors.Is(err, errMachineTableNaNKey) {
		t.Fatalf("NaN key error = %v", err)
	}
	if err := public.rawSet(NumberValue(math.NaN()), BoolValue(true)); err == nil {
		t.Fatal("public NaN key was accepted")
	}
}

func TestMachineTableKeyAcceptsStructurallyValidCoroutineHandle(t *testing.T) {
	value, err := slotPackHandle(slotTagCoroutine, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	key, err := machineTableKeyFromScalar(value, 0)
	if err != nil {
		t.Fatal(err)
	}
	if key != machineTableSlotKey(value) {
		t.Fatalf("coroutine key = %#v, want slot identity", key)
	}
}

func TestMachineTableRawOperationsMatchPublicDeletionLengthAndQuota(t *testing.T) {
	var arena machineTableArena
	table, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	public := NewTable()

	for _, entry := range []struct {
		index uint32
		value slot
	}{
		{index: 1, value: slotBool(true)},
		{index: 2, value: slotBool(false)},
		{index: 4, value: slotBool(true)},
	} {
		key := machineTableArrayKey(entry.index)
		if err := arena.rawSetStopped(table, key, entry.value, 0); err != nil {
			t.Fatal(err)
		}
		if err := public.rawSet(NumberValue(float64(entry.index)), BoolValue(entry.value == slotBool(true))); err != nil {
			t.Fatal(err)
		}
	}

	length, err := arena.rawLen(table)
	if err != nil {
		t.Fatal(err)
	}
	publicLength, err := public.rawLen()
	if err != nil {
		t.Fatal(err)
	}
	if length != uint32(publicLength) || length != 2 {
		t.Fatalf("raw lengths = Machine %d, public %d; want 2", length, publicLength)
	}

	if err := arena.rawSetStopped(table, machineTableArrayKey(2), slotNil, 0); err != nil {
		t.Fatal(err)
	}
	if err := public.rawSet(NumberValue(2), NilValue()); err != nil {
		t.Fatal(err)
	}
	if value, err := arena.rawGet(table, machineTableArrayKey(2)); err != nil || value != slotNil {
		t.Fatalf("deleted Machine value = %#x, err %v", value, err)
	}
	length, err = arena.rawLen(table)
	if err != nil {
		t.Fatal(err)
	}
	publicLength, err = public.rawLen()
	if err != nil {
		t.Fatal(err)
	}
	if length != uint32(publicLength) || length != 1 {
		t.Fatalf("post-delete raw lengths = Machine %d, public %d; want 1", length, publicLength)
	}

	limited, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(limited, machineTableSlotKey(slotBool(false)), slotBool(true), 2); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(limited, machineTableSlotKey(slotBool(true)), slotBool(false), 2); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(limited, machineTableSlotKey(slotBool(false)), slotBool(false), 2); err != nil {
		t.Fatalf("existing-key update consumed quota: %v", err)
	}
	err = arena.rawSetStopped(limited, machineTableArrayKey(1), slotBool(true), 2)
	var limitErr *LimitError
	if !errors.As(err, &limitErr) || limitErr.Kind != LimitTableEntriesPerTable || limitErr.Limit != 2 || limitErr.Used != 3 {
		t.Fatalf("entry limit error = %#v", err)
	}
	if err := arena.rawSetStopped(limited, machineTableSlotKey(slotBool(true)), slotNil, 2); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(limited, machineTableArrayKey(1), slotBool(true), 2); err != nil {
		t.Fatalf("deletion did not release entry quota: %v", err)
	}
	record, ok := arena.lookup(limited)
	if !ok || record.entryCount != 2 {
		t.Fatalf("limited record = %#v (%t), want two entries", record, ok)
	}

	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxTableEntriesPerTable: 2})
	if err != nil {
		t.Fatal(err)
	}
	publicLimited := NewTable()
	if err := publicLimited.rawSetWithController(controller, BoolValue(false), BoolValue(true)); err != nil {
		t.Fatal(err)
	}
	if err := publicLimited.rawSetWithController(controller, BoolValue(true), BoolValue(false)); err != nil {
		t.Fatal(err)
	}
	err = publicLimited.rawSetWithController(controller, NumberValue(1), BoolValue(true))
	limitErr = nil
	if !errors.As(err, &limitErr) || limitErr.Kind != LimitTableEntriesPerTable || limitErr.Limit != 2 || limitErr.Used != 3 {
		t.Fatalf("public entry limit error = %#v", err)
	}
	if err := publicLimited.rawSetWithController(controller, BoolValue(true), NilValue()); err != nil {
		t.Fatal(err)
	}
	if err := publicLimited.rawSetWithController(controller, NumberValue(1), BoolValue(true)); err != nil {
		t.Fatalf("public deletion did not release entry quota: %v", err)
	}
}

func TestMachineTableMetatableStateProtectsAndInvalidatesVersions(t *testing.T) {
	var arena machineTableArena
	object, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	firstMetatable, err := arena.newTableStopped(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	secondMetatable, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}

	rawVersion, metaVersion, err := arena.tableVersions(object)
	if err != nil || rawVersion != 0 || metaVersion != 0 {
		t.Fatalf("initial versions = %d/%d, err %v", rawVersion, metaVersion, err)
	}
	if err := arena.setMetatableStopped(object, firstMetatable); err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(object, firstMetatable); err != nil {
		t.Fatal(err)
	}
	gotMetatable, err := arena.metatable(object)
	if err != nil || gotMetatable != firstMetatable {
		t.Fatalf("metatable = %d, err %v; want %d", gotMetatable, err, firstMetatable)
	}
	_, metaVersion, err = arena.tableVersions(object)
	if err != nil || metaVersion != 1 {
		t.Fatalf("metatable version after idempotent set = %d, err %v; want 1", metaVersion, err)
	}

	indexKey := machineStringID(1)
	if err := arena.rawSetStopped(firstMetatable, machineTableStringKey(indexKey), slotBool(true), 0); err != nil {
		t.Fatal(err)
	}
	rawVersion, _, err = arena.tableVersions(firstMetatable)
	if err != nil || rawVersion != 1 {
		t.Fatalf("raw version after insert = %d, err %v; want 1", rawVersion, err)
	}
	if err := arena.rawSetStopped(firstMetatable, machineTableStringKey(indexKey), slotBool(false), 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(firstMetatable, machineTableStringKey(indexKey), slotBool(false), 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(firstMetatable, machineTableStringKey(indexKey), slotNil, 0); err != nil {
		t.Fatal(err)
	}
	rawVersion, _, err = arena.tableVersions(firstMetatable)
	if err != nil || rawVersion != 3 {
		t.Fatalf("raw version after update/no-op/delete = %d, err %v; want 3", rawVersion, err)
	}

	if err := arena.setMetatableProtectionStopped(firstMetatable, slotBool(true)); err != nil {
		t.Fatal(err)
	}
	protection, protected, err := arena.protectedMetatable(object)
	if err != nil || !protected || protection != slotBool(true) {
		t.Fatalf("protected metatable = %#x (%t), err %v", protection, protected, err)
	}
	if err := arena.setMetatableStopped(object, secondMetatable); !errors.Is(err, errMachineTableProtectedMetatable) {
		t.Fatalf("protected replacement error = %v", err)
	}
	if got, _ := arena.metatable(object); got != firstMetatable {
		t.Fatalf("protected replacement changed metatable to %d", got)
	}

	publicObject := NewTable()
	publicMetatable := NewTable()
	publicMetatable.setRawStringField("__metatable", BoolValue(true))
	publicObject.setMetatable(publicMetatable)
	publicProtection, err := publicTableAccess().protectedMetatable(publicObject)
	if err != nil {
		t.Fatal(err)
	}
	if boolean, ok := publicProtection.Bool(); !ok || !boolean {
		t.Fatalf("public protected metatable = %#v", publicProtection)
	}
	if _, err := baseSetMetatable(nil, []Value{TableValue(publicObject), TableValue(NewTable())}); err == nil ||
		!strings.Contains(err.Error(), "cannot change protected metatable") {
		t.Fatalf("public protected replacement error = %v", err)
	}

	if err := arena.setMetatableProtectionStopped(firstMetatable, slotNil); err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(object, secondMetatable); err != nil {
		t.Fatalf("cleared protection still blocks replacement: %v", err)
	}
	_, metaVersion, err = arena.tableVersions(object)
	if err != nil || metaVersion != 2 {
		t.Fatalf("replacement metatable version = %d, err %v; want 2", metaVersion, err)
	}
}

func TestMachineTableIndexDecisionMatchesPublicChainsAndFunctionSideExit(t *testing.T) {
	var arena machineTableArena
	object, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	metatable, err := arena.newTableStopped(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := arena.newTableStopped(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(object, metatable); err != nil {
		t.Fatal(err)
	}
	indexName := machineStringID(1)
	fieldName := machineStringID(2)
	fallbackSlot, err := slotPackHandle(slotTagTable, uint32(fallback), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(metatable, machineTableStringKey(indexName), fallbackSlot, 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(fallback, machineTableStringKey(fieldName), slotBool(true), 0); err != nil {
		t.Fatal(err)
	}

	action, err := arena.decideIndex(object, machineTableStringKey(fieldName), indexName)
	if err != nil {
		t.Fatal(err)
	}
	if action.kind != machineTableActionReturn || action.value != slotBool(true) {
		t.Fatalf("Machine __index table decision = %#v", action)
	}

	publicObject := NewTable()
	publicMetatable := NewTable()
	publicFallback := NewTable()
	if err := publicMetatable.rawSet(StringValue("__index"), TableValue(publicFallback)); err != nil {
		t.Fatal(err)
	}
	if err := publicFallback.rawSet(StringValue("field"), BoolValue(true)); err != nil {
		t.Fatal(err)
	}
	publicObject.setMetatable(publicMetatable)
	publicValue, err := publicObject.Get(StringValue("field"))
	if err != nil {
		t.Fatal(err)
	}
	if boolean, ok := publicValue.Bool(); !ok || !boolean {
		t.Fatalf("public __index table result = %#v", publicValue)
	}

	callable, err := slotPackHandle(slotTagClosure, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(metatable, machineTableStringKey(indexName), callable, 0); err != nil {
		t.Fatal(err)
	}
	action, err = arena.decideIndex(object, machineTableStringKey(fieldName), indexName)
	if err != nil {
		t.Fatal(err)
	}
	if action.kind != machineTableActionCall || action.event != machineTableEventIndex ||
		action.table != object || action.callable != callable || action.key != machineTableStringKey(fieldName) {
		t.Fatalf("Machine __index function decision = %#v", action)
	}
}

func TestMachineTableNewIndexDecisionMatchesPublicChainAndDetectsCycles(t *testing.T) {
	var arena machineTableArena
	object, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	objectMetatable, err := arena.newTableStopped(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(object, objectMetatable); err != nil {
		t.Fatal(err)
	}
	newIndexName := machineStringID(1)
	fieldName := machineStringID(2)
	fallbackSlot, err := slotPackHandle(slotTagTable, uint32(fallback), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(objectMetatable, machineTableStringKey(newIndexName), fallbackSlot, 0); err != nil {
		t.Fatal(err)
	}

	action, err := arena.decideNewIndex(object, machineTableStringKey(fieldName), slotBool(true), newIndexName)
	if err != nil {
		t.Fatal(err)
	}
	if action.kind != machineTableActionStore || action.table != fallback ||
		action.key != machineTableStringKey(fieldName) || action.value != slotBool(true) {
		t.Fatalf("Machine __newindex table decision = %#v", action)
	}
	if err := arena.rawSetStopped(action.table, action.key, action.value, 0); err != nil {
		t.Fatal(err)
	}

	publicObject := NewTable()
	publicMetatable := NewTable()
	publicFallback := NewTable()
	if err := publicMetatable.rawSet(StringValue("__newindex"), TableValue(publicFallback)); err != nil {
		t.Fatal(err)
	}
	publicObject.setMetatable(publicMetatable)
	if err := publicObject.Set(StringValue("field"), BoolValue(true)); err != nil {
		t.Fatal(err)
	}
	publicValue, err := publicFallback.rawGet(StringValue("field"))
	if err != nil {
		t.Fatal(err)
	}
	if boolean, ok := publicValue.Bool(); !ok || !boolean {
		t.Fatalf("public __newindex table result = %#v", publicValue)
	}

	fallbackMetatable, err := arena.newTableStopped(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(fallback, fallbackMetatable); err != nil {
		t.Fatal(err)
	}
	objectSlot, err := slotPackHandle(slotTagTable, uint32(object), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(fallback, machineTableStringKey(fieldName), slotNil, 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(fallbackMetatable, machineTableStringKey(newIndexName), objectSlot, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := arena.decideNewIndex(object, machineTableStringKey(fieldName), slotBool(false), newIndexName); !errors.Is(err, errMachineTableNewIndexCycle) {
		t.Fatalf("cyclic __newindex error = %v", err)
	}
	publicCycleFirst, publicCycleSecond := NewTable(), NewTable()
	publicCycleFirstMetatable, publicCycleSecondMetatable := NewTable(), NewTable()
	if err := publicCycleFirstMetatable.rawSet(StringValue("__newindex"), TableValue(publicCycleSecond)); err != nil {
		t.Fatal(err)
	}
	if err := publicCycleSecondMetatable.rawSet(StringValue("__newindex"), TableValue(publicCycleFirst)); err != nil {
		t.Fatal(err)
	}
	publicCycleFirst.setMetatable(publicCycleFirstMetatable)
	publicCycleSecond.setMetatable(publicCycleSecondMetatable)
	if err := publicCycleFirst.Set(StringValue("field"), BoolValue(true)); err == nil ||
		!strings.Contains(err.Error(), "cyclic __newindex chain") {
		t.Fatalf("public cyclic __newindex error = %v", err)
	}

	callable, err := slotPackHandle(slotTagClosure, 9, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(objectMetatable, machineTableStringKey(newIndexName), callable, 0); err != nil {
		t.Fatal(err)
	}
	action, err = arena.decideNewIndex(object, machineTableStringKey(fieldName), slotBool(false), newIndexName)
	if err != nil {
		t.Fatal(err)
	}
	if action.kind != machineTableActionCall || action.event != machineTableEventNewIndex ||
		action.table != object || action.callable != callable || action.value != slotBool(false) {
		t.Fatalf("Machine __newindex function decision = %#v", action)
	}
}

func TestMachineTableIndexDecisionDetectsCycles(t *testing.T) {
	var arena machineTableArena
	first, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	firstMetatable, err := arena.newTableStopped(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	secondMetatable, err := arena.newTableStopped(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(first, firstMetatable); err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(second, secondMetatable); err != nil {
		t.Fatal(err)
	}
	indexName := machineStringID(1)
	firstSlot, _ := slotPackHandle(slotTagTable, uint32(first), 1)
	secondSlot, _ := slotPackHandle(slotTagTable, uint32(second), 1)
	if err := arena.rawSetStopped(firstMetatable, machineTableStringKey(indexName), secondSlot, 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(secondMetatable, machineTableStringKey(indexName), firstSlot, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := arena.decideIndex(first, machineTableStringKey(2), indexName); !errors.Is(err, errMachineTableIndexCycle) {
		t.Fatalf("cyclic __index error = %v", err)
	}

	publicFirst, publicSecond := NewTable(), NewTable()
	publicFirstMetatable, publicSecondMetatable := NewTable(), NewTable()
	if err := publicFirstMetatable.rawSet(StringValue("__index"), TableValue(publicSecond)); err != nil {
		t.Fatal(err)
	}
	if err := publicSecondMetatable.rawSet(StringValue("__index"), TableValue(publicFirst)); err != nil {
		t.Fatal(err)
	}
	publicFirst.setMetatable(publicFirstMetatable)
	publicSecond.setMetatable(publicSecondMetatable)
	if _, err := publicFirst.Get(StringValue("missing")); err == nil || !strings.Contains(err.Error(), "cyclic __index chain") {
		t.Fatalf("public cyclic __index error = %v", err)
	}
}
