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
