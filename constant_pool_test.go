package ember

import (
	"math"
	"testing"
)

func TestConstantPoolUsesExactNumberBits(t *testing.T) {
	var builder bytecodeBuilder
	positiveZero := builder.addConstant(NumberValue(0))
	negativeZero := builder.addConstant(NumberValue(math.Copysign(0, -1)))
	if positiveZero == negativeZero {
		t.Fatalf("+0 and -0 share constant %d", positiveZero)
	}

	firstNaN := math.Float64frombits(0x7ff8000000000001)
	sameNaN := math.Float64frombits(0x7ff8000000000001)
	otherNaN := math.Float64frombits(0x7ff8000000000002)
	first := builder.addConstant(NumberValue(firstNaN))
	if got := builder.addConstant(NumberValue(sameNaN)); got != first {
		t.Fatalf("same NaN bits produced constants %d and %d", first, got)
	}
	if got := builder.addConstant(NumberValue(otherNaN)); got == first {
		t.Fatalf("different NaN bits share constant %d", first)
	}
}

func TestConstantPoolInternsStringsBeforeBoxing(t *testing.T) {
	var builder bytecodeBuilder
	first := builder.addStringConstant("health")
	second := builder.addStringConstant("health")
	if first != second || len(builder.constants) != 1 {
		t.Fatalf("string constants = %d, %d with %d values", first, second, len(builder.constants))
	}
	if len(builder.constantStrings) != 1 {
		t.Fatalf("interned strings = %d, want 1", len(builder.constantStrings))
	}
	if got := builder.constants[first].stringText(); got != "health" {
		t.Fatalf("constant text = %q, want health", got)
	}
}

func TestConstantPoolKeysNativeFunctionsByID(t *testing.T) {
	var builder bytecodeBuilder
	fn := func(*globalEnv, []Value) ([]Value, error) { return nil, nil }
	first := builder.addConstant(nativeFuncValueWithID(fn, nativeFuncRawLen))
	if got := builder.addConstant(nativeFuncValueWithID(fn, nativeFuncRawLen)); got != first {
		t.Fatalf("same native ID produced constants %d and %d", first, got)
	}
	if got := builder.addConstant(nativeFuncValueWithID(fn, nativeFuncSelect)); got == first {
		t.Fatalf("different native IDs share constant %d", first)
	}
}

func TestConstantPoolInternsTableShapes(t *testing.T) {
	shape := func(fields ...string) Value {
		table := newTableWithCapacity(0, len(fields))
		for _, field := range fields {
			table.stringFields = append(table.stringFields, tableStringField{key: field})
		}
		return TableValue(table)
	}

	var builder bytecodeBuilder
	first := builder.addConstant(shape("health", "mana"))
	if got := builder.addConstant(shape("health", "mana")); got != first {
		t.Fatalf("same table shape produced constants %d and %d", first, got)
	}
	if got := builder.addConstant(shape("mana", "health")); got == first {
		t.Fatalf("different table shape order shares constant %d", first)
	}
}
