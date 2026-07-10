package ember

import "testing"

func TestDenseSymbolSlotsUseNegativeSentinel(t *testing.T) {
	slots := newDenseSymbolSlots(4)
	for i := range slots {
		if _, ok := denseSymbolSlot(slots, i); ok {
			t.Fatalf("slot %d is populated before assignment", i)
		}
	}
	slots[2] = 7
	if value, ok := denseSymbolSlot(slots, 2); !ok || value != 7 {
		t.Fatalf("slot 2 = %d, %t, want 7, true", value, ok)
	}
	if _, ok := denseSymbolSlot(slots, -1); ok {
		t.Fatal("negative symbol ID resolved")
	}
}

func TestCompilerShapeMapsAllocateOnFirstWrite(t *testing.T) {
	c := compiler{}
	if c.localStringSlots != nil || c.localFieldArrayElemSlots != nil {
		t.Fatal("shape maps are eager")
	}

	setLocalSlots(&c.localStringSlots, 3, map[string]int{"x": 1})
	if got := c.localStringSlots[3]["x"]; got != 1 {
		t.Fatalf("string slot = %d, want 1", got)
	}
	if c.localFieldArrayElemSlots != nil {
		t.Fatal("unwritten nested shape map was allocated")
	}

	setLocalNestedSlots(&c.localFieldArrayElemSlots, 4, map[string]map[string]int{"row": {"x": 2}})
	if got := c.localFieldArrayElemSlots[4]["row"]["x"]; got != 2 {
		t.Fatalf("nested slot = %d, want 2", got)
	}
}
