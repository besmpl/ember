package ember

import (
	"errors"
	"reflect"
	"testing"
)

func TestMachineCoroutineArenaLayoutIsPointerFree(t *testing.T) {
	for _, value := range []any{
		machineCoroutineHandle{},
		machineCoroutineRoot{},
		machineCoroutineFrameState{},
		machineCoroutineOpenCell{},
		machineCoroutineRecord{},
		machineCoroutineAction{},
	} {
		if machineTestTypeContainsPointers(reflect.TypeOf(value)) {
			t.Fatalf("%T contains a Go pointer", value)
		}
	}
	for _, element := range []reflect.Type{
		reflect.TypeOf([]machineCoroutineRecord{}).Elem(),
		reflect.TypeOf([]slot{}).Elem(),
		reflect.TypeOf([]uint64{}).Elem(),
		reflect.TypeOf([]machineContinuation{}).Elem(),
		reflect.TypeOf([]machineCoroutineOpenCell{}).Elem(),
	} {
		if machineTestTypeContainsPointers(element) {
			t.Fatalf("coroutine arena element %s contains a Go pointer", element)
		}
	}
}

func TestMachineCoroutineArenaLifecycleLimitAndHandles(t *testing.T) {
	var arena machineCoroutineArena
	if err := arena.bindStopped(41, 1); err != nil {
		t.Fatal(err)
	}
	root := machineCoroutineRoot{module: 2, proto: 3, closure: machineClosureHandle{owner: 41, index: 7, generation: 1}}
	handle, err := arena.createStopped(root)
	if err != nil {
		t.Fatal(err)
	}
	if status, err := arena.statusStopped(handle); err != nil || status.String() != string(vmCoroutineSuspended) {
		t.Fatalf("created status=%v err=%v", status, err)
	}
	if _, err := arena.createStopped(root); err == nil {
		t.Fatal("second live coroutine unexpectedly bypassed MaxCoroutines")
	} else {
		var limit *LimitError
		if !errors.As(err, &limit) || limit.Kind != LimitCoroutines || limit.Used != 2 {
			t.Fatalf("limit error=%v", err)
		}
	}
	if _, _, depth, err := arena.beginResumeStopped(handle); err != nil || depth != 1 {
		t.Fatalf("begin depth=%d err=%v", depth, err)
	}
	if _, _, _, err := arena.beginResumeStopped(handle); !errors.Is(err, errMachineCoroutineRunning) {
		t.Fatalf("reentry err=%v", err)
	}
	snapshot := machineCoroutineSnapshot{
		frame: machineCoroutineFrameState{
			module: 2, proto: 3, pc: 9, base: 0, resumeRegister: 2, resumeCount: 1, callDepth: 2,
			resume: machineSemanticResume{kind: machineResumeGetStringFieldIndex, temporaryReg: 7, stackLength: 11},
		},
		registers:     []slot{slotBool(true), slotNil},
		numberBits:    []uint64{11, 12},
		continuations: []machineContinuation{{moduleID: 2, protoID: 3, returnPC: 8}},
		openCells:     []machineCoroutineOpenCell{{register: 1, cell: 1, generation: 1}},
	}
	if err := arena.suspendStopped(handle, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, _, depth, err := arena.beginResumeStopped(handle); err != nil || depth != 2 {
		t.Fatalf("resumed depth=%d err=%v", depth, err)
	}
	var restored machineCoroutineSnapshot
	if err := arena.snapshotStopped(handle, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, snapshot) {
		t.Fatalf("restored=%#v, want %#v", restored, snapshot)
	}
	if _, err := arena.completeStopped(handle); err != nil {
		t.Fatal(err)
	}
	if status, err := arena.statusStopped(handle); err != nil || status.String() != string(vmCoroutineDead) {
		t.Fatalf("completed status=%v err=%v", status, err)
	}
	if err := arena.releaseDeadStopped(handle); err != nil {
		t.Fatal(err)
	}
	reused, err := arena.createStopped(root)
	if err != nil {
		t.Fatal(err)
	}
	if reused.index != handle.index || reused.generation == handle.generation {
		t.Fatalf("reused handle=%#v, old=%#v", reused, handle)
	}
	if _, err := arena.statusStopped(handle); !errors.Is(err, errMachineCoroutineGeneration) {
		t.Fatalf("stale handle err=%v", err)
	}
	foreign := reused
	foreign.owner++
	if _, err := arena.statusStopped(foreign); !errors.Is(err, errMachineCoroutineOwner) {
		t.Fatalf("cross-owner err=%v", err)
	}
	arena.close()
	if _, err := arena.statusStopped(reused); !errors.Is(err, errMachineCoroutineArenaClosed) {
		t.Fatalf("closed status err=%v", err)
	}
}

func TestMachineCoroutineArenaReusesSnapshotSpansAcrossYields(t *testing.T) {
	var arena machineCoroutineArena
	if err := arena.bindStopped(51, 1); err != nil {
		t.Fatal(err)
	}
	handle, err := arena.createStopped(machineCoroutineRoot{
		proto: 1, closure: machineClosureHandle{owner: 51, index: 1, generation: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := arena.beginResumeStopped(handle); err != nil {
		t.Fatal(err)
	}
	snapshot := machineCoroutineSnapshot{
		frame:         machineCoroutineFrameState{proto: 1, pc: 3, callDepth: 2},
		registers:     []slot{slotBool(true), slotNil, slotBool(false)},
		numberBits:    []uint64{1, 2, 3},
		continuations: []machineContinuation{{protoID: 1, returnPC: 2}},
		openCells:     []machineCoroutineOpenCell{{register: 1, cell: 1, generation: 1}},
	}
	var restored machineCoroutineSnapshot
	var firstLengths [4]int
	for cycle := range 64 {
		if err := arena.suspendStopped(handle, snapshot); err != nil {
			t.Fatalf("suspend cycle %d: %v", cycle, err)
		}
		if cycle == 0 {
			firstLengths = [4]int{len(arena.registers), len(arena.numberBits), len(arena.continuations), len(arena.openCells)}
		}
		if _, _, _, err := arena.beginResumeStopped(handle); err != nil {
			t.Fatalf("begin cycle %d: %v", cycle, err)
		}
		if err := arena.snapshotStopped(handle, &restored); err != nil {
			t.Fatalf("snapshot cycle %d: %v", cycle, err)
		}
	}
	gotLengths := [4]int{len(arena.registers), len(arena.numberBits), len(arena.continuations), len(arena.openCells)}
	if gotLengths != firstLengths {
		t.Fatalf("snapshot arena lengths after repeated yields = %v, want bounded %v", gotLengths, firstLengths)
	}
}
