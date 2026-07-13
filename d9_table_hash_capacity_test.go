package ember

import (
	"strconv"
	"testing"
	"unsafe"
)

var (
	d9TableSink *Table
	d9ValueSink Value
	d9KeySink   string
)

func d9Keys(dynamicCount, distribution int) []string {
	keys := make([]string, 0, maxInlineStringFields+dynamicCount)
	for index := 0; index < maxInlineStringFields; index++ {
		keys = append(keys, "inline_"+strconv.Itoa(index))
	}
	prefix := "dynamic_a_"
	if distribution%2 != 0 {
		prefix = "dynamic_b_"
	}
	for index := 0; index < dynamicCount; index++ {
		keys = append(keys, prefix+strconv.Itoa(index*7+distribution))
	}
	return keys
}

func d9BuildTable(keys []string) *Table {
	table := NewTable()
	for index, key := range keys {
		table.setRawStringField(key, NumberValue(float64(index+1)))
	}
	return table
}

func d9ReadKeys(table *Table, keys []string) {
	for _, key := range keys {
		value, ok := table.rawStringField(key)
		if !ok || value.IsNil() {
			d9KeySink, d9ValueSink = key, NilValue()
			return
		}
		d9ValueSink = value
	}
}

func TestD9OverflowTablePreservesSemantics(t *testing.T) {
	keys := d9Keys(1, 0)
	table := d9BuildTable(keys)
	fields := table.hashFields()
	if fields == nil || len(fields.entries) != tableHashInitialCapacity {
		t.Fatalf("hash capacity = %d, want %d", len(fields.entries), tableHashInitialCapacity)
	}
	if got := table.activeInlineStringFieldCount(); got != maxInlineStringFields {
		t.Fatalf("inline field count = %d, want %d", got, maxInlineStringFields)
	}
	if got := table.hashFieldCount(); got != 1 {
		t.Fatalf("overflow field count = %d, want 1", got)
	}
	d9ReadKeys(table, keys)
	if d9ValueSink.IsNil() {
		t.Fatalf("lookup failed for %q", d9KeySink)
	}
	table.ensureIterationJournal()
	if got, want := len(table.iteration.keys), len(keys); got != want {
		t.Fatalf("iteration key count = %d, want %d", got, want)
	}
	before := append([]tableIterationKey(nil), table.iteration.keys...)
	table.setRawStringField(keys[len(keys)-1], NumberValue(999))
	if !tableKeysEqual(table.iteration.keys[len(keys)-1].key, before[len(keys)-1].key) {
		t.Fatal("existing-key update changed iteration identity")
	}
	table.setRawStringField(keys[0], NilValue())
	table.setRawStringField(keys[0], NumberValue(1000))
	d9ReadKeys(table, keys)
	if d9ValueSink.IsNil() {
		t.Fatalf("reinserted lookup failed for %q", d9KeySink)
	}
}

