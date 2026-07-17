package ember

import (
	"errors"
	"reflect"
	"testing"
)

func TestMachineModuleArenaLayout(t *testing.T) {
	for _, element := range []reflect.Type{
		reflect.TypeOf(machineModuleLoadState(0)),
		reflect.TypeOf(slot(0)),
		reflect.TypeOf(programModuleID(0)),
	} {
		if element.Kind() != reflect.Uint8 && element.Kind() != reflect.Uint32 && element.Kind() != reflect.Uint64 {
			t.Fatalf("module storage element %s is not pointer-free", element)
		}
	}
	for _, field := range []string{"states", "exports", "loading"} {
		typeOfField, ok := reflect.TypeOf(machineModuleArena{}).FieldByName(field)
		if !ok || typeOfField.Type.Kind() != reflect.Slice {
			t.Fatalf("arena field %s missing or not scalar slice", field)
		}
	}
}

func TestMachineModuleArenaTransitionsAndCachedNilExport(t *testing.T) {
	var arena machineModuleArena
	if err := arena.bindStopped(3); err != nil {
		t.Fatal(err)
	}
	state, err := arena.state(0)
	if err != nil || state != machineModuleUnloaded {
		t.Fatalf("initial state=%v err=%v, want unloaded", state, err)
	}
	if export, cached, err := arena.begin(0); err != nil || cached || export != slotNil {
		t.Fatalf("begin 0 export=%v cached=%t err=%v", export, cached, err)
	}
	if depth := arena.loadingDepth(); depth != 1 {
		t.Fatalf("loading depth=%d, want 1", depth)
	}
	if export, cached, err := arena.begin(1); err != nil || cached || export != slotNil {
		t.Fatalf("begin 1 export=%v cached=%t err=%v", export, cached, err)
	}
	if err := arena.finish(1, slotBool(true)); err != nil {
		t.Fatal(err)
	}
	if err := arena.finish(0, slotNil); err != nil {
		t.Fatal(err)
	}
	if depth := arena.loadingDepth(); depth != 0 {
		t.Fatalf("loading depth=%d after finish, want 0", depth)
	}
	if export, cached, err := arena.export(0); err != nil || !cached || export != slotNil {
		t.Fatalf("cached nil export=%v cached=%t err=%v", export, cached, err)
	}
	if export, cached, err := arena.begin(0); err != nil || !cached || export != slotNil {
		t.Fatalf("loaded begin export=%v cached=%t err=%v", export, cached, err)
	}
}

func TestMachineModuleArenaDetectsCyclesAndCompletesIndependentLoadsByName(t *testing.T) {
	var arena machineModuleArena
	if err := arena.bindStopped(3); err != nil {
		t.Fatal(err)
	}
	if _, _, err := arena.begin(0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := arena.begin(1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := arena.begin(0); !errors.Is(err, errMachineModuleCycle) {
		t.Fatalf("cycle begin err=%v, want cycle error", err)
	}
	if err := arena.finish(0, slotNil); err != nil {
		t.Fatalf("finish independent load 0: %v", err)
	}
	if depth := arena.loadingDepth(); depth != 1 {
		t.Fatalf("loading depth after finish = %d, want 1", depth)
	}
	if err := arena.abort(1); err != nil {
		t.Fatal(err)
	}
	if state, err := arena.state(0); err != nil || state != machineModuleLoaded {
		t.Fatalf("finished state=%v err=%v, want loaded", state, err)
	}
	if export, present, err := arena.export(0); err != nil || !present || export != slotNil {
		t.Fatalf("finished export=%v present=%t err=%v, want cached nil", export, present, err)
	}
	if state, err := arena.state(1); err != nil || state != machineModuleUnloaded {
		t.Fatalf("aborted state=%v err=%v, want unloaded", state, err)
	}
}

func TestMachineModuleArenaChecksBoundsAndInvalidTransitions(t *testing.T) {
	var arena machineModuleArena
	if err := arena.bindStopped(1); err != nil {
		t.Fatal(err)
	}
	for _, id := range []programModuleID{1, programModuleID(^uint32(0))} {
		if _, err := arena.state(id); !errors.Is(err, errMachineModuleIndexInvalid) {
			t.Fatalf("state(%d) err=%v, want bounds error", id, err)
		}
		if _, _, err := arena.begin(id); !errors.Is(err, errMachineModuleIndexInvalid) {
			t.Fatalf("begin(%d) err=%v, want bounds error", id, err)
		}
	}
	if err := arena.finish(0, slotNil); !errors.Is(err, errMachineModuleTransition) {
		t.Fatalf("finish unloaded err=%v, want transition error", err)
	}
	if err := arena.abort(0); !errors.Is(err, errMachineModuleTransition) {
		t.Fatalf("abort unloaded err=%v, want transition error", err)
	}
	if _, present, err := arena.export(0); err != nil || present {
		t.Fatalf("unloaded export present=%t err=%v, want absent", present, err)
	}
}

func TestMachineModuleArenaOwnersAndLifecycle(t *testing.T) {
	var first, second machineModuleArena
	if err := first.bindStopped(2); err != nil {
		t.Fatal(err)
	}
	if err := second.bindStopped(2); err != nil {
		t.Fatal(err)
	}
	if _, _, err := first.begin(1); err != nil {
		t.Fatal(err)
	}
	if err := first.finish(1, slot(42)); err != nil {
		t.Fatal(err)
	}
	if _, present, err := second.export(1); err != nil || present {
		t.Fatalf("second owner export present=%t err=%v, want absent", present, err)
	}
	stateCapacity, exportCapacity := cap(first.states), cap(first.exports)
	first.reset()
	if first.closed || len(first.states) != 0 || len(first.exports) != 0 || len(first.loading) != 0 {
		t.Fatalf("reset retained logical state: %#v", first)
	}
	if cap(first.states) != stateCapacity || cap(first.exports) != exportCapacity {
		t.Fatalf("reset discarded capacities states=%d/%d exports=%d/%d", cap(first.states), stateCapacity, cap(first.exports), exportCapacity)
	}
	if err := first.bindStopped(1); err != nil {
		t.Fatal(err)
	}
	first.close()
	if !first.closed || first.states != nil || first.exports != nil || first.loading != nil {
		t.Fatalf("close retained storage: %#v", first)
	}
	if _, err := first.state(0); !errors.Is(err, errMachineModuleArenaClosed) {
		t.Fatalf("closed state err=%v, want closed error", err)
	}
	if _, _, err := first.begin(0); !errors.Is(err, errMachineModuleArenaClosed) {
		t.Fatalf("closed begin err=%v, want closed error", err)
	}
	first.close()
}
