package ember

import (
	"errors"
	"testing"
)

func TestMachineCoroutineBoundaryValidatesOwnerAndGeneration(t *testing.T) {
	var controller machineCoroutineController
	if err := controller.bindStopped(101, 1); err != nil {
		t.Fatal(err)
	}
	defer controller.close()
	handle, err := controller.createStopped(machineCoroutineRoot{
		proto: 1, closure: machineClosureHandle{owner: 101, index: 1, generation: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := slotPackHandle(slotTagCoroutine, handle.index, handle.generation)
	if err != nil {
		t.Fatal(err)
	}
	if got := slotValueKind(encoded); got != UserDataKind {
		t.Fatalf("coroutine slot kind = %s, want userdata", got)
	}
	public, err := controller.exportValueStopped(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if public.Kind() != UserDataKind {
		t.Fatalf("public coroutine kind = %s, want userdata", public.Kind())
	}
	if _, ok := public.UserData(); ok {
		t.Fatal("Machine coroutine value was exposed as a host UserData pointer")
	}
	roundTrip, err := controller.importValueStopped(public)
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip != encoded {
		t.Fatalf("round-trip slot = %#x, want %#x", roundTrip, encoded)
	}
	decoded, err := controller.handleFromSlot(roundTrip)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != handle {
		t.Fatalf("round-trip handle = %#v, want %#v", decoded, handle)
	}

	foreign, err := machineCoroutineValue(machineCoroutineHandle{owner: 102, index: handle.index, generation: handle.generation})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.importValueStopped(foreign); !errors.Is(err, errMachineCoroutineValueCrossOwner) {
		t.Fatalf("foreign value error = %v, want cross-owner", err)
	}
	if _, err := controller.handleFromSlot(slotBool(true)); err == nil {
		t.Fatal("boolean slot was accepted as a coroutine handle")
	}

	if err := controller.closeCoroutineStopped(handle); err != nil {
		t.Fatal(err)
	}
	controller.mu.Lock()
	err = controller.arena.releaseDeadStopped(handle)
	controller.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.importValueStopped(public); !errors.Is(err, errMachineCoroutineValueStale) {
		t.Fatalf("stale public value error = %v, want stale", err)
	}
	if _, err := controller.handleFromSlot(encoded); !errors.Is(err, errMachineCoroutineIndex) {
		t.Fatalf("stale slot error = %v, want invalid index", err)
	}
}

func TestMachineCoroutineNativeIDsAreStableAndMapped(t *testing.T) {
	if nativeFuncCoroutineCreate != nativeFuncGetMetatable+1 || nativeFuncCoroutineYield != nativeFuncCoroutineCreate+1 {
		t.Fatalf("coroutine create/yield IDs = %d/%d, want appended after getmetatable %d", nativeFuncCoroutineCreate, nativeFuncCoroutineYield, nativeFuncGetMetatable)
	}
	table := baseCoroutine()
	for _, test := range []struct {
		name string
		id   nativeFuncID
	}{
		{name: "create", id: nativeFuncCoroutineCreate},
		{name: "resume", id: nativeFuncCoroutineResume},
		{name: "yield", id: nativeFuncCoroutineYield},
		{name: "status", id: nativeFuncCoroutineStatus},
	} {
		value, err := table.rawGet(StringValue(test.name))
		if err != nil {
			t.Fatal(err)
		}
		if got := valueNativeID(value); got != test.id {
			t.Fatalf("coroutine.%s native ID = %d, want %d", test.name, got, test.id)
		}
		if _, ok := nativeFuncByID(test.id); !ok {
			t.Fatalf("native ID %d for coroutine.%s has no VM callback", test.id, test.name)
		}
		if _, ok := baseNativeFuncName(test.id); !ok {
			t.Fatalf("native ID %d for coroutine.%s has no stable image name", test.id, test.name)
		}
	}
}
