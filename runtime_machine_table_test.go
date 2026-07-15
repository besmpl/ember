package ember

import (
	"reflect"
	"testing"
)

func TestMachineTableArenaUsesDensePointerFreeStorage(t *testing.T) {
	if reflect.TypeOf(machineTableID(0)).Kind() != reflect.Uint32 {
		t.Fatal("machineTableID is not a dense uint32 handle")
	}
	for _, value := range []any{
		machineTableRecord{},
		machineTableKey{},
		machineTableField{},
		machineTableOrderEntry{},
		machineTableCursor{},
		machineTableAction{},
		slot(0),
	} {
		if machineTableTypeHasPointers(reflect.TypeOf(value)) {
			t.Fatalf("hot machine table storage %T contains a Go pointer, map, or interface", value)
		}
	}
}

func TestMachineTableArenaStoresAndIteratesMixedInsertionOrder(t *testing.T) {
	var arena machineTableArena
	first, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 || second != 2 {
		t.Fatalf("dense table IDs = %d, %d; want 1, 2", first, second)
	}

	stringKey := machineStringID(7)
	scalarKey := slotBool(true)
	if err := arena.setArrayStopped(first, 2, slotBool(false)); err != nil {
		t.Fatal(err)
	}
	if err := arena.setSlotStopped(first, scalarKey, slotBool(true)); err != nil {
		t.Fatal(err)
	}
	if err := arena.setArrayStopped(first, 1, scalarKey); err != nil {
		t.Fatal(err)
	}
	if err := arena.setStringStopped(first, stringKey, slotBool(false)); err != nil {
		t.Fatal(err)
	}

	if got, ok := arena.getArray(first, 1); !ok || got != scalarKey {
		t.Fatalf("array lookup = %#x (%t), want %#x", got, ok, scalarKey)
	}
	if got, ok := arena.getSlot(first, scalarKey); !ok || got != slotBool(true) {
		t.Fatalf("scalar lookup = %#x (%t)", got, ok)
	}
	if got, ok := arena.getString(first, stringKey); !ok || got != slotBool(false) {
		t.Fatalf("string lookup = %#x (%t)", got, ok)
	}

	wantKeys := []machineTableKey{
		machineTableArrayKey(2),
		machineTableSlotKey(scalarKey),
		machineTableArrayKey(1),
		machineTableStringKey(stringKey),
	}
	var cursor machineTableCursor
	for index, want := range wantKeys {
		key, _, next, ok, err := arena.next(first, cursor)
		if err != nil {
			t.Fatalf("next %d: %v", index, err)
		}
		if !ok || key != want {
			t.Fatalf("next %d key = %#v (%t), want %#v", index, key, ok, want)
		}
		cursor = next
	}
	if _, _, _, ok, err := arena.next(first, cursor); err != nil || ok {
		t.Fatalf("iteration end = ok %t, err %v", ok, err)
	}
}

func TestMachineTableArenaDeleteAndReinsertResurrectsOrderPosition(t *testing.T) {
	var arena machineTableArena
	table, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	firstKey, secondKey := slot(1), slot(2)
	if err := arena.setSlotStopped(table, firstKey, slotBool(true)); err != nil {
		t.Fatal(err)
	}
	if err := arena.setSlotStopped(table, secondKey, slotBool(false)); err != nil {
		t.Fatal(err)
	}
	if err := arena.setSlotStopped(table, firstKey, slotNil); err != nil {
		t.Fatal(err)
	}
	if _, ok := arena.getSlot(table, firstKey); ok {
		t.Fatal("deleted scalar key remained readable")
	}
	if err := arena.setSlotStopped(table, firstKey, slot(3)); err != nil {
		t.Fatal(err)
	}

	if err := arena.setArrayStopped(table, 3, slot(4)); err != nil {
		t.Fatal(err)
	}
	if err := arena.setArrayStopped(table, 3, slotNil); err != nil {
		t.Fatal(err)
	}
	if _, ok := arena.getArray(table, 3); ok {
		t.Fatal("deleted array key remained readable")
	}
	if err := arena.setArrayStopped(table, 3, slot(5)); err != nil {
		t.Fatal(err)
	}

	wantKeys := []machineTableKey{
		machineTableSlotKey(firstKey),
		machineTableSlotKey(secondKey),
		machineTableArrayKey(3),
	}
	wantValues := []slot{slot(3), slotBool(false), slot(5)}
	var cursor machineTableCursor
	for index, want := range wantKeys {
		key, value, next, ok, err := arena.next(table, cursor)
		if err != nil || !ok || key != want || value != wantValues[index] {
			t.Fatalf("next %d = %#v %#x (%t), err %v; want %#v %#x", index, key, value, ok, err, want, wantValues[index])
		}
		cursor = next
	}
}

