package ember_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/besmpl/ember"
)

func TestResumableHooksSupportMultiplePendingInvocationsAndRepeatedWaits(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			source := `return {
    update = function(id)
        local first = wait("first-" .. id)
        local second = wait("second-" .. id)
        observe(id, first, second)
    end,
}`
			program := loadSingleProgram(t, source)
			var observed []string
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
							token, _ := args[0].String()
							return ember.HostSuspend(token)
						}),
						"observe": ember.ContextHostFuncValue(func(_ context.Context, args []ember.Value) ([]ember.Value, error) {
							parts := make([]string, len(args))
							for i, value := range args {
								if text, ok := value.String(); ok {
									parts[i] = text
									continue
								}
								if number, ok := value.Number(); ok {
									parts[i] = fmt.Sprint(number)
								}
							}
							observed = append(observed, strings.Join(parts, ":"))
							return nil, nil
						}),
					}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			first, err := runtime.RunHookResumable(context.Background(), "update", ember.StringValue("A"))
			if err != nil {
				t.Fatal(err)
			}
			second, err := runtime.RunHookResumable(context.Background(), "update", ember.StringValue("B"))
			if err != nil {
				t.Fatal(err)
			}
			assertSuspensionToken(t, first, "first-A")
			assertSuspensionToken(t, second, "first-B")

			secondNext, err := second.Suspension.Resume(context.Background(), ember.StringValue("B1"))
			if err != nil {
				t.Fatal(err)
			}
			assertSuspensionToken(t, secondNext, "second-B")
			if _, err := second.Suspension.Resume(context.Background()); !errors.Is(err, ember.ErrSuspensionStale) {
				t.Fatalf("reused suspension error = %v, want stale", err)
			}
			secondDone, err := secondNext.Suspension.Resume(context.Background(), ember.StringValue("B2"))
			if err != nil {
				t.Fatal(err)
			}
			if secondDone.Suspension != nil || secondDone.Hook == nil || len(secondDone.Hook.Calls) != 1 {
				t.Fatalf("second completion = %#v", secondDone)
			}

			firstNext, err := first.Suspension.Resume(context.Background(), ember.StringValue("A1"))
			if err != nil {
				t.Fatal(err)
			}
			assertSuspensionToken(t, firstNext, "second-A")
			firstDone, err := firstNext.Suspension.Resume(context.Background(), ember.StringValue("A2"))
			if err != nil {
				t.Fatal(err)
			}
			if firstDone.Hook == nil || len(firstDone.Hook.Calls) != 1 {
				t.Fatalf("first completion = %#v", firstDone)
			}
			if got := strings.Join(observed, ","); got != "B:B1:B2,A:A1:A2" {
				t.Fatalf("observed order = %q", got)
			}
		})
	}
}

func TestCapturedCallbackCanSuspendAndResumeOnBothEngines(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			program := loadSingleProgram(t, `return {
    startup = function()
        capture(function(seed)
            local resumed = wait("callback")
            return seed + resumed
        end)
    end,
}`)
			var callback ember.Callback
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"capture": ember.ContextHostFuncValue(func(ctx context.Context, args []ember.Value) ([]ember.Value, error) {
							captured, err := ember.CaptureCallback(ctx, args[0])
							if err == nil {
								callback = captured
							}
							return nil, err
						}),
						"wait": ember.ResumableHostFuncValue(func(context.Context, []ember.Value) ember.HostResult {
							return ember.HostSuspend("callback")
						}),
					}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			defer callback.Close()
			if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
				t.Fatal(err)
			}
			step, err := callback.CallResumable(context.Background(), ember.NumberValue(40))
			if err != nil {
				t.Fatal(err)
			}
			assertSuspensionToken(t, step, "callback")
			done, err := step.Suspension.Resume(context.Background(), ember.NumberValue(2))
			if err != nil {
				t.Fatal(err)
			}
			if len(done.Values) != 1 {
				t.Fatalf("callback values = %#v", done.Values)
			}
			got, ok := done.Values[0].Number()
			if !ok || got != 42 {
				t.Fatalf("callback result = %v (%t)", got, ok)
			}
		})
	}
}

