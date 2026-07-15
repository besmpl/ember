package ember

import (
	"reflect"
	"strings"
	"testing"
)

func TestMachineMethodOneMatchesRawAndPrototypeLookup(t *testing.T) {
	var arena machineTableArena
	object := mustMachineMetatableTable(t, &arena)
	metatable := mustMachineMetatableTable(t, &arena)
	prototype := mustMachineMetatableTable(t, &arena)
	objectValue := mustMachineMetatableHandle(t, slotTagTable, uint32(object), 1)
	prototypeValue := mustMachineMetatableHandle(t, slotTagTable, uint32(prototype), 1)
	method := mustMachineMetatableHandle(t, slotTagClosure, 7, 2)
	indexName, callName, methodName := machineStringID(1), machineStringID(2), machineStringID(3)

	if err := arena.rawSetStopped(object, machineTableStringKey(methodName), method, 0); err != nil {
		t.Fatal(err)
	}
	action, err := arena.prepareMethodOne(objectValue, methodName, indexName, callName)
	if err != nil {
		t.Fatal(err)
	}
	if action.kind != machineMethodOneScript || action.callee != method || action.receiver != objectValue {
		t.Fatalf("raw method action = %#v", action)
	}

	if err := arena.rawSetStopped(object, machineTableStringKey(methodName), slotNil, 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(object, metatable); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(metatable, machineTableStringKey(indexName), prototypeValue, 0); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(prototype, machineTableStringKey(methodName), method, 0); err != nil {
		t.Fatal(err)
	}
	action, err = arena.prepareMethodOne(objectValue, methodName, indexName, callName)
	if err != nil {
		t.Fatal(err)
	}
	if action.kind != machineMethodOneScript || action.callee != method || action.receiver != objectValue {
		t.Fatalf("prototype method action = %#v", action)
	}

	proto, err := Compile(`
local prototype = {}
function prototype:add(amount)
	return self.base + amount
end
local object = setmetatable({base = 40}, {__index = prototype})
return object:add(3)
`)
	if err != nil {
		t.Fatal(err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("public prototype method returned %d results, want 1", len(results))
	}
	got, ok := results[0].Number()
	if !ok {
		t.Fatalf("public prototype method result is %s, want number", results[0].Kind())
	}
	if got != 43 {
		t.Fatalf("public prototype method result = %v, want 43", got)
	}
}

func TestMachineMethodOneDefersIndexAndClassifiesHostAndCallMetamethods(t *testing.T) {
	var arena machineTableArena
	object := mustMachineMetatableTable(t, &arena)
	metatable := mustMachineMetatableTable(t, &arena)
	callableTable := mustMachineMetatableTable(t, &arena)
	callableMetatable := mustMachineMetatableTable(t, &arena)
	objectValue := mustMachineMetatableHandle(t, slotTagTable, uint32(object), 1)
	callableValue := mustMachineMetatableHandle(t, slotTagTable, uint32(callableTable), 1)
	indexHandler := mustMachineMetatableHandle(t, slotTagClosure, 8, 2)
	callHandler := mustMachineMetatableHandle(t, slotTagClosure, 9, 2)
	hostMethod := mustMachineMetatableHandle(t, slotTagHostCallable, 2, 1)
	indexName, callName, methodName := machineStringID(1), machineStringID(2), machineStringID(3)

	if err := arena.setMetatableStopped(object, metatable); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(metatable, machineTableStringKey(indexName), indexHandler, 0); err != nil {
		t.Fatal(err)
	}
	action, err := arena.prepareMethodOne(objectValue, methodName, indexName, callName)
	if err != nil {
		t.Fatal(err)
	}
	if action.kind != machineMethodOneLookup || action.receiver != objectValue {
		t.Fatalf("deferred method action = %#v", action)
	}
	arguments, err := machineTableActionArguments(action.lookup)
	if err != nil || arguments.count != 2 || arguments.first != objectValue || arguments.second != mustMachineMetatableHandle(t, slotTagString, uint32(methodName), 1) {
		t.Fatalf("__index arguments = %#v, err %v", arguments, err)
	}
	action, err = arena.resumeMethodOneLookup(action, hostMethod, 1)
	if err != nil || action.kind != machineMethodOneHost || action.callee != hostMethod || action.receiver != objectValue {
		t.Fatalf("resumed host method = %#v, err %v", action, err)
	}

	action, err = arena.prepareMethodOne(objectValue, methodName, indexName, callName)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(callableTable, callableMetatable); err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetStopped(callableMetatable, machineTableStringKey(callName), callHandler, 0); err != nil {
		t.Fatal(err)
	}
	action, err = arena.resumeMethodOneLookup(action, callableValue, 1)
	if err != nil || action.kind != machineMethodOneMetamethod || action.callee != callableValue || action.metamethod != callHandler || action.receiver != objectValue {
		t.Fatalf("resumed __call method = %#v, err %v", action, err)
	}
	if got := machineMethodOneResult(slotBool(true), 0); got != slotNil {
		t.Fatalf("zero-result method adjustment = %#x, want nil", got)
	}
}

func TestMachineMetatableAndMethodContractsArePointerFreeAndPreserveErrors(t *testing.T) {
	for _, value := range []any{
		machineSetMetatablePlan{},
		machineMetatableGuard{},
		machineTableCallArguments{},
		machineMethodOneAction{},
	} {
		if machineTableTypeHasPointers(reflect.TypeOf(value)) {
			t.Fatalf("%T contains a Go pointer", value)
		}
	}

	var arena machineTableArena
	if _, err := arena.prepareMethodOne(slotBool(true), 1, 2, 3); err == nil || err.Error() != "run: get field target is boolean, want table" {
		t.Fatalf("non-table receiver error = %v", err)
	}
	object := mustMachineMetatableTable(t, &arena)
	objectValue := mustMachineMetatableHandle(t, slotTagTable, uint32(object), 1)
	if _, err := arena.prepareMethodOne(objectValue, 1, 2, 3); err == nil || !strings.Contains(err.Error(), "call target is nil, want function") {
		t.Fatalf("missing method error = %v", err)
	}
	if _, err := arena.prepareSetMetatable(slotBool(true), slotNil, 1); err == nil || err.Error() != "setmetatable: argument #1 is boolean, want table" {
		t.Fatalf("setmetatable argument error = %v", err)
	}
}
