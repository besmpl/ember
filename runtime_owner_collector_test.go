package ember

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestRuntimeOwnerCollectKeepsPinnedBoxedNumber(t *testing.T) {
	owner := newRuntimeOwner()
	wantBits := uint64(0x7ff8_0000_0000_0042)
	pin, err := owner.pin(NumberValue(math.Float64frombits(wantBits)))
	if err != nil {
		t.Fatalf("pin boxed number: %v", err)
	}
	if _, err := owner.collect(nil); err != nil {
		t.Fatalf("collect pinned boxed number: %v", err)
	}
	value, err := pin.value()
	if err != nil {
		t.Fatalf("pinned boxed number after collection: %v", err)
	}
	if value.bits != wantBits {
		t.Fatalf("pinned boxed number bits = %#x, want %#x", value.bits, wantBits)
	}
	handle := pin.handle
	pin.release()
	if _, err := owner.collect(nil); err != nil {
		t.Fatalf("collect released boxed number: %v", err)
	}
	if err := owner.heap.validateSlot(handle); err == nil {
		t.Fatal("released boxed number pin survived collection")
	}
}

func TestRuntimeOwnerCollectKeepsRootsPinsAndRetainedCoroutines(t *testing.T) {
	owner := newRuntimeOwner()
	rootTable := NewTable()
	childTable := NewTable()
	if err := rootTable.Set(StringValue("child"), TableValue(childTable)); err != nil {
		t.Fatalf("set rooted child: %v", err)
	}
	rootHandle, err := owner.heap.importTable(rootTable)
	if err != nil {
		t.Fatalf("import root table: %v", err)
	}
	childHandle, err := owner.heap.importTable(childTable)
	if err != nil {
		t.Fatalf("import child table: %v", err)
	}
	root, err := owner.root(rootHandle)
	if err != nil {
		t.Fatalf("root table: %v", err)
	}
	deadHandle, err := owner.heap.importTable(NewTable())
	if err != nil {
		t.Fatalf("import dead table: %v", err)
	}

	coroutineClosure := &closure{upvalueValues: []Value{TableValue(childTable)}, upvalueValueOK: []bool{true}}
	coroutine := &vmCoroutine{root: coroutineClosure}
	if err := owner.retainCoroutine(coroutine); err != nil {
		t.Fatalf("retain coroutine: %v", err)
	}

	stats, err := owner.collect(nil)
	if err != nil {
		t.Fatalf("collect owner: %v", err)
	}
	if stats.reclaimed != 1 {
		t.Fatalf("reclaimed = %d, want one dead table", stats.reclaimed)
	}
	for name, handle := range map[string]slot{"root": rootHandle, "child": childHandle} {
		if err := owner.heap.validateSlot(handle); err != nil {
			t.Fatalf("%s handle after collection: %v", name, err)
		}
	}
	if err := owner.heap.validateSlot(deadHandle); err == nil {
		t.Fatal("unrooted owner handle survived collection")
	}
	if handle, ok := slabHandle(&owner.heap.closures, slotTagClosure, coroutineClosure); !ok {
		t.Fatal("retained coroutine closure was not rooted")
	} else if err := owner.heap.validateSlot(handle); err != nil {
		t.Fatalf("retained coroutine closure handle: %v", err)
	}

	root.release()
	owner.releaseCoroutine(coroutine)
	if _, err := owner.collect(nil); err != nil {
		t.Fatalf("collect after releasing roots: %v", err)
	}
	if err := owner.heap.validateSlot(rootHandle); err == nil {
		t.Fatal("released root survived the next collection")
	}
}

func TestRuntimeOwnerCollectRejectsActiveAndClosedOwners(t *testing.T) {
	owner := newRuntimeOwner()
	lease, err := owner.beginRun()
	if err != nil {
		t.Fatalf("begin run: %v", err)
	}
	if _, err := owner.collect(nil); !errors.Is(err, errRuntimeOwnerActive) {
		t.Fatalf("collect during run = %v, want %v", err, errRuntimeOwnerActive)
	}
	lease.end()
	thread := &vmThread{}
	if err := owner.registerThread(thread); err != nil {
		t.Fatalf("register thread: %v", err)
	}
	if _, err := owner.collect(nil); !errors.Is(err, errRuntimeOwnerActive) {
		t.Fatalf("collect with thread = %v, want %v", err, errRuntimeOwnerActive)
	}
	owner.unregisterThread(thread)
	if err := owner.close(); err != nil {
		t.Fatalf("close owner: %v", err)
	}
	if _, err := owner.collect(nil); !errors.Is(err, errRuntimeOwnerClosed) {
		t.Fatalf("collect closed owner = %v, want %v", err, errRuntimeOwnerClosed)
	}
}

