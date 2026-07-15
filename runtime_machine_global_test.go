package ember

import (
	"errors"
	"reflect"
	"testing"
)

func TestMachineGlobalArenaLayout(t *testing.T) {
	for _, element := range []reflect.Type{
		reflect.TypeOf(slot(0)),
		reflect.TypeOf(machineGlobalName(0)),
		reflect.TypeOf(uint64(0)),
		reflect.TypeOf(uint8(0)),
	} {
		if element.Kind() != reflect.Uint64 && element.Kind() != reflect.Uint32 && element.Kind() != reflect.Uint8 {
			t.Fatalf("global storage element %s is not pointer-free", element)
		}
	}
	for _, element := range []reflect.Type{
		reflect.TypeOf([]machineGlobalName{}).Elem(),
		reflect.TypeOf([]slot{}).Elem(),
		reflect.TypeOf([]uint64{}).Elem(),
		reflect.TypeOf([]uint8{}).Elem(),
	} {
		if element.Kind() != reflect.Uint64 && element.Kind() != reflect.Uint32 && element.Kind() != reflect.Uint8 {
			t.Fatalf("global arena SoA element %s is not pointer-free", element)
		}
	}
}

func TestMachineGlobalArenaBindsDeterministicNamesAndNilPresence(t *testing.T) {
	var arena machineGlobalArena
	if err := arena.bindNamesStopped([]machineGlobalName{4, 1, 3}); err != nil {
		t.Fatal(err)
	}
	if got := arena.names; !reflect.DeepEqual(got, []machineGlobalName{1, 3, 4}) {
		t.Fatalf("bound names=%v, want sorted scalar IDs", got)
	}
	if _, present, err := arena.get(3); err != nil || present {
		t.Fatalf("unset global get present=%t err=%v, want absent", present, err)
	}
	if err := arena.set(3, slotNil); err != nil {
		t.Fatal(err)
	}
	value, present, err := arena.get(3)
	if err != nil || !present || value != slotNil {
		t.Fatalf("present nil global value=%v present=%t err=%v", value, present, err)
	}
	if version, ok, err := arena.version(3); err != nil || !ok || version != 1 {
		t.Fatalf("global version=%d present=%t err=%v, want 1/present", version, ok, err)
	}
	if err := arena.set(3, slotBool(true)); err != nil {
		t.Fatal(err)
	}
	index, err := arena.lookupIndex(3)
	if err != nil {
		t.Fatal(err)
	}
	if got, present, err := arena.getAt(index); err != nil || !present || got != slotBool(true) {
		t.Fatalf("dense getAt value=%v present=%t err=%v", got, present, err)
	}
	if err := arena.setAt(index, slotBool(false)); err != nil {
		t.Fatal(err)
	}
	if got, present, err := arena.getAt(index); err != nil || !present || got != slotBool(false) {
		t.Fatalf("dense setAt/getAt value=%v present=%t err=%v", got, present, err)
	}
	if version, ok, err := arena.version(3); err != nil || !ok || version != 3 {
		t.Fatalf("updated global version=%d present=%t err=%v, want 3/present", version, ok, err)
	}
}

