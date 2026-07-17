package ember_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/besmpl/ember"
)

func TestResumableHookPumpsEntrypointsToQuiescentSnapshot(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			loader := &programTestLoader{sources: map[string]string{
				"logical:first":  `return { update = function() wait("first") end }`,
				"logical:second": `return { update = function() observe("second") end }`,
				"logical:third":  `return { update = function() wait("third") end }`,
			}}
			program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
				Entrypoints: []ember.Entrypoint{
					{Name: "first", Module: ember.LogicalModule("first")},
					{Name: "second", Module: ember.LogicalModule("second")},
					{Name: "third", Module: ember.LogicalModule("third")},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			var observed []string
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
							token, _ := args[0].String()
							return ember.HostSuspend(token)
						}),
						"observe": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
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
			defer runtime.Close()

			step, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, []string{"first", "third"}, []string{"first", "third"})
			assertProgramHookCalls(t, step.Hook, []string{"second"})
			if !reflect.DeepEqual(observed, []string{"second"}) {
				t.Fatalf("observed = %v, want [second]", observed)
			}

			step, err = step.Suspensions[1].Resume(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, []string{"first"}, []string{"first"})
			assertProgramHookCalls(t, step.Hook, []string{"second", "third"})

			step, err = step.Suspensions[0].Resume(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, nil, nil)
			assertProgramHookCalls(t, step.Hook, []string{"first", "second", "third"})
		})
	}
}

func TestResumableHookSuspendsDuringEntrypointInitialization(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			program := loadSingleProgram(t, `
local initialized = wait("load")
return {
    update = function()
        observe(initialized)
    end,
}`)
			var observed []string
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
							token, _ := args[0].String()
							return ember.HostSuspend(token)
						}),
						"observe": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
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
			defer runtime.Close()

			step, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, []string{"main"}, []string{"load"})
			assertProgramHookCalls(t, step.Hook, nil)

			step, err = step.Suspension.Resume(context.Background(), ember.StringValue("ready"))
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, nil, nil)
			assertProgramHookCalls(t, step.Hook, []string{"main"})
			if !reflect.DeepEqual(observed, []string{"ready"}) {
				t.Fatalf("observed = %v, want [ready]", observed)
			}
		})
	}
}

func TestResumableHookSuspendsDuringRequiredModuleInitialization(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			loader := &programTestLoader{sources: map[string]string{
				"logical:game/main": `return {
    update = function()
        observe(require("./dependency"))
    end,
}`,
				"logical:game/dependency": `return wait("dependency")`,
			}}
			program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
				Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/main")}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var observed []string
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
							token, _ := args[0].String()
							return ember.HostSuspend(token)
						}),
						"observe": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
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
			defer runtime.Close()

			step, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, []string{"main"}, []string{"dependency"})
			assertProgramHookCalls(t, step.Hook, nil)

			step, err = step.Suspension.Resume(context.Background(), ember.StringValue("ready"))
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, nil, nil)
			assertProgramHookCalls(t, step.Hook, []string{"main"})
			if !reflect.DeepEqual(observed, []string{"ready"}) {
				t.Fatalf("observed = %v, want [ready]", observed)
			}
		})
	}
}

func TestResumableHookSharesRequiredModuleInitializationAcrossEntrypoints(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			loader := &programTestLoader{sources: map[string]string{
				"logical:game/a": `return { update = function() observe("a", require("./shared")) end }`,
				"logical:game/b": `return { update = function() observe("b", require("./shared")) end }`,
				"logical:game/shared": `
initialized()
return wait("shared")`,
			}}
			program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
				Entrypoints: []ember.Entrypoint{
					{Name: "a", Module: ember.LogicalModule("game/a")},
					{Name: "b", Module: ember.LogicalModule("game/b")},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			var initialized int
			var observed []string
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
							token, _ := args[0].String()
							return ember.HostSuspend(token)
						}),
						"initialized": ember.HostFuncValue(func([]ember.Value) ([]ember.Value, error) {
							initialized++
							return nil, nil
						}),
						"observe": ember.HostFuncValue(func(args []ember.Value) ([]ember.Value, error) {
							entrypoint, _ := args[0].String()
							value, _ := args[1].String()
							observed = append(observed, entrypoint+":"+value)
							return nil, nil
						}),
					}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			step, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, []string{"a"}, []string{"shared"})
			assertProgramHookCalls(t, step.Hook, nil)
			if initialized != 1 {
				t.Fatalf("initializer calls = %d, want 1", initialized)
			}

			step, err = step.Suspension.Resume(context.Background(), ember.StringValue("ready"))
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, nil, nil)
			assertProgramHookCalls(t, step.Hook, []string{"a", "b"})
			if initialized != 1 {
				t.Fatalf("initializer calls after resume = %d, want 1", initialized)
			}
			if !reflect.DeepEqual(observed, []string{"a:ready", "b:ready"}) {
				t.Fatalf("observed = %v, want [a:ready b:ready]", observed)
			}
		})
	}
}

