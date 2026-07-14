package ember

import (
	"errors"
	"math"
	"testing"
)

func TestRuntimeHeapCollectMarksRootsAndEdges(t *testing.T) {
	var heap runtimeHeap
	rootTable := NewTable()
	childTable := NewTable()
	leaf := newStringBox("leaf")
	capturedCell := &cell{value: TableValue(childTable)}
	closure := &closure{
		upvalues:      []*cell{capturedCell},
		upvalueValues: []Value{stringValueFromBox(leaf)},
		upvalueValueOK: []bool{
			true,
		},
	}
	if err := rootTable.Set(StringValue("child"), TableValue(childTable)); err != nil {
		t.Fatalf("set child table: %v", err)
	}
	if err := childTable.Set(StringValue("leaf"), stringValueFromBox(leaf)); err != nil {
		t.Fatalf("set leaf string: %v", err)
	}
	if err := childTable.Set(StringValue("closure"), closureFunctionValue(closure)); err != nil {
		t.Fatalf("set closure: %v", err)
	}

	root, err := heap.importTable(rootTable)
	if err != nil {
		t.Fatalf("import root table: %v", err)
	}
	child, err := heap.importTable(childTable)
	if err != nil {
		t.Fatalf("import child table: %v", err)
	}
	leafHandle, err := heap.importString(leaf)
	if err != nil {
		t.Fatalf("import leaf string: %v", err)
	}
	closureHandle, err := heap.importClosure(closure)
	if err != nil {
		t.Fatalf("import closure: %v", err)
	}
	cellHandle, err := heap.importCell(capturedCell)
	if err != nil {
		t.Fatalf("import cell: %v", err)
	}
	deadTable, err := heap.importTable(NewTable())
	if err != nil {
		t.Fatalf("import dead table: %v", err)
	}
	deadString, err := heap.importString(newStringBox("dead"))
	if err != nil {
		t.Fatalf("import dead string: %v", err)
	}

	stats, err := heap.collect([]slot{root})
	if err != nil {
		t.Fatalf("collect rooted graph: %v", err)
	}
	if stats.allocated != stats.live+stats.reclaimed {
		t.Fatalf("stats = allocated %d, live %d, reclaimed %d", stats.allocated, stats.live, stats.reclaimed)
	}
	if stats.reclaimed != 2 {
		t.Fatalf("reclaimed = %d, want 2", stats.reclaimed)
	}
	if stats.live != 8 {
		t.Fatalf("live = %d, want reachable tables, keys, leaf, closure, and cell = 8", stats.live)
	}
	for name, handle := range map[string]slot{
		"root": root, "child": child, "leaf": leafHandle,
		"closure": closureHandle, "cell": cellHandle,
	} {
		if err := heap.validateSlot(handle); err != nil {
			t.Fatalf("%s handle after collection: %v", name, err)
		}
	}
	if err := heap.validateSlot(deadTable); err == nil {
		t.Fatal("unrooted table survived collection")
	}
	if err := heap.validateSlot(deadString); err == nil {
		t.Fatal("unrooted string survived collection")
	}

	// A cycle remains live from one root and is still reclaimed when the root
	// handle is no longer supplied.
	cycleA := NewTable()
	cycleB := NewTable()
	if err := cycleA.Set(StringValue("next"), TableValue(cycleB)); err != nil {
		t.Fatalf("set cycle A: %v", err)
	}
	if err := cycleB.Set(StringValue("next"), TableValue(cycleA)); err != nil {
		t.Fatalf("set cycle B: %v", err)
	}
	cycleAHandle, err := heap.importTable(cycleA)
	if err != nil {
		t.Fatalf("import cycle A: %v", err)
	}
	if _, err := heap.importTable(cycleB); err != nil {
		t.Fatalf("import cycle B: %v", err)
	}
	if got, err := heap.collect([]slot{cycleAHandle}); err != nil {
		t.Fatalf("collect rooted cycle: %v", err)
	} else if got.reclaimed != 8 {
		t.Fatalf("rooted cycle reclaimed = %d, want 8 from the previous graph", got.reclaimed)
	}
	if got, err := heap.collect(nil); err != nil {
		t.Fatalf("collect unrooted cycle: %v", err)
	} else if got.reclaimed != 4 {
		t.Fatalf("unrooted cycle reclaimed = %d, want cycle tables and keys = 4", got.reclaimed)
	}
}

