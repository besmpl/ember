package ember

import (
	"math"
	"testing"
)

func TestTableNumericZeroKeysShareOneBucket(t *testing.T) {
	table := NewTable()
	negativeZero := NumberValue(math.Copysign(0, -1))
	if err := table.Set(negativeZero, NumberValue(7)); err != nil {
		t.Fatalf("Set(-0): %v", err)
	}
	for _, key := range []Value{NumberValue(0), negativeZero} {
		value, err := table.Get(key)
		if err != nil {
			t.Fatalf("Get(%v): %v", key, err)
		}
		if got, ok := value.Number(); !ok || got != 7 {
			t.Fatalf("zero-key lookup = %v, %t; want 7", got, ok)
		}
	}
}

func TestCompileAndRunNumericZeroTableKeysAreEqual(t *testing.T) {
	proto, err := Compile(`
local values = {}
values[-0.0] = 7
return values[0.0], values[-0.0]
`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i, value := range results {
		if got, ok := value.Number(); !ok || got != 7 {
			t.Fatalf("result %d = %v, %t; want 7", i, got, ok)
		}
	}
}
