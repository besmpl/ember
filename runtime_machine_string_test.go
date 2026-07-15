package ember

import (
	"math"
	"reflect"
	"testing"
)

func TestMachineStringArenaLayout(t *testing.T) {
	recordType := reflect.TypeOf(machineStringRecord{})
	if recordType.NumField() != 3 {
		t.Fatalf("machineStringRecord fields = %d, want 3", recordType.NumField())
	}
	for _, field := range []reflect.StructField{
		recordType.Field(0), recordType.Field(1), recordType.Field(2),
	} {
		switch field.Type.Kind() {
		case reflect.Uint32, reflect.Uint64:
		default:
			t.Fatalf("machineStringRecord.%s has pointer-bearing/non-scalar type %s", field.Name, field.Type)
		}
	}
	if got := reflect.TypeOf(machineStringRecord{}).Align(); got != 8 {
		t.Fatalf("machineStringRecord alignment = %d, want 8", got)
	}
	if got := reflect.TypeOf(machineStringRecord{}).Size(); got != 16 {
		t.Fatalf("machineStringRecord size = %d, want 16", got)
	}
	if reflect.TypeOf(machineStringID(0)).Kind() != reflect.Uint32 {
		t.Fatal("machineStringID is not a pointer-free uint32")
	}
	if reflect.TypeOf(byte(0)).Kind() != reflect.Uint8 {
		t.Fatal("string byte storage is not a pointer-free byte element")
	}
}

func TestMachineStringArenaInternsExactBytesAndDenseIDs(t *testing.T) {
	var arena machineStringArena
	cases := [][]byte{
		{},
		{},
		{0x00, 0xff, 0x80, 0x00},
		[]byte("ember"),
		[]byte("ember"),
	}
	wantIDs := []machineStringID{1, 1, 2, 3, 3}
	for index, value := range cases {
		id, err := arena.internBytesStopped(value)
		if err != nil {
			t.Fatalf("case %d: %v", index, err)
		}
		if id != wantIDs[index] || id == invalidMachineStringID {
			t.Fatalf("case %d ID = %d, want %d", index, id, wantIDs[index])
		}
	}
	stringID, err := arena.internStringStopped(string([]byte{0x00, 0xff, 0x80, 0x00}))
	if err != nil {
		t.Fatal(err)
	}
	if stringID != 2 {
		t.Fatalf("string input ID = %d, want duplicate ID 2", stringID)
	}
	if got, ok := arena.bytesFor(2); !ok || !bytesEqual(got, cases[2]) {
		t.Fatalf("exact bytes = %#v (%t), want %#v", got, ok, cases[2])
	}
	record, ok := arena.lookup(2)
	if !ok || record.length != 4 || record.hash != machineStringHash(cases[2]) {
		t.Fatalf("record = %#v (%t), want length/hash for exact bytes", record, ok)
	}
}

