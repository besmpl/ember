package ember

import (
	"errors"
	"reflect"
	"testing"
)

func TestMachineClosureArenaLayout(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(machineProtoID(0)),
		reflect.TypeOf(machineCellID(0)),
		reflect.TypeOf(machineClosureID(0)),
		reflect.TypeOf(machineCaptureMode(0)),
		reflect.TypeOf(slot(0)),
	} {
		if typ.Kind() != reflect.Uint8 && typ.Kind() != reflect.Uint32 && typ.Kind() != reflect.Uint64 {
			t.Fatalf("scalar type %s is not pointer-free", typ)
		}
	}
	for _, typ := range []reflect.Type{reflect.TypeOf(machineClosureRecord{}), reflect.TypeOf(machineCellRecord{}), reflect.TypeOf(machineOpenCellRecord{}), reflect.TypeOf(machineCellHandle{}), reflect.TypeOf(machineClosureHandle{})} {
		for index := 0; index < typ.NumField(); index++ {
			kind := typ.Field(index).Type.Kind()
			if kind != reflect.Uint8 && kind != reflect.Uint16 && kind != reflect.Uint32 && kind != reflect.Uint64 {
				t.Fatalf("%s field %s kind=%s is not scalar", typ, typ.Field(index).Name, kind)
			}
		}
	}
}