func TestMachineTableArenaReinsertedStringPublishesInternedIdentity(t *testing.T) {
	var strings machineStringArena
	first, err := strings.internStringStopped("field")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := strings.internBytesStopped([]byte{'f', 'i', 'e', 'l', 'd'})
	if err != nil {
		t.Fatal(err)
	}
	if first != replacement {
		t.Fatalf("equal-content Machine strings have IDs %d and %d; want one interned identity", first, replacement)
	}

	var arena machineTableArena
	table, err := arena.newTableStopped(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.setStringStopped(table, first, slot(1)); err != nil {
		t.Fatal(err)
	}
	if err := arena.setStringStopped(table, 2, slot(2)); err != nil {
		t.Fatal(err)
	}
	if err := arena.setStringStopped(table, first, slotNil); err != nil {
		t.Fatal(err)
	}
	if err := arena.setStringStopped(table, replacement, slot(3)); err != nil {
		t.Fatal(err)
	}
	key, value, _, ok, err := arena.next(table, machineTableCursor{})
	if err != nil || !ok || key != machineTableStringKey(replacement) || value != slot(3) {
		t.Fatalf("replacement iteration = %#v %#x (%t), err %v", key, value, ok, err)
	}
}

func TestMachineTableArenaCompactsSparseOrderBeforeReinsert(t *testing.T) {
	var arena machineTableArena
	table, err := arena.newTableStopped(0, 40)
	if err != nil {
		t.Fatal(err)
	}
	for key := machineStringID(1); key <= 40; key++ {
		if err := arena.setStringStopped(table, key, slot(key)); err != nil {
			t.Fatalf("set key %d: %v", key, err)
		}
	}
	for key := machineStringID(1); key <= 20; key++ {
		if err := arena.setStringStopped(table, key, slotNil); err != nil {
			t.Fatalf("delete key %d: %v", key, err)
		}
	}
	record, ok := arena.lookup(table)
	if !ok {
		t.Fatal("table missing before compaction threshold")
	}
	if record.orderLength != 40 || record.orderTombstone != 20 {
		t.Fatalf("order before threshold = length %d, tombstones %d; want 40, 20", record.orderLength, record.orderTombstone)
	}
	if err := arena.setStringStopped(table, 21, slotNil); err != nil {
		t.Fatal(err)
	}
	record, ok = arena.lookup(table)
	if !ok || record.orderLength != 19 || record.orderTombstone != 0 {
		t.Fatalf("compacted order = %#v (%t); want length 19 and no tombstones", record, ok)
	}
	if err := arena.setStringStopped(table, 1, slot(100)); err != nil {
		t.Fatal(err)
	}
	record, _ = arena.lookup(table)
	last := arena.orders[int(record.orderOffset+record.orderLength-1)]
	if last.key != machineTableStringKey(1) || last.value != slot(100) || last.present == 0 {
		t.Fatalf("reinserted compacted key = %#v; want appended string key 1", last)
	}
}

func TestMachineTableArenaRejectsInvalidIterationResumptionKeys(t *testing.T) {
	var arena machineTableArena
	table, err := arena.newTableStopped(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	for key := slot(1); key <= 2; key++ {
		if err := arena.setSlotStopped(table, key, slot(key+10)); err != nil {
			t.Fatal(err)
		}
	}
	first, _, cursor, ok, err := arena.next(table, machineTableCursor{})
	if err != nil || !ok || first != machineTableSlotKey(1) {
		t.Fatalf("first iteration = %#v (%t), err %v", first, ok, err)
	}
	if err := arena.setSlotStopped(table, 1, slotNil); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok, err := arena.next(table, cursor); ok || err != errMachineTableInvalidKey {
		t.Fatalf("resume after deleting prior key = ok %t, err %v", ok, err)
	}
	missing := machineTableCursor{key: machineTableSlotKey(99), set: 1}
	if _, _, _, ok, err := arena.next(table, missing); ok || err != errMachineTableInvalidKey {
		t.Fatalf("resume after missing key = ok %t, err %v", ok, err)
	}
	malformed := machineTableCursor{key: machineTableSlotKey(2), set: 2}
	if _, _, _, ok, err := arena.next(table, malformed); ok || err != errMachineTableInvalidKey {
		t.Fatalf("resume with malformed cursor = ok %t, err %v", ok, err)
	}
}

func TestMachineTableArenaGrowthKeepsTablesIndependent(t *testing.T) {
	var arena machineTableArena
	first, err := arena.newTableStopped(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := arena.newTableStopped(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.setStringStopped(second, 1, slot(99)); err != nil {
		t.Fatal(err)
	}
	for index := uint32(1); index <= 128; index++ {
		if err := arena.setArrayStopped(first, index, slot(index)); err != nil {
			t.Fatalf("array %d: %v", index, err)
		}
		if err := arena.setSlotStopped(first, slot(index), slot(index+1000)); err != nil {
			t.Fatalf("record %d: %v", index, err)
		}
	}
	for index := uint32(1); index <= 128; index++ {
		if got, ok := arena.getArray(first, index); !ok || got != slot(index) {
			t.Fatalf("array %d = %#x (%t)", index, got, ok)
		}
		if got, ok := arena.getSlot(first, slot(index)); !ok || got != slot(index+1000) {
			t.Fatalf("record %d = %#x (%t)", index, got, ok)
		}
	}
	if got, ok := arena.getString(second, 1); !ok || got != slot(99) {
		t.Fatalf("second table changed during first growth: %#x (%t)", got, ok)
	}
	firstRecord, ok := arena.lookup(first)
	if !ok || firstRecord.arrayCapacity < 128 || firstRecord.fieldCapacity <= machineTableInitialFieldCapacity {
		t.Fatalf("growth record = %#v (%t)", firstRecord, ok)
	}
	third, err := arena.newTableStopped(0, 0)
	if err != nil || first != 1 || second != 2 || third != 3 {
		t.Fatalf("table identities after growth = %d, %d, %d; err %v", first, second, third, err)
	}
}

func TestMachineTableArenaBoundsResetAndClose(t *testing.T) {
	var arena machineTableArena
	table, err := arena.newTableStopped(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.setArrayStopped(table, 2, slot(22)); err != nil {
		t.Fatal(err)
	}
	if err := arena.setStringStopped(table, 2, slot(33)); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []machineTableID{invalidMachineTableID, table + 1, machineTableID(^uint32(0))} {
		if _, ok := arena.getArray(invalid, 1); ok {
			t.Fatalf("invalid table %d was readable", invalid)
		}
		if err := arena.setArrayStopped(invalid, 1, slot(1)); err != errMachineTableInvalidID {
			t.Fatalf("invalid table %d set error = %v", invalid, err)
		}
		if _, _, _, ok, err := arena.next(invalid, machineTableCursor{}); ok || err != errMachineTableInvalidID {
			t.Fatalf("invalid table %d next = ok %t, err %v", invalid, ok, err)
		}
	}
	if err := arena.setArrayStopped(table, 0, slot(1)); err != errMachineTableInvalidKey {
		t.Fatalf("zero array key error = %v", err)
	}
	if err := arena.setStringStopped(table, invalidMachineStringID, slot(1)); err != errMachineTableInvalidKey {
		t.Fatalf("zero string key error = %v", err)
	}

	arrayCapacity, fieldCapacity, orderCapacity, tableCapacity :=
		cap(arena.arrays), cap(arena.fields), cap(arena.orders), cap(arena.tables)
	arena.reset()
	if arena.closed || len(arena.arrays) != 0 || len(arena.fields) != 0 || len(arena.orders) != 0 || len(arena.tables) != 0 {
		t.Fatalf("reset retained logical state: %#v", arena)
	}
	if cap(arena.arrays) != arrayCapacity || cap(arena.fields) != fieldCapacity ||
		cap(arena.orders) != orderCapacity || cap(arena.tables) != tableCapacity {
		t.Fatal("reset discarded reusable backing capacity")
	}
	if _, ok := arena.getArray(table, 2); ok {
		t.Fatal("reset table remained readable")
	}
	reused, err := arena.newTableStopped(0, 0)
	if err != nil || reused != 1 {
		t.Fatalf("post-reset table = %d, err %v; want dense ID 1", reused, err)
	}

	arena.close()
	if !arena.closed || arena.arrays != nil || arena.fields != nil || arena.orders != nil || arena.tables != nil {
		t.Fatalf("close retained arena state: %#v", arena)
	}
	if _, err := arena.newTableStopped(0, 0); err != errMachineTableArenaClosed {
		t.Fatalf("closed create error = %v", err)
	}
	if err := arena.setArrayStopped(reused, 1, slot(1)); err != errMachineTableArenaClosed {
		t.Fatalf("closed set error = %v", err)
	}
	if _, ok := arena.lookup(reused); ok {
		t.Fatal("closed table remained readable")
	}
	arena.close()
}

func TestMachineTableArenaRollbackReusesHighWaterStorageWithoutStaleEntries(t *testing.T) {
	var arena machineTableArena
	checkpoint := arena.checkpointStopped()
	first, err := arena.newTableStopped(4, 1)
	if err != nil {
		t.Fatal(err)
	}
	for index := uint32(1); index <= 4; index++ {
		if err := arena.setArrayStopped(first, index, slot(index)); err != nil {
			t.Fatal(err)
		}
	}
	if err := arena.setSlotStopped(first, slotBool(true), slot(9)); err != nil {
		t.Fatal(err)
	}
	arrayHighWater, fieldHighWater, orderHighWater := len(arena.arrays), len(arena.fields), len(arena.orders)
	if !arena.rollbackStopped(checkpoint) {
		t.Fatal("rollback rejected transient-only arena")
	}

	second, err := arena.newTableStopped(4, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("reused table ID = %d, want %d", second, first)
	}
	if err := arena.setArrayStopped(second, 4, slot(44)); err != nil {
		t.Fatal(err)
	}
	for index := uint32(1); index < 4; index++ {
		if value, present := arena.getArray(second, index); present || value != slotNil {
			t.Fatalf("reused sparse index %d = %#x (%t), want nil", index, value, present)
		}
	}
	if value, present := arena.getSlot(second, slotBool(true)); present || value != slotNil {
		t.Fatalf("reused record field = %#x (%t), want nil", value, present)
	}
	if len(arena.arrays) != arrayHighWater || len(arena.fields) != fieldHighWater || len(arena.orders) != orderHighWater {
		t.Fatalf("reuse regrew storage: arrays=%d fields=%d orders=%d, want %d/%d/%d",
			len(arena.arrays), len(arena.fields), len(arena.orders), arrayHighWater, fieldHighWater, orderHighWater)
	}
}

func machineTableTypeHasPointers(valueType reflect.Type) bool {
	switch valueType.Kind() {
	case reflect.Array:
		return machineTableTypeHasPointers(valueType.Elem())
	case reflect.Struct:
		for index := 0; index < valueType.NumField(); index++ {
			if machineTableTypeHasPointers(valueType.Field(index).Type) {
				return true
			}
		}
		return false
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer,
		reflect.Slice, reflect.String, reflect.UnsafePointer:
		return true
	default:
		return false
	}
}