func TestMachineGlobalArenaRejectsInvalidAndDuplicateNames(t *testing.T) {
	for _, names := range [][]machineGlobalName{{0}, {2, 2}} {
		var arena machineGlobalArena
		if err := arena.bindNamesStopped(names); err == nil {
			t.Fatalf("bind names=%v unexpectedly succeeded", names)
		}
	}
	var arena machineGlobalArena
	if err := arena.bindNamesStopped([]machineGlobalName{1, 3}); err != nil {
		t.Fatal(err)
	}
	if err := arena.set(1, slotBool(true)); err != nil {
		t.Fatal(err)
	}
	if err := arena.bindNamesStopped([]machineGlobalName{2, 2}); !errors.Is(err, errMachineGlobalNameDuplicate) {
		t.Fatalf("duplicate rebind err=%v, want duplicate error", err)
	}
	if value, present, err := arena.get(1); err != nil || !present || value != slotBool(true) {
		t.Fatalf("failed duplicate bind changed prior state value=%v present=%t err=%v", value, present, err)
	}
	for _, name := range []machineGlobalName{0, 2, 99} {
		if _, _, err := arena.get(name); err == nil {
			t.Fatalf("get unknown name %d unexpectedly succeeded", name)
		}
		if err := arena.set(name, slotBool(true)); err == nil {
			t.Fatalf("set unknown name %d unexpectedly succeeded", name)
		}
	}
	for _, index := range []int{-1, 2} {
		if _, _, err := arena.getAt(index); !errors.Is(err, errMachineGlobalIndexInvalid) {
			t.Fatalf("getAt(%d) err=%v, want invalid index", index, err)
		}
		if err := arena.setAt(index, slotNil); !errors.Is(err, errMachineGlobalIndexInvalid) {
			t.Fatalf("setAt(%d) err=%v, want invalid index", index, err)
		}
	}
}

func TestMachineGlobalArenaOwnersAreIndependent(t *testing.T) {
	names := []machineGlobalName{7, 2}
	var first, second machineGlobalArena
	if err := first.bindNamesStopped(names); err != nil {
		t.Fatal(err)
	}
	if err := second.bindNamesStopped(names); err != nil {
		t.Fatal(err)
	}
	if err := first.set(7, slot(42)); err != nil {
		t.Fatal(err)
	}
	value, present, err := second.get(7)
	if err != nil || present || value != slotNil {
		t.Fatalf("second owner value=%v present=%t err=%v, want absent nil", value, present, err)
	}
	value, present, err = first.get(7)
	if err != nil || !present || value != slot(42) {
		t.Fatalf("first owner value=%v present=%t err=%v, want 42/present", value, present, err)
	}
}

func TestMachineGlobalArenaResetAndCloseLifecycle(t *testing.T) {
	var arena machineGlobalArena
	if err := arena.bindNamesStopped([]machineGlobalName{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := arena.set(1, slotBool(true)); err != nil {
		t.Fatal(err)
	}
	nameCapacity, valueCapacity := cap(arena.names), cap(arena.values)
	arena.reset()
	if arena.closed || len(arena.names) != 0 || len(arena.values) != 0 || arena.epoch != 0 {
		t.Fatalf("reset retained logical state: %#v", arena)
	}
	if cap(arena.names) != nameCapacity || cap(arena.values) != valueCapacity {
		t.Fatalf("reset discarded capacity names=%d/%d values=%d/%d", cap(arena.names), nameCapacity, cap(arena.values), valueCapacity)
	}
	if err := arena.bindNamesStopped([]machineGlobalName{1}); err != nil {
		t.Fatal(err)
	}
	if _, present, err := arena.get(1); err != nil || present {
		t.Fatalf("post-reset global present=%t err=%v, want absent", present, err)
	}
	arena.close()
	if !arena.closed || arena.names != nil || arena.values != nil || arena.versions != nil || arena.present != nil {
		t.Fatalf("close retained storage: %#v", arena)
	}
	if _, _, err := arena.get(1); !errors.Is(err, errMachineGlobalArenaClosed) {
		t.Fatalf("closed get err=%v, want closed error", err)
	}
	if err := arena.set(1, slotNil); !errors.Is(err, errMachineGlobalArenaClosed) {
		t.Fatalf("closed set err=%v, want closed error", err)
	}
	arena.close()
}

func TestMachineGlobalArenaSetDoesNotGrow(t *testing.T) {
	var arena machineGlobalArena
	if err := arena.bindNamesStopped([]machineGlobalName{1}); err != nil {
		t.Fatal(err)
	}
	namesCapacity, valuesCapacity := cap(arena.names), cap(arena.values)
	if err := arena.set(1, slotBool(true)); err != nil {
		t.Fatal(err)
	}
	if cap(arena.names) != namesCapacity || cap(arena.values) != valuesCapacity {
		t.Fatal("hot set changed arena capacities")
	}
}
