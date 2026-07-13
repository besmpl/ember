package ember

import (
	"strconv"
	"testing"
)

func d6StringJournalTable(count int) *Table {
	table := NewTable()
	for index := 0; index < count; index++ {
		table.setRawStringField("key"+strconv.Itoa(index), NumberValue(float64(index)))
	}
	table.iteration = nil
	return table
}

func TestIterationJournalPreallocatesActiveKeys(t *testing.T) {
	table := NewTable()
	if err := table.rawSetArrayIndex(1, NumberValue(1)); err != nil {
		t.Fatalf("set array key: %v", err)
	}
	table.setRawStringField("inline", NumberValue(2))
	table.setRawStringField("inline2", NumberValue(3))
	for index := 2; index < maxInlineStringFields; index++ {
		table.setRawStringField("field"+strconv.Itoa(index), NumberValue(float64(index)))
	}
	table.setRawStringField("overflow", NumberValue(9))
	table.iteration = nil

	table.ensureIterationJournal()
	want := 1 + table.activeInlineStringFieldCount() + table.hashFieldCount()
	if got := len(table.iteration.keys); got != want {
		t.Fatalf("journal length = %d, want active count %d", got, want)
	}
	if got := cap(table.iteration.keys); got != want {
		t.Fatalf("journal capacity = %d, want exact initial active count %d", got, want)
	}
}

func TestIterationJournalIndexBoundary(t *testing.T) {
	for _, test := range []struct {
		count     int
		wantIndex bool
	}{
		{count: 32, wantIndex: false},
		{count: 33, wantIndex: true},
	} {
		t.Run(strconv.Itoa(test.count), func(t *testing.T) {
			table := d6StringJournalTable(test.count)
			table.ensureIterationJournal()
			if got := table.iteration.index != nil; got != test.wantIndex {
				t.Fatalf("journal index present = %t, want %t", got, test.wantIndex)
			}
		})
	}
}

func TestIterationJournalPreallocatesActiveKeysExcludingTombstones(t *testing.T) {
	table := d6StringJournalTable(40)
	for index := 8; index < 13; index++ {
		table.setRawStringField("key"+strconv.Itoa(index), NilValue())
	}
	table.iteration = nil
	table.ensureIterationJournal()
	if got := len(table.iteration.keys); got != 35 {
		t.Fatalf("journal length with tombstones = %d, want 35 active keys", got)
	}
	if got := cap(table.iteration.keys); got != 35 {
		t.Fatalf("journal capacity with tombstones = %d, want 35", got)
	}
	for _, entry := range table.iteration.keys {
		if !entry.present {
			t.Fatal("journal retained a tombstone")
		}
	}
}

func TestIterationJournalPreservesMixedStorageOrder(t *testing.T) {
	table := NewTable()
	if err := table.rawSetArrayIndex(1, NumberValue(1)); err != nil {
		t.Fatalf("set array key: %v", err)
	}
	table.setRawStringField("inline", NumberValue(2))
	table.setRawStringField("inline2", NumberValue(3))
	for index := 2; index < maxInlineStringFields; index++ {
		table.setRawStringField("field"+strconv.Itoa(index), NumberValue(float64(index)))
	}
	table.setRawStringField("overflow", NumberValue(9))
	table.iteration = nil
	table.ensureIterationJournal()

	want := make([]tableKey, 0, len(table.iteration.keys))
	for index, value := range table.array {
		if !value.IsNil() {
			want = append(want, tableKey{kind: NumberKind, number: float64(index + 1)})
		}
	}
	for _, field := range table.stringFields {
		if !field.value.IsNil() {
			want = append(want, tableKey{kind: StringKind, str: field.key, strBox: field.box, strHash: stringBoxHash(field.box, field.key)})
		}
	}
	if fields := table.hashFields(); fields != nil {
		fields.forEach(func(key tableKey, _ Value) { want = append(want, key) })
	}
	if len(want) != len(table.iteration.keys) {
		t.Fatalf("want %d mixed-order keys, got %d", len(want), len(table.iteration.keys))
	}
	for index, wantKey := range want {
		if !tableKeysEqual(table.iteration.keys[index].key, wantKey) {
			t.Fatalf("journal key %d = %#v, want %#v", index, table.iteration.keys[index].key, wantKey)
		}
	}
	expectedPrefix := []tableKey{{kind: NumberKind, number: 1}, {kind: StringKind, str: "inline"}, {kind: StringKind, str: "inline2"}}
	for index := 2; index < maxInlineStringFields; index++ {
		expectedPrefix = append(expectedPrefix, tableKey{kind: StringKind, str: "field" + strconv.Itoa(index)})
	}
	for index, wantKey := range expectedPrefix {
		if !tableKeysEqual(table.iteration.keys[index].key, wantKey) {
			t.Fatalf("mixed-order prefix key %d = %#v, want %#v", index, table.iteration.keys[index].key, wantKey)
		}
	}
}

