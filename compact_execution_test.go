package ember

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"unsafe"
)

func compileCompactTestProgram(t *testing.T, source string) *Proto {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.compact == nil {
		t.Fatal("compiled prototype has no compact call program")
	}
	return proto
}

func compactTestNumber(t *testing.T, values []Value, want float64) {
	t.Helper()
	if len(values) != 1 {
		t.Fatalf("execution returned %d values, want 1", len(values))
	}
	got, ok := values[0].Number()
	if !ok || got != want {
		t.Fatalf("execution returned %v (%t), want number %v", got, ok, want)
	}
}

func TestCompactCallGraphExecutesNestedDirectCalls(t *testing.T) {
	proto := compileCompactTestProgram(t, `
local function add(left, right)
    return left + right
end
return add(add(1, 2), add(3, 4))
`)
	if got, want := len(proto.compact.functions), 2; got != want {
		t.Fatalf("compact program has %d functions, want %d", got, want)
	}
	if got, want := len(proto.compact.functions[0].callSites), 3; got != want {
		t.Fatalf("compact root has %d call sites, want %d", got, want)
	}
	borrowed, copied := 0, 0
	for _, site := range proto.compact.functions[0].callSites {
		if site.flags&compactCallBorrow != 0 {
			borrowed++
		} else {
			copied++
		}
	}
	if borrowed == 0 || copied == 0 {
		t.Fatalf("nested graph exercised borrowed=%d copied=%d call paths, want both", borrowed, copied)
	}

	state := acquireCompactExecutionState()
	values, handled, err := runCompactNumericExecution(proto, nil, state)
	releaseCompactExecutionState(state)
	if err != nil {
		t.Fatalf("compact execution returned error: %v", err)
	}
	if !handled {
		t.Fatal("compact execution declined an admitted graph")
	}
	compactTestNumber(t, values, 10)

	values, err = Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	compactTestNumber(t, values, 10)
}

func TestCompactCallGraphExecutesSelfRecursionWithoutClosureCells(t *testing.T) {
	proto := compileCompactTestProgram(t, `
local function sum(n)
    if n == 0 then
        return 0
    end
    return n + sum(n - 1)
end
return sum(64)
`)
	if got, want := len(proto.compact.functions), 2; got != want {
		t.Fatalf("compact program has %d functions, want %d", got, want)
	}
	recursive := proto.compact.functions[1]
	if recursive.selfUpvalue != 0 {
		t.Fatalf("recursive compact function self upvalue is %d, want 0", recursive.selfUpvalue)
	}
	if got, want := len(recursive.callSites), 1; got != want {
		t.Fatalf("recursive compact function has %d calls, want %d", got, want)
	}
	if recursive.callSites[0].target != 1 {
		t.Fatalf("recursive call target is %d, want self id 1", recursive.callSites[0].target)
	}

	state := acquireCompactExecutionState()
	values, handled, err := runCompactNumericExecution(proto, nil, state)
	if err != nil {
		releaseCompactExecutionState(state)
		t.Fatalf("compact execution returned error: %v", err)
	}
	if !handled {
		releaseCompactExecutionState(state)
		t.Fatal("compact execution declined recursive graph")
	}
	if len(state.frames) != 0 {
		releaseCompactExecutionState(state)
		t.Fatalf("compact execution retained %d caller records", len(state.frames))
	}
	if cap(state.frames) < 64 {
		releaseCompactExecutionState(state)
		t.Fatalf("compact recursion grew only %d frame records, want at least 64", cap(state.frames))
	}
	releaseCompactExecutionState(state)
	compactTestNumber(t, values, 2080)
}

func TestCompactCallGraphHandlesExtraArgumentsAndControlFlow(t *testing.T) {
	proto := compileCompactTestProgram(t, `
local function choose(value)
    if value < 0 then
        return -value
    end
    return value + 1
end
return choose(6, 99)
`)
	values, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	compactTestNumber(t, values, 7)
}

func TestCompactCallGraphConcurrentRuns(t *testing.T) {
	proto := compileCompactTestProgram(t, `
local function sum(n)
    if n == 0 then
        return 0
    end
    return n + sum(n - 1)
end
return sum(12)
`)
	const workers = 8
	const runs = 20
	var wait sync.WaitGroup
	errors := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for run := 0; run < runs; run++ {
				values, err := Run(proto)
				if err != nil {
					errors <- err
					return
				}
				if len(values) != 1 {
					errors <- fmt.Errorf("Run returned %d values", len(values))
					return
				}
				value, ok := values[0].Number()
				if !ok || value != 78 {
					errors <- fmt.Errorf("Run returned %v (%t), want 78", value, ok)
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

func TestCompactCallGraphRejectsUnprovenGraphs(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "nonnumeric argument", source: `
local function identity(value)
    return value
end
return identity("not numeric")
`},
		{name: "function escapes as argument", source: `
local function add(left, right)
    return left + right
end
local function ignore(value)
    return 1
end
return ignore(add)
`},
		{name: "transitive global access", source: `
local function readGlobal()
    return externalValue
end
return readGlobal()
`},
		{name: "recursive function captures another local", source: `
local offset = 1
local function sum(n)
    if n == 0 then
        return offset
    end
    return sum(n - 1)
end
return sum(4)
`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proto, err := Compile(test.source)
			if err != nil {
				t.Fatalf("Compile returned error: %v", err)
			}
			if proto.compact != nil {
				t.Fatal("unsafe graph was admitted to compact execution")
			}
		})
	}
}

func TestCompactCallGraphAllocatesOnlyPublicResult(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with normal instrumentation")
	}
	programs := []struct {
		name   string
		source string
	}{
		{name: "nested calls", source: `
local function add(left, right)
    return left + right
end
return add(add(1, 2), add(3, 4))
`},
		{name: "recursion", source: `
local function sum(n)
    if n == 0 then
        return 0
    end
    return n + sum(n - 1)
end
return sum(12)
`},
	}
	for _, program := range programs {
		t.Run(program.name, func(t *testing.T) {
			proto := compileCompactTestProgram(t, program.source)
			if _, err := Run(proto); err != nil {
				t.Fatalf("warm Run returned error: %v", err)
			}
			allocations := testing.AllocsPerRun(100, func() {
				values, err := Run(proto)
				if err != nil || len(values) != 1 {
					t.Fatalf("Run returned %d values and error %v", len(values), err)
				}
			})
			if allocations != 1 {
				t.Fatalf("Run allocated %.2f objects, want exactly 1 public result slice", allocations)
			}
		})
	}
}

func TestCompactCallFrameLayoutBudget(t *testing.T) {
	if got, limit := unsafe.Sizeof(compactCallFrame{}), uintptr(24); got > limit {
		t.Fatalf("compactCallFrame size is %d bytes, want at most %d", got, limit)
	}
	frameType := reflect.TypeOf(compactCallFrame{})
	for index := 0; index < frameType.NumField(); index++ {
		kind := frameType.Field(index).Type.Kind()
		switch kind {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface, reflect.Func, reflect.String:
			t.Fatalf("compactCallFrame field %q has pointer-bearing kind %s", frameType.Field(index).Name, kind)
		}
	}
	if got, limit := unsafe.Sizeof(compactCallSite{}), uintptr(16); got > limit {
		t.Fatalf("compactCallSite size is %d bytes, want at most %d", got, limit)
	}
}
