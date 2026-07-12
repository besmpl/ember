package ember

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"sync"
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

func TestSlotExecutionNumericForAccumulatorParity(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 5, 2 do
	total = total + i
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !proto.slotExecutionEligible {
		t.Fatal("numeric accumulator prototype is not slot eligible")
	}
	values, handled, err := runSlotExecution(proto)
	if err != nil || !handled {
		t.Fatalf("slot execution = (%#v, %t, %v), want handled result", values, handled, err)
	}
	if len(values) != 1 || values[0] != NumberValue(9) {
		t.Fatalf("slot result = %#v, want [9]", values)
	}

	public, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !reflect.DeepEqual(public, values) {
		t.Fatalf("Run result = %#v, slot result = %#v", public, values)
	}
}

func TestSlotExecutionNumericForCheckBranchParity(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 3, 1 do
	total = total + i
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !proto.slotExecutionEligible {
		t.Fatal("zero-iteration numeric loop is not slot eligible")
	}
	values, handled, err := runSlotExecution(proto)
	if err != nil || !handled {
		t.Fatalf("slot execution = (%#v, %t, %v), want handled result", values, handled, err)
	}
	if len(values) != 1 || values[0] != NumberValue(0) {
		t.Fatalf("slot result = %#v, want [0]", values)
	}
}

func TestSlotExecutionNumericForDescendingAccumulatorParity(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 5, 1, -2 do
	total = total + i
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !proto.slotExecutionEligible {
		t.Fatal("descending numeric loop is not slot eligible")
	}
	got, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	want, err := runWithSlotExecutionDisabled(proto)
	if err != nil {
		t.Fatalf("established VM returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) || len(got) != 1 || got[0] != NumberValue(9) {
		t.Fatalf("descending results = (%#v, %#v), want exact [9] parity", got, want)
	}
}

func TestSlotExecutionNumericForWrongTypeMatchesVM(t *testing.T) {
	proto := newProto(
		[]Value{NilValue(), NumberValue(3), NumberValue(1)},
		[]instruction{
			{op: opLoadConst, a: 0, b: 0},
			{op: opLoadConst, a: 1, b: 1},
			{op: opLoadConst, a: 2, b: 2},
			{op: opNumericForCheck, a: 0, b: 1, c: 2, d: 4},
			{op: opReturnOne, a: 0},
		},
		nil, nil, 3, 0, false,
	)
	if !proto.slotExecutionEligible {
		t.Fatal("wrong-type numeric loop is not slot eligible")
	}
	if _, handled, err := runSlotExecution(proto); handled || err != nil {
		t.Fatalf("slot wrong-type execution = (handled %t, err %v), want safe fallback", handled, err)
	}

	got, gotErr := Run(proto)
	want, wantErr := runWithSlotExecutionDisabled(proto)
	if got != nil || want != nil {
		t.Fatalf("wrong-type results = (%#v, %#v), want nil", got, want)
	}
	if gotErr == nil || wantErr == nil || gotErr.Error() != wantErr.Error() {
		t.Fatalf("wrong-type errors = (%v, %v), want exact parity", gotErr, wantErr)
	}
}

func TestSlotExecutionTagCollidingNaNFallsBackBeforeResult(t *testing.T) {
	const signalingNaN = uint64(0x7ff0_0000_0000_0001)
	proto := newProto(
		[]Value{
			NumberValue(math.Float64frombits(signalingNaN)),
			NumberValue(math.Float64frombits(signalingNaN)),
		},
		[]instruction{
			{op: opLoadConst, a: 0, b: 0},
			{op: opLoadConst, a: 1, b: 1},
			{op: opAdd, a: 0, b: 0, c: 1},
			{op: opReturnOne, a: 0},
		},
		nil, nil, 2, 0, false,
	)
	if !proto.slotExecutionEligible {
		t.Fatal("safe signaling-NaN inputs are not slot eligible")
	}
	if values, handled, err := runSlotExecution(proto); handled || err != nil || values != nil {
		t.Fatalf("tag-colliding NaN slot execution = (%#v, %t, %v), want safe fallback", values, handled, err)
	}

	got, gotErr := Run(proto)
	want, wantErr := runWithSlotExecutionDisabled(proto)
	if gotErr != nil || wantErr != nil {
		t.Fatalf("NaN runs returned errors = (%v, %v)", gotErr, wantErr)
	}
	if len(got) != 1 || len(want) != 1 || valueFloat64Bits(valueNumber(got[0])) != valueFloat64Bits(valueNumber(want[0])) {
		t.Fatalf("NaN results = (%#v, %#v), want exact parity", got, want)
	}
}

func runWithSlotExecutionDisabled(proto *Proto) ([]Value, error) {
	thread := acquireVMThread(context.Background(), nil)
	defer releaseVMThread(thread)
	thread.instructionBudget = -1
	return thread.runWithUpvalues(proto, nil, nil, nil, nil)
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

func TestSlotExecutionReferenceAllocationBudget(t *testing.T) {
	proto, err := Compile(`return "slot-reference"`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		values, err := Run(proto)
		var got string
		var ok bool
		if len(values) == 1 {
			got, ok = values[0].String()
		}
		if err != nil || len(values) != 1 || !ok || got != "slot-reference" {
			t.Fatalf("warm reference Run = %#v, %v; want [slot-reference]", values, err)
		}
	})
	if allocs > 1 {
		t.Fatalf("warm reference Run allocations = %.2f, want <= 1", allocs)
	}
}

func TestSlotExecutionInactiveHeapSurvivesImmediateRuns(t *testing.T) {
	reference := newProto(
		[]Value{StringValue("reference")},
		[]instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}},
		nil, nil, 1, 0, false,
	)
	immediate := newProto(
		[]Value{NumberValue(7)},
		[]instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}},
		nil, nil, 1, 0, false,
	)
	state := acquireSlotExecutionState(1, 1)
	defer releaseSlotExecutionState(state)
	if !slotExecutionImportConstants(state, reference.constants) || !state.heapActive {
		t.Fatal("reference import did not activate pooled heap")
	}
	_, index, generation, err := slotUnpackHandle(state.constants[0])
	if err != nil {
		t.Fatalf("unpack reference handle: %v", err)
	}
	state.resetForPool()
	if state.heap == nil || state.heapActive {
		t.Fatal("reference reset did not retain an inactive heap")
	}
	baseline := state.heap.strings.entries[index].generation
	if baseline == generation {
		t.Fatalf("reset did not advance string generation: still %d", baseline)
	}
	for run := 0; run < 100; run++ {
		state.constants = state.constants[:1]
		if !slotExecutionImportConstants(state, immediate.constants) {
			t.Fatalf("immediate import %d failed", run)
		}
		if state.heapActive {
			t.Fatalf("immediate import %d activated pooled heap", run)
		}
		state.resetForPool()
		if got := state.heap.strings.entries[index].generation; got != baseline {
			t.Fatalf("immediate reset %d changed string generation to %d, want %d", run, got, baseline)
		}
	}
}

