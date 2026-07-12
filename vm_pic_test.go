package ember

import "testing"

func TestDynamicStringIndexCachePointerHitsAvoidHashFallback(t *testing.T) {
	proto, err := Compile(`
local row = {hp = 1}
local key = "hp"
local total = 0
for i = 1, 8 do
	 total = total + row[key]
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 8 {
		t.Fatalf("result is %v (%t), want 8", got, ok)
	}
	if snapshot.picCounts.pointerHits == 0 {
		t.Fatal("pointer hits = 0, want warmed interned key loop to hit the live slot")
	}
	if snapshot.picCounts.hashByteFallbacks != 0 {
		t.Fatalf("hash/byte fallbacks = %d, want zero for warmed interned key loop", snapshot.picCounts.hashByteFallbacks)
	}
}

func TestDynamicStringIndexCacheInlinePointerHitPreservesLayoutAndInvalidation(t *testing.T) {
	table := NewTable()
	box := newStringBox("hp")
	key := stringValueFromBox(box)
	table.setRawStringFieldBox(box.text, box, NumberValue(1))
	slot, ok := table.rawStringFieldSlotBox(box)
	if !ok || slot.token.storage != 0 {
		t.Fatalf("hp slot = %#v, %t; want inline slot", slot, ok)
	}

	var cache dynamicStringIndexCache
	cache.storeValue(table, key, slot)
	layoutVersion := table.stringVersion
	valueVersion := table.stringValueVersion
	if !cache.writeValue(table, key, NumberValue(7)) {
		t.Fatal("cache.writeValue(hp) missed, want inline pointer hit")
	}
	if table.stringVersion != layoutVersion {
		t.Fatalf("inline value write changed layout version from %d to %d", layoutVersion, table.stringVersion)
	}
	if table.stringValueVersion != valueVersion+1 {
		t.Fatalf("inline value version = %d, want %d", table.stringValueVersion, valueVersion+1)
	}
	value, ok := cache.getValue(table, key)
	if got, numberOK := value.Number(); !ok || !numberOK || got != 7 {
		t.Fatalf("cached hp = %v, %t/%t; want 7", got, ok, numberOK)
	}

	table.setRawStringFieldBox(box.text, box, NilValue())
	if _, ok := cache.getValue(table, key); ok {
		t.Fatal("cache survived inline field deletion with a stale slot")
	}
	if cache.writeValue(table, key, NumberValue(9)) {
		t.Fatal("stale cache write resurrected a deleted inline field")
	}
}

func TestDynamicStringIndexCacheOverflowHitUsesCurrentIndexedValue(t *testing.T) {
	table := NewTable()
	for index := 0; index < maxInlineStringFields; index++ {
		table.setRawStringFieldBox("field"+string(rune('0'+index)), nil, NumberValue(float64(index)))
	}
	keyBox := newStringBox("target")
	table.setRawStringFieldBox(keyBox.text, keyBox, NumberValue(1))
	slot, ok := table.rawStringFieldSlot(keyBox.text)
	if !ok || slot.token.storage != 1 {
		t.Fatalf("rawStringFieldSlot(target) = %#v, %t; want overflow slot", slot, ok)
	}

	var cache dynamicStringIndexCache
	cache.storeValue(table, stringValueFromBox(keyBox), slot)
	var counts directFramePICCounts
	value, ok := cache.getValueCounted(table, stringValueFromBox(keyBox), &counts)
	if !ok {
		t.Fatal("cache.getValueCounted(target) missed, want indexed hit")
	}
	if got, ok := value.Number(); !ok || got != 1 {
		t.Fatalf("initial cached value is %v (%t), want 1", got, ok)
	}
	if !cache.writeValueCounted(table, stringValueFromBox(keyBox), NumberValue(7), &counts) {
		t.Fatal("cache.writeValueCounted(target) missed, want indexed hit")
	}
	value, ok = cache.getValueCounted(table, stringValueFromBox(keyBox), &counts)
	if !ok {
		t.Fatal("cache.getValueCounted(target) missed after value update")
	}
	if got, ok := value.Number(); !ok || got != 7 {
		t.Fatalf("updated cached value is %v (%t), want current value 7", got, ok)
	}
	if counts.indexedHashHits != 3 {
		t.Fatalf("indexedHashHits = %d, want read/write/read indexed hits", counts.indexedHashHits)
	}
	if counts.pointerHits != 3 {
		t.Fatalf("pointerHits = %d, want read/write/read pointer hits", counts.pointerHits)
	}
	if counts.hashByteFallbacks != 0 {
		t.Fatalf("hashByteFallbacks = %d, want zero for the same key box", counts.hashByteFallbacks)
	}
}

func TestDynamicStringIndexCacheInvalidatesAcrossOverflowRehashAndReinsert(t *testing.T) {
	table := NewTable()
	for index := 0; index < maxInlineStringFields; index++ {
		table.setRawStringField("field"+string(rune('0'+index)), NumberValue(float64(index)))
	}
	box := newStringBox("target")
	table.setRawStringFieldBox(box.text, box, NumberValue(1))
	slot, ok := table.rawStringFieldSlot(box.text)
	if !ok || slot.token.storage != 1 {
		t.Fatalf("target slot = %#v, %t; want overflow slot", slot, ok)
	}
	var cache dynamicStringIndexCache
	key := stringValueFromBox(box)
	cache.storeValue(table, key, slot)

	for index := 1; index <= 64; index++ {
		if err := table.rawSet(NumberValue(float64(index+100)), NumberValue(float64(index))); err != nil {
			t.Fatalf("insert generic key %d: %v", index, err)
		}
	}
	if _, ok := cache.getValueCounted(table, key, nil); ok {
		t.Fatal("cache survived overflow sidecar rehash with a stale slot")
	}

	table.setRawStringFieldBox(box.text, box, NilValue())
	table.setRawStringFieldBox(box.text, box, NumberValue(7))
	if _, ok := cache.getValueCounted(table, key, nil); ok {
		t.Fatal("cache survived overflow tombstone delete/reinsert with a stale slot")
	}
	refreshed, ok := table.rawStringFieldSlot(box.text)
	if !ok {
		t.Fatal("reinserted target has no live slot")
	}
	cache.storeValue(table, key, refreshed)
	value, ok := cache.getValueCounted(table, key, nil)
	if got, numberOK := value.Number(); !ok || !numberOK || got != 7 {
		t.Fatalf("refreshed cache value = %v, %t/%t; want 7", got, ok, numberOK)
	}
}

func TestWarmBoxedStringFieldLookupDoesNotAllocate(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	table := NewTable()
	box := newStringBox("target")
	table.setRawStringFieldBox(box.text, box, NumberValue(1))
	if _, ok := table.rawStringFieldBox(box); !ok {
		t.Fatal("warm boxed lookup missed during setup")
	}
	allocs := testing.AllocsPerRun(1000, func() {
		if _, ok := table.rawStringFieldBox(box); !ok {
			t.Fatal("warm boxed lookup missed")
		}
	})
	if allocs != 0 {
		t.Fatalf("warm boxed string lookup allocated %.2f times, want zero", allocs)
	}
}