func TestSuspensionCancelAbandonsOnlyOnePendingEntrypoint(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			loader := &programTestLoader{sources: map[string]string{
				"logical:first":  `return { update = function() wait("first") end }`,
				"logical:second": `return { update = function() end }`,
				"logical:third":  `return { update = function() wait("third") end }`,
			}}
			program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
				Entrypoints: []ember.Entrypoint{
					{Name: "first", Module: ember.LogicalModule("first")},
					{Name: "second", Module: ember.LogicalModule("second")},
					{Name: "third", Module: ember.LogicalModule("third")},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
							token, _ := args[0].String()
							return ember.HostSuspend(token)
						}),
					}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			step, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, []string{"first", "third"}, []string{"first", "third"})
			if err := step.Suspensions[0].Cancel(); err != nil {
				t.Fatal(err)
			}
			if err := step.Suspensions[0].Cancel(); !errors.Is(err, ember.ErrSuspensionStale) {
				t.Fatalf("repeated cancel error = %v, want stale", err)
			}
			step, err = step.Suspensions[1].Resume(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, nil, nil)
			assertProgramHookCalls(t, step.Hook, []string{"second", "third"})
		})
	}
}

func TestResumableModuleInitializationPreservesTrueCycleDetection(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			loader := &programTestLoader{sources: map[string]string{
				"logical:game/main": `
local shared = require("./shared")
return { update = function() end }`,
				"logical:game/shared": `
local path = "./main"
return require(path)`,
			}}
			program, report, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
				Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/main")}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Diagnostics) != 0 {
				t.Fatalf("load diagnostics = %#v, want runtime-only cycle", report.Diagnostics)
			}
			runtime, err := program.NewRuntime(ember.RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			_, err = runtime.RunHookResumable(context.Background(), "update")
			if err == nil || !strings.Contains(err.Error(), "active-loading cycle") ||
				!strings.Contains(err.Error(), "logical:game/main") ||
				!strings.Contains(err.Error(), "logical:game/shared") {
				t.Fatalf("cycle error = %v", err)
			}
		})
	}
}

func assertProgramSuspensions(t *testing.T, result ember.ExecutionResult, entrypoints, tokens []string) {
	t.Helper()
	if len(result.Suspensions) != len(entrypoints) {
		t.Fatalf("suspensions = %d, want %d: %#v", len(result.Suspensions), len(entrypoints), result)
	}
	for index, suspension := range result.Suspensions {
		if got := suspension.Entrypoint(); got != entrypoints[index] {
			t.Fatalf("suspension %d entrypoint = %q, want %q", index, got, entrypoints[index])
		}
		if got := suspension.Token(); got != tokens[index] {
			t.Fatalf("suspension %d token = %#v, want %q", index, got, tokens[index])
		}
		if got := suspension.Hook(); got != "update" {
			t.Fatalf("suspension %d hook = %q, want update", index, got)
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

func assertProgramHookCalls(t *testing.T, report *ember.HookReport, entrypoints []string) {
	t.Helper()
	if report == nil {
		t.Fatal("hook report is nil")
	}
	if len(report.Calls) != len(entrypoints) {
		t.Fatalf("hook calls = %#v, want entrypoints %v", report.Calls, entrypoints)
	}
	for index, call := range report.Calls {
		if call.Entrypoint != entrypoints[index] {
			t.Fatalf("hook call %d entrypoint = %q, want %q", index, call.Entrypoint, entrypoints[index])
		}
		if !call.Called {
			t.Fatalf("hook call %d = %#v, want called", index, call)
		}
	}
}
