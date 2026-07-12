package ember

import (
	"math"
	"reflect"
	"testing"
)

func TestSlotExecutionImmediateScalarParity(t *testing.T) {
	proto, err := Compile(`return (1+2)*3-2`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !proto.slotExecutionEligible {
		t.Fatal("constant-folded scalar prototype is not slot eligible")
	}
	values, handled, err := runSlotExecution(proto)
	if err != nil || !handled {
		t.Fatalf("slot execution = (%#v, %t, %v), want handled result", values, handled, err)
	}
	if len(values) != 1 {
		t.Fatalf("slot result count = %d, want 1", len(values))
	}
	got, ok := values[0].Number()
	if !ok || got != 7 {
		t.Fatalf("slot result = %v (%t), want number 7", values[0], ok)
	}
	public, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !reflect.DeepEqual(public, values) {
		t.Fatalf("Run result = %#v, slot result = %#v", public, values)
	}
}

func TestSlotExecutionImmediateScalarAllocationBudget(t *testing.T) {
	proto, err := Compile(`return (1+2)*3-2`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := Run(proto); err != nil {
			t.Fatalf("warm scalar Run returned error: %v", err)
		}
	})
	if allocs > 1 {
		t.Fatalf("warm scalar Run allocations = %.2f, want <= 1", allocs)
	}
}

func TestSlotExecutionImmediateScalarBitsAndSafeNaN(t *testing.T) {
	for _, want := range []uint64{
		math.Float64bits(0),
		math.Float64bits(math.Copysign(0, -1)),
		math.Float64bits(math.Inf(1)),
		math.Float64bits(math.Inf(-1)),
		0x7ff0_0000_0000_0001, // non-tag-colliding signaling NaN
	} {
		proto := newProto(
			[]Value{NumberValue(math.Float64frombits(want))},
			[]instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}},
			nil, nil, 1, 0, false,
		)
		if !proto.slotExecutionEligible {
			t.Fatalf("number bits %#x unexpectedly rejected", want)
		}
		got, err := Run(proto)
		if err != nil {
			t.Fatalf("Run(%#x) returned error: %v", want, err)
		}
		if len(got) != 1 || valueFloat64Bits(valueNumber(got[0])) != want {
			t.Fatalf("Run(%#x) = %#v, want exact bits", want, got)
		}
	}
	for _, want := range []Value{NilValue(), BoolValue(false), BoolValue(true)} {
		proto := newProto(
			[]Value{want},
			[]instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}},
			nil, nil, 1, 0, false,
		)
		if !proto.slotExecutionEligible {
			t.Fatalf("immediate %s unexpectedly rejected", want.Kind())
		}
		got, err := Run(proto)
		if err != nil || len(got) != 1 || !valuesEqual(got[0], want) {
			t.Fatalf("Run(%s) = %#v, %v; want %#v", want.Kind(), got, err, want)
		}
	}

	// Quiet NaNs in the tag prefix are safely rejected by the scalar runner;
	// the established VM still preserves their bits through the public API.
	proto := newProto(
		[]Value{NumberValue(math.Float64frombits(0x7ff8_0000_0000_0042))},
		[]instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}},
		nil, nil, 1, 0, false,
	)
	if proto.slotExecutionEligible {
		t.Fatal("tag-colliding NaN unexpectedly slot eligible")
	}
	got, err := Run(proto)
	if err != nil {
		t.Fatalf("fallback NaN Run returned error: %v", err)
	}
	if len(got) != 1 || valueFloat64Bits(valueNumber(got[0])) != 0x7ff8_0000_0000_0042 {
		t.Fatalf("fallback NaN result = %#v, want exact bits", got)
	}
}

func TestSlotExecutionLoadConstAuxUsesPhysicalWordPC(t *testing.T) {
	constants := make([]Value, 65_537)
	for index := range constants {
		constants[index] = NumberValue(float64(index))
	}
	proto := newProto(
		constants,
		[]instruction{{op: opLoadConst, a: 0, b: 65_536}, {op: opReturnOne, a: 0}},
		nil, nil, 1, 0, false,
	)
	if !proto.slotExecutionEligible {
		t.Fatal("AUX-bearing LOAD_CONST prototype is not slot eligible")
	}
	if len(proto.words) != 3 {
		t.Fatalf("AUX-bearing prototype has %d words, want 3", len(proto.words))
	}
	got, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(got) != 1 || got[0] != NumberValue(65_536) {
		t.Fatalf("AUX-bearing Run = %#v, want 65536", got)
	}
}

func TestSlotExecutionRejectsReferenceAndRichPrototypes(t *testing.T) {
	for _, source := range []string{
		`return "reference"`,
		`return {value = 1}`,
		`return missingGlobal`,
		`return tostring(1)`,
		`return function() return 1 end`,
	} {
		proto, err := Compile(source)
		if err != nil {
			t.Fatalf("Compile(%q) returned error: %v", source, err)
		}
		if proto.slotExecutionEligible {
			t.Fatalf("rich prototype %q unexpectedly slot eligible", source)
		}
		if _, err := Run(proto); err == nil && source == `return missingGlobal` {
			t.Fatalf("global prototype %q unexpectedly succeeded", source)
		}
	}
}

func TestSlotExecutionPoolResetOwnsNoRuntimeReferences(t *testing.T) {
	typ := reflect.TypeOf(slotExecutionState{})
	for index := 0; index < typ.NumField(); index++ {
		if typ.Field(index).Type != reflect.TypeOf([]slot(nil)) {
			t.Fatalf("slot state field %q has type %s, want []slot", typ.Field(index).Name, typ.Field(index).Type)
		}
	}
	state := acquireSlotExecutionState(2, 2)
	state.registers[0] = slotNil
	state.constants[0] = slotBool(true)
	state.results = append(state.results, slot(1))
	registerBacking := state.registers[:cap(state.registers)]
	constantBacking := state.constants[:cap(state.constants)]
	resultBacking := state.results[:cap(state.results)]
	state.resetForPool()
	if len(state.registers) != 0 || len(state.constants) != 0 || len(state.results) != 0 {
		t.Fatalf("reset state lengths = (%d, %d, %d), want all zero", len(state.registers), len(state.constants), len(state.results))
	}
	for _, values := range [][]slot{state.registers, state.constants, state.results} {
		if len(values) != 0 {
			t.Fatal("reset state retained slot values")
		}
	}
	for name, values := range map[string][]slot{
		"registers": registerBacking,
		"constants": constantBacking,
		"results":   resultBacking,
	} {
		for index, value := range values {
			if value != 0 {
				t.Fatalf("reset %s backing slot %d = %#x, want zero", name, index, value)
			}
		}
	}
	// Return the state once; this test does not rely on sync.Pool eviction.
	slotExecutionPool.Put(state)

	oversized := &slotExecutionState{
		registers: make([]slot, maxPooledSlotExecutionCapacity+1),
		constants: make([]slot, maxPooledSlotExecutionCapacity+1),
		results:   make([]slot, maxPooledSlotExecutionCapacity+1),
	}
	oversized.resetForPool()
	if oversized.registers != nil || oversized.constants != nil || oversized.results != nil {
		t.Fatal("pool reset retained oversized slot buffers")
	}
}

func BenchmarkRunSlotScalar(b *testing.B) {
	proto, err := Compile(`return (1+2)*3-2`)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}