func TestSlotExecutionNumericForAllocationBudget(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 5, 2 do
	total = total + i
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !proto.slotExecutionEligible {
		t.Fatal("numeric accumulator prototype is not slot eligible")
	}
	allocs := testing.AllocsPerRun(100, func() {
		values, err := Run(proto)
		if err != nil || len(values) != 1 || values[0] != NumberValue(9) {
			t.Fatalf("warm numeric Run = %#v, %v; want [9]", values, err)
		}
	})
	if allocs > 1 {
		t.Fatalf("warm numeric Run allocations = %.2f, want <= 1", allocs)
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

	// Quiet NaNs in the tag prefix use a rare boxed-number handle. The slot
	// runner preserves their bits through the same public seam as references.
	proto := newProto(
		[]Value{NumberValue(math.Float64frombits(0x7ff8_0000_0000_0042))},
		[]instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}},
		nil, nil, 1, 0, false,
	)
	if !proto.slotExecutionEligible {
		t.Fatal("tag-colliding NaN is not slot eligible")
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

func TestSlotExecutionHeapStringRoundTripIdentityAndLifetime(t *testing.T) {
	box := newStringBox("slot-reference")
	proto := newProto(
		[]Value{stringValueFromBox(box)},
		[]instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}},
		nil, nil, 1, 0, false,
	)
	if !proto.slotExecutionEligible {
		t.Fatal("string constant is not slot eligible")
	}
	values, handled, err := runSlotExecution(proto)
	if err != nil || !handled || len(values) != 1 {
		t.Fatalf("slot string execution = (%#v, %t, %v), want one handled result", values, handled, err)
	}
	if got := values[0].stringBox(); got != box {
		t.Fatalf("slot string box = %p, want %p", got, box)
	}

	runtime.GC()
	if got, ok := values[0].String(); !ok || got != "slot-reference" {
		t.Fatalf("slot string after GC = (%q, %t), want slot-reference", got, ok)
	}
	public, err := Run(proto)
	if err != nil || len(public) != 1 || public[0].stringBox() != box {
		t.Fatalf("public string result = (%#v, %v), want original box", public, err)
	}
}

