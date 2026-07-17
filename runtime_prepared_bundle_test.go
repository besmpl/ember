package ember

import (
	"context"
	"errors"
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
	bundle := replayPreparedBundleForTest(t, program)

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

func replayPreparedBundleForTest(t *testing.T, program *Program) *PreparedBundle {
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
