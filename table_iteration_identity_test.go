package ember

import "testing"

func TestTableIterationUsesReinsertedStringIdentity(t *testing.T) {
	table := NewTable()
	first := StringValue("field")
	if err := table.rawSet(first, NumberValue(1)); err != nil {
		t.Fatalf("set first key: %v", err)
	}
	table.ensureIterationJournal()
	if err := table.rawSet(first, NilValue()); err != nil {
		t.Fatalf("delete first key: %v", err)
	}

	second := StringValue("field")
	if first.stringBox() == second.stringBox() {
		t.Fatal("test requires distinct equal-content string boxes")
	}
	if err := table.rawSet(second, NumberValue(2)); err != nil {
		t.Fatalf("reinsert second key: %v", err)
	}

	key, value, err := table.rawNext(NilValue())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if key.stringBox() != second.stringBox() {
		t.Fatal("iteration returned the deleted string box instead of the reinserted key")
	}
	if number, ok := value.Number(); !ok || number != 2 {
		t.Fatalf("iteration value = %v, %t; want 2", number, ok)
	}
}