func TestCapturedCallbackCanRequireSuspendedModuleOnBothEngines(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			loader := &programTestLoader{sources: map[string]string{
				"logical:game/main": `return {
    startup = function()
        capture(function() return require("./dependency") end)
    end,
}`,
				"logical:game/dependency": `return wait("callback-module")`,
			}}
			program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
				Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/main")}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var callback ember.Callback
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"capture": ember.ContextHostFuncValue(func(ctx context.Context, args []ember.Value) ([]ember.Value, error) {
							captured, err := ember.CaptureCallback(ctx, args[0])
							if err == nil {
								callback = captured
							}
							return nil, err
						}),
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
			defer callback.Close()
			if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
				t.Fatal(err)
			}

			step, err := callback.CallResumable(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			assertSuspensionToken(t, step, "callback-module")
			done, err := step.Suspension.Resume(context.Background(), ember.StringValue("ready"))
			if err != nil {
				t.Fatal(err)
			}
			if len(done.Values) != 1 || done.Suspension != nil {
				t.Fatalf("callback completion = %#v", done)
			}
			value, _ := done.Values[0].String()
			if value != "ready" {
				t.Fatalf("callback value = %q, want ready", value)
			}

			cached, err := callback.CallResumable(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(cached.Values) != 1 || cached.Suspension != nil {
				t.Fatalf("cached callback = %#v", cached)
			}
			cachedValue, _ := cached.Values[0].String()
			if cachedValue != "ready" {
				t.Fatalf("cached callback value = %q, want ready", cachedValue)
			}
		})
	}
}

func TestSuspensionFailPreservesProtectedCallAndRuntimeErrorSemantics(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run("pcall "+engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			program := loadSingleProgram(t, `return {
    update = function()
        local ok, message = pcall(function()
            wait("protected")
        end)
        observe(ok, message)
    end,
}`)
			var observed []ember.Value
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(context.Context, []ember.Value) ember.HostResult {
							return ember.HostSuspend("protected")
						}),
						"observe": ember.ContextHostFuncValue(func(_ context.Context, args []ember.Value) ([]ember.Value, error) {
							observed = append([]ember.Value(nil), args...)
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
			done, err := step.Suspension.Fail(context.Background(), errors.New("missing child"))
			if err != nil {
				t.Fatal(err)
			}
			if done.Hook == nil || len(observed) != 2 {
				t.Fatalf("protected completion = %#v observed %#v", done, observed)
			}
			ok, _ := observed[0].Bool()
			message, _ := observed[1].String()
			if ok || !strings.Contains(message, "missing child") {
				t.Fatalf("pcall observed = %v, %q", ok, message)
			}
		})
		t.Run("xpcall "+engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			program := loadSingleProgram(t, `return {
    update = function()
        local ok, message = xpcall(function()
            wait("protected")
        end, function(failure)
            return "handled: " .. failure
        end)
        observe(ok, message)
    end,
}`)
			var observed []ember.Value
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(context.Context, []ember.Value) ember.HostResult {
							return ember.HostSuspend("protected")
						}),
						"observe": ember.ContextHostFuncValue(func(_ context.Context, args []ember.Value) ([]ember.Value, error) {
							observed = append([]ember.Value(nil), args...)
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
			done, err := step.Suspension.Fail(context.Background(), errors.New("missing child"))
			if err != nil {
				t.Fatal(err)
			}
			if done.Hook == nil || len(observed) != 2 {
				t.Fatalf("protected completion = %#v observed %#v", done, observed)
			}
			ok, _ := observed[0].Bool()
			message, _ := observed[1].String()
			if ok || !strings.Contains(message, "handled:") || !strings.Contains(message, "missing child") {
				t.Fatalf("xpcall observed = %v, %q", ok, message)
			}
		})
	}

	for _, engine := range []string{"vm", "machine"} {
		t.Run("unprotected "+engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			program := loadSingleProgram(t, `return {update = function() wait("unprotected") end}`)
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(context.Context, []ember.Value) ember.HostResult {
							return ember.HostSuspend("unprotected")
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
			_, err = step.Suspension.Fail(context.Background(), errors.New("missing child"))
			var runtimeErr *ember.RuntimeError
			if !errors.As(err, &runtimeErr) || len(runtimeErr.Frames) == 0 || !strings.Contains(err.Error(), "missing child") {
				t.Fatalf("failed resume error = %#v", err)
			}
		})
	}
}

