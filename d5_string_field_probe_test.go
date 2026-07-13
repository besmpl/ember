package ember

import "testing"

func d5OverflowTable() *Table {
	table := NewTable()
	table.setRawStringFieldBox("inline", newStringBox("inline"), NumberValue(1))
	for index := 1; index < maxInlineStringFields; index++ {
		table.setRawStringField("field"+string(rune('0'+index)), NumberValue(float64(index)))
	}
	table.setRawStringFieldBox("hash", newStringBox("hash"), NumberValue(9))
	return table
}

func TestStringFieldProbeChecksOverflowBeforeInline(t *testing.T) {
	table := d5OverflowTable()
	if !table.hasStringOverflow() {
		t.Fatal("fixture did not create overflow storage")
	}

	if value, ok := table.rawStringFieldWithProbe(tableStringProbeFromText("hash")); !ok {
		t.Fatal("hash lookup missed")
	} else if got, numberOK := value.Number(); !numberOK || got != 9 {
		t.Fatalf("hash value = %v (%t), want 9", value, numberOK)
	}
	inlineProbe := tableStringProbeFromBox(newStringBox("inline"))
	if value, ok := table.rawStringFieldWithProbe(inlineProbe); !ok {
		t.Fatal("inline lookup after hash miss failed")
	} else if got, numberOK := value.Number(); !numberOK || got != 1 {
		t.Fatalf("inline value = %v (%t), want 1", value, numberOK)
	}

	hashSlot, ok := table.rawStringFieldSlotWithProbe(tableStringProbeFromText("hash"))
	if !ok || hashSlot.token.storage != 1 {
		t.Fatalf("hash slot = %#v, %t; want hash-backed slot", hashSlot, ok)
	}
	inlineSlot, ok := table.rawStringFieldSlotWithProbe(inlineProbe)
	if ok {
		t.Fatalf("inline slot = %#v, %t; want no cacheable slot after overflow hash miss", inlineSlot, ok)
	}
}

func TestStringFieldProbeMatchesEqualContentBoxesAndDeletedHash(t *testing.T) {
	table := d5OverflowTable()
	hashBox := newStringBox("hash")
	table.setRawStringFieldBox(hashBox.text, hashBox, NumberValue(11))
	otherHashBox := newStringBox("hash")
	if hashBox == otherHashBox {
		t.Fatal("test requires distinct equal-content boxes")
	}
	if value, ok := table.rawStringFieldBox(otherHashBox); !ok {
		t.Fatal("equal-content hash lookup missed")
	} else if got, numberOK := value.Number(); !numberOK || got != 11 {
		t.Fatalf("equal-content hash value = %v (%t), want 11", value, numberOK)
	}

	table.setRawStringFieldBox(hashBox.text, hashBox, NilValue())
	if _, ok := table.rawStringFieldBox(otherHashBox); ok {
		t.Fatal("deleted hash key remained visible")
	}
}

func TestStringFieldSlotInvalidatesAfterOverflowGrowth(t *testing.T) {
	table := NewTable()
	table.setRawStringField("inline", NumberValue(1))
	inlineSlot, ok := table.rawStringFieldSlot("inline")
	if !ok || inlineSlot.token.storage != 0 {
		t.Fatalf("inline slot = %#v, %t; want inline slot before overflow", inlineSlot, ok)
	}
	for index := 1; index < maxInlineStringFields; index++ {
		table.setRawStringField("field"+string(rune('0'+index)), NumberValue(float64(index)))
	}
	table.setRawStringField("hash", NumberValue(9))
	if value, ok := table.rawStringField("inline"); !ok {
		t.Fatal("inline value disappeared after overflow")
	} else if got, numberOK := value.Number(); !numberOK || got != 1 {
		t.Fatalf("inline value = %v (%t), want 1", value, numberOK)
	}
	if _, ok := table.rawStringFieldAtSlot(inlineSlot, "inline"); ok {
		t.Fatal("pre-overflow inline slot survived storage transition")
	}

	slot, ok := table.rawStringFieldSlot("hash")
	if !ok || slot.token.storage != 1 {
		t.Fatalf("hash slot = %#v, %t; want hash-backed slot", slot, ok)
	}
	for index := 0; index < 64; index++ {
		table.setRawStringField("growth"+string(rune(index)), NumberValue(float64(index)))
	}
	if _, ok := table.rawStringFieldAtSlot(slot, "hash"); ok {
		t.Fatal("hash slot survived overflow growth")
	}
	refreshed, ok := table.rawStringFieldSlot("hash")
	if !ok {
		t.Fatal("hash key disappeared after growth")
	}
	if value, ok := table.rawStringFieldAtSlot(refreshed, "hash"); !ok {
		t.Fatal("refreshed hash slot missed")
	} else if got, numberOK := value.Number(); !numberOK || got != 9 {
		t.Fatalf("refreshed hash value = %v (%t), want 9", value, numberOK)
	}
}

func TestDirectFrameStringFieldProbeParityAfterOverflow(t *testing.T) {
	table := d5OverflowTable()
	for _, key := range []string{"hash", "inline", "missing"} {
		raw, rawOK := table.rawStringField(key)
		direct, directOK := directFrameRawStringField(table, key)
		if rawOK != directOK || (rawOK && !valuesEqual(raw, direct)) {
			t.Fatalf("key %q raw=(%v, %t), direct=(%v, %t)", key, raw, rawOK, direct, directOK)
		}
	}
}

func TestNeedsJournalChecksInlineAfterOverflowHashMiss(t *testing.T) {
	table := d5OverflowTable()
	// Overflow insertion normally starts the iteration journal. Isolate the
	// classification helper so this test covers the hash-miss/inline-hit branch.
	table.iteration = nil
	if table.needsJournalForNewStringKey("inline") {
		t.Fatal("existing inline key after hash miss was classified as new")
	}
	if table.needsJournalForNewStringKey("hash") {
		t.Fatal("existing hash key was classified as new")
	}
	if !table.needsJournalForNewStringKey("missing") {
		t.Fatal("missing overflow key was classified as existing")
	}
}