func TestMachineClosureArenaChecksModuleLocalPrototypeBounds(t *testing.T) {
	var arena machineClosureArena
	if err := arena.bindModulesWithOwnerStopped(41, []uint32{1, 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := arena.createClosureStopped(0, 1, nil); !errors.Is(err, errMachineClosureProto) {
		t.Fatalf("module 0 proto 1 err=%v, want module-local prototype rejection", err)
	}
	if _, err := arena.createClosureStopped(1, 2, nil); err != nil {
		t.Fatalf("module 1 proto 2: %v", err)
	}
}

func TestMachineClosureArenaCreatesSharedAndByValueCaptures(t *testing.T) {
	var arena machineClosureArena
	if err := arena.bindStopped(2, 4); err != nil {
		t.Fatal(err)
	}
	shared, err := arena.newCellStopped(slot(7))
	if err != nil {
		t.Fatal(err)
	}
	first, err := arena.createClosureStopped(1, 2, []machineCaptureDescriptor{
		{mode: machineCaptureShared, cell: shared},
		{mode: machineCaptureByValue, value: slot(11)},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := arena.createClosureStopped(1, 3, []machineCaptureDescriptor{{mode: machineCaptureShared, cell: shared}})
	if err != nil {
		t.Fatal(err)
	}
	firstRecord, err := arena.closureRecord(first)
	if err != nil {
		t.Fatal(err)
	}
	if firstRecord.module != 1 || firstRecord.proto != 2 || firstRecord.captureCount != 2 || first.index == 0 {
		t.Fatalf("first closure record=%#v handle=%#v", firstRecord, first)
	}
	firstShared, err := arena.captureCell(first, 0)
	if err != nil {
		t.Fatal(err)
	}
	secondShared, err := arena.captureCell(second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if firstShared.index != shared.index || secondShared.index != shared.index {
		t.Fatalf("shared cell IDs first=%#v second=%#v source=%#v", firstShared, secondShared, shared)
	}
	if err := arena.cellSet(firstShared, slot(42)); err != nil {
		t.Fatal(err)
	}
	if value, err := arena.cellGet(secondShared); err != nil || value != slot(42) {
		t.Fatalf("shared cell through second closure=%v err=%v, want 42", value, err)
	}
	firstByValue, err := arena.captureCell(first, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.cellSet(firstByValue, slot(99)); err != nil {
		t.Fatal(err)
	}
	if value, err := arena.cellGet(firstByValue); err != nil || value != slot(99) {
		t.Fatalf("by-value cell=%v err=%v, want 99", value, err)
	}
	if firstByValue.index == shared.index {
		t.Fatal("by-value capture unexpectedly aliases shared cell")
	}
}

func TestMachineClosureArenaRejectsOpenAndInvalidCaptures(t *testing.T) {
	var arena machineClosureArena
	if err := arena.bindStopped(1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := arena.createClosureStopped(0, 0, []machineCaptureDescriptor{{mode: machineCaptureOpen}}); !errors.Is(err, errMachineClosureOpen) {
		t.Fatalf("open capture err=%v, want unsupported error", err)
	}
	if _, err := arena.createClosureStopped(1, 0, nil); !errors.Is(err, errMachineClosureModule) {
		t.Fatalf("invalid module err=%v, want module error", err)
	}
	if _, err := arena.createClosureStopped(0, 1, nil); !errors.Is(err, errMachineClosureProto) {
		t.Fatalf("invalid proto err=%v, want proto error", err)
	}
	foreign := machineCellHandle{owner: 999, index: 1, generation: 1}
	if _, err := arena.createClosureStopped(0, 0, []machineCaptureDescriptor{{mode: machineCaptureShared, cell: foreign}}); !errors.Is(err, errMachineClosureCapture) {
		t.Fatalf("foreign capture err=%v, want capture error", err)
	}
}

func TestMachineClosureArenaChecksHandlesAndOwners(t *testing.T) {
	var first, second machineClosureArena
	if err := first.bindStopped(1, 1); err != nil {
		t.Fatal(err)
	}
	if err := second.bindStopped(1, 1); err != nil {
		t.Fatal(err)
	}
	cell, err := first.newCellStopped(slotBool(true))
	if err != nil {
		t.Fatal(err)
	}
	closure, err := first.createClosureStopped(0, 0, []machineCaptureDescriptor{{mode: machineCaptureShared, cell: cell}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.cellGet(cell); !errors.Is(err, errMachineClosureOwner) {
		t.Fatalf("foreign cell get err=%v, want owner error", err)
	}
	if _, err := first.closureRecord(machineClosureHandle{owner: first.owner, index: closure.index, generation: 2}); !errors.Is(err, errMachineClosureGeneration) {
		t.Fatalf("stale closure generation err=%v, want generation error", err)
	}
	if _, err := first.captureCell(closure, -1); !errors.Is(err, errMachineClosureIndex) {
		t.Fatalf("negative capture index err=%v, want index error", err)
	}
	if _, err := first.captureCell(closure, 1); !errors.Is(err, errMachineClosureIndex) {
		t.Fatalf("large capture index err=%v, want index error", err)
	}
}

func TestMachineClosureArenaResetCloseAndIndependentStorage(t *testing.T) {
	var first, second machineClosureArena
	if err := first.bindStopped(1, 1); err != nil {
		t.Fatal(err)
	}
	if err := second.bindStopped(1, 1); err != nil {
		t.Fatal(err)
	}
	cell, err := first.newCellStopped(slot(5))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.cellGet(cell); !errors.Is(err, errMachineClosureOwner) {
		t.Fatalf("cross-owner cell err=%v, want owner error", err)
	}
	closureCapacity, cellCapacity := cap(first.closures), cap(first.cells)
	oldOwner := first.owner
	first.reset()
	if first.closed || first.owner == oldOwner || len(first.closures) != 0 || len(first.cells) != 0 || len(first.captureCells) != 0 || len(first.openCells) != 0 {
		t.Fatalf("reset state=%#v, want cleared/new owner", first)
	}
	if cap(first.closures) != closureCapacity || cap(first.cells) != cellCapacity {
		t.Fatalf("reset discarded capacities closures=%d/%d cells=%d/%d", cap(first.closures), closureCapacity, cap(first.cells), cellCapacity)
	}
	if _, err := first.createClosureStopped(0, 0, nil); !errors.Is(err, errMachineClosureModule) {
		t.Fatalf("reset without rebind err=%v, want module-bound error", err)
	}
	if err := first.bindStopped(1, 1); err != nil {
		t.Fatal(err)
	}
	first.close()
	if !first.closed || first.owner != 0 || first.closures != nil || first.cells != nil || first.captureCells != nil || first.openCells != nil {
		t.Fatalf("close state=%#v", first)
	}
	if _, err := first.cellGet(cell); !errors.Is(err, errMachineClosureArenaClosed) {
		t.Fatalf("closed cell get err=%v, want closed error", err)
	}
	first.close()
}
