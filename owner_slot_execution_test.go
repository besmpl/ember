package ember

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestOwnerSlotExecutionReferenceArgumentsStayEphemeral(t *testing.T) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto := newProto(nil, []instruction{{op: opReturnOne, a: 0}}, nil, nil, 1, 1, false)

	box := newStringBox("owner-slot-reference")
	values := []Value{
		stringValueFromBox(box),
		TableValue(NewTable()),
		closureFunctionValue(&closure{}),
		UserDataValue(NewUserData("owner-slot")),
		HostFuncValue(func([]Value) ([]Value, error) { return nil, nil }),
		NumberValue(math.Float64frombits(0x7ff8_0000_0000_0042)),
	}
	for index, value := range values {
		got, err := executeProto(context.Background(), proto, globals, executeOptions{
			args:            []Value{value},
			maxInstructions: -1,
		})
		if err != nil || len(got) != 1 {
			t.Fatalf("argument %d result = (%#v, %v), want one value", index, got, err)
		}
		if value.Kind() == NumberKind {
			if valueFloat64Bits(valueNumber(got[0])) != valueFloat64Bits(valueNumber(value)) {
				t.Fatalf("argument %d changed boxed-number bits", index)
			}
		} else if valueRef(got[0]) != valueRef(value) || got[0].Kind() != value.Kind() {
			t.Fatalf("argument %d identity = %#v, want %#v", index, got[0], value)
		}
	}
	assertRuntimeOwnerHeapEmpty(t, owner)

	for index := 0; index < 100; index++ {
		value := stringValueFromBox(newStringBox(fmt.Sprintf("transient-%d", index)))
		if _, err := executeProto(context.Background(), proto, globals, executeOptions{
			args:            []Value{value},
			maxInstructions: -1,
		}); err != nil {
			t.Fatalf("transient argument %d: %v", index, err)
		}
	}
	assertRuntimeOwnerHeapEmpty(t, owner)
}

func TestOwnerSlotExecutionReferenceConstantStaysEphemeral(t *testing.T) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto, err := Compile(`return "owner-slot-constant"`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	var first *stringBox
	for run := 0; run < 10; run++ {
		values, runErr := executeProto(context.Background(), proto, globals, executeOptions{maxInstructions: -1})
		if runErr != nil || len(values) != 1 {
			t.Fatalf("owner slot run %d = (%#v, %v), want one result", run, values, runErr)
		}
		if run == 0 {
			first = values[0].stringBox()
		} else if values[0].stringBox() != first {
			t.Fatalf("owner slot run %d changed constant identity", run)
		}
	}
	assertRuntimeOwnerHeapEmpty(t, owner)
}

func TestOwnerSlotExecutionGuardsLifecycleAndCancellation(t *testing.T) {
	owner := newRuntimeOwner()
	if err := owner.beginSlotRun(); err != nil {
		t.Fatalf("begin slot run: %v", err)
	}
	if err := owner.close(); !errors.Is(err, errRuntimeOwnerActive) {
		t.Fatalf("close during slot run = %v, want %v", err, errRuntimeOwnerActive)
	}
	owner.endSlotRun()

	proto := newProto(nil, []instruction{{op: opReturnOne, a: 0}}, nil, nil, 1, 1, false)
	globals := runtimeGlobalsWithOwner(nil, owner)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if values, err := executeProto(cancelled, proto, globals, executeOptions{
		args:            []Value{NumberValue(7)},
		maxInstructions: -1,
	}); err != nil || !reflect.DeepEqual(values, []Value{NumberValue(7)}) {
		t.Fatalf("cancelled owner execution = (%#v, %v), want established VM result [7]", values, err)
	}
	if values, err := executeProto(context.Background(), proto, globals, executeOptions{
		args:            []Value{NumberValue(7)},
		maxInstructions: 0,
	}); values != nil || err == nil {
		t.Fatalf("budgeted owner execution = (%#v, %v), want budget error", values, err)
	}

	if err := owner.close(); err != nil {
		t.Fatalf("close after slot run: %v", err)
	}
	if _, err := executeProto(context.Background(), proto, globals, executeOptions{
		args:            []Value{NumberValue(7)},
		maxInstructions: -1,
	}); !errors.Is(err, errRuntimeOwnerClosed) {
		t.Fatalf("execution after close = %v, want %v", err, errRuntimeOwnerClosed)
	}
}

func TestOwnerSlotExecutionFallbackMatchesEstablishedVM(t *testing.T) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto := newProto(
		nil,
		[]instruction{{op: opAdd, a: 0, b: 0, c: 1}, {op: opReturnOne, a: 0}},
		nil, nil, 2, 2, false,
	)
	args := []Value{StringValue("not a number"), NumberValue(2)}
	got, gotErr := executeProto(context.Background(), proto, globals, executeOptions{args: args, maxInstructions: -1})
	want, wantErr := runWithSlotExecutionDisabledArgs(proto, args)
	if got != nil || want != nil || gotErr == nil || wantErr == nil || gotErr.Error() != wantErr.Error() {
		t.Fatalf("owner fallback = (%#v, %v), established = (%#v, %v)", got, gotErr, want, wantErr)
	}
	assertRuntimeOwnerHeapEmpty(t, owner)
}

