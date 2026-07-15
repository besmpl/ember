package ember

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestMachinePrepareAndArrayNextMatchPublicArrayIterator(t *testing.T) {
	var arena machineTableArena
	table, err := arena.newTableStopped(2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(table, machineTableArrayKey(1), slotBool(true), 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(table, machineTableArrayKey(2), slotBool(false), 0); err != nil {
		t.Fatal(err)
	}
	tableSlot, err := slotPackHandle(slotTagTable, uint32(table), 1)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := arena.prepareIterator(tableSlot, machineStringID(1))
	if err != nil {
		t.Fatal(err)
	}
	if plan.ready == 0 || plan.mode != machineIteratorArray || plan.generator != slotNativeID(nativeFuncArrayNext) ||
		plan.state != tableSlot || plan.control != slotNil {
		t.Fatalf("array iterator plan = %#v", plan)
	}

	public := NewTable()
	if err := public.rawSet(NumberValue(1), BoolValue(true)); err != nil {
		t.Fatal(err)
	}
	if err := public.rawSet(NumberValue(2), BoolValue(false)); err != nil {
		t.Fatal(err)
	}
	publicControl := NilValue()
	var cursor machineTableCursor
	for index, wantValue := range []slot{slotBool(true), slotBool(false)} {
		step, err := arena.arrayNext(table, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if step.done != 0 || step.count != 2 || step.key != slot(math.Float64bits(float64(index+1))) || step.value != wantValue {
			t.Fatalf("array step %d = %#v", index, step)
		}
		publicResults, publicCount, err := baseArrayNextInline(TableValue(public), publicControl)
		if err != nil {
			t.Fatal(err)
		}
		if publicCount != 2 {
			t.Fatalf("public array step %d count = %d", index, publicCount)
		}
		publicKey, _ := publicResults[0].Number()
		publicValue, _ := publicResults[1].Bool()
		machineValue, _ := slotBoolValue(step.value)
		if publicKey != float64(index+1) || publicValue != machineValue {
			t.Fatalf("step %d differs: Machine %#v, public %#v", index, step, publicResults)
		}
		cursor = step.cursor
		publicControl = publicResults[0]
	}
	step, err := arena.arrayNext(table, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if step.done == 0 || step.count != 1 || step.key != slotNil || step.value != slotNil {
		t.Fatalf("array exhaustion = %#v", step)
	}
}

func TestMachineSparseIterationUsesGenericJump2Semantics(t *testing.T) {
	var arena machineTableArena
	table := mustMachineTable(t, &arena)
	if err := arena.rawSetStopped(table, machineTableArrayKey(1), slotBool(true), 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(table, machineTableArrayKey(3), slotBool(false), 0); err != nil {
		t.Fatal(err)
	}
	tableSlot := mustMachineHandle(t, slotTagTable, uint32(table), 1)
	plan, err := arena.prepareIterator(tableSlot, 1)
	if err != nil {
		t.Fatal(err)
	}
	if plan.mode != machineIteratorGeneric || plan.generator != slotNativeID(nativeFuncTableNext) {
		t.Fatalf("sparse iterator plan = %#v", plan)
	}

	var cursor machineTableCursor
	for index, want := range []uint32{1, 3} {
		step, jump, err := arena.arrayNextJump2(plan.mode, table, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if jump || step.done != 0 || math.Float64frombits(uint64(step.key)) != float64(want) {
			t.Fatalf("sparse jump2 step %d = %#v, jump %t", index, step, jump)
		}
		cursor = step.cursor
	}
	step, jump, err := arena.arrayNextJump2(plan.mode, table, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if !jump || step.done == 0 || step.key != slotNil || step.value != slotNil {
		t.Fatalf("sparse jump2 exhaustion = %#v, jump %t", step, jump)
	}
}

func TestMachineIterationRecordsArePointerFree(t *testing.T) {
	for _, value := range []any{
		machineIteratorPlan{},
		machineIteratorStep{},
		machineStringFieldIndexContinuation{},
	} {
		if machineTableTypeHasPointers(reflect.TypeOf(value)) {
			t.Fatalf("Machine iteration record %T contains a Go pointer, map, or interface", value)
		}
	}
}

func TestMachineGenericNextMatchesPublicMixedInsertionOrder(t *testing.T) {
	var arena machineTableArena
	table, err := arena.newTableStopped(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	nameID := machineStringID(7)
	entries := []struct {
		key   machineTableKey
		value slot
	}{
		{machineTableStringKey(nameID), slotBool(true)},
		{machineTableArrayKey(2), slotBool(false)},
		{machineTableArrayKey(1), slotBool(true)},
	}
	for _, entry := range entries {
		if err := arena.rawSetStopped(table, entry.key, entry.value, 0); err != nil {
			t.Fatal(err)
		}
	}
	tableSlot, err := slotPackHandle(slotTagTable, uint32(table), 1)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := arena.prepareIterator(tableSlot, 1)
	if err != nil {
		t.Fatal(err)
	}
	if plan.mode != machineIteratorGeneric || plan.generator != slotNativeID(nativeFuncTableNext) {
		t.Fatalf("mixed iterator plan = %#v", plan)
	}

	public := NewTable()
	if err := public.rawSet(StringValue("name"), BoolValue(true)); err != nil {
		t.Fatal(err)
	}
	if err := public.rawSet(NumberValue(2), BoolValue(false)); err != nil {
		t.Fatal(err)
	}
	if err := public.rawSet(NumberValue(1), BoolValue(true)); err != nil {
		t.Fatal(err)
	}

	wantKeys := []slot{
		mustMachineHandle(t, slotTagString, uint32(nameID), 1),
		slot(math.Float64bits(2)),
		slot(math.Float64bits(1)),
	}
	publicControl := NilValue()
	var cursor machineTableCursor
	for index, wantKey := range wantKeys {
		step, err := arena.genericNext(table, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if step.done != 0 || step.count != 2 || step.key != wantKey || step.value != entries[index].value {
			t.Fatalf("generic step %d = %#v", index, step)
		}
		publicKey, publicValue, err := public.rawNext(publicControl)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			if text, ok := publicKey.String(); !ok || text != "name" {
				t.Fatalf("public first key = %#v", publicKey)
			}
		} else if number, ok := publicKey.Number(); !ok || number != float64(3-index) {
			t.Fatalf("public key %d = %#v", index, publicKey)
		}
		publicBool, _ := publicValue.Bool()
		machineBool, _ := slotBoolValue(step.value)
		if publicBool != machineBool {
			t.Fatalf("generic value %d differs: Machine %t, public %t", index, machineBool, publicBool)
		}
		cursor = step.cursor
		publicControl = publicKey
	}
	step, err := arena.genericNext(table, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if step.done == 0 || step.count != 1 || step.key != slotNil {
		t.Fatalf("generic exhaustion = %#v", step)
	}
}

func TestMachineGenericCursorSurvivesCompactionAndRejectsDeletedPriorKey(t *testing.T) {
	var arena machineTableArena
	table, err := arena.newTableStopped(0, 40)
	if err != nil {
		t.Fatal(err)
	}
	public := NewTable()
	for index := 1; index <= 40; index++ {
		if err := arena.rawSetStopped(table, machineTableStringKey(machineStringID(index)), slot(math.Float64bits(float64(index))), 0); err != nil {
			t.Fatal(err)
		}
		if err := public.rawSet(StringValue("k"+strconv.Itoa(index)), NumberValue(float64(index))); err != nil {
			t.Fatal(err)
		}
	}

	var cursor machineTableCursor
	publicControl := NilValue()
	for index := 1; index <= 25; index++ {
		step, err := arena.genericNext(table, cursor)
		if err != nil {
			t.Fatal(err)
		}
		cursor = step.cursor
		publicKey, _, err := public.rawNext(publicControl)
		if err != nil {
			t.Fatal(err)
		}
		publicControl = publicKey
	}
	if cursor.key != machineTableStringKey(25) {
		t.Fatalf("cursor before compaction = %#v, want string key 25", cursor)
	}

	for index := 1; index <= 21; index++ {
		if err := arena.rawSetStopped(table, machineTableStringKey(machineStringID(index)), slotNil, 0); err != nil {
			t.Fatal(err)
		}
		if err := public.rawSet(StringValue("k"+strconv.Itoa(index)), NilValue()); err != nil {
			t.Fatal(err)
		}
	}
	record, ok := arena.lookup(table)
	if !ok || record.orderTombstone != 0 || record.orderLength != 19 {
		t.Fatalf("compacted Machine record = %#v (%t)", record, ok)
	}

	if err := arena.rawSetStopped(table, machineTableStringKey(27), slotNil, 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(table, machineTableStringKey(27), slot(math.Float64bits(270)), 0); err != nil {
		t.Fatal(err)
	}
	if err := public.rawSet(StringValue("k27"), NilValue()); err != nil {
		t.Fatal(err)
	}
	if err := public.rawSet(StringValue("k27"), NumberValue(270)); err != nil {
		t.Fatal(err)
	}

	step, err := arena.genericNext(table, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if step.cursor.key != machineTableStringKey(26) || math.Float64frombits(uint64(step.value)) != 26 {
		t.Fatalf("post-compaction step = %#v, want key/value 26", step)
	}
	publicKey, publicValue, err := public.rawNext(publicControl)
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := publicKey.String(); text != "k26" {
		t.Fatalf("public post-compaction key = %q, want k26", text)
	}
	if number, _ := publicValue.Number(); number != 26 {
		t.Fatalf("public post-compaction value = %v, want 26", number)
	}

	reinserted, err := arena.genericNext(table, step.cursor)
	if err != nil {
		t.Fatal(err)
	}
	if reinserted.cursor.key != machineTableStringKey(27) || math.Float64frombits(uint64(reinserted.value)) != 270 {
		t.Fatalf("delete/reinsert step = %#v, want key 27 value 270", reinserted)
	}
	publicReinsertedKey, publicReinsertedValue, err := public.rawNext(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := publicReinsertedKey.String(); text != "k27" {
		t.Fatalf("public delete/reinsert key = %q, want k27", text)
	}
	if number, _ := publicReinsertedValue.Number(); number != 270 {
		t.Fatalf("public delete/reinsert value = %v, want 270", number)
	}

	if err := arena.rawSetStopped(table, machineTableStringKey(27), slotNil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := arena.genericNext(table, reinserted.cursor); err != errMachineTableInvalidKey {
		t.Fatalf("resume after deleting prior Machine key error = %v", err)
	}
	if err := public.rawSet(StringValue("k27"), NilValue()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := public.rawNext(publicReinsertedKey); err == nil {
		t.Fatal("public rawNext accepted a deleted prior key")
	}
}

func TestMachinePrepareIteratorMatchesMetatableFallbackAndDefersCustomIter(t *testing.T) {
	var arena machineTableArena
	table, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	metatable, err := arena.newTableStopped(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(table, metatable); err != nil {
		t.Fatal(err)
	}
	tableSlot := mustMachineHandle(t, slotTagTable, uint32(table), 1)
	iterName := machineStringID(9)

	plan, err := arena.prepareIterator(tableSlot, iterName)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ready == 0 || plan.mode != machineIteratorGeneric || plan.generator != slotNativeID(nativeFuncNext) {
		t.Fatalf("metatable fallback iterator plan = %#v", plan)
	}

	publicTable, publicMetatable := NewTable(), NewTable()
	publicTable.setMetatable(publicMetatable)
	publicGenerator, publicState, publicControl, ok, err := prepareIterator(TableValue(publicTable), nil)
	if err != nil || !ok {
		t.Fatalf("public metatable fallback = ok %t, err %v", ok, err)
	}
	if valueNativeID(publicGenerator) != nativeFuncNext || publicState != TableValue(publicTable) || !publicControl.IsNil() {
		t.Fatalf("public metatable fallback = %#v %#v %#v", publicGenerator, publicState, publicControl)
	}

	callable := mustMachineHandle(t, slotTagClosure, 4, 2)
	if err := arena.rawSetStopped(metatable, machineTableStringKey(iterName), callable, 0); err != nil {
		t.Fatal(err)
	}
	plan, err = arena.prepareIterator(tableSlot, iterName)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ready != 0 || plan.action.kind != machineTableActionCall || plan.action.event != machineTableEventIter ||
		plan.action.table != table || plan.action.callable != callable {
		t.Fatalf("custom __iter plan = %#v", plan)
	}

	tableHandler := mustMachineTable(t, &arena)
	tableCallable := mustMachineHandle(t, slotTagTable, uint32(tableHandler), 1)
	if err := arena.rawSetStopped(metatable, machineTableStringKey(iterName), tableCallable, 0); err != nil {
		t.Fatal(err)
	}
	plan, err = arena.prepareIterator(tableSlot, iterName)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ready != 0 || plan.action.kind != machineTableActionCall || plan.action.event != machineTableEventIter ||
		plan.action.table != table || plan.action.value != tableSlot || plan.action.callable != tableCallable {
		t.Fatalf("table-valued __iter plan = %#v", plan)
	}
}

func TestMachineTableValuedIterMatchesOldVMCallChain(t *testing.T) {
	const source = `local object = {}
local first = {}
local second = {}
setmetatable(first, {__call = second})
setmetatable(second, {__call = function(secondReceiver, firstReceiver, iterated)
    if secondReceiver ~= second or firstReceiver ~= first or iterated ~= object then
        return nil, nil, nil
    end
    return next, {left = 19, right = 23}, nil
end})
setmetatable(object, {__iter = first})
local total = 0
for _, value in object do
    total = total + value
end
return total`

	// The legacy VM runs a table-valued __iter handler through its detached
	// metamethod path and does not charge the handler's bytecode to the caller's
	// instruction controller. The Machine keeps one owner-local controller and
	// intentionally charges all guest bytecode, so compare the observable
	// language result here without freezing that legacy accounting gap.
	assertMachineOwnerSemanticMatchesVM(t, source)
}

func TestMachineNonCallableTableValuedIterMatchesOldVMError(t *testing.T) {
	publicHandler := NewTable()
	publicMetatable := NewTable()
	if err := publicMetatable.rawSet(StringValue("__iter"), TableValue(publicHandler)); err != nil {
		t.Fatal(err)
	}
	publicObject := NewTable()
	publicObject.setMetatable(publicMetatable)
	if _, _, _, _, err := prepareIterator(TableValue(publicObject), nil); err == nil || !strings.Contains(err.Error(), "call target is table, want function") {
		t.Fatalf("public non-callable __iter error = %v", err)
	}

	const source = `local handler = {}
local object = setmetatable({}, {__iter = handler})
local count = 0
for _ in object do
    count = count + 1
end
return count`
	assertMachineOwnerDispatchMatchesVM(t, source, nil)
}

func assertMachineOwnerSemanticMatchesVM(t *testing.T, source string) {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	want, vmErr := Run(proto)

	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{source}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	machineErr := owner.executeRoot(0, nil)
	got, exportErr := owner.exportResults()
	if machineErr == nil {
		machineErr = exportErr
	}
	if difference := errorsEquivalent(vmErr, machineErr); difference != "" {
		t.Fatalf("errors differ: %s; VM=%v Machine=%v", difference, vmErr, machineErr)
	}
	if !valuesSliceEquivalent(want, got) {
		t.Fatalf("values differ: VM=%s Machine=%s", valuesDiagnostic(want), valuesDiagnostic(got))
	}
}

func TestMachineGetStringFieldIndexMatchesTableIndexChains(t *testing.T) {
	var arena machineTableArena
	base := mustMachineTable(t, &arena)
	baseMetatable := mustMachineTable(t, &arena)
	firstBacking := mustMachineTable(t, &arena)
	nested := mustMachineTable(t, &arena)
	nestedMetatable := mustMachineTable(t, &arena)
	valueBacking := mustMachineTable(t, &arena)
	if err := arena.setMetatableStopped(base, baseMetatable); err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(nested, nestedMetatable); err != nil {
		t.Fatal(err)
	}
	indexName, stockName, woodName := machineStringID(1), machineStringID(2), machineStringID(3)
	if err := arena.rawSetStopped(baseMetatable, machineTableStringKey(indexName), mustMachineHandle(t, slotTagTable, uint32(firstBacking), 1), 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(firstBacking, machineTableStringKey(stockName), mustMachineHandle(t, slotTagTable, uint32(nested), 1), 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(nestedMetatable, machineTableStringKey(indexName), mustMachineHandle(t, slotTagTable, uint32(valueBacking), 1), 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(valueBacking, machineTableStringKey(woodName), slotBool(true), 0); err != nil {
		t.Fatal(err)
	}

	action, continuation, err := arena.decideGetStringFieldIndex(base, stockName, machineTableStringKey(woodName), indexName)
	if err != nil {
		t.Fatal(err)
	}
	if continuation.active != 0 || action.kind != machineTableActionReturn || action.value != slotBool(true) {
		t.Fatalf("Machine GET_STRING_FIELD_INDEX decision = action %#v continuation %#v", action, continuation)
	}

	publicBase, publicBaseMetatable := NewTable(), NewTable()
	publicFirstBacking, publicNested := NewTable(), NewTable()
	publicNestedMetatable, publicValueBacking := NewTable(), NewTable()
	if err := publicBaseMetatable.rawSet(StringValue("__index"), TableValue(publicFirstBacking)); err != nil {
		t.Fatal(err)
	}
	if err := publicFirstBacking.rawSet(StringValue("stock"), TableValue(publicNested)); err != nil {
		t.Fatal(err)
	}
	if err := publicNestedMetatable.rawSet(StringValue("__index"), TableValue(publicValueBacking)); err != nil {
		t.Fatal(err)
	}
	if err := publicValueBacking.rawSet(StringValue("wood"), BoolValue(true)); err != nil {
		t.Fatal(err)
	}
	publicBase.setMetatable(publicBaseMetatable)
	publicNested.setMetatable(publicNestedMetatable)
	publicValue, err := getStringField2(publicTableAccess(), publicBase, "stock", StringValue("stock"), "wood", StringValue("wood"))
	if err != nil {
		t.Fatal(err)
	}
	if boolean, ok := publicValue.Bool(); !ok || !boolean {
		t.Fatalf("public nested index result = %#v", publicValue)
	}
}

func TestMachineStringFieldIndexDefersAndResumesFunctionHandlers(t *testing.T) {
	var arena machineTableArena
	base := mustMachineTable(t, &arena)
	baseMetatable := mustMachineTable(t, &arena)
	nested := mustMachineTable(t, &arena)
	nestedMetatable := mustMachineTable(t, &arena)
	backing := mustMachineTable(t, &arena)
	if err := arena.setMetatableStopped(base, baseMetatable); err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(nested, nestedMetatable); err != nil {
		t.Fatal(err)
	}
	indexName, newIndexName := machineStringID(1), machineStringID(2)
	stockName, woodName := machineStringID(3), machineStringID(4)
	firstCallable := mustMachineHandle(t, slotTagClosure, 11, 2)
	if err := arena.rawSetStopped(baseMetatable, machineTableStringKey(indexName), firstCallable, 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(nested, machineTableStringKey(woodName), slotBool(true), 0); err != nil {
		t.Fatal(err)
	}

	action, continuation, err := arena.decideGetStringFieldIndex(base, stockName, machineTableStringKey(woodName), indexName)
	if err != nil {
		t.Fatal(err)
	}
	if action.kind != machineTableActionCall || action.callable != firstCallable || continuation.active == 0 {
		t.Fatalf("first-level function decision = action %#v continuation %#v", action, continuation)
	}
	action, err = arena.resumeStringFieldIndex(continuation, mustMachineHandle(t, slotTagTable, uint32(nested), 1))
	if err != nil {
		t.Fatal(err)
	}
	if action.kind != machineTableActionReturn || action.value != slotBool(true) {
		t.Fatalf("resumed GET_STRING_FIELD_INDEX = %#v", action)
	}

	if err := arena.rawSetStopped(baseMetatable, machineTableStringKey(indexName), slotNil, 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(base, machineTableStringKey(stockName), mustMachineHandle(t, slotTagTable, uint32(nested), 1), 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(nested, machineTableStringKey(woodName), slotNil, 0); err != nil {
		t.Fatal(err)
	}
	secondCallable := mustMachineHandle(t, slotTagClosure, 12, 3)
	if err := arena.rawSetStopped(nestedMetatable, machineTableStringKey(indexName), secondCallable, 0); err != nil {
		t.Fatal(err)
	}
	action, continuation, err = arena.decideGetStringFieldIndex(base, stockName, machineTableStringKey(woodName), indexName)
	if err != nil {
		t.Fatal(err)
	}
	if continuation.active != 0 || action.kind != machineTableActionCall || action.event != machineTableEventIndex ||
		action.table != nested || action.callable != secondCallable {
		t.Fatalf("second-level __index function decision = action %#v continuation %#v", action, continuation)
	}

	if err := arena.rawSetStopped(nestedMetatable, machineTableStringKey(indexName), slotNil, 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(nestedMetatable, machineTableStringKey(newIndexName), mustMachineHandle(t, slotTagTable, uint32(backing), 1), 0); err != nil {
		t.Fatal(err)
	}
	action, continuation, err = arena.decideSetStringFieldIndex(
		base, stockName, machineTableStringKey(woodName), slotBool(false), indexName, newIndexName,
	)
	if err != nil {
		t.Fatal(err)
	}
	if continuation.active != 0 || action.kind != machineTableActionStore || action.table != backing || action.value != slotBool(false) {
		t.Fatalf("table __newindex decision = action %#v continuation %#v", action, continuation)
	}

	newIndexCallable := mustMachineHandle(t, slotTagClosure, 13, 4)
	if err := arena.rawSetStopped(nestedMetatable, machineTableStringKey(newIndexName), newIndexCallable, 0); err != nil {
		t.Fatal(err)
	}
	action, continuation, err = arena.decideSetStringFieldIndex(
		base, stockName, machineTableStringKey(woodName), slotBool(true), indexName, newIndexName,
	)
	if err != nil {
		t.Fatal(err)
	}
	if continuation.active != 0 || action.kind != machineTableActionCall || action.event != machineTableEventNewIndex ||
		action.table != nested || action.callable != newIndexCallable || action.value != slotBool(true) {
		t.Fatalf("function __newindex decision = action %#v continuation %#v", action, continuation)
	}

	publicBase, publicNested, publicBacking := NewTable(), NewTable(), NewTable()
	publicNestedMetatable := NewTable()
	if err := publicBase.rawSet(StringValue("stock"), TableValue(publicNested)); err != nil {
		t.Fatal(err)
	}
	if err := publicNestedMetatable.rawSet(StringValue("__newindex"), TableValue(publicBacking)); err != nil {
		t.Fatal(err)
	}
	publicNested.setMetatable(publicNestedMetatable)
	if err := setStringField2(publicTableAccess(), publicBase, "stock", StringValue("stock"), "wood", StringValue("wood"), BoolValue(false)); err != nil {
		t.Fatal(err)
	}
	publicValue, err := publicBacking.rawGet(StringValue("wood"))
	if err != nil {
		t.Fatal(err)
	}
	if boolean, ok := publicValue.Bool(); !ok || boolean {
		t.Fatalf("public nested __newindex table result = %#v", publicValue)
	}
}

func mustMachineHandle(t *testing.T, tag slotTag, index uint32, generation uint16) slot {
	t.Helper()
	value, err := slotPackHandle(tag, index, generation)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustMachineTable(t *testing.T, arena *machineTableArena) machineTableID {
	t.Helper()
	id, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
