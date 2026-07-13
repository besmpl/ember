package ember

import (
	"math"
	"reflect"
	"testing"
)

func TestOptimizationConstantPoolPreservesScalarIdentity(t *testing.T) {
	nanA := NumberValue(math.Float64frombits(0x7ff8000000000001))
	nanB := NumberValue(math.Float64frombits(0x7ff8000000000002))
	seed := []Value{
		NilValue(),
		BoolValue(false),
		BoolValue(true),
		NumberValue(0),
		NumberValue(math.Copysign(0, -1)),
		nanA,
		nanB,
		StringValue("same"),
	}
	pool := newOptimizationConstantPool(seed, 0)

	for index, value := range seed {
		if got := pool.intern(value); got != index {
			t.Fatalf("seed value %d interned at %d, want %d", index, got, index)
		}
	}
	if got := pool.intern(StringValue("same")); got != 7 {
		t.Fatalf("equal-content string interned at %d, want 7", got)
	}
	if got := pool.intern(NumberValue(math.Copysign(0, -1))); got != 4 {
		t.Fatalf("-0 interned at %d, want 4", got)
	}
	if got := pool.intern(NumberValue(math.Float64frombits(0x7ff8000000000002))); got != 6 {
		t.Fatalf("second NaN payload interned at %d, want 6", got)
	}
	if got := pool.intern(NumberValue(3)); got != len(seed) {
		t.Fatalf("new number interned at %d, want %d", got, len(seed))
	}
	if got, want := len(pool.pending), 1; got != want {
		t.Fatalf("pending pool length = %d, want %d", got, want)
	}
}

func TestOptimizationConstantPoolDeduplicatesPendingStringsWhenMaterialized(t *testing.T) {
	pool := newOptimizationConstantPool(nil, 2)
	first := pool.intern(StringValue("same"))
	second := pool.intern(StringValue("same"))
	if first == second {
		t.Fatal("pending strings unexpectedly shared an index before materialization")
	}
	ir := lowerInstructionsToBytecodeIR([]instruction{
		{op: opLoadConst, a: 0, b: first},
		{op: opLoadConst, a: 1, b: second},
	})
	materialized, values := compactOptimizationConstantPool(ir, pool, nil)
	if materialized[0].operands.b.value != materialized[1].operands.b.value {
		t.Fatalf("pending equal strings materialized at %d and %d", materialized[0].operands.b.value, materialized[1].operands.b.value)
	}
	if got, want := len(values), 1; got != want {
		t.Fatalf("materialized string count = %d, want %d", got, want)
	}
}

func TestOptimizationConstantPoolMaterializesAllPendingSurvivors(t *testing.T) {
	pool := newOptimizationConstantPool([]Value{NumberValue(0)}, 3)
	first := pool.intern(StringValue("first"))
	second := pool.intern(StringValue("second"))
	duplicate := pool.intern(StringValue("first"))
	ir := lowerInstructionsToBytecodeIR([]instruction{
		{op: opLoadConst, a: 0, b: first},
		{op: opLoadConst, a: 1, b: second},
		{op: opLoadConst, a: 2, b: duplicate},
	})
	materialized, values := compactOptimizationConstantPool(ir, pool, nil)
	got := []int{materialized[0].operands.b.value, materialized[1].operands.b.value, materialized[2].operands.b.value}
	if want := []int{0, 1, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("materialized pending indices = %v, want %v", got, want)
	}
	if len(values) != 2 || values[0].stringText() != "first" || values[1].stringText() != "second" {
		t.Fatalf("materialized values = %#v, want first/second", values)
	}
}

func TestOptimizationConstantPoolMaterializesDistinctFloatBits(t *testing.T) {
	pool := newOptimizationConstantPool(nil, 4)
	values := []Value{
		NumberValue(0),
		NumberValue(math.Copysign(0, -1)),
		NumberValue(math.Float64frombits(0x7ff8000000000001)),
		NumberValue(math.Float64frombits(0x7ff8000000000002)),
	}
	ir := make([]bytecodeIRInstruction, 0, len(values))
	for index, value := range values {
		constant := pool.intern(value)
		ir = append(ir, lowerInstructionToBytecodeIR(instruction{op: opLoadConst, a: index, b: constant}, sourceRange{}))
	}
	optimized, constants := compactOptimizationConstantPool(ir, pool, nil)
	if len(constants) != len(values) {
		t.Fatalf("materialized float constants = %d, want %d", len(constants), len(values))
	}
	for index, value := range constants {
		if got, want := math.Float64bits(valueNumber(value)), math.Float64bits(valueNumber(values[index])); got != want {
			t.Fatalf("constant %d bits = %#x, want %#x", index, got, want)
		}
		if got, want := optimized[index].operands.b.value, index; got != want {
			t.Fatalf("constant %d remapped to %d, want %d", index, got, want)
		}
	}
}

func TestOptimizationWithoutDurablePoolDoesNotCreateUnmaterializedIndices(t *testing.T) {
	ir := lowerInstructionsToBytecodeIR([]instruction{
		{op: opLoadConst, a: 0, b: 0},
		{op: opLoadConst, a: 1, b: 1},
		{op: opAdd, a: 2, b: 0, c: 1},
		{op: opReturnOne, a: 2},
	})
	optimized := optimizeBytecodeIRWithConstants(ir, []Value{NumberValue(1), NumberValue(2)}, optimizationOptions{})
	if optimized[2].op != opAdd {
		t.Fatalf("optimization without durable pool folded op to %v, want ADD", optimized[2].op)
	}
	for pc, raw := range optimized {
		if raw.op != opLoadConst {
			continue
		}
		if raw.operands.b.kind != bytecodeOperandConstant || raw.operands.b.value < 0 || raw.operands.b.value >= 2 {
			t.Fatalf("load-constant at pc %d has unmaterialized index %d", pc, raw.operands.b.value)
		}
	}
}

func TestOptimizedCompileDropsDeadFoldedConstants(t *testing.T) {
	proto, err := Compile(`local dead = 40 + 2
return 1 + 2`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	want := []Value{NumberValue(3)}
	if !reflect.DeepEqual(proto.constants, want) {
		t.Fatalf("optimized constants = %#v, want %#v", proto.constants, want)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("Run values = %#v, want %#v", values, want)
	}
}

func TestChildOptimizationConstantPoolsAreIsolated(t *testing.T) {
	proto, err := Compile(`local function child()
 return 1 + 2
end
return child(), 4 + 5`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) != 1 {
		t.Fatalf("compiled child prototype count = %d, want 1", len(proto.prototypes))
	}
	if got, want := proto.constants, []Value{NumberValue(9)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parent constants = %#v, want %#v", got, want)
	}
	if got, want := proto.prototypes[0].constants, []Value{NumberValue(3)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("child constants = %#v, want %#v", got, want)
	}
}