func TestOwnerSlotExecutionConcurrentRuns(t *testing.T) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto, err := Compile(`return "owner-slot-concurrent"`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	const workers = 16
	const iterations = 100
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for range iterations {
				values, runErr := executeProto(context.Background(), proto, globals, executeOptions{maxInstructions: -1})
				if runErr != nil || len(values) != 1 {
					errs <- fmt.Errorf("result = %#v, err = %v", values, runErr)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if owner.activeSlotRuns != 0 {
		t.Fatalf("active slot runs after join = %d, want 0", owner.activeSlotRuns)
	}
	assertRuntimeOwnerHeapEmpty(t, owner)
}

func TestOwnerSlotExecutionAllocationBudget(t *testing.T) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto := newProto(nil, []instruction{{op: opReturnOne, a: 0}}, nil, nil, 1, 1, false)
	for _, args := range [][]Value{{NumberValue(7)}, {StringValue("owner-slot-allocation")}} {
		allocs := testing.AllocsPerRun(100, func() {
			values, err := executeProto(context.Background(), proto, globals, executeOptions{args: args, maxInstructions: -1})
			if err != nil || len(values) != 1 || values[0] != args[0] {
				t.Fatalf("allocation run = (%#v, %v), want %#v", values, err, args)
			}
		})
		if allocs > 1 {
			t.Fatalf("owner slot allocations for %s = %.2f, want <= 1", args[0].Kind(), allocs)
		}
	}
}

func assertRuntimeOwnerHeapEmpty(t *testing.T, owner *runtimeOwner) {
	t.Helper()
	if owner == nil || owner.heap == nil {
		t.Fatal("runtime owner heap is nil")
	}
	heap := owner.heap
	if len(heap.boxedNumbers.entries) != 0 || len(heap.strings.entries) != 0 ||
		len(heap.tables.entries) != 0 || len(heap.closures.entries) != 0 ||
		len(heap.upvalues.entries) != 0 || len(heap.userdata.entries) != 0 ||
		len(heap.hostCallables.entries) != 0 {
		t.Fatalf("owner heap retained ephemeral values: boxed=%d strings=%d tables=%d closures=%d upvalues=%d userdata=%d host=%d",
			len(heap.boxedNumbers.entries), len(heap.strings.entries), len(heap.tables.entries),
			len(heap.closures.entries), len(heap.upvalues.entries), len(heap.userdata.entries), len(heap.hostCallables.entries))
	}
}

func BenchmarkRunOwnerSlotFixedParameter(b *testing.B) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto := newProto(
		nil,
		[]instruction{{op: opAdd, a: 0, b: 0, c: 1}, {op: opReturnOne, a: 0}},
		nil, nil, 2, 2, false,
	)
	benchmarkOwnerProto(b, globals, proto, []Value{NumberValue(4), NumberValue(5)})
}

func BenchmarkRunOwnerEstablishedFixedParameter(b *testing.B) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto := newProto(
		nil,
		[]instruction{{op: opAdd, a: 0, b: 0, c: 1}, {op: opReturnOne, a: 0}},
		nil, nil, 2, 2, false,
	)
	copy := *proto
	copy.slotExecutionEligible = false
	benchmarkOwnerProto(b, globals, &copy, []Value{NumberValue(4), NumberValue(5)})
}

func BenchmarkRunOwnerSlotStringReference(b *testing.B) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto, err := Compile(`return "owner-slot-reference"`)
	if err != nil {
		b.Fatal(err)
	}
	benchmarkOwnerProto(b, globals, proto, nil)
}

func BenchmarkRunOwnerEstablishedStringReference(b *testing.B) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	proto, err := Compile(`return "owner-slot-reference"`)
	if err != nil {
		b.Fatal(err)
	}
	copy := *proto
	copy.slotExecutionEligible = false
	benchmarkOwnerProto(b, globals, &copy, nil)
}

func benchmarkOwnerProto(b *testing.B, globals *globalEnv, proto *Proto, args []Value) {
	b.Helper()
	for range 100 {
		if _, err := executeProto(context.Background(), proto, globals, executeOptions{args: args, maxInstructions: -1}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		values, err := executeProto(context.Background(), proto, globals, executeOptions{args: args, maxInstructions: -1})
		if err != nil || len(values) != 1 {
			b.Fatalf("owner run = (%#v, %v), want one result", values, err)
		}
	}
}
