package ember

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"unsafe"
)

func TestCompactCallProgramRunsNestedDirectCalls(t *testing.T) {
	proto, err := Compile(`
local function add(left, right)
	return left + right
end
return add(add(1, 2), add(3, 4))
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.compact == nil {
		t.Fatal("nested direct-call program did not receive a compact call graph")
	}
	if got, want := len(proto.compact.functions), 2; got != want {
		t.Fatalf("compact function count = %d, want %d", got, want)
	}
	if got, want := len(proto.compact.calls), 3; got != want {
		t.Fatalf("compact call-site count = %d, want %d", got, want)
	}
	for index, site := range proto.compact.calls {
		if site.flags&compactCallBorrowed == 0 {
			t.Fatalf("compact call site %d is not using the compiler-proven borrowed window", index)
		}
	}

	got, handled, err := runSlotExecution(proto, nil)
	if err != nil || !handled {
		t.Fatalf("compact execution = (%#v, %t, %v), want handled result", got, handled, err)
	}
	want, err := runWithSlotExecutionDisabled(proto)
	if err != nil {
		t.Fatalf("established VM returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) || len(got) != 1 || got[0] != NumberValue(10) {
		t.Fatalf("nested call results = (%#v, %#v), want exact [10] parity", got, want)
	}
}

func TestCompactCallProgramRunsDirectSelfRecursion(t *testing.T) {
	proto, err := Compile(`
local function sum(n)
	if n == 0 then
		return 0
	end
	return n + sum(n - 1)
end
return sum(12)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.compact == nil {
		t.Fatal("direct recursive program did not receive a compact call graph")
	}
	if got, want := len(proto.compact.calls), 2; got != want {
		t.Fatalf("compact call-site count = %d, want %d", got, want)
	}
	child := proto.compact.functions[1].proto
	if child == nil || len(child.upvalues) != 1 {
		t.Fatalf("recursive child upvalues = %#v, want one canonical self cell", child)
	}

	got, handled, err := runSlotExecution(proto, nil)
	if err != nil || !handled {
		t.Fatalf("compact recursive execution = (%#v, %t, %v), want handled result", got, handled, err)
	}
	want, err := runWithSlotExecutionDisabled(proto)
	if err != nil {
		t.Fatalf("established VM returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) || len(got) != 1 || got[0] != NumberValue(78) {
		t.Fatalf("recursive results = (%#v, %#v), want exact [78] parity", got, want)
	}
}

func TestCompactCallProgramRejectsRuntimeCaptures(t *testing.T) {
	proto, err := Compile(`
local offset = 4
local function add(value)
	return value + offset
end
return add(3)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.compact != nil {
		t.Fatal("capturing call graph reached compact execution")
	}
	values, err := Run(proto)
	if err != nil || len(values) != 1 || values[0] != NumberValue(7) {
		t.Fatalf("capturing fallback result = (%#v, %v), want [7]", values, err)
	}
}

func TestCompactCallProgramRejectsObservableFunctionIdentity(t *testing.T) {
	proto, err := Compile(`
local function add(left, right)
	return left + right
end
return add
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.compact != nil {
		t.Fatal("observable function identity reached compact execution")
	}
}

func TestCompactCallFrameIsPointerFreeAndSmall(t *testing.T) {
	if got := unsafe.Sizeof(compactCallFrame{}); got > 24 {
		t.Fatalf("compact call frame size = %d bytes, want <= 24", got)
	}
	typeOf := reflect.TypeOf(compactCallFrame{})
	for index := 0; index < typeOf.NumField(); index++ {
		field := typeOf.Field(index)
		switch field.Type.Kind() {
		case reflect.Pointer, reflect.UnsafePointer, reflect.Slice, reflect.Map, reflect.Interface, reflect.Func, reflect.Chan:
			t.Fatalf("compact call frame field %q has pointer-bearing kind %s", field.Name, field.Type.Kind())
		}
	}
}

func TestCompactCallProgramWarmRunAllocationBudget(t *testing.T) {
	proto, err := Compile(`
local function add(left, right)
	return left + right
end
return add(add(1, 2), add(3, 4))
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.compact == nil {
		t.Fatal("nested direct-call program did not receive a compact call graph")
	}
	allocs := testing.AllocsPerRun(100, func() {
		values, err := Run(proto)
		if err != nil || len(values) != 1 || values[0] != NumberValue(10) {
			t.Fatalf("compact Run = (%#v, %v), want [10]", values, err)
		}
	})
	if allocs > 1 {
		t.Fatalf("warm compact call-graph allocations = %.2f, want <= 1", allocs)
	}
}

func TestCompactCallProgramFallsBackBeforeWrongTypeEffects(t *testing.T) {
	child := newProto(nil, []instruction{
		{op: opAdd, a: 1, b: 0, c: 0},
		{op: opReturnOne, a: 1},
	}, nil, nil, 2, 1, false)
	proto := newProto(nil, []instruction{
		{op: opClosure, a: 1, b: 0},
		{op: opMove, a: 2, b: 0},
		{op: opCallLocalOne, a: 2, b: 1, c: 2, d: 1},
		{op: opReturnOne, a: 2},
	}, []*Proto{child}, nil, 3, 1, false)
	if proto.verifyErr != nil {
		t.Fatalf("manual prototype verification failed: %v", proto.verifyErr)
	}
	if proto.compact == nil {
		t.Fatal("fixed-parameter numeric graph did not receive compact execution")
	}

	args := []Value{StringValue("not a number")}
	if values, handled, err := runSlotExecution(proto, args); err != nil || handled || values != nil {
		t.Fatalf("wrong-type compact attempt = (%#v, %t, %v), want clean fallback", values, handled, err)
	}
	_, wantErr := runWithSlotExecutionDisabledArgs(proto, args)
	if wantErr == nil {
		t.Fatal("established VM accepted wrong-type arithmetic")
	}
	_, gotErr := executeProto(context.Background(), proto, nil, executeOptions{args: args, controller: nil})
	if gotErr == nil || gotErr.Error() != wantErr.Error() {
		t.Fatalf("fallback error = %v, want %v", gotErr, wantErr)
	}
}

func TestCompactCallProgramCopiesNonBorrowableArguments(t *testing.T) {
	child := newProto(nil, []instruction{
		{op: opAdd, a: 2, b: 0, c: 1},
		{op: opReturnOne, a: 2},
	}, nil, nil, 3, 2, false)
	proto := newProto([]Value{NumberValue(1), NumberValue(2)}, []instruction{
		{op: opClosure, a: 0, b: 0},
		{op: opLoadConst, a: 1, b: 0},
		{op: opLoadConst, a: 2, b: 1},
		{op: opCallOne, a: 3, b: 0, c: 2, d: 1},
		{op: opReturnOne, a: 3},
	}, []*Proto{child}, nil, 4, 0, false)
	if proto.verifyErr != nil {
		t.Fatalf("manual prototype verification failed: %v", proto.verifyErr)
	}
	if proto.compact == nil || len(proto.compact.calls) != 1 {
		t.Fatalf("compact call program = %#v, want one copied call", proto.compact)
	}
	if proto.compact.calls[0].flags&compactCallBorrowed != 0 {
		t.Fatal("overlapping result window was incorrectly marked borrowed")
	}
	values, handled, err := runSlotExecution(proto, nil)
	if err != nil || !handled || len(values) != 1 || values[0] != NumberValue(3) {
		t.Fatalf("copied compact call = (%#v, %t, %v), want [3]", values, handled, err)
	}
}

func TestCompactCallProgramGrowsStacksForDeepRecursion(t *testing.T) {
	proto, err := Compile(`
local function sum(n)
	if n == 0 then
		return 0
	end
	return n + sum(n - 1)
end
return sum(256)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.compact == nil {
		t.Fatal("deep recursive program did not receive a compact call graph")
	}
	state := acquireSlotExecutionState(0, 0)
	values, handled, err := runCompactCallProgram(proto, nil, state)
	if err != nil || !handled || len(values) != 1 || values[0] != NumberValue(32896) {
		releaseSlotExecutionState(state)
		t.Fatalf("deep compact recursion = (%#v, %t, %v), want [32896]", values, handled, err)
	}
	if len(state.compactFrames) != 0 {
		releaseSlotExecutionState(state)
		t.Fatalf("deep compact recursion retained %d caller records", len(state.compactFrames))
	}
	if len(state.numericRegisters) <= proto.registers {
		releaseSlotExecutionState(state)
		t.Fatalf("numeric stack length = %d, want growth beyond root frame %d", len(state.numericRegisters), proto.registers)
	}
	releaseSlotExecutionState(state)
}

func TestCompactCallProgramSupportsConcurrentRuns(t *testing.T) {
	proto, err := Compile(`
local function sum(n)
	if n == 0 then
		return 0
	end
	return n + sum(n - 1)
end
return sum(12)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.compact == nil {
		t.Fatal("recursive program did not receive a compact call graph")
	}

	const workers = 8
	const runs = 50
	var wait sync.WaitGroup
	errors := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range runs {
				values, err := Run(proto)
				if err != nil {
					errors <- err
					return
				}
				if len(values) != 1 || values[0] != NumberValue(78) {
					errors <- fmt.Errorf("concurrent compact call returned %#v, want [78]", values)
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}
