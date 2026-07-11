package ember

import (
	"fmt"
	"testing"
)

func TestTableHashValueUpdatePreservesEntryAndGeneration(t *testing.T) {
	var fields tableHashFields
	keys := make([]tableKey, 11)
	for i := range keys {
		text := fmt.Sprintf("key_%d", i)
		box := newStringBox(text)
		keys[i] = tableKey{kind: StringKind, str: text, strBox: box, strHash: box.hash}
		if added := fields.set(keys[i], NumberValue(float64(i))); !added {
			t.Fatalf("initial set %d updated an existing key", i)
		}
	}
	index, ok := fields.find(keys[0])
	if !ok {
		t.Fatal("existing key is missing before update")
	}
	generation := fields.generation
	capacity := len(fields.entries)

	if added := fields.set(keys[0], NumberValue(99)); added {
		t.Fatal("existing-key update reported an insertion")
	}
	updatedIndex, ok := fields.find(keys[0])
	if !ok || updatedIndex != index {
		t.Fatalf("existing-key update moved entry from %d to %d (found=%t)", index, updatedIndex, ok)
	}
	if fields.generation != generation {
		t.Fatalf("existing-key update changed generation from %d to %d", generation, fields.generation)
	}
	if len(fields.entries) != capacity {
		t.Fatalf("existing-key update grew capacity from %d to %d", capacity, len(fields.entries))
	}
	value, ok := fields.get(keys[0])
	if got, numberOK := value.Number(); !ok || !numberOK || got != 99 {
		t.Fatalf("updated value = %v, %t; want 99", got, numberOK)
	}
}
