package ember

import "testing"

func TestTableRefillingLastArrayHoleRestoresFastStorage(t *testing.T) {
	table := NewTable()
	for index := 1; index <= 4; index++ {
		if err := table.rawSetArrayIndex(index, NumberValue(float64(index))); err != nil {
			t.Fatalf("set array index %d: %v", index, err)
		}
	}
	if !table.canUseFastArrayStorage() {
		t.Fatal("dense array did not start in fast storage")
	}
	if err := table.rawSetArrayIndex(2, NilValue()); err != nil {
		t.Fatalf("delete array index: %v", err)
	}
	if table.canUseFastArrayStorage() {
		t.Fatal("array with a hole remained in fast storage")
	}
	if err := table.rawSetArrayIndex(2, NumberValue(2)); err != nil {
		t.Fatalf("refill array index: %v", err)
	}
	if !table.canUseFastArrayStorage() {
		t.Fatal("refilling the last hole did not restore fast array storage")
	}
}
