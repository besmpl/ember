package ember

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRuntimeOptionsPreparedSelectsCopiedBundleExplicitly(t *testing.T) {
	t.Setenv(runtimeEngineEnvironment, "invalid-but-overridden")
	program := preparedBundleRuntimeTestProgram(t)
	image, err := program.preparedProgramImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	functions := make([][]PreparedFunction, len(ir.modules))
	for moduleIndex := range ir.modules {
		functions[moduleIndex] = make([]PreparedFunction, len(ir.modules[moduleIndex].protos))
	}
	functions[0][1] = func(PreparedContext) PreparedExit {
		calls++
		return PreparedReturnOneNumber(42)
	}
	bundle := NewPreparedBundle(ir.abiVersion, ir.semanticVersion, ir.programHash, functions)
	functions[0][1] = func(PreparedContext) PreparedExit {
		return PreparedReturnOneNumber(-1)
	}

	first, err := program.NewRuntime(RuntimeOptions{Prepared: bundle})
	if err != nil {
		t.Fatal(err)
	}
	second, err := program.NewRuntime(RuntimeOptions{Prepared: bundle})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	for index, runtime := range []*Runtime{first, second} {
		execution, ok := runtime.execution.(*machineRuntimeExecution)
		if !ok || execution.owner == nil || execution.owner.prepared == nil {
			t.Fatalf("runtime %d execution = %T, want bound prepared Machine", index, runtime.execution)
		}
		if _, err := runtime.Dispatch(context.Background(), "update"); err != nil {
			t.Fatalf("runtime %d Dispatch: %v", index, err)
		}
	}
	if calls != 2 {
		t.Fatalf("prepared calls = %d, want 2", calls)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Dispatch(context.Background(), "update"); err == nil {
		t.Fatal("closed prepared runtime Dispatch succeeded")
	}
	if _, err := second.Dispatch(context.Background(), "update"); err != nil {
		t.Fatalf("closing first owner affected second: %v", err)
	}
}

func TestRuntimeOptionsPreparedInvokeUsesBoundFunction(t *testing.T) {
	t.Setenv(runtimeEngineEnvironment, "invalid-but-overridden")
	program := preparedBundleRuntimeTestProgram(t)
	image, err := program.preparedProgramImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	functions := make([][]PreparedFunction, len(ir.modules))
	for moduleIndex := range ir.modules {
		functions[moduleIndex] = make([]PreparedFunction, len(ir.modules[moduleIndex].protos))
	}
	functions[0][1] = func(PreparedContext) PreparedExit {
		calls++
		return PreparedReturnOneNumber(42)
	}
	bundle := NewPreparedBundle(ir.abiVersion, ir.semanticVersion, ir.programHash, functions)
	runtime, err := program.NewRuntime(RuntimeOptions{Prepared: bundle})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	values, err := runtime.Invoke(context.Background(), Invocation{
		Module: LogicalModule("prepared/bundle"),
		Export: "update",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("Invoke results = %d, want 1", len(values))
	}
	if number, ok := values[0].Number(); !ok || number != 42 {
		t.Fatalf("Invoke result = %v/%t, want 42", number, ok)
	}
	if calls != 1 {
		t.Fatalf("prepared Invoke calls = %d, want 1", calls)
	}
}

func TestRuntimeOptionsPreparedMismatchIsTypedAndPrecedesOwnerMutation(t *testing.T) {
	t.Setenv(runtimeEngineEnvironment, "vm")
	program := preparedBundleRuntimeTestProgram(t)
	image, err := program.preparedProgramImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}
	functions := make([][]PreparedFunction, len(ir.modules))
	for moduleIndex := range ir.modules {
		functions[moduleIndex] = make([]PreparedFunction, len(ir.modules[moduleIndex].protos))
	}
	badHash := ir.programHash
	badHash[0] ^= 0xff
	extraModules := make([][]PreparedFunction, len(functions)+1)
	copy(extraModules, functions)
	badFunctions := make([][]PreparedFunction, len(functions))
	copy(badFunctions, functions)
	badFunctions[0] = append([]PreparedFunction(nil), functions[0]...)
	badFunctions[0] = append(badFunctions[0], nil)

	tests := []struct {
		name       string
		bundle     *PreparedBundle
		wantReason string
	}{
		{
			name:       "ABI version",
			bundle:     NewPreparedBundle(ir.abiVersion+1, ir.semanticVersion, ir.programHash, functions),
			wantReason: "ABI version",
		},
		{
			name:       "semantic version",
			bundle:     NewPreparedBundle(ir.abiVersion, ir.semanticVersion+1, ir.programHash, functions),
			wantReason: "semantic version",
		},
		{
			name:       "Program hash",
			bundle:     NewPreparedBundle(ir.abiVersion, ir.semanticVersion, badHash, functions),
			wantReason: "Program hash mismatch",
		},
		{
			name:       "module inventory",
			bundle:     NewPreparedBundle(ir.abiVersion, ir.semanticVersion, ir.programHash, extraModules),
			wantReason: "module inventory",
		},
		{
			name:       "function inventory",
			bundle:     NewPreparedBundle(ir.abiVersion, ir.semanticVersion, ir.programHash, badFunctions),
			wantReason: "function inventory",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := machineClosureOwnerSequence.Load()
			runtime, err := program.NewRuntime(RuntimeOptions{Prepared: test.bundle})
			if runtime != nil || err == nil {
				t.Fatalf("NewRuntime mismatch = (%v, %v), want nil typed error", runtime, err)
			}
			var mismatch *PreparedBundleError
			if !errors.As(err, &mismatch) {
				t.Fatalf("NewRuntime error = %T %v, want *PreparedBundleError", err, err)
			}
			if !strings.Contains(err.Error(), test.wantReason) {
				t.Fatalf("NewRuntime error = %q, want reason containing %q", err, test.wantReason)
			}
			if after := machineClosureOwnerSequence.Load(); after != before {
				t.Fatalf("mismatched bundle advanced owner sequence from %d to %d", before, after)
			}
		})
	}
}