func TestSlotExecutionHeapReferenceKindsPreserveIdentity(t *testing.T) {
	table := NewTable()
	userdata := NewUserData("slot-userdata")
	closure := &closure{}
	host := HostFuncValue(func([]Value) ([]Value, error) { return nil, nil })
	cases := []struct {
		name  string
		want  Value
		check func(t *testing.T, got Value)
	}{
		{name: "table", want: TableValue(table), check: func(t *testing.T, got Value) {
			value, ok := got.Table()
			if !ok || value != table {
				t.Fatalf("table identity = (%p, %t), want (%p, true)", value, ok, table)
			}
		}},
		{name: "userdata", want: UserDataValue(userdata), check: func(t *testing.T, got Value) {
			value, ok := got.UserData()
			if !ok || value != userdata {
				t.Fatalf("userdata identity = (%p, %t), want (%p, true)", value, ok, userdata)
			}
		}},
		{name: "closure", want: closureFunctionValue(closure), check: func(t *testing.T, got Value) {
			value, ok := got.scriptFunction()
			if !ok || value != closure {
				t.Fatalf("closure identity = (%p, %t), want (%p, true)", value, ok, closure)
			}
		}},
		{name: "host callable", want: host, check: func(t *testing.T, got Value) {
			if got.hostCallableRef() != host.hostCallableRef() {
				t.Fatalf("host callable identity = %p, want %p", got.hostCallableRef(), host.hostCallableRef())
			}
		}},
	}
	for _, test := range cases {
		proto := newProto(
			[]Value{test.want},
			[]instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}},
			nil, nil, 1, 0, false,
		)
		if !proto.slotExecutionEligible {
			t.Fatalf("%s constant is not slot eligible", test.name)
		}
		values, handled, err := runSlotExecution(proto)
		if err != nil || !handled || len(values) != 1 {
			t.Fatalf("%s slot execution = (%#v, %t, %v), want one handled result", test.name, values, handled, err)
		}
		test.check(t, values[0])
		public, err := Run(proto)
		if err != nil || len(public) != 1 {
			t.Fatalf("%s public execution = (%#v, %v), want one result", test.name, public, err)
		}
		test.check(t, public[0])
	}
	runtime.GC()
}

func TestSlotExecutionHeapBoxedNaNPreservesBits(t *testing.T) {
	const wantBits = uint64(0x7ff8_0000_0000_0042)
	proto := newProto(
		[]Value{NumberValue(math.Float64frombits(wantBits))},
		[]instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}},
		nil, nil, 1, 0, false,
	)
	values, handled, err := runSlotExecution(proto)
	if err != nil || !handled || len(values) != 1 {
		t.Fatalf("slot boxed NaN execution = (%#v, %t, %v), want one handled result", values, handled, err)
	}
	if got := valueFloat64Bits(valueNumber(values[0])); got != wantBits {
		t.Fatalf("slot boxed NaN bits = %#x, want %#x", got, wantBits)
	}
	public, err := Run(proto)
	if err != nil || len(public) != 1 || valueFloat64Bits(valueNumber(public[0])) != wantBits {
		t.Fatalf("public boxed NaN result = (%#v, %v), want bits %#x", public, err, wantBits)
	}
}

