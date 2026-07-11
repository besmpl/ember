package ember

import (
	"fmt"
	"testing"
)

func TestTableClearInvalidatesSlotsJournalAndOverflowState(t *testing.T) {
	table := NewTable()
	metatable := NewTable()
	table.setMetatable(metatable)
	for i := 0; i < maxInlineStringFields; i++ {
		key := fmt.Sprintf("inline_%d", i)
		table.setRawStringFieldBox(key, newStringBox(key), NumberValue(float64(i)))
	}
	overflowBox := newStringBox("overflow")
	table.setRawStringFieldBox(overflowBox.text, overflowBox, NumberValue(9))
	oldSlot, ok := table.rawStringFieldSlot(overflowBox.text)
	if !ok || oldSlot.token.storage != 1 {
		t.Fatalf("overflow slot = %#v, %t; want hash slot", oldSlot, ok)
	}
	oldKey, _, err := table.rawNext(NilValue())
	if err != nil || oldKey.IsNil() {
		t.Fatalf("rawNext(nil) before clear = %v, %v", oldKey, err)
	}
	before := table.shapeToken()

	(rawSequence{table: table}).clear()

	if table.metatable != metatable {
		t.Fatal("table.clear changed the metatable")
	}
	if table.iteration != nil {
		t.Fatal("table.clear retained the iteration journal")
	}
	if table.hasStringOverflow() || table.hashFieldCount() != 0 {
		t.Fatalf("table.clear retained overflow state: overflow=%t fields=%d", table.hasStringOverflow(), table.hashFieldCount())
	}
	if _, ok := table.rawStringFieldAtSlot(oldSlot, overflowBox.text); ok {
		t.Fatal("pre-clear overflow slot remained valid")
	}
	if _, _, err := table.rawNext(oldKey); err == nil {
		t.Fatal("pre-clear iteration key remained valid")
	}
	if err := table.rawSetArrayIndex(99, NilValue()); err != nil {
		t.Fatalf("delete missing array/generic key after clear: %v", err)
	}
	if err := table.rawSet(BoolValue(true), NilValue()); err != nil {
		t.Fatalf("delete missing generic key after clear: %v", err)
	}
	after := table.shapeToken()
	if after.stringLayout == before.stringLayout || after.arrayLayout == before.arrayLayout || after.genericLayout == before.genericLayout {
		t.Fatalf("table.clear did not advance every layout generation: before=%#v after=%#v", before, after)
	}

	newBox := newStringBox("new")
	table.setRawStringFieldBox(newBox.text, newBox, NumberValue(11))
	if table.hasStringOverflow() {
		t.Fatal("first post-clear field used stale overflow storage")
	}
	key, value, err := table.rawNext(NilValue())
	if err != nil {
		t.Fatalf("rawNext(nil) after clear: %v", err)
	}
	if got, ok := key.String(); !ok || got != "new" {
		t.Fatalf("post-clear key = %q, %t; want new", got, ok)
	}
	if got, ok := value.Number(); !ok || got != 11 {
		t.Fatalf("post-clear value = %v, %t; want 11", got, ok)
	}
}
