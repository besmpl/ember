package ember

import "testing"

func TestMetamethodNegativeCachesInvalidateOnInsertDeleteAndReinsert(t *testing.T) {
	object := NewTable()
	metatable := NewTable()
	object.setMetatable(metatable)

	if _, ok, err := object.cachedIndexFallback(); err != nil || ok {
		t.Fatalf("initial __index lookup = ok %t err %v, want negative", ok, err)
	}
	if _, ok, err := object.cachedNewIndexFallback(); err != nil || ok {
		t.Fatalf("initial __newindex lookup = ok %t err %v, want negative", ok, err)
	}
	indexFirst := NewTable()
	newIndexFirst := NewTable()
	metatable.setRawStringField("__index", TableValue(indexFirst))
	metatable.setRawStringField("__newindex", TableValue(newIndexFirst))
	assertCachedMetamethodTable(t, object.cachedIndexFallback, indexFirst, "inserted __index")
	assertCachedMetamethodTable(t, object.cachedNewIndexFallback, newIndexFirst, "inserted __newindex")

	metatable.setRawStringField("__index", NilValue())
	metatable.setRawStringField("__newindex", NilValue())
	if _, ok, err := object.cachedIndexFallback(); err != nil || ok {
		t.Fatalf("deleted __index lookup = ok %t err %v, want negative", ok, err)
	}
	if _, ok, err := object.cachedNewIndexFallback(); err != nil || ok {
		t.Fatalf("deleted __newindex lookup = ok %t err %v, want negative", ok, err)
	}
	indexSecond := NewTable()
	newIndexSecond := NewTable()
	metatable.setRawStringField("__index", TableValue(indexSecond))
	metatable.setRawStringField("__newindex", TableValue(newIndexSecond))
	assertCachedMetamethodTable(t, object.cachedIndexFallback, indexSecond, "reinserted __index")
	assertCachedMetamethodTable(t, object.cachedNewIndexFallback, newIndexSecond, "reinserted __newindex")
}

func TestMetamethodCachesStayLiveAcrossUnrelatedValueWritesAndReplacement(t *testing.T) {
	object := NewTable()
	firstMetatable := NewTable()
	firstIndex := NewTable()
	firstMetatable.setRawStringField("__index", TableValue(firstIndex))
	firstMetatable.setRawStringField("other", NumberValue(1))
	object.setMetatable(firstMetatable)
	assertCachedMetamethodTable(t, object.cachedIndexFallback, firstIndex, "initial __index")
	firstSlot := object.cold.indexCache.slot

	firstMetatable.setRawStringField("other", NumberValue(2))
	assertCachedMetamethodTable(t, object.cachedIndexFallback, firstIndex, "__index after unrelated value write")
	if object.cold.indexCache.slot != firstSlot {
		t.Fatal("unrelated metatable value write replaced the live __index slot cache")
	}

	updatedIndex := NewTable()
	firstMetatable.setRawStringField("__index", TableValue(updatedIndex))
	assertCachedMetamethodTable(t, object.cachedIndexFallback, updatedIndex, "updated __index value")

	secondMetatable := NewTable()
	secondIndex := NewTable()
	secondMetatable.setRawStringField("__index", TableValue(secondIndex))
	object.setMetatable(secondMetatable)
	assertCachedMetamethodTable(t, object.cachedIndexFallback, secondIndex, "replacement metatable __index")
}

func TestMetamethodFallbackFindsInlineKeysAfterOverflow(t *testing.T) {
	index := NewTable()
	newIndex := NewTable()
	metatable := NewTable()
	metatable.setRawStringField("__index", TableValue(index))
	metatable.setRawStringField("__newindex", TableValue(newIndex))
	for field := 0; field < maxInlineStringFields-2; field++ {
		metatable.setRawStringField("field"+string(rune('a'+field)), NumberValue(float64(field)))
	}
	metatable.setRawStringField("overflow", NumberValue(1))
	if !metatable.hasStringOverflow() {
		t.Fatal("metatable did not create overflow storage")
	}

	object := NewTable()
	object.setMetatable(metatable)
	assertCachedMetamethodTable(t, object.cachedIndexFallback, index, "overflow inline __index")
	assertCachedMetamethodTable(t, object.cachedNewIndexFallback, newIndex, "overflow inline __newindex")
	if object.cold.indexCache.ready || object.cold.newIndexCache.ready {
		t.Fatal("uncacheable inline metamethod installed a cache record")
	}
}

func assertCachedMetamethodTable(t *testing.T, lookup func() (Value, bool, error), want *Table, label string) {
	t.Helper()
	value, ok, err := lookup()
	if err != nil || !ok {
		t.Fatalf("%s lookup = ok %t err %v", label, ok, err)
	}
	got, tableOK := value.Table()
	if !tableOK || got != want {
		t.Fatalf("%s value = %p, %t; want %p", label, got, tableOK, want)
	}
}