func TestRuntimeOptionsNilPreparedPreservesDefaultSelection(t *testing.T) {
	t.Setenv(runtimeEngineEnvironment, "vm")
	program := preparedBundleRuntimeTestProgram(t)
	runtime, err := program.NewRuntime(RuntimeOptions{Prepared: nil})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, ok := runtime.execution.(vmRuntimeExecution); !ok {
		t.Fatalf("nil Prepared execution = %T, want vmRuntimeExecution", runtime.execution)
	}
}

func TestRuntimeOptionsPreparedDetachesResultsAndIsolatesOwners(t *testing.T) {
	t.Setenv(runtimeEngineEnvironment, "invalid-but-overridden")
	module := LogicalModule("prepared/detached-owner")
	program, _, err := LoadProgram(context.Background(), machineRuntimeTestLoader{
		module.String(): `
return {
    update = function(value)
        local result = {value = value}
        result.self = result
        return result
    end,
}
`,
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: module}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	bundle := replayPreparedBundleForTest(t, program, nil)

	first, err := program.NewRuntime(RuntimeOptions{Prepared: bundle})
	if err != nil {
		t.Fatal(err)
	}
	second, err := program.NewRuntime(RuntimeOptions{Prepared: bundle})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	invocation := Invocation{Module: module, Export: "update"}
	firstValues, err := first.Invoke(context.Background(), invocation, NumberValue(11))
	if err != nil {
		t.Fatal(err)
	}
	secondValues, err := second.Invoke(context.Background(), invocation, NumberValue(22))
	if err != nil {
		t.Fatal(err)
	}
	firstTable := singleTableResult(t, firstValues)
	secondTable := singleTableResult(t, secondValues)
	if firstTable == secondTable {
		t.Fatal("independent prepared owners returned the same detached table")
	}
	assertDetachedOwnerTable(t, firstTable, 11)
	assertDetachedOwnerTable(t, secondTable, 22)

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	assertDetachedOwnerTable(t, firstTable, 11)
	if err := firstTable.Set(StringValue("value"), NumberValue(33)); err != nil {
		t.Fatalf("mutate first detached result after owner close: %v", err)
	}
	assertDetachedOwnerTable(t, firstTable, 33)
	assertDetachedOwnerTable(t, secondTable, 22)

	secondValues, err = second.Invoke(context.Background(), invocation, NumberValue(44))
	if err != nil {
		t.Fatalf("closing first prepared owner affected second: %v", err)
	}
	thirdTable := singleTableResult(t, secondValues)
	if thirdTable == firstTable || thirdTable == secondTable {
		t.Fatal("prepared owner reused a detached result table across invocations")
	}
	assertDetachedOwnerTable(t, thirdTable, 44)
}

func replayPreparedBundleForTest(t testing.TB, program *Program, onCall func()) *PreparedBundle {
	t.Helper()
	image, err := program.preparedProgramImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}
	functions := make([][]PreparedFunction, len(ir.modules))
	for moduleIndex := range ir.modules {
		functions[moduleIndex] = make([]PreparedFunction, len(ir.modules[moduleIndex].protos))
		for protoIndex := range functions[moduleIndex] {
			functions[moduleIndex][protoIndex] = func(PreparedContext) PreparedExit {
				if onCall != nil {
					onCall()
				}
				return PreparedReplayEntry()
			}
		}
	}
	return NewPreparedBundle(ir.abiVersion, ir.semanticVersion, ir.programHash, functions)
}