func TestRuntimeHeapCollectPinnedAndStaleHandles(t *testing.T) {
	var heap runtimeHeap
	pinned := NewUserData("pinned")
	pinnedHandle, err := heap.importUserData(pinned)
	if err != nil {
		t.Fatalf("import pinned userdata: %v", err)
	}
	dead, err := heap.importTable(NewTable())
	if err != nil {
		t.Fatalf("import dead table: %v", err)
	}
	if got, err := heap.collect(nil); err != nil {
		t.Fatalf("collect pinned and dead values: %v", err)
	} else if got.reclaimed != 1 {
		t.Fatalf("reclaimed = %d, want 1", got.reclaimed)
	}
	if err := heap.validateSlot(pinnedHandle); err != nil {
		t.Fatalf("pinned handle after collection: %v", err)
	}
	if err := heap.validateSlot(dead); err == nil {
		t.Fatal("dead handle validated after collection")
	}

	// The stale root is rejected without reviving the recycled entry.
	stats, err := heap.collect([]slot{dead})
	if err != nil {
		t.Fatalf("collect stale root: %v", err)
	}
	if stats.staleHandles != 1 {
		t.Fatalf("stale handles = %d, want 1", stats.staleHandles)
	}
	replacement, err := heap.importTable(NewTable())
	if err != nil {
		t.Fatalf("import replacement table: %v", err)
	}
	_, oldIndex, oldGeneration, err := slotUnpackHandle(dead)
	if err != nil {
		t.Fatalf("unpack dead handle: %v", err)
	}
	_, replacementIndex, replacementGeneration, err := slotUnpackHandle(replacement)
	if err != nil {
		t.Fatalf("unpack replacement handle: %v", err)
	}
	if replacementIndex != oldIndex || replacementGeneration == oldGeneration {
		t.Fatalf("replacement = (index %d, generation %d), want index %d and new generation", replacementIndex, replacementGeneration, oldIndex)
	}
}

func TestRuntimeHeapCollectClearsDeadGoReferences(t *testing.T) {
	var heap runtimeHeap
	box := newStringBox("clear me")
	table := NewTable()
	stringHandle, err := heap.importString(box)
	if err != nil {
		t.Fatalf("import string: %v", err)
	}
	tableHandle, err := heap.importTable(table)
	if err != nil {
		t.Fatalf("import table: %v", err)
	}
	_, stringIndex, _, err := slotUnpackHandle(stringHandle)
	if err != nil {
		t.Fatalf("unpack string: %v", err)
	}
	_, tableIndex, _, err := slotUnpackHandle(tableHandle)
	if err != nil {
		t.Fatalf("unpack table: %v", err)
	}
	if got, err := heap.collect(nil); err != nil {
		t.Fatalf("collect dead references: %v", err)
	} else if got.reclaimed != 2 {
		t.Fatalf("reclaimed = %d, want 2", got.reclaimed)
	}
	if entry := heap.strings.entries[stringIndex]; entry.value != nil || entry.live || entry.mark {
		t.Fatalf("dead string entry retained Go reference: %#v", entry)
	}
	if entry := heap.tables.entries[tableIndex]; entry.value != nil || entry.live || entry.mark {
		t.Fatalf("dead table entry retained Go reference: %#v", entry)
	}
}

func TestRuntimeHeapCollectMarksBoxedNumberFromPublicValueEdge(t *testing.T) {
	var heap runtimeHeap
	bits := uint64(0x7ff8_0000_0000_0042)
	value := NumberValue(math.Float64frombits(bits))
	table := NewTable()
	if err := table.Set(StringValue("number"), value); err != nil {
		t.Fatalf("set boxed number: %v", err)
	}
	root, err := heap.importTable(table)
	if err != nil {
		t.Fatalf("import root table: %v", err)
	}
	boxed, err := heap.importValue(value)
	if err != nil {
		t.Fatalf("import boxed number: %v", err)
	}
	if _, err := heap.collect([]slot{root}); err != nil {
		t.Fatalf("collect boxed number edge: %v", err)
	}
	if err := heap.validateSlot(boxed); err != nil {
		t.Fatalf("boxed number edge after collection: %v", err)
	}
}

