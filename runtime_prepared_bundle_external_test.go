package ember_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/besmpl/ember"
	"github.com/besmpl/ember/internal/preparedfixture"
)

const externalPreparedBundleSource = `
return {
    update = function()
        local total = 0
        for index = 1, 64 do
            total = total + index
        end
        return total
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
	report, err := runtime.Dispatch(context.Background(), "update")
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

type externalPreparedLoader map[string]string

func (loader externalPreparedLoader) LoadModule(_ context.Context, id ember.ModuleID) (ember.Source, error) {
	text, ok := loader[id.String()]
	if !ok {
		return ember.Source{}, fmt.Errorf("missing module %s", id)
	}
	return ember.Source{Name: id.String(), Text: text}, nil
}