func TestRuntimeCloseInvalidatesPendingSuspensions(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			program := loadSingleProgram(t, `return {update = function() wait() end}`)
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(context.Context, []ember.Value) ember.HostResult {
							return ember.HostSuspend("close")
						}),
					}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			step, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
			if step.Suspension.Token() != nil {
				t.Fatal("closed suspension retained token")
			}
			if _, err := step.Suspension.Resume(context.Background()); !errors.Is(err, ember.ErrSuspensionStale) {
				t.Fatalf("resume after close error = %v", err)
			}
		})
	}
}

func TestSuspensionStepsReceiveFreshLimits(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			program := loadSingleProgram(t, `return {
    update = function()
        local first = 0
        for i = 1, 8 do first = first + i end
        wait("first")
        local second = 0
        for i = 1, 8 do second = second + i end
        wait("second")
        local third = 0
        for i = 1, 8 do third = third + i end
        return first + second + third
    end,
}`)
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
							token, _ := args[0].String()
							return ember.HostSuspend(token)
						}),
					}, nil
				}),
				Limits: ember.ExecutionLimits{MaxInstructions: 50},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			first, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatal(err)
			}
			assertSuspensionToken(t, first, "first")
			second, err := first.Suspension.Resume(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			assertSuspensionToken(t, second, "second")
			done, err := second.Suspension.Resume(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if done.Hook == nil || done.Suspension != nil {
				t.Fatalf("completion = %#v", done)
			}
		})
	}
}

func TestCanceledSuspensionStepDoesNotPoisonRuntime(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			program := loadSingleProgram(t, `return {update = function() wait("pause") end}`)
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(context.Context, []ember.Value) ember.HostResult {
							return ember.HostSuspend("pause")
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
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := step.Suspension.Resume(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled resume error = %v", err)
			}
			done, err := step.Suspension.Resume(context.Background())
			if err != nil {
				t.Fatalf("retry after canceled resume: %v", err)
			}
			if done.Hook == nil || done.Suspension != nil {
				t.Fatalf("retry completion = %#v", done)
			}
			next, err := runtime.RunHookResumable(context.Background(), "update")
			if err != nil {
				t.Fatalf("runtime after canceled step: %v", err)
			}
			assertSuspensionToken(t, next, "pause")
		})
	}
}

func TestSuspensionResumeRejectsBusyRuntime(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			entered := make(chan struct{})
			release := make(chan struct{})
			program := loadSingleProgram(t, `return {
    update = function(mode)
        if mode == "wait" then wait("pause") else block() end
    end,
}`)
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(context.Context, []ember.Value) ember.HostResult {
							return ember.HostSuspend("pause")
						}),
						"block": ember.ContextHostFuncValue(func(context.Context, []ember.Value) ([]ember.Value, error) {
							close(entered)
							<-release
							return nil, nil
						}),
					}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			step, err := runtime.RunHookResumable(context.Background(), "update", ember.StringValue("wait"))
			if err != nil {
				t.Fatal(err)
			}
			activeDone := make(chan error, 1)
			go func() {
				_, err := runtime.RunHook(context.Background(), "update", ember.StringValue("block"))
				activeDone <- err
			}()
			<-entered
			if _, err := step.Suspension.Resume(context.Background()); !errors.Is(err, ember.ErrRuntimeBusy) {
				close(release)
				t.Fatalf("busy resume error = %v", err)
			}
			close(release)
			if err := <-activeDone; err != nil {
				t.Fatalf("active hook returned error: %v", err)
			}
			done, err := step.Suspension.Resume(context.Background())
			if err != nil {
				t.Fatalf("retry after busy resume: %v", err)
			}
			if done.Hook == nil || done.Suspension != nil {
				t.Fatalf("retry completion = %#v", done)
			}
		})
	}
}