func TestRuntimeHeapCollectFailsClosedBeforeSweep(t *testing.T) {
	var heap runtimeHeap
	handle, err := heap.importTable(NewTable())
	if err != nil {
		t.Fatalf("import candidate: %v", err)
	}
	want := errors.New("root scan failed")
	stats, err := heap.collectWithScanner(nil, func(collector *runtimeHeapCollector) {
		collector.fail(want)
	})
	if !errors.Is(err, want) {
		t.Fatalf("collect error = %v, want %v", err, want)
	}
	if stats.reclaimed != 0 {
		t.Fatalf("failed collect reclaimed %d entries", stats.reclaimed)
	}
	if err := heap.validateSlot(handle); err != nil {
		t.Fatalf("failed collect swept handle: %v", err)
	}
}

func TestRuntimeHeapCollectClearsOnlyInactiveCoroutineFrameSlots(t *testing.T) {
	var heap runtimeHeap
	liveTable := NewTable()
	deadTable := NewTable()
	liveHandle, err := heap.importTable(liveTable)
	if err != nil {
		t.Fatalf("import live table: %v", err)
	}
	deadHandle, err := heap.importTable(deadTable)
	if err != nil {
		t.Fatalf("import dead table: %v", err)
	}
	activeOwner := &vmStackOwner{values: []Value{TableValue(liveTable)}}
	active := &vmFrame{
		registers:       []Value{NilValue()},
		openResultStart: 0,
		openRangeOwner:  activeOwner,
		cold: &vmFrameCold{
			openRangeBase:       0,
			openRangeCount:      1,
			openRangeLogicalTop: 0,
		},
	}
	staleOwner := &vmStackOwner{values: []Value{NilValue(), TableValue(deadTable)}}
	stale := &vmFrame{
		registers:       []Value{TableValue(deadTable)},
		cells:           []*cell{{value: TableValue(deadTable)}},
		openResultStart: 0,
		openRangeOwner:  staleOwner,
		cold: &vmFrameCold{
			openResults:         vmOwnedResultWindow([]Value{TableValue(deadTable)}),
			openRangeBase:       1,
			openRangeCount:      1,
			openRangeLogicalTop: 1,
		},
	}
	coroutine := &vmCoroutine{
		thread:    vmThread{frameSlots: []*vmFrame{active, stale}},
		suspended: vmSuspendedFrames{frames: []*vmFrame{active}},
	}
	userdata := NewUserData(coroutine)
	root, err := heap.importUserData(userdata)
	if err != nil {
		t.Fatalf("import coroutine userdata: %v", err)
	}
	if _, err := heap.collect([]slot{root}); err != nil {
		t.Fatalf("collect coroutine frame slots: %v", err)
	}
	if err := heap.validateSlot(liveHandle); err != nil {
		t.Fatalf("active frame value was swept: %v", err)
	}
	if err := heap.validateSlot(deadHandle); err == nil {
		t.Fatal("inactive frame slot retained a dead value")
	}
	if len(active.registers) != 1 || len(activeOwner.values) != 1 || activeOwner.values[0].tableRef() != liveTable {
		t.Fatalf("active frame range was cleared: registers=%#v owner=%#v", active.registers, activeOwner.values)
	}
	if stale.registers != nil || stale.cells != nil || stale.openResultValues() != nil {
		t.Fatalf("inactive frame retained references: %#v", stale)
	}
	if len(staleOwner.values) != 1 {
		t.Fatalf("inactive frame owner length = %d, want stale range truncated", len(staleOwner.values))
	}
}

func TestRuntimeHeapCollectMarksThreadOpenUpvalues(t *testing.T) {
	var heap runtimeHeap
	table := NewTable()
	tableHandle, err := heap.importTable(table)
	if err != nil {
		t.Fatalf("import open-upvalue table: %v", err)
	}
	owner := &vmStackOwner{values: []Value{TableValue(table)}}
	open := &cell{}
	open.openAt(owner, 0)
	cellHandle, err := heap.importCell(open)
	if err != nil {
		t.Fatalf("import open upvalue: %v", err)
	}
	coroutine := &vmCoroutine{thread: vmThread{openUpvalues: []*cell{open}}}
	root, err := heap.importUserData(NewUserData(coroutine))
	if err != nil {
		t.Fatalf("import coroutine userdata: %v", err)
	}

	if _, err := heap.collect([]slot{root}); err != nil {
		t.Fatalf("collect thread open upvalues: %v", err)
	}
	if err := heap.validateSlot(cellHandle); err != nil {
		t.Fatalf("thread open upvalue was swept: %v", err)
	}
	if err := heap.validateSlot(tableHandle); err != nil {
		t.Fatalf("value reachable through open upvalue was swept: %v", err)
	}
}
