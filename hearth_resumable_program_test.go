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

func TestResumableEntrypointInitializerCanSuspendRepeatedly(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			program := loadSingleProgram(t, `
initialized()
local first = wait("first")
local second = wait("second")
return { update = function() observe(first, second) end }`)
			var initialized int
			var observed string
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
							first, _ := args[0].String()
							second, _ := args[1].String()
							observed = first + ":" + second
							return nil, nil
						}),
					}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			first, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, first, []string{"main"}, []string{"first"})
			second, err := first.Suspension.Resume(context.Background(), ember.StringValue("A"))
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, second, []string{"main"}, []string{"second"})
			done, err := second.Suspension.Resume(context.Background(), ember.StringValue("B"))
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, done, nil, nil)
			assertProgramHookCalls(t, done.Hook, []string{"main"})
			if initialized != 1 || observed != "A:B" {
				t.Fatalf("initialized = %d observed = %q", initialized, observed)
			}
		})
	}
}

func TestResumableHookSharesSuspendedEntrypointInitializationWithRequire(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			loader := &programTestLoader{sources: map[string]string{
				"logical:game/a": `
initialized()
local value = wait("entrypoint")
return {
    value = value,
    update = function() observe("a", value) end,
}`,
				"logical:game/b": `
local shared = require("./a")
return { update = function() observe("b", shared.value) end }`,
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
			assertProgramSuspensions(t, step, []string{"a"}, []string{"entrypoint"})
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

func TestResumableHookCompletesIndependentEntrypointInitializersInHostOrder(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			loader := &programTestLoader{sources: map[string]string{
				"logical:a": `
local value = wait("load-a")
return { update = function() observe("a", value) end }`,
				"logical:b": `
local value = wait("load-b")
return { update = function() observe("b", value) end }`,
			}}
			program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
				Entrypoints: []ember.Entrypoint{
					{Name: "a", Module: ember.LogicalModule("a")},
					{Name: "b", Module: ember.LogicalModule("b")},
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
			assertProgramSuspensions(t, step, []string{"a", "b"}, []string{"load-a", "load-b"})

			step, err = step.Suspensions[1].Resume(context.Background(), ember.StringValue("B"))
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, []string{"a"}, []string{"load-a"})
			assertProgramHookCalls(t, step.Hook, []string{"b"})

			step, err = step.Suspension.Resume(context.Background(), ember.StringValue("A"))
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, nil, nil)
			assertProgramHookCalls(t, step.Hook, []string{"a", "b"})
			if !reflect.DeepEqual(observed, []string{"b:B", "a:A"}) {
				t.Fatalf("observed = %v, want [b:B a:A]", observed)
			}

			cached, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, cached, nil, nil)
			assertProgramHookCalls(t, cached.Hook, []string{"a", "b"})
			if !reflect.DeepEqual(observed, []string{"b:B", "a:A", "a:A", "b:B"}) {
				t.Fatalf("cached observed = %v", observed)
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

func TestResumableHookCompletesIndependentRequiredModulesInHostOrder(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			loader := &programTestLoader{sources: map[string]string{
				"logical:game/a":            `return { update = function() observe("a", require("./dependency-a")) end }`,
				"logical:game/b":            `return { update = function() observe("b", require("./dependency-b")) end }`,
				"logical:game/dependency-a": `return wait("dependency-a")`,
				"logical:game/dependency-b": `return wait("dependency-b")`,
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
			var observed []string
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
							token, _ := args[0].String()
							return ember.HostSuspend(token)
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
			assertProgramSuspensions(t, step, []string{"a", "b"}, []string{"dependency-a", "dependency-b"})

			step, err = step.Suspensions[1].Resume(context.Background(), ember.StringValue("B"))
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, []string{"a"}, []string{"dependency-a"})
			assertProgramHookCalls(t, step.Hook, []string{"b"})

			step, err = step.Suspension.Resume(context.Background(), ember.StringValue("A"))
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, nil, nil)
			assertProgramHookCalls(t, step.Hook, []string{"a", "b"})
			if !reflect.DeepEqual(observed, []string{"b:B", "a:A"}) {
				t.Fatalf("observed = %v, want [b:B a:A]", observed)
			}

			cached, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, cached, nil, nil)
			assertProgramHookCalls(t, cached.Hook, []string{"a", "b"})
			if !reflect.DeepEqual(observed, []string{"b:B", "a:A", "a:A", "b:B"}) {
				t.Fatalf("cached observed = %v", observed)
			}
		})
	}
}

