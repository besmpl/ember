package ember_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/besmpl/ember"
	"github.com/besmpl/ember/internal/preparedfixture"
)

const externalPreparedBundleSource = `
return {
    update = function(value)
        return value + 1
    end,
}
`

func TestPreparedBundleGeneratedSurfaceIsOpaqueAndConstructible(t *testing.T) {
	function := ember.PreparedFunction(func(context ember.PreparedContext) ember.PreparedExit {
		if number, ok := context.NumberParameter(0); ok {
			return ember.PreparedReturnOneNumber(number)
		}
		return ember.PreparedReplayEntry()
	})
	bundle := ember.NewPreparedBundle(1, 1, [32]byte{}, [][]ember.PreparedFunction{{function}})
	if bundle == nil {
		t.Fatal("NewPreparedBundle returned nil")
	}
}

func TestGeneratedPreparedBundleBindsRunsAndClosesAcrossPackage(t *testing.T) {
	t.Setenv("EMBER_RUNTIME_ENGINE", "invalid-but-overridden")
	module := ember.LogicalModule("prepared/external")
	program, _, err := ember.LoadProgram(context.Background(), externalPreparedLoader{
		module.String(): externalPreparedBundleSource,
	}, ember.ProgramOptions{Entrypoints: []ember.Entrypoint{{Name: "main", Module: module}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{Prepared: preparedfixture.Bundle})
	if err != nil {
		t.Fatal(err)
	}
	report, err := runtime.Dispatch(context.Background(), "update", ember.NumberValue(41))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Calls) != 1 || !report.Calls[0].Loaded || !report.Calls[0].Called {
		t.Fatalf("Dispatch report = %#v, want one loaded and called entrypoint", report)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Dispatch(context.Background(), "update"); err == nil {
		t.Fatal("closed generated prepared runtime Dispatch succeeded")
	}
}

func TestGeneratedPreparedBundleReplayPreservesPublicRuntimeError(t *testing.T) {
	module := ember.LogicalModule("prepared/external")
	program, _, err := ember.LoadProgram(context.Background(), externalPreparedLoader{
		module.String(): externalPreparedBundleSource,
	}, ember.ProgramOptions{Entrypoints: []ember.Entrypoint{{Name: "main", Module: module}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("EMBER_RUNTIME_ENGINE", "machine")
	canonical, err := program.NewRuntime(ember.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer canonical.Close()
	t.Setenv("EMBER_RUNTIME_ENGINE", "invalid-but-overridden")
	prepared, err := program.NewRuntime(ember.RuntimeOptions{Prepared: preparedfixture.Bundle})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()

	invocation := ember.Invocation{Module: module, Export: "update"}
	for _, test := range []struct {
		name    string
		runtime *ember.Runtime
	}{{name: "canonical", runtime: canonical}, {name: "prepared", runtime: prepared}} {
		t.Run(test.name+"/fast-return", func(t *testing.T) {
			values, err := test.runtime.Invoke(context.Background(), invocation, ember.NumberValue(41))
			if err != nil {
				t.Fatal(err)
			}
			if len(values) != 1 {
				t.Fatalf("results = %d, want 1", len(values))
			}
			if result, ok := values[0].Number(); !ok || result != 42 {
				t.Fatalf("result = %v/%t, want 42", result, ok)
			}
		})
		t.Run(test.name+"/guard-replay-error", func(t *testing.T) {
			_, err := test.runtime.Invoke(context.Background(), invocation, ember.StringValue("bad"))
			if err == nil {
				t.Fatal("Invoke succeeded")
			}
			var runtimeErr *ember.RuntimeError
			if !errors.As(err, &runtimeErr) {
				t.Fatalf("error = %T %v, want *ember.RuntimeError", err, err)
			}
			const wantMessage = "run: add failed: add left operand is string, want number"
			if runtimeErr.Message != wantMessage || !strings.Contains(err.Error(), wantMessage) {
				t.Fatalf("message/error = %q/%q, want %q", runtimeErr.Message, err.Error(), wantMessage)
			}
			wantFrame := ember.ScriptFrame{
				Source:   "logical:prepared/external",
				Function: "<anonymous>",
				Line:     4,
			}
			if len(runtimeErr.Frames) != 1 || runtimeErr.Frames[0] != wantFrame {
				t.Fatalf("frames = %#v, want %#v", runtimeErr.Frames, []ember.ScriptFrame{wantFrame})
			}
		})
	}
}

func TestGeneratedPreparedBundleHonorsPublicInstructionLimits(t *testing.T) {
	module := ember.LogicalModule("prepared/external")
	program, _, err := ember.LoadProgram(context.Background(), externalPreparedLoader{
		module.String(): externalPreparedBundleSource,
	}, ember.ProgramOptions{Entrypoints: []ember.Entrypoint{{Name: "main", Module: module}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}

	failedBudgets := 0
	for budget := uint64(1); budget <= 128; budget++ {
		canonicalValue, canonicalLimit := invokeExternalPreparedWithLimit(t, program, module, nil, budget)
		preparedValue, preparedLimit := invokeExternalPreparedWithLimit(t, program, module, preparedfixture.Bundle, budget)
		if (canonicalLimit == nil) != (preparedLimit == nil) {
			t.Fatalf("budget %d limit results = canonical %v, prepared %v", budget, canonicalLimit, preparedLimit)
		}
		if canonicalLimit != nil {
			failedBudgets++
			if *canonicalLimit != *preparedLimit {
				t.Fatalf("budget %d limit errors = canonical %#v, prepared %#v", budget, canonicalLimit, preparedLimit)
			}
			continue
		}
		if failedBudgets == 0 {
			t.Fatal("instruction limit did not reject any smaller budget")
		}
		if canonicalValue != 42 || preparedValue != canonicalValue {
			t.Fatalf("budget %d results = canonical %v, prepared %v, want 42", budget, canonicalValue, preparedValue)
		}
		return
	}
	t.Fatal("no successful invocation within instruction-budget search range")
}

func invokeExternalPreparedWithLimit(
	t *testing.T,
	program *ember.Program,
	module ember.ModuleID,
	bundle *ember.PreparedBundle,
	budget uint64,
) (float64, *ember.LimitError) {
	t.Helper()
	if bundle == nil {
		t.Setenv("EMBER_RUNTIME_ENGINE", "machine")
	} else {
		t.Setenv("EMBER_RUNTIME_ENGINE", "invalid-but-overridden")
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Limits:   ember.ExecutionLimits{MaxInstructions: budget},
		Prepared: bundle,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	values, err := runtime.Invoke(context.Background(), ember.Invocation{Module: module, Export: "update"}, ember.NumberValue(41))
	if err != nil {
		var limit *ember.LimitError
		if !errors.As(err, &limit) || limit.Kind != ember.LimitInstructions {
			t.Fatalf("budget %d error = %T %v, want instruction *ember.LimitError", budget, err, err)
		}
		return 0, limit
	}
	if len(values) != 1 {
		t.Fatalf("budget %d results = %d, want 1", budget, len(values))
	}
	value, ok := values[0].Number()
	if !ok {
		t.Fatalf("budget %d result = %v, want number", budget, values[0])
	}
	return value, nil
}

type externalPreparedLoader map[string]string

func (loader externalPreparedLoader) LoadModule(_ context.Context, id ember.ModuleID) (ember.Source, error) {
	text, ok := loader[id.String()]
	if !ok {
		return ember.Source{}, fmt.Errorf("missing module %s", id)
	}
	return ember.Source{Name: id.String(), Text: text}, nil
}