func TestRuntimeOwnerCollectRejectsOpaqueSuspendedContinuation(t *testing.T) {
	owner := newRuntimeOwner()
	deadHandle, err := owner.heap.importTable(NewTable())
	if err != nil {
		t.Fatalf("import candidate: %v", err)
	}
	coroutine := &vmCoroutine{suspended: vmSuspendedFrames{frames: []*vmFrame{{
		hasPendingCall: true,
		pendingCall:    vmPendingCall{host: &vmPendingHostCall{}},
	}}}}
	if err := owner.retainCoroutine(coroutine); err != nil {
		t.Fatalf("retain coroutine: %v", err)
	}
	if _, err := owner.collect(nil); !errors.Is(err, errRuntimeOwnerOpaque) {
		t.Fatalf("collect opaque continuation = %v, want %v", err, errRuntimeOwnerOpaque)
	}
	if err := owner.heap.validateSlot(deadHandle); err != nil {
		t.Fatalf("failed collection swept a handle: %v", err)
	}
}

func TestRuntimeCollectScansPersistentValuesAndProgramConstants(t *testing.T) {
	owner := newRuntimeOwner()
	entrypoint := NewTable()
	loaded := NewTable()
	constant := newStringBox("constant root")
	proto := &Proto{constants: []Value{stringValueFromBox(constant)}}
	runtime := &Runtime{
		owner:       owner,
		program:     &Program{protos: map[moduleKey]*Proto{{}: proto}},
		entrypoints: map[moduleKey]Value{{}: TableValue(entrypoint)},
		loaded:      map[moduleKey]Value{{}: TableValue(loaded)},
	}
	deadHandle, err := owner.heap.importTable(NewTable())
	if err != nil {
		t.Fatalf("import dead table: %v", err)
	}

	stats, err := runtime.collect()
	if err != nil {
		t.Fatalf("collect runtime: %v", err)
	}
	if stats.reclaimed != 1 {
		t.Fatalf("reclaimed = %d, want one dead table", stats.reclaimed)
	}
	for name, value := range map[string]Value{
		"entrypoint": TableValue(entrypoint),
		"loaded":     TableValue(loaded),
		"constant":   stringValueFromBox(constant),
	} {
		handle, found, _ := owner.heap.lookupExistingValue(value)
		if !found {
			t.Fatalf("%s was not imported as a collection root", name)
		}
		if err := owner.heap.validateSlot(handle); err != nil {
			t.Fatalf("%s root handle: %v", name, err)
		}
	}
	if err := owner.heap.validateSlot(deadHandle); err == nil {
		t.Fatal("runtime collection retained dead handle")
	}

	lease, err := runtime.beginRun()
	if err != nil {
		t.Fatalf("begin runtime run: %v", err)
	}
	if _, err := runtime.collect(); !errors.Is(err, errRuntimeOwnerActive) {
		t.Fatalf("runtime collect during run = %v, want %v", err, errRuntimeOwnerActive)
	}
	lease.end()
}

func TestRuntimeCollectSerializesWithClose(t *testing.T) {
	for iteration := 0; iteration < 20; iteration++ {
		runtime := &Runtime{owner: newRuntimeOwner(), program: &Program{}}
		var ready sync.WaitGroup
		ready.Add(2)
		start := make(chan struct{})
		collectResult := make(chan error, 1)
		closeResult := make(chan error, 1)
		go func() {
			ready.Done()
			<-start
			_, err := runtime.collect()
			collectResult <- err
		}()
		go func() {
			ready.Done()
			<-start
			closeResult <- runtime.Close()
		}()
		ready.Wait()
		close(start)
		collectErr := <-collectResult
		if collectErr != nil && !errors.Is(collectErr, errRuntimeOwnerClosed) {
			t.Fatalf("iteration %d collect = %v, want nil or closed", iteration, collectErr)
		}
		if err := <-closeResult; err != nil {
			t.Fatalf("iteration %d close: %v", iteration, err)
		}
	}
}