func TestSlotExecutionHeapReferenceConcurrentRuns(t *testing.T) {
	box := newStringBox("concurrent-slot-reference")
	proto := newProto(
		[]Value{stringValueFromBox(box)},
		[]instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}},
		nil, nil, 1, 0, false,
	)
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			values, err := Run(proto)
			if err != nil {
				errs <- err
				return
			}
			if len(values) != 1 || values[0].stringBox() != box {
				errs <- fmt.Errorf("result = %#v, want original string box", values)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestSlotExecutionReferenceAndRichPrototypeEligibility(t *testing.T) {
	for _, test := range []struct {
		source   string
		eligible bool
	}{
		{source: `return "reference"`, eligible: true},
		{source: `return {value = 1}`},
		{source: `return missingGlobal`},
		{source: `return tostring(1)`},
		{source: `return function() return 1 end`},
		{source: "sideEffect = 1\nreturn 1"},
	} {
		proto, err := Compile(test.source)
		if err != nil {
			t.Fatalf("Compile(%q) returned error: %v", test.source, err)
		}
		if proto.slotExecutionEligible != test.eligible {
			t.Fatalf("prototype %q eligibility = %t, want %t", test.source, proto.slotExecutionEligible, test.eligible)
		}
		if _, err := Run(proto); err == nil && test.source == `return missingGlobal` {
			t.Fatalf("global prototype %q unexpectedly succeeded", test.source)
		}
	}
}

func TestSlotExecutionPoolResetOwnsNoRuntimeReferences(t *testing.T) {
	typ := reflect.TypeOf(slotExecutionState{})
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		if field.Name == "heap" {
			if field.Type != reflect.TypeOf((*runtimeHeap)(nil)) {
				t.Fatalf("slot state field %q has type %s, want *runtimeHeap", field.Name, field.Type)
			}
			continue
		}
		if field.Name == "heapActive" {
			if field.Type.Kind() != reflect.Bool {
				t.Fatalf("slot state field %q has type %s, want bool", field.Name, field.Type)
			}
			continue
		}
		if field.Type != reflect.TypeOf([]slot(nil)) {
			t.Fatalf("slot state field %q has type %s, want []slot", field.Name, field.Type)
		}
	}
	state := acquireSlotExecutionState(2, 2)
	state.heap = &runtimeHeap{}
	state.heapActive = true
	state.registers[0] = slotNil
	state.constants[0] = slotBool(true)
	state.results = append(state.results, slot(1))
	registerBacking := state.registers[:cap(state.registers)]
	constantBacking := state.constants[:cap(state.constants)]
	resultBacking := state.results[:cap(state.results)]
	state.resetForPool()
	if state.heap == nil {
		t.Fatal("reset state discarded reusable runtime heap")
	}
	if state.heapActive {
		t.Fatal("reset state left heap active")
	}
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
		heap: &runtimeHeap{strings: slotSlab[*stringBox]{
			entries: make([]slotSlabEntry[*stringBox], maxPooledSlotExecutionCapacity+1),
		}},
		heapActive: true,
	}
	oversized.resetForPool()
	if oversized.registers != nil || oversized.constants != nil || oversized.results != nil {
		t.Fatal("pool reset retained oversized slot buffers")
	}
	if oversized.heap != nil {
		t.Fatal("pool reset retained oversized runtime heap")
	}
}

func TestSlotExecutionPoolDropsGenerationExhaustedHeap(t *testing.T) {
	state := &slotExecutionState{
		heap: &runtimeHeap{
			strings: slotSlab[*stringBox]{
				entries: []slotSlabEntry[*stringBox]{
					{},
					{value: newStringBox("exhausted"), generation: slotMaxGeneration, live: true},
				},
			},
		},
		heapActive: true,
	}
	state.resetForPool()
	if state.heap != nil {
		t.Fatal("generation-exhausted heap was retained")
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

func BenchmarkRunSlotNumericFor(b *testing.B) {
	proto, err := Compile(`
local total = 0
for i = 1, 100 do
	total = total + i
end
return total
`)
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

func BenchmarkRunSlotStringReference(b *testing.B) {
	proto, err := Compile(`return "slot-reference"`)
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

func BenchmarkRunEstablishedStringReference(b *testing.B) {
	proto, err := Compile(`return "slot-reference"`)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := runWithSlotExecutionDisabled(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunEstablishedNumericFor(b *testing.B) {
	proto, err := Compile(`
local total = 0
for i = 1, 100 do
	total = total + i
end
return total
`)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := runWithSlotExecutionDisabled(proto); err != nil {
			b.Fatal(err)
		}
	}
}