func TestMachineStringArenaGrowthAndHashCollisions(t *testing.T) {
	var arena machineStringArena
	values := make([][]byte, 0, 96)
	for index := 0; index < 96; index++ {
		value := []byte{byte(index), byte(index >> 8), 'x', byte(index * 31)}
		values = append(values, value)
		id, err := arena.internBytesStopped(value)
		if err != nil {
			t.Fatalf("intern %d: %v", index, err)
		}
		if id != machineStringID(index+1) {
			t.Fatalf("intern %d ID = %d, want %d", index, id, index+1)
		}
	}
	if len(arena.index) <= machineStringInitialIndex || len(arena.records) != len(values) {
		t.Fatalf("growth did not occur: index=%d records=%d", len(arena.index), len(arena.records))
	}
	for index, value := range values {
		id, err := arena.internBytesStopped(value)
		if err != nil || id != machineStringID(index+1) {
			t.Fatalf("duplicate %d = %d, err=%v; want %d", index, id, err, index+1)
		}
	}

	// Find two values that land in the same initial table bucket. This exercises
	// linear probing without relying on a particular fixture string/hash pair.
	buckets := make(map[int][]byte)
	var first, second []byte
	for index := 0; index < 256 && second == nil; index++ {
		value := []byte{byte(index), byte(index >> 8), 0x7f}
		bucket := int(machineStringHash(value)) & (machineStringInitialIndex - 1)
		if previous, exists := buckets[bucket]; exists && !bytesEqual(previous, value) {
			first, second = previous, value
			break
		}
		buckets[bucket] = value
	}
	if second == nil {
		t.Fatal("failed to construct a hash collision fixture")
	}
	var collisionArena machineStringArena
	firstID, err := collisionArena.internBytesStopped(first)
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := collisionArena.internBytesStopped(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstID == secondID || len(collisionArena.records) != 2 {
		t.Fatalf("collision IDs = %d,%d records=%d", firstID, secondID, len(collisionArena.records))
	}
	for _, id := range []machineStringID{firstID, secondID} {
		if _, ok := collisionArena.bytesFor(id); !ok {
			t.Fatalf("collision ID %d is not readable", id)
		}
	}
}

func TestMachineStringArenaLookupIsCheckedAndIndexIndependent(t *testing.T) {
	var arena machineStringArena
	id, err := arena.internBytesStopped([]byte("stable"))
	if err != nil {
		t.Fatal(err)
	}
	indexCapacity := cap(arena.index)
	arena.index = nil
	if got, ok := arena.bytesFor(id); !ok || string(got) != "stable" {
		t.Fatalf("index-independent lookup = %q (%t)", got, ok)
	}
	if cap(arena.index) != 0 || indexCapacity == 0 {
		t.Fatalf("stable lookup unexpectedly touched index: old cap=%d new cap=%d", indexCapacity, cap(arena.index))
	}
	for _, invalid := range []machineStringID{invalidMachineStringID, id + 1, machineStringID(math.MaxUint32)} {
		if _, ok := arena.lookup(invalid); ok {
			t.Fatalf("lookup(%d) unexpectedly succeeded", invalid)
		}
	}
	arena.records[0].offset = uint32(len(arena.data) + 1)
	if _, ok := arena.lookup(id); ok {
		t.Fatal("lookup accepted record offset past byte arena")
	}
	arena.records[0].offset = 0
	arena.records[0].length = uint32(len(arena.data) + 1)
	if _, ok := arena.bytesFor(id); ok {
		t.Fatal("bytesFor accepted record length past byte arena")
	}
}

func TestMachineStringArenaResetAndCloseLifecycle(t *testing.T) {
	var arena machineStringArena
	if _, err := arena.internStringStopped("before reset"); err != nil {
		t.Fatal(err)
	}
	byteCapacity, recordCapacity := cap(arena.data), cap(arena.records)
	arena.reset()
	if arena.closed || len(arena.data) != 0 || len(arena.records) != 0 || len(arena.index) != 0 {
		t.Fatalf("reset left live logical state: %#v", arena)
	}
	if cap(arena.data) != byteCapacity || cap(arena.records) != recordCapacity {
		t.Fatalf("reset discarded reusable capacities: data=%d/%d records=%d/%d", cap(arena.data), byteCapacity, cap(arena.records), recordCapacity)
	}
	for _, value := range arena.data[:cap(arena.data)] {
		if value != 0 {
			t.Fatal("reset retained old string bytes")
		}
	}
	id, err := arena.internStringStopped("after reset")
	if err != nil || id != 1 {
		t.Fatalf("post-reset intern = %d, err=%v; want ID 1", id, err)
	}
	arena.close()
	if !arena.closed || arena.records != nil || arena.data != nil || arena.index != nil {
		t.Fatalf("close did not release state: %#v", arena)
	}
	if _, err := arena.internStringStopped("closed"); err != errMachineStringArenaClosed {
		t.Fatalf("closed intern error = %v, want %v", err, errMachineStringArenaClosed)
	}
	if _, ok := arena.lookup(id); ok {
		t.Fatal("closed lookup unexpectedly succeeded")
	}
	arena.close()
}

func TestMachineStringArenaReserveStopped(t *testing.T) {
	var arena machineStringArena
	if err := arena.reserveStopped(128, 12); err != nil {
		t.Fatal(err)
	}
	if cap(arena.data) < 128 || cap(arena.records) < 12 {
		t.Fatalf("reservation capacities data=%d records=%d", cap(arena.data), cap(arena.records))
	}
	if _, err := arena.internStringStopped("seed"); err != nil {
		t.Fatal(err)
	}
	if err := arena.reserveStopped(1, 1); err == nil {
		t.Fatal("smaller reservation unexpectedly succeeded")
	}
}
