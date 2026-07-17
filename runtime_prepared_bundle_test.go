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