func TestSuspensionAdmissionKeepsBusyLoserRetryable(t *testing.T) {
	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("EMBER_RUNTIME_ENGINE", engine)
			entered := make(chan struct{})
			release := make(chan struct{})
			program := loadSingleProgram(t, `return {
    update = function(id)
        wait(id)
        block(id)
    end,
}`)
			runtime, err := program.NewRuntime(ember.RuntimeOptions{
				Host: ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
					return map[string]ember.Value{
						"wait": ember.ResumableHostFuncValue(func(_ context.Context, args []ember.Value) ember.HostResult {
							token, _ := args[0].String()
							return ember.HostSuspend(token)
						}),
						"block": ember.ContextHostFuncValue(func(_ context.Context, args []ember.Value) ([]ember.Value, error) {
							id, _ := args[0].String()
							if id == "A" {
								close(entered)
								<-release
							}
							return nil, nil
						}),
					}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			first, err := runtime.RunHookResumable(context.Background(), "update", ember.StringValue("A"))
			if err != nil {
				t.Fatal(err)
			}
			second, err := runtime.RunHookResumable(context.Background(), "update", ember.StringValue("B"))
			if err != nil {
				t.Fatal(err)
			}
			assertSuspensionToken(t, first, "A")
			assertSuspensionToken(t, second, "B")

			firstDone := make(chan error, 1)
			go func() {
				result, err := first.Suspension.Resume(context.Background())
				if err == nil && (result.Hook == nil || result.Suspension != nil) {
					err = fmt.Errorf("first completion = %#v", result)
				}
				firstDone <- err
			}()
			<-entered

			if _, err := second.Suspension.Resume(context.Background()); !errors.Is(err, ember.ErrRuntimeBusy) {
				close(release)
				t.Fatalf("second resume error = %v, want busy", err)
			}
			if _, err := runtime.RunHook(context.Background(), "update", ember.StringValue("C")); !errors.Is(err, ember.ErrRuntimeBusy) {
				close(release)
				t.Fatalf("normal hook error = %v, want busy", err)
			}

			close(release)
			if err := <-firstDone; err != nil {
				t.Fatal(err)
			}
			if _, err := first.Suspension.Resume(context.Background()); !errors.Is(err, ember.ErrSuspensionStale) {
				t.Fatalf("first reused error = %v, want stale", err)
			}
			done, err := second.Suspension.Resume(context.Background())
			if err != nil {
				t.Fatalf("second retry: %v", err)
			}
			if done.Hook == nil || done.Suspension != nil {
				t.Fatalf("second completion = %#v", done)
			}
		})
	}
}

func loadSingleProgram(t *testing.T, source string) *ember.Program {
	t.Helper()
	loader := &programTestLoader{sources: map[string]string{
		"logical:game/init": source,
	}}
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: ember.LogicalModule("game/init")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func assertSuspensionToken(t *testing.T, result ember.ExecutionResult, want any) {
	t.Helper()
	if result.Suspension == nil {
		t.Fatalf("result = %#v, want suspension", result)
	}
	if got := result.Suspension.Token(); got != want {
		t.Fatalf("suspension token = %#v, want %#v", got, want)
	}
}