func TestD9OverflowHashGenerationBoundaries(t *testing.T) {
	initial := tableHashInitialCapacity
	var fields tableHashFields
	fields.grow()
	if fields.generation != 1 || len(fields.entries) != initial {
		t.Fatalf("initial generation/capacity = %d/%d, want 1/%d", fields.generation, len(fields.entries), initial)
	}
	key := func(index int) tableKey {
		text := "generation_" + strconv.Itoa(index)
		box := newStringBox(text)
		return tableKey{kind: StringKind, str: text, strBox: box, strHash: box.hash}
	}
	if !fields.set(key(0), NumberValue(0)) || fields.generation != 2 {
		t.Fatalf("first set generation = %d, want 2", fields.generation)
	}
	if fields.set(key(0), NumberValue(1)) || fields.generation != 2 {
		t.Fatalf("existing update changed generation to %d", fields.generation)
	}
	threshold := initial * 3 / 4
	for index := 1; index < threshold-1; index++ {
		fields.set(key(index), NumberValue(float64(index)))
	}
	beforeGrowth := fields.generation
	if !fields.set(key(threshold-1), NumberValue(float64(threshold))) {
		t.Fatal("growth-boundary insert did not add a key")
	}
	if fields.generation != beforeGrowth+2 || len(fields.entries) != initial*2 {
		t.Fatalf("growth generation/capacity = %d/%d, want %d/%d", fields.generation, len(fields.entries), beforeGrowth+2, initial*2)
	}
	beforeDelete := fields.generation
	if !fields.delete(key(0)) || fields.generation != beforeDelete+1 || fields.tombstones != 1 {
		t.Fatalf("delete generation/tombstones = %d/%d, want %d/1", fields.generation, fields.tombstones, beforeDelete+1)
	}
	beforeReuse := fields.generation
	if !fields.set(key(0), NumberValue(2)) || fields.generation != beforeReuse+1 || fields.tombstones != 0 {
		t.Fatalf("tombstone reuse generation/tombstones = %d/%d, want %d/0", fields.generation, fields.tombstones, beforeReuse+1)
	}

	var tombstoneGrowth tableHashFields
	tombstoneGrowth.grow()
	for index := 0; index < threshold-1; index++ {
		tombstoneGrowth.set(key(index), NumberValue(float64(index)))
	}
	beforeTombstoneDelete := tombstoneGrowth.generation
	if !tombstoneGrowth.delete(key(0)) || tombstoneGrowth.generation != beforeTombstoneDelete+1 || tombstoneGrowth.tombstones != 1 {
		t.Fatalf("tombstone setup generation/tombstones = %d/%d, want %d/1", tombstoneGrowth.generation, tombstoneGrowth.tombstones, beforeTombstoneDelete+1)
	}
	beforeTombstoneGrowth := tombstoneGrowth.generation
	tombstoneGrowth.set(key(threshold-1), NumberValue(float64(threshold-1)))
	if tombstoneGrowth.generation != beforeTombstoneGrowth+2 || tombstoneGrowth.tombstones != 0 || len(tombstoneGrowth.entries) != initial*2 {
		t.Fatalf("tombstone growth generation/capacity/tombstones = %d/%d/%d, want %d/%d/0", tombstoneGrowth.generation, len(tombstoneGrowth.entries), tombstoneGrowth.tombstones, beforeTombstoneGrowth+2, initial*2)
	}
}

func BenchmarkD9OverflowHashCapacity(b *testing.B) {
	cases := []struct {
		name         string
		dynamicCount int
		tableCount   int
	}{
		{name: "one_overflow", dynamicCount: 1, tableCount: 1},
		{name: "dynamic_8", dynamicCount: 8, tableCount: 1},
		{name: "dynamic_16", dynamicCount: 16, tableCount: 1},
		{name: "dynamic_32", dynamicCount: 32, tableCount: 1},
		{name: "dynamic_128", dynamicCount: 128, tableCount: 1},
		{name: "many_barely_overflow", dynamicCount: 1, tableCount: 128},
	}
	for _, testCase := range cases {
		for distribution := 0; distribution < 2; distribution++ {
			keys := d9Keys(testCase.dynamicCount, distribution)
			name := testCase.name + "/distribution_" + strconv.Itoa(distribution)
			b.Run(name+"/fresh", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					for count := 0; count < testCase.tableCount; count++ {
						d9TableSink = d9BuildTable(keys)
					}
				}
			})
			b.Run(name+"/steady", func(b *testing.B) {
				tables := make([]*Table, testCase.tableCount)
				for count := range tables {
					tables[count] = d9BuildTable(keys)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					for _, table := range tables {
						d9ReadKeys(table, keys[maxInlineStringFields:])
					}
				}
			})
		}
	}
}

func TestD9OverflowHashCapacityFootprint(t *testing.T) {
	table := d9BuildTable(d9Keys(1, 0))
	fields := table.hashFields()
	hashBytes := len(fields.entries) * int(unsafe.Sizeof(tableHashEntry{}))
	journalBytes := 0
	journalCapacity := 0
	journalIndexed := false
	if table.iteration != nil {
		journalCapacity = cap(table.iteration.keys)
		journalBytes = journalCapacity * int(unsafe.Sizeof(tableIterationKey{}))
		journalIndexed = table.iteration.index != nil
	}
	t.Logf("capacity=%d hash_entries=%d hash_bytes=%d journal_cap=%d journal_bytes=%d journal_index=%t coupled_bytes=%d entry_size=%d journal_entry_size=%d", len(fields.entries), len(fields.entries), hashBytes, journalCapacity, journalBytes, journalIndexed, hashBytes+journalBytes, unsafe.Sizeof(tableHashEntry{}), unsafe.Sizeof(tableIterationKey{}))
}
