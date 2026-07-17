package ember_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/besmpl/ember"
)

func TestHearthShapedEmbeddingContract(t *testing.T) {
	stringType := ember.TypeSummary{Kind: ember.TypeSummaryName, Display: "string"}
	receiver := ember.TypeSummary{Kind: ember.TypeSummaryTable, Display: "table"}
	childType := ember.TypeSummary{
		Kind:       ember.TypeSummaryTable,
		Display:    "table",
		Properties: []ember.TablePropertySummary{{Name: "Name", Access: "read", Type: stringType}},
	}
	widgetType := ember.TypeSummary{
		Kind:    ember.TypeSummaryTable,
		Display: "table",
		Properties: []ember.TablePropertySummary{{
			Name: "WaitForChild",
			Type: ember.TypeSummary{
				Kind:   ember.TypeSummaryFunction,
				Params: []ember.TypeSummary{receiver, stringType},
				ReturnPack: ember.TypePackSummary{
					Head: []ember.TypeSummary{childType},
				},
			},
		}},
	}
	factoryType := ember.TypeSummary{
		Kind:    ember.TypeSummaryTable,
		Display: "table",
		Properties: []ember.TablePropertySummary{{
			Name: "new",
			Type: ember.TypeSummary{
				Kind: ember.TypeSummaryIntersection,
				Types: []ember.TypeSummary{{
					Kind: ember.TypeSummaryFunction,
					Params: []ember.TypeSummary{{
						Kind:    ember.TypeSummarySingleton,
						Display: `"Widget"`,
					}},
					ReturnPack: ember.TypePackSummary{Head: []ember.TypeSummary{widgetType}},
				}},
			},
		}},
	}
	stringFunction := func(params int, returns int) ember.TypeSummary {
		function := ember.TypeSummary{Kind: ember.TypeSummaryFunction}
		for range params {
			function.Params = append(function.Params, stringType)
		}
		for range returns {
			function.ReturnPack.Head = append(function.ReturnPack.Head, stringType)
		}
		return function
	}
	loader := &programTestLoader{sources: map[string]string{
		"logical:game/a": `--!strict
local boot = wait("boot-a")
local shared = require("./shared")
return {
    startup = function()
        local widget = Factory.new("Widget")
        local child = widget:WaitForChild("Button")
        local name: string = child.Name
        observe("a:" .. boot .. ":" .. shared .. ":" .. name)
        hookWait("hook-a")
    end,
}`,
		"logical:game/b": `--!strict
return { startup = function() observe("independent") end }`,
		"logical:game/c": `--!strict
local shared = require("./shared")
return { startup = function() observe("c:" .. shared) end }`,
		"logical:game/shared": `--!strict
initialized()
return wait("shared")`,
	}}
	program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{
			{Name: "a", Module: ember.LogicalModule("game/a")},
			{Name: "b", Module: ember.LogicalModule("game/b")},
			{Name: "c", Module: ember.LogicalModule("game/c")},
		},
		Check:       true,
		Parallelism: 4,
		Analysis: ember.AnalysisConfig{Globals: map[string]ember.TypeSummary{
			"Factory":     factoryType,
			"wait":        stringFunction(1, 1),
			"hookWait":    stringFunction(1, 0),
			"initialized": stringFunction(0, 0),
			"observe":     stringFunction(1, 0),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) != 0 {
		t.Fatalf("typed load diagnostics = %#v", report.Diagnostics)
	}

	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			var initialized int
			var observed []string
			widget := ember.NewTable()
			_ = widget.Set(ember.StringValue("WaitForChild"), ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
				name, _ := args[1].String()
				return ember.HostSuspend("child:" + name)
			}))
			factory := ember.NewTable()
			_ = factory.Set(ember.StringValue("new"), ember.ContextHostFuncValue(func(_ context.Context, args []ember.Value) ([]ember.Value, error) {
				return []ember.Value{ember.TableValue(widget)}, nil
			}))
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"Factory": ember.TableValue(factory),
						"wait": ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
							token, _ := args[0].String()
							return ember.HostSuspend(token)
						}),
						"hookWait": ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
							token, _ := args[0].String()
							return ember.HostSuspend(token)
						}),
						"initialized": ember.HostFuncValue(func([]ember.Value) ([]ember.Value, error) {
							initialized++
							return nil, nil
						}),
						"observe": ember.ContextHostFuncValue(func(_ context.Context, args []ember.Value) ([]ember.Value, error) {
							value, _ := args[0].String()
							observed = append(observed, value)
							return nil, nil
						}),
					}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			step, err := runtime.RunHookResumable(context.Background(), "startup")
			if err != nil {
				t.Fatal(err)
			}
			assertHearthSuspensions(t, step, []string{"a", "c"}, []string{"boot-a", "shared"})
			assertProgramHookCalls(t, step.Hook, []string{"b"})
			if initialized != 1 || !reflect.DeepEqual(observed, []string{"independent"}) {
				t.Fatalf("initial initialized = %d observed = %v", initialized, observed)
			}

			step, err = step.Suspensions[0].Resume(context.Background(), ember.StringValue("A"))
			if err != nil {
				t.Fatal(err)
			}
			assertHearthSuspensions(t, step, []string{"c"}, []string{"shared"})
			if err := step.Suspension.Cancel(); err != nil {
				t.Fatal(err)
			}
			if err := step.Suspension.Cancel(); err != nil {
				t.Fatalf("repeated shared cancel: %v", err)
			}

			retry, err := runtime.RunHookResumable(context.Background(), "startup")
			if err != nil {
				t.Fatal(err)
			}
			assertHearthSuspensions(t, retry, []string{"a", "c"}, []string{"boot-a", "shared"})
			assertProgramHookCalls(t, retry.Hook, []string{"b"})
			if initialized != 2 {
				t.Fatalf("initializer calls after retry = %d, want 2", initialized)
			}

			retry, err = retry.Suspensions[1].Resume(context.Background(), ember.StringValue("ready"))
			if err != nil {
				t.Fatal(err)
			}
			assertHearthSuspensions(t, retry, []string{"a"}, []string{"boot-a"})
			assertProgramHookCalls(t, retry.Hook, []string{"b", "c"})

			retry, err = retry.Suspension.Resume(context.Background(), ember.StringValue("A"))
			if err != nil {
				t.Fatal(err)
			}
			assertHearthSuspensions(t, retry, []string{"a"}, []string{"child:Button"})
			child := ember.NewTable()
			_ = child.Set(ember.StringValue("Name"), ember.StringValue("Button"))
			retry, err = retry.Suspension.Resume(context.Background(), ember.TableValue(child))
			if err != nil {
				t.Fatal(err)
			}
			assertHearthSuspensions(t, retry, []string{"a"}, []string{"hook-a"})
			done, err := retry.Suspension.Resume(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			assertHearthSuspensions(t, done, nil, nil)
			assertProgramHookCalls(t, done.Hook, []string{"a", "b", "c"})
			wantObserved := []string{"independent", "independent", "c:ready", "a:A:ready:Button"}
			if !reflect.DeepEqual(observed, wantObserved) {
				t.Fatalf("completed observed = %v, want %v", observed, wantObserved)
			}

			pendingClose, err := runtime.RunHookResumable(context.Background(), "startup")
			if err != nil {
				t.Fatal(err)
			}
			assertHearthSuspensions(t, pendingClose, []string{"a"}, []string{"child:Button"})
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
			if pendingClose.Suspension.Token() != nil {
				t.Fatal("closed Hearth suspension retained its token")
			}
			if _, err := pendingClose.Suspension.Resume(context.Background()); !errors.Is(err, ember.ErrSuspensionStale) {
				t.Fatalf("resume after close error = %v, want stale", err)
			}
		})
	}
}

func assertHearthSuspensions(t *testing.T, result ember.ExecutionResult, entrypoints, tokens []string) {
	t.Helper()
	if len(result.Suspensions) != len(entrypoints) {
		t.Fatalf("suspensions = %d, want %d: %#v", len(result.Suspensions), len(entrypoints), result)
	}
	for index, suspension := range result.Suspensions {
		if suspension.Entrypoint() != entrypoints[index] || suspension.Token() != tokens[index] || suspension.Hook() != "startup" {
			t.Fatalf("suspension %d = entrypoint %q token %#v hook %q", index, suspension.Entrypoint(), suspension.Token(), suspension.Hook())
		}
	}
	if len(result.Suspensions) == 0 {
		if result.Suspension != nil {
			t.Fatalf("compatibility suspension = %#v, want nil", result.Suspension)
		}
		return
	}
	if result.Suspension != result.Suspensions[0] {
		t.Fatal("compatibility suspension is not the first pending suspension")
	}
}
