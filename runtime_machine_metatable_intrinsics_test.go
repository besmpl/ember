package ember

import (
	"reflect"
	"testing"
)

func TestMachineGuardedMetatableIntrinsicsPreserveIdentityNilAndGuardMiss(t *testing.T) {
	var arena machineTableArena
	object := mustMachineMetatableTable(t, &arena)
	metatable := mustMachineMetatableTable(t, &arena)
	objectValue := mustMachineMetatableHandle(t, slotTagTable, uint32(object), 1)
	metatableValue := mustMachineMetatableHandle(t, slotTagTable, uint32(metatable), 1)

	miss, err := runMachineGuardedMetatableIntrinsicStopped(&arena, machineMetatableIntrinsicRequest{
		nativeID:      nativeFuncSetMetatable,
		callee:        slotNativeID(nativeFuncGetMetatable),
		first:         objectValue,
		second:        metatableValue,
		argumentCount: 2,
	})
	if err != nil || miss.matched {
		t.Fatalf("identity guard miss = %#v, err %v", miss, err)
	}
	if got, err := arena.metatable(object); err != nil || got != invalidMachineTableID {
		t.Fatalf("guard miss metatable = %d, err %v", got, err)
	}

	set, err := runMachineGuardedMetatableIntrinsicStopped(&arena, machineMetatableIntrinsicRequest{
		nativeID:      nativeFuncSetMetatable,
		callee:        slotNativeID(nativeFuncSetMetatable),
		first:         objectValue,
		second:        metatableValue,
		argumentCount: 2,
	})
	if err != nil || !set.matched || set.resultCount != 1 || set.value != objectValue {
		t.Fatalf("setmetatable outcome = %#v, err %v", set, err)
	}
	get, err := runMachineGuardedMetatableIntrinsicStopped(&arena, machineMetatableIntrinsicRequest{
		nativeID:      nativeFuncGetMetatable,
		callee:        slotNativeID(nativeFuncGetMetatable),
		first:         objectValue,
		argumentCount: 1,
	})
	if err != nil || !get.matched || get.resultCount != 1 || get.value != metatableValue {
		t.Fatalf("getmetatable outcome = %#v, err %v", get, err)
	}

	clear, err := runMachineGuardedMetatableIntrinsicStopped(&arena, machineMetatableIntrinsicRequest{
		nativeID:      nativeFuncSetMetatable,
		callee:        slotNativeID(nativeFuncSetMetatable),
		first:         objectValue,
		second:        slotNil,
		argumentCount: 2,
	})
	if err != nil || clear.value != objectValue {
		t.Fatalf("clear metatable outcome = %#v, err %v", clear, err)
	}
	get, err = runMachineGuardedMetatableIntrinsicStopped(&arena, machineMetatableIntrinsicRequest{
		nativeID:      nativeFuncGetMetatable,
		callee:        slotNativeID(nativeFuncGetMetatable),
		first:         objectValue,
		argumentCount: 1,
	})
	if err != nil || get.value != slotNil || get.resultCount != 1 {
		t.Fatalf("getmetatable after clear = %#v, err %v", get, err)
	}
}

func TestMachineGuardedMetatableIntrinsicsMatchProtectionAndErrors(t *testing.T) {
	for _, value := range []any{machineMetatableIntrinsicRequest{}, machineMetatableIntrinsicOutcome{}} {
		if machineTableTypeHasPointers(reflect.TypeOf(value)) {
			t.Fatalf("%T contains a Go pointer", value)
		}
	}

	var arena machineTableArena
	guardMiss, err := runMachineGuardedMetatableIntrinsicStopped(&arena, machineMetatableIntrinsicRequest{
		nativeID:      nativeFuncSetMetatable,
		callee:        slotBool(true),
		first:         slotBool(false),
		argumentCount: 1,
	})
	if err != nil || guardMiss.matched {
		t.Fatalf("invalid-argument guard miss = %#v, err %v", guardMiss, err)
	}
	errorObject := mustMachineMetatableTable(t, &arena)
	errorObjectValue := mustMachineMetatableHandle(t, slotTagTable, uint32(errorObject), 1)

	tests := []struct {
		name          string
		nativeID      nativeFuncID
		first         slot
		second        slot
		argumentCount uint32
		base          func(*globalEnv, []Value) ([]Value, error)
		baseArgs      []Value
	}{
		{name: "set missing", nativeID: nativeFuncSetMetatable, first: slotNil, argumentCount: 0, base: baseSetMetatable},
		{name: "set first kind", nativeID: nativeFuncSetMetatable, first: slotBool(true), argumentCount: 1, base: baseSetMetatable, baseArgs: []Value{BoolValue(true)}},
		{name: "set second kind", nativeID: nativeFuncSetMetatable, first: errorObjectValue, second: slotBool(true), argumentCount: 2, base: baseSetMetatable, baseArgs: []Value{TableValue(NewTable()), BoolValue(true)}},
		{name: "get missing", nativeID: nativeFuncGetMetatable, first: slotNil, argumentCount: 0, base: baseGetMetatable},
		{name: "get first kind", nativeID: nativeFuncGetMetatable, first: slotBool(false), argumentCount: 1, base: baseGetMetatable, baseArgs: []Value{BoolValue(false)}},
	}
	for _, test := range tests {
		_, gotErr := runMachineGuardedMetatableIntrinsicStopped(&arena, machineMetatableIntrinsicRequest{
			nativeID:      test.nativeID,
			callee:        slotNativeID(test.nativeID),
			first:         test.first,
			second:        test.second,
			argumentCount: test.argumentCount,
		})
		_, wantErr := test.base(nil, test.baseArgs)
		if gotErr == nil || wantErr == nil || gotErr.Error() != wantErr.Error() {
			t.Fatalf("%s error = %v, want %v", test.name, gotErr, wantErr)
		}
	}

	object := mustMachineMetatableTable(t, &arena)
	protectedMetatable := mustMachineMetatableTable(t, &arena)
	replacement := mustMachineMetatableTable(t, &arena)
	objectValue := mustMachineMetatableHandle(t, slotTagTable, uint32(object), 1)
	if err := arena.setMetatableStopped(object, protectedMetatable); err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableProtectionStopped(protectedMetatable, slotBool(false)); err != nil {
		t.Fatal(err)
	}
	get, err := runMachineGuardedMetatableIntrinsicStopped(&arena, machineMetatableIntrinsicRequest{
		nativeID: nativeFuncGetMetatable, callee: slotNativeID(nativeFuncGetMetatable),
		first: objectValue, argumentCount: 1,
	})
	if err != nil || get.value != slotBool(false) {
		t.Fatalf("protected getmetatable = %#v, err %v", get, err)
	}
	_, err = runMachineGuardedMetatableIntrinsicStopped(&arena, machineMetatableIntrinsicRequest{
		nativeID: nativeFuncSetMetatable, callee: slotNativeID(nativeFuncSetMetatable),
		first: objectValue, second: mustMachineMetatableHandle(t, slotTagTable, uint32(replacement), 1), argumentCount: 2,
	})
	if err == nil || err.Error() != "setmetatable: cannot change protected metatable" {
		t.Fatalf("protected setmetatable error = %v", err)
	}
	if got, lookupErr := arena.metatable(object); lookupErr != nil || got != protectedMetatable {
		t.Fatalf("protected set changed metatable to %d, err %v", got, lookupErr)
	}
}