func singleTableResult(t *testing.T, values []Value) *Table {
	t.Helper()
	if len(values) != 1 {
		t.Fatalf("results = %d, want 1", len(values))
	}
	table, ok := values[0].Table()
	if !ok {
		t.Fatalf("result kind = %s, want table", values[0].Kind())
	}
	return table
}

func assertDetachedOwnerTable(t *testing.T, table *Table, want float64) {
	t.Helper()
	value, err := table.Get(StringValue("value"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := value.Number()
	if !ok || got != want {
		t.Fatalf("detached value = %v/%t, want %v", got, ok, want)
	}
	self, err := table.Get(StringValue("self"))
	if err != nil {
		t.Fatal(err)
	}
	selfTable, ok := self.Table()
	if !ok || selfTable != table {
		t.Fatalf("detached self = %v/%t, want original table", selfTable, ok)
	}
}

func preparedBundleRuntimeTestProgram(t *testing.T) *Program {
	t.Helper()
	module := LogicalModule("prepared/bundle")
	program, _, err := LoadProgram(context.Background(), machineRuntimeTestLoader{
		module.String(): `return { update = function() return 42 end }`,
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: module}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func TestRuntimeOptionsPreparedPreservesBusyAndCloseOwnership(t *testing.T) {
	t.Setenv(runtimeEngineEnvironment, "invalid-but-overridden")
	program := preparedBundleRuntimeTestProgram(t)
	image, err := program.preparedProgramImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProgramIR(image)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := closeOnce(release)
	defer unblock()
	functions := make([][]PreparedFunction, len(ir.modules))
	for moduleIndex := range ir.modules {
		functions[moduleIndex] = make([]PreparedFunction, len(ir.modules[moduleIndex].protos))
	}
	functions[0][1] = func(PreparedContext) PreparedExit {
		close(entered)
		<-release
		return PreparedReturnOneNumber(42)
	}
	bundle := NewPreparedBundle(ir.abiVersion, ir.semanticVersion, ir.programHash, functions)
	runtime, err := program.NewRuntime(RuntimeOptions{Prepared: bundle})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	invocation := Invocation{Module: LogicalModule("prepared/bundle"), Export: "update"}
	firstDone := make(chan error, 1)
	go func() {
		_, callErr := runtime.Invoke(context.Background(), invocation)
		firstDone <- callErr
	}()
	<-entered
	if _, err := runtime.Invoke(context.Background(), invocation); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("overlapping prepared Invoke error = %v, want ErrRuntimeBusy", err)
	}
	if err := runtime.Close(); err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("Close during prepared Invoke error = %v, want active error", err)
	}
	unblock()
	if err := <-firstDone; err != nil {
		t.Fatalf("first prepared Invoke: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Invoke(context.Background(), invocation); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("prepared Invoke after Close error = %v, want closed error", err)
	}
}

func TestRuntimeOptionsPreparedReplayPreservesPublicEffectsAcrossEngines(t *testing.T) {
	root := LogicalModule("prepared/effects/main")
	dependency := LogicalModule("prepared/effects/dependency")
	program, _, err := LoadProgram(context.Background(), machineRuntimeTestLoader{
		root.String(): `
local dependency = require("./dependency")
return {
    update = function(seed)
        local state = setmetatable({value = seed}, {__index = {bonus = dependency.bonus}})
        local thread = coroutine.create(function(value)
            local first = bump(value + state.bonus)
            coroutine.yield(first)
            return first + 1
        end)
        local ok1, first = coroutine.resume(thread, state.value)
        local ok2, second = coroutine.resume(thread)
        retain(function(value) return value + state.bonus end)
        return ok1, first, ok2, second, coroutine.status(thread)
    end,
}
`,
		dependency.String(): `return {bonus = 3}`,
	}, ProgramOptions{
		Entrypoints: []Entrypoint{{Name: "main", Module: root}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	type snapshot struct {
		firstOK       bool
		first         float64
		secondOK      bool
		second        float64
		status        string
		callbackValue float64
		effects       string
	}
	want := snapshot{
		firstOK:       true,
		first:         15,
		secondOK:      true,
		second:        16,
		status:        "dead",
		callbackValue: 23,
		effects:       "bump:13,capture",
	}
	for _, mode := range []string{"vm", "machine", "prepared"} {
		t.Run(mode, func(t *testing.T) {
			var callback Callback
			effects := make([]string, 0, 2)
			globals := map[string]Value{
				"bump": HostFuncValue(func(args []Value) ([]Value, error) {
					number, ok := args[0].Number()
					if !ok {
						return nil, fmt.Errorf("bump: want number")
					}
					effects = append(effects, fmt.Sprintf("bump:%g", number))
					return []Value{NumberValue(number + 2)}, nil
				}),
				"retain": ContextHostFuncValue(func(ctx context.Context, args []Value) ([]Value, error) {
					var captureErr error
					callback, captureErr = CaptureCallback(ctx, args[0])
					if captureErr == nil {
						effects = append(effects, "capture")
					}
					return nil, captureErr
				}),
			}

			var bundle *PreparedBundle
			preparedCalls := 0
			if mode == "prepared" {
				bundle = replayPreparedBundleForTest(t, program, func() { preparedCalls++ })
				t.Setenv(runtimeEngineEnvironment, "invalid-but-overridden")
			} else {
				t.Setenv(runtimeEngineEnvironment, mode)
			}
			runtime, err := program.NewRuntime(RuntimeOptions{Prepared: bundle})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			defer func() { _ = callback.Close() }()
			invocation := Invocation{Module: root, Export: "update", Globals: globals}
			values, err := runtime.Invoke(context.Background(), invocation, NumberValue(10))
			if err != nil {
				_ = runtime.Close()
				t.Fatal(err)
			}
			if callback.target == nil {
				_ = runtime.Close()
				t.Fatal("host did not capture callback")
			}
			callbackValues, err := callback.Call(context.Background(), NumberValue(20))
			if err != nil {
				_ = runtime.Close()
				t.Fatal(err)
			}
			if len(callbackValues) != 1 {
				t.Fatalf("callback results = %d, want 1", len(callbackValues))
			}
			callbackValue, ok := callbackValues[0].Number()
			if !ok {
				t.Fatalf("callback result = %v, want number", callbackValues[0])
			}
			beforeCancel := preparedCalls
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := runtime.Invoke(canceled, invocation, NumberValue(10)); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled Invoke error = %v, want context.Canceled", err)
			}
			if mode == "prepared" && preparedCalls != beforeCancel {
				t.Fatalf("canceled Invoke entered prepared code: calls %d -> %d", beforeCancel, preparedCalls)
			}
			if err := callback.Close(); err != nil {
				t.Fatal(err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
			if mode == "prepared" && preparedCalls == 0 {
				t.Fatal("prepared runtime never entered its verified bundle")
			}
			if len(values) != 5 {
				t.Fatalf("Invoke results = %d, want 5", len(values))
			}
			firstOK, firstOKKind := values[0].Bool()
			first, firstKind := values[1].Number()
			secondOK, secondOKKind := values[2].Bool()
			second, secondKind := values[3].Number()
			status, statusKind := values[4].String()
			if !firstOKKind || !firstKind || !secondOKKind || !secondKind || !statusKind {
				t.Fatalf("Invoke result kinds = %v, want bool/number/bool/number/string", values)
			}

			got := snapshot{
				firstOK:       firstOK,
				first:         first,
				secondOK:      secondOK,
				second:        second,
				status:        status,
				callbackValue: callbackValue,
				effects:       strings.Join(effects, ","),
			}
			if got != want {
				t.Fatalf("snapshot = %#v, want semantic oracle %#v", got, want)
			}
		})
	}
}