func TestIterationJournalIndexFallsBackForEqualStringBoxes(t *testing.T) {
	table := d6StringJournalTable(33)
	box := newStringBox("key0")
	table.setRawStringFieldBox(box.text, box, NumberValue(100))
	table.iteration = nil
	table.ensureIterationJournal()
	other := newStringBox("key0")
	if box == other {
		t.Fatal("test requires distinct equal-content boxes")
	}
	index, ok := table.iterationKeyIndex(tableKey{kind: StringKind, str: other.text, strBox: other, strHash: other.hash})
	if !ok {
		t.Fatal("equal-content string box missed indexed journal fallback")
	}
	if !tableKeysEqual(table.iteration.keys[index].key, tableKey{kind: StringKind, str: box.text, strBox: box, strHash: box.hash}) {
		t.Fatalf("equal-content journal key = %#v", table.iteration.keys[index].key)
	}
}

func TestIterationJournalCompactsAfterSparseDeletesAndReinsert(t *testing.T) {
	table := d6StringJournalTable(40)
	table.ensureIterationJournal()
	if table.iteration.index == nil {
		t.Fatal("journal index missing for 40 keys")
	}
	for index := 0; index < 21; index++ {
		table.setRawStringField("key"+strconv.Itoa(index), NilValue())
	}
	if got := len(table.iteration.keys); got != 19 {
		t.Fatalf("compacted journal length = %d, want 19", got)
	}
	if table.iteration.tombstones != 0 {
		t.Fatalf("compacted journal tombstones = %d, want zero", table.iteration.tombstones)
	}
	if table.iteration.index == nil {
		t.Fatal("compacted journal dropped its index")
	}

	table.setRawStringField("key0", NumberValue(100))
	if index, ok := table.iterationKeyIndex(tableKey{kind: StringKind, str: "key0", strHash: hashString("key0")}); !ok || index != len(table.iteration.keys)-1 {
		t.Fatalf("reinserted key index = %d, %t; want appended at %d", index, ok, len(table.iteration.keys)-1)
	}
}

func TestIterationJournalClearsIndexAndJournal(t *testing.T) {
	table := d6StringJournalTable(33)
	table.ensureIterationJournal()
	if table.iteration == nil || table.iteration.index == nil {
		t.Fatal("setup did not build indexed journal")
	}
	table.clearRawStorage()
	if table.iteration != nil {
		t.Fatal("clear retained iteration journal")
	}
}

func TestIterationJournalReservesAfterHashGrowth(t *testing.T) {
	table := NewTable()
	for index := 0; index < maxInlineStringFields; index++ {
		table.setRawStringField("inline"+strconv.Itoa(index), NumberValue(float64(index)))
	}
	table.iteration = nil
	table.setRawStringField("overflow", NumberValue(9))
	if table.iteration == nil {
		t.Fatal("overflow insertion did not create iteration journal")
	}
	if got := len(table.iteration.keys); got != maxInlineStringFields+1 {
		t.Fatalf("journal length after first overflow = %d, want %d", got, maxInlineStringFields+1)
	}
	if cap(table.iteration.keys) <= len(table.iteration.keys) {
		t.Fatalf("journal capacity = %d, want reserve beyond length %d", cap(table.iteration.keys), len(table.iteration.keys))
	}
	if table.iteration.index != nil {
		t.Fatal("small journal built index early")
	}
	before := append([]tableIterationKey(nil), table.iteration.keys...)
	for index := 0; index < 4; index++ {
		table.setRawStringField("later"+strconv.Itoa(index), NumberValue(float64(index)))
	}
	if len(table.iteration.keys) != len(before)+4 {
		t.Fatalf("journal length after reserved appends = %d, want %d", len(table.iteration.keys), len(before)+4)
	}
	for index, entry := range before {
		if !tableKeysEqual(table.iteration.keys[index].key, entry.key) {
			t.Fatalf("journal key %d changed after reserved appends", index)
		}
	}
}

func TestIndexedIterationDeleteReinsertPublishesNewStringBox(t *testing.T) {
	table := d6StringJournalTable(40)
	first := newStringBox("target")
	table.setRawStringFieldBox(first.text, first, NumberValue(1))
	if table.iteration == nil || table.iteration.index == nil {
		t.Fatal("indexed journal setup failed")
	}
	table.setRawStringFieldBox(first.text, first, NilValue())
	second := newStringBox("target")
	if first == second {
		t.Fatal("test requires distinct string boxes")
	}
	table.setRawStringFieldBox(second.text, second, NumberValue(2))
	index, ok := table.iterationKeyIndex(tableKey{kind: StringKind, str: second.text, strBox: second, strHash: second.hash})
	if !ok {
		t.Fatal("reinserted indexed key missing")
	}
	if table.iteration.keys[index].key.strBox != second {
		t.Fatalf("reinserted journal box = %p, want %p", table.iteration.keys[index].key.strBox, second)
	}
}
