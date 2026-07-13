package ember

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestOwnerSlotExecutionReusesOwnerHeapWithoutLiveRetention(t *testing.T) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto := newProto(nil, []instruction{{op: opReturnOne, a: 0}}, nil, nil, 1, 1, false)
	values := []Value{
		stringValueFromBox(newStringBox("owner heap")),
		TableValue(NewTable()),
		closureFunctionValue(&closure{}),
		UserDataValue(NewUserData("owner heap")),
		HostFuncValue(func([]Value) ([]Value, error) { return nil, nil }),
		NumberValue(math.Float64frombits(0x7ff8_0000_0000_0042)),
	}
	for index, value := range values {
		results, err := executeProto(context.Background(), proto, globals, executeOptions{
			args:       []Value{value},
			controller: nil,
		})
		if err != nil || len(results) != 1 {
			t.Fatalf("value %d result = (%#v, %v)", index, results, err)
		}
		if value.Kind() == NumberKind {
			if results[0].bits != value.bits {
				t.Fatalf("value %d bits = %#x, want %#x", index, results[0].bits, value.bits)
			}
		} else if valueRef(results[0]) != valueRef(value) {
			t.Fatalf("value %d identity changed", index)
		}
		assertRuntimeOwnerHeapEmpty(t, owner)
	}
	if len(owner.heap.strings.entries) != 2 || len(owner.heap.tables.entries) != 2 ||
		len(owner.heap.closures.entries) != 2 || len(owner.heap.userdata.entries) != 2 ||
		len(owner.heap.hostCallables.entries) != 2 || len(owner.heap.boxedNumbers.entries) != 2 {
		t.Fatalf("owner slabs did not retain one reusable slot: strings=%d tables=%d closures=%d userdata=%d host=%d boxed=%d",
			len(owner.heap.strings.entries), len(owner.heap.tables.entries), len(owner.heap.closures.entries),
			len(owner.heap.userdata.entries), len(owner.heap.hostCallables.entries), len(owner.heap.boxedNumbers.entries))
	}
	if owner.heap.userdata.entries[1].pinned || owner.heap.hostCallables.entries[1].pinned {
		t.Fatal("transient opaque argument remained pinned")
	}

	for index := 0; index < 1000; index++ {
		value := stringValueFromBox(newStringBox(fmt.Sprintf("unique-%d", index)))
		if _, err := executeProto(context.Background(), proto, globals, executeOptions{
			args:       []Value{value},
			controller: nil,
		}); err != nil {
			t.Fatalf("unique run %d: %v", index, err)
		}
	}
	if len(owner.heap.strings.entries) != 2 || cap(owner.heap.strings.entries) > 2 {
		t.Fatalf("unique strings grew slab to len=%d cap=%d", len(owner.heap.strings.entries), cap(owner.heap.strings.entries))
	}
	assertRuntimeOwnerHeapEmpty(t, owner)
}

func TestOwnerSlotExecutionPreservesExistingRootsAndPins(t *testing.T) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto := newProto(nil, []instruction{{op: opReturnOne, a: 0}}, nil, nil, 1, 1, false)
	table := NewTable()
	handle, err := owner.heap.importTable(table)
	if err != nil {
		t.Fatalf("import rooted table: %v", err)
	}
	root, err := owner.root(handle)
	if err != nil {
		t.Fatalf("root table: %v", err)
	}
	pin, err := owner.pin(TableValue(table))
	if err != nil {
		t.Fatalf("pin table: %v", err)
	}
	if _, err := executeProto(context.Background(), proto, globals, executeOptions{
		args:       []Value{TableValue(table)},
		controller: nil,
	}); err != nil {
		t.Fatalf("execute rooted table: %v", err)
	}
	if got, err := root.value(); err != nil || got != handle {
		t.Fatalf("root after execution = (%#x, %v), want %#x", got, err, handle)
	}
	if value, err := pin.value(); err != nil || value.tableRef() != table {
		t.Fatalf("pin after execution = (%#v, %v)", value, err)
	}
	if _, err := owner.collect(nil); err != nil {
		t.Fatalf("collect rooted table: %v", err)
	}
	if err := owner.heap.validateSlot(handle); err != nil {
		t.Fatalf("rooted handle after collect: %v", err)
	}

	userdata := NewUserData("rooted opaque value")
	userdataHandle, err := owner.heap.importUserData(userdata)
	if err != nil {
		t.Fatalf("import rooted userdata: %v", err)
	}
	if err := owner.heap.unpinHandle(userdataHandle); err != nil {
		t.Fatalf("remove default userdata pin: %v", err)
	}
	userdataRoot, err := owner.root(userdataHandle)
	if err != nil {
		t.Fatalf("root userdata: %v", err)
	}
	if _, err := executeProto(context.Background(), proto, globals, executeOptions{
		args:       []Value{UserDataValue(userdata)},
		controller: nil,
	}); err != nil {
		t.Fatalf("execute rooted userdata: %v", err)
	}
	if pinned, err := owner.heap.handlePinned(userdataHandle); err != nil || pinned {
		t.Fatalf("rooted userdata pin after execution = (%v, %v), want false", pinned, err)
	}
	userdataRoot.release()
	pin.release()
	root.release()
}

func TestOwnerSlotExecutionReturnedValueSurvivesHandleReuse(t *testing.T) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto := newProto(nil, []instruction{{op: opReturnOne, a: 0}}, nil, nil, 1, 1, false)
	first := StringValue("first")
	results, err := executeProto(context.Background(), proto, globals, executeOptions{
		args:       []Value{first},
		controller: nil,
	})
	if err != nil || len(results) != 1 {
		t.Fatalf("first run = (%#v, %v)", results, err)
	}
	releasedGeneration := owner.heap.strings.entries[1].generation - 1
	stale, err := slotPackHandle(slotTagString, 1, releasedGeneration)
	if err != nil {
		t.Fatalf("pack stale handle: %v", err)
	}
	if _, err := executeProto(context.Background(), proto, globals, executeOptions{
		args:       []Value{StringValue("second")},
		controller: nil,
	}); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got, ok := results[0].String(); !ok || got != "first" {
		t.Fatalf("exported first result = (%q, %v)", got, ok)
	}
	if err := owner.heap.validateSlot(stale); err == nil {
		t.Fatal("released transient handle aliased a reused slot")
	}
}

func TestOwnerSlotExecutionSerializesHeapWithPins(t *testing.T) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto := newProto(nil, []instruction{{op: opReturnOne, a: 0}}, nil, nil, 1, 1, false)
	const workers = 8
	const iterations = 100
	var wait sync.WaitGroup
	wait.Add(workers + 1)
	errors := make(chan error, workers+1)
	for worker := 0; worker < workers; worker++ {
		go func(worker int) {
			defer wait.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				value := StringValue(fmt.Sprintf("worker-%d-%d", worker, iteration))
				if _, err := executeProto(context.Background(), proto, globals, executeOptions{
					args:       []Value{value},
					controller: nil,
				}); err != nil {
					errors <- err
					return
				}
			}
		}(worker)
	}
	go func() {
		defer wait.Done()
		for iteration := 0; iteration < iterations; iteration++ {
			pin, err := owner.pin(TableValue(NewTable()))
			if err != nil {
				errors <- err
				return
			}
			pin.release()
		}
	}()
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}