func TestFailedResumableModuleInitializerRetriesCleanly(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			loader := &programTestLoader{sources: map[string]string{
				"logical:game/main": `return { update = function() observe(require("./dependency")) end }`,
				"logical:game/dependency": `
initialized()
return wait("dependency")`,
			}}
			program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
				Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/main")}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var initialized int
			var observed string
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
							observed, _ = args[0].String()
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
			_, err = step.Suspension.Fail(context.Background(), errors.New("dependency unavailable"))
			var runtimeErr *ember.RuntimeError
			if !errors.As(err, &runtimeErr) || !strings.Contains(err.Error(), "dependency unavailable") {
				t.Fatalf("initializer failure = %#v", err)
			}
			if !runtimeErrorHasSource(runtimeErr, "logical:game/dependency") || !runtimeErrorHasSource(runtimeErr, "logical:game/main") {
				t.Fatalf("initializer failure frames = %#v", runtimeErr.Frames)
			}

			retry, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, retry, []string{"main"}, []string{"dependency"})
			if initialized != 2 {
				t.Fatalf("initializer calls after failure = %d, want 2", initialized)
			}
			done, err := retry.Suspension.Resume(context.Background(), ember.StringValue("ready"))
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, done, nil, nil)
			assertProgramHookCalls(t, done.Hook, []string{"main"})
			if observed != "ready" {
				t.Fatalf("observed = %q, want ready", observed)
			}
		})
	}
}

func runtimeErrorHasSource(err *ember.RuntimeError, source string) bool {
	if err == nil {
		return false
	}
	for _, frame := range err.Frames {
		if frame.Source == source {
			return true
		}
	}
	return false
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
			if err := step.Suspensions[0].Cancel(); err != nil {
				t.Fatalf("repeated cancel: %v", err)
			}
			if _, err := step.Suspensions[0].Resume(context.Background()); !errors.Is(err, ember.ErrSuspensionStale) {
				t.Fatalf("resume after cancel error = %v, want stale", err)
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

func TestSuspensionCancelAbortsSharedInitializerAndAllowsRetry(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			loader := &programTestLoader{sources: map[string]string{
				"logical:game/a": `
initialized()
local value = wait("shared")
return { value = value, update = function() observe("a", value) end }`,
				"logical:game/b": `
local shared = require("./a")
return { update = function() observe("b", shared.value) end }`,
				"logical:game/c": `
local value = wait("unrelated")
return { update = function() observe("c", value) end }`,
			}}
			program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
				Entrypoints: []ember.Entrypoint{
					{Name: "a", Module: ember.LogicalModule("game/a")},
					{Name: "b", Module: ember.LogicalModule("game/b")},
					{Name: "c", Module: ember.LogicalModule("game/c")},
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
			assertProgramSuspensions(t, step, []string{"a", "c"}, []string{"shared", "unrelated"})
			if err := step.Suspensions[0].Cancel(); err != nil {
				t.Fatal(err)
			}
			if err := step.Suspensions[0].Cancel(); err != nil {
				t.Fatalf("repeated cancel: %v", err)
			}

			step, err = step.Suspensions[1].Resume(context.Background(), ember.StringValue("C"))
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, step, nil, nil)
			assertProgramHookCalls(t, step.Hook, []string{"c"})
			if initialized != 1 || !reflect.DeepEqual(observed, []string{"c:C"}) {
				t.Fatalf("after cancel initialized = %d observed = %v", initialized, observed)
			}

			retry, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, retry, []string{"a"}, []string{"shared"})
			assertProgramHookCalls(t, retry.Hook, []string{"c"})
			done, err := retry.Suspension.Resume(context.Background(), ember.StringValue("ready"))
			if err != nil {
				t.Fatal(err)
			}
			assertProgramSuspensions(t, done, nil, nil)
			assertProgramHookCalls(t, done.Hook, []string{"a", "b", "c"})
			if initialized != 2 {
				t.Fatalf("initializer calls after retry = %d, want 2", initialized)
			}
			if !reflect.DeepEqual(observed, []string{"c:C", "c:C", "a:ready", "b:ready"}) {
				t.Fatalf("observed after retry = %v", observed)
			}
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
			if err == nil || !strings.Contains(err.Error(), "active-loading cycle logical:game/main -> logical:game/shared -> logical:game/main") {
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
