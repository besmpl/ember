package ember

import (
	"errors"
	"testing"
)

func TestMachineSetAndGetMetatableMatchProtectionSemantics(t *testing.T) {
	var arena machineTableArena
	object := mustMachineMetatableTable(t, &arena)
	metatable := mustMachineMetatableTable(t, &arena)
	objectValue := mustMachineMetatableHandle(t, slotTagTable, uint32(object), 1)
	metatableValue := mustMachineMetatableHandle(t, slotTagTable, uint32(metatable), 1)

	plan, err := arena.prepareSetMetatable(objectValue, metatableValue, 2)
	if err != nil {
		t.Fatal(err)
	}
	result, err := arena.applySetMetatableStopped(plan)
	if err != nil {
		t.Fatal(err)
	}
	if result != objectValue {
		t.Fatalf("setmetatable result = %#x, want object %#x", result, objectValue)
	}
	got, err := arena.getMetatableValue(objectValue, 1)
	if err != nil || got != metatableValue {
		t.Fatalf("getmetatable = %#x, err %v; want %#x", got, err, metatableValue)
	}

	protection := slotBool(true)
	if err := arena.setMetatableProtectionStopped(metatable, protection); err != nil {
		t.Fatal(err)
	}
	got, err = arena.getMetatableValue(objectValue, 1)
	if err != nil || got != protection {
		t.Fatalf("protected getmetatable = %#x, err %v; want %#x", got, err, protection)
	}
	if _, err := arena.prepareSetMetatable(objectValue, slotNil, 2); !errors.Is(err, errMachineTableProtectedMetatable) {
		t.Fatalf("protected clear error = %v", err)
	}

	publicObject, publicMetatable := NewTable(), NewTable()
	publicMetatable.setRawStringField("__metatable", BoolValue(true))
	if _, err := baseSetMetatable(nil, []Value{TableValue(publicObject), TableValue(publicMetatable)}); err != nil {
		t.Fatal(err)
	}
	publicGot, err := baseGetMetatable(nil, []Value{TableValue(publicObject)})
	if err != nil || len(publicGot) != 1 || publicGot[0] != BoolValue(true) {
		t.Fatalf("public protected getmetatable = %#v, err %v", publicGot, err)
	}
}

func TestMachineMetatableWritesInvalidateGuardsAndJumpPredicate(t *testing.T) {
	var arena machineTableArena
	object := mustMachineMetatableTable(t, &arena)
	metatable := mustMachineMetatableTable(t, &arena)
	objectValue := mustMachineMetatableHandle(t, slotTagTable, uint32(object), 1)
	if jump, err := arena.jumpIfTableHasMetatable(objectValue); err != nil || jump {
		t.Fatalf("jump without metatable = %t, err %v", jump, err)
	}
	if jump, err := arena.jumpIfTableHasMetatable(slotBool(true)); err != nil || jump {
		t.Fatalf("jump for boolean = %t, err %v", jump, err)
	}
	if err := arena.setMetatableStopped(object, metatable); err != nil {
		t.Fatal(err)
	}
	if jump, err := arena.jumpIfTableHasMetatable(objectValue); err != nil || !jump {
		t.Fatalf("jump with metatable = %t, err %v", jump, err)
	}

	guard, err := arena.metatableGuard(object)
	if err != nil {
		t.Fatal(err)
	}
	if valid, err := arena.metatableGuardValid(guard); err != nil || !valid {
		t.Fatalf("fresh guard valid = %t, err %v", valid, err)
	}
	indexName, protectionName := machineStringID(1), machineStringID(2)
	if err := arena.rawSetMetatableAwareStopped(metatable, machineTableStringKey(indexName), slotBool(true), protectionName, 0); err != nil {
		t.Fatal(err)
	}
	if valid, err := arena.metatableGuardValid(guard); err != nil || valid {
		t.Fatalf("dirty __index guard valid = %t, err %v", valid, err)
	}

	guard, err = arena.metatableGuard(object)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.rawSetMetatableAwareStopped(metatable, machineTableStringKey(protectionName), slotBool(false), protectionName, 0); err != nil {
		t.Fatal(err)
	}
	protection, protected, err := arena.protectedMetatable(object)
	if err != nil || !protected || protection != slotBool(false) {
		t.Fatalf("dirty __metatable protection = %#x (%t), err %v", protection, protected, err)
	}
	if valid, err := arena.metatableGuardValid(guard); err != nil || valid {
		t.Fatalf("dirty __metatable guard valid = %t, err %v", valid, err)
	}
}

func mustMachineMetatableTable(t *testing.T, arena *machineTableArena) machineTableID {
	t.Helper()
	id, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustMachineMetatableHandle(t *testing.T, tag slotTag, index uint32, generation uint16) slot {
	t.Helper()
	value, err := slotPackHandle(tag, index, generation)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
