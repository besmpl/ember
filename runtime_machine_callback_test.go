package ember

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMachineCallbackCloseInvalidatesCopies(t *testing.T) {
	runtime, callback := captureMachineRuntimeCallback(t, `function() return 7 end`)
	defer runtime.Close()

	copyOfCallback := callback
	if err := copyOfCallback.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := callback.Call(context.Background()); err == nil || !strings.Contains(err.Error(), "callback: released") {
		t.Fatalf("Callback.Call after copy Close error = %v, want released", err)
	}
	if err := callback.Close(); err != nil {
		t.Fatalf("repeated Callback.Close: %v", err)
	}
}

func TestMachineCallbackRootsCapturedCoroutineAcrossUnrelatedRun(t *testing.T) {
	t.Setenv(runtimeEngineEnvironment, "machine")
	entry := LogicalModule("machine/callback-coroutine-root")
	program, _, err := LoadProgram(context.Background(), machineRuntimeTestLoader{
		entry.String(): `
return {
    startup = function()
        local co = coroutine.create(function() end)
        coroutine.resume(co)
        capture(function() return coroutine.status(co) end)
    end,
    noop = function() return 0 end,
}
`,
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: entry}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}

	var callback Callback
	runtime, err := program.NewRuntime(RuntimeOptions{Host: RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
		return map[string]Value{
			"capture": ContextHostFuncValue(func(ctx context.Context, args []Value) ([]Value, error) {
				var captureErr error
				callback, captureErr = CaptureCallback(ctx, args[0])
				return nil, captureErr
			}),
		}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.RunHook(context.Background(), "noop"); err != nil {
		t.Fatal(err)
	}
	values, err := callback.Call(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("callback values = %#v, want one status", values)
	}
	if status, ok := values[0].String(); !ok || status != "dead" {
		t.Fatalf("callback status = %#v, want dead", values[0])
	}

	if err := callback.Close(); err != nil {
		t.Fatal(err)
	}
	if err := callback.Close(); err != nil {
		t.Fatalf("repeated Callback.Close: %v", err)
	}
	if _, err := runtime.RunHook(context.Background(), "noop"); err != nil {
		t.Fatal(err)
	}
	execution := runtime.execution.(*machineRuntimeExecution)
	execution.owner.coroutines.mu.Lock()
	recordLive := len(execution.owner.coroutines.arena.records) != 0 && execution.owner.coroutines.arena.records[0].live != 0
	execution.owner.coroutines.mu.Unlock()
	if recordLive {
		t.Fatal("closed callback kept captured coroutine rooted")
	}
}

func TestMachineCallbackCallAfterRuntimeCloseFailsClosed(t *testing.T) {
	runtime, callback := captureMachineRuntimeCallback(t, `function() return 7 end`)
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := callback.Call(context.Background()); err == nil || !strings.Contains(err.Error(), "callback: runtime is closed") {
		t.Fatalf("Callback.Call after Runtime.Close error = %v, want closed runtime", err)
	}
	if err := callback.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMachineCallbackRejectsOverlappingCalls(t *testing.T) {
	runtime, callback, entered, unblock := newBlockingMachineCallback(t)
	defer runtime.Close()
	defer callback.Close()
	defer unblock()

	firstDone := make(chan error, 1)
	go func() {
		_, err := callback.Call(context.Background())
		firstDone <- err
	}()
	<-entered
	if _, err := callback.Call(context.Background()); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("overlapping Callback.Call error = %v, want ErrRuntimeBusy", err)
	}
	unblock()
	if err := <-firstDone; err != nil {
		t.Fatalf("first Callback.Call returned error: %v", err)
	}
}

func TestMachineRuntimeRejectsRunHookWhileCallbackIsActive(t *testing.T) {
	runtime, callback, entered, unblock := newBlockingMachineCallback(t)
	defer runtime.Close()
	defer callback.Close()
	defer unblock()

	callDone := make(chan error, 1)
	go func() {
		_, err := callback.Call(context.Background())
		callDone <- err
	}()
	<-entered
	if _, err := runtime.RunHook(context.Background(), "startup"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("RunHook during Callback.Call error = %v, want ErrRuntimeBusy", err)
	}
	unblock()
	if err := <-callDone; err != nil {
		t.Fatalf("Callback.Call returned error: %v", err)
	}
}

func TestMachineRuntimeCloseDoesNotTearDownActiveCallback(t *testing.T) {
	runtime, callback, entered, unblock := newBlockingMachineCallback(t)
	defer callback.Close()
	defer unblock()

	callDone := make(chan error, 1)
	go func() {
		_, err := callback.Call(context.Background())
		callDone <- err
	}()
	<-entered
	if err := runtime.Close(); err == nil || !strings.Contains(err.Error(), "runtime: active") {
		t.Fatalf("Runtime.Close during Callback.Call error = %v, want active", err)
	}
	unblock()
	if err := <-callDone; err != nil {
		t.Fatalf("Callback.Call after rejected Runtime.Close: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("Runtime.Close after Callback.Call: %v", err)
	}
	if _, err := callback.Call(context.Background()); err == nil || !strings.Contains(err.Error(), "callback: runtime is closed") {
		t.Fatalf("Callback.Call after Runtime.Close error = %v, want closed runtime", err)
	}
}

func TestMachineCallbackCaptureRejectsStaleTransientCallable(t *testing.T) {
	t.Setenv(runtimeEngineEnvironment, "machine")
	entry := LogicalModule("machine/stale-callback")
	program, _, err := LoadProgram(context.Background(), machineRuntimeTestLoader{
		entry.String(): `return { startup = function() probe() end }`,
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: entry}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}

	var runtime *Runtime
	runtime, err = program.NewRuntime(RuntimeOptions{Host: RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
		return map[string]Value{
			"probe": ContextHostFuncValue(func(ctx context.Context, _ []Value) ([]Value, error) {
				execution := runtime.execution.(*machineRuntimeExecution)
				stale, valueErr := transientScriptCallableValue(scriptCallableHandle{
					owner:      execution.owner.closures.owner,
					index:      1,
					generation: 99,
				})
				if valueErr != nil {
					return nil, valueErr
				}
				_, captureErr := CaptureCallback(ctx, stale)
				return nil, captureErr
			}),
		}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err == nil || !strings.Contains(err.Error(), "script callable: stale value") {
		t.Fatalf("RunHook stale capture error = %v, want stale script callable", err)
	}
}

func TestMachineCallbackCaptureRejectsCrossOwnerTransientCallable(t *testing.T) {
	t.Setenv(runtimeEngineEnvironment, "machine")
	entry := LogicalModule("machine/cross-owner-callback")
	program, _, err := LoadProgram(context.Background(), machineRuntimeTestLoader{
		entry.String(): `return { startup = function() probe() end }`,
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: entry}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}

	other, err := program.NewRuntime(RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	otherExecution := other.execution.(*machineRuntimeExecution)
	crossOwner, err := transientScriptCallableValue(scriptCallableHandle{
		owner:      otherExecution.owner.closures.owner,
		index:      1,
		generation: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := program.NewRuntime(RuntimeOptions{Host: RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
		return map[string]Value{
			"probe": ContextHostFuncValue(func(ctx context.Context, _ []Value) ([]Value, error) {
				_, captureErr := CaptureCallback(ctx, crossOwner)
				return nil, captureErr
			}),
		}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err == nil || !strings.Contains(err.Error(), "script callable: cross-owner value") {
		t.Fatalf("RunHook cross-owner capture error = %v, want cross-owner script callable", err)
	}
}

func TestMachineCallbackUsesCapturedHostGlobalsAndCallContext(t *testing.T) {
	type contextKey struct{}
	wantContextValue := &struct{}{}
	seenContext := false
	runtime, callback := captureMachineRuntimeCallbackWithGlobals(t, `function(value) return read(value) end`, map[string]Value{
		"read": ContextHostFuncValue(func(ctx context.Context, args []Value) ([]Value, error) {
			seenContext = ctx.Value(contextKey{}) == wantContextValue
			if _, ok := invocationScopeFromContext(ctx); !ok {
				return nil, errors.New("missing callback invocation scope")
			}
			return args, nil
		}),
	})
	defer runtime.Close()
	defer callback.Close()

	ctx := context.WithValue(context.Background(), contextKey{}, wantContextValue)
	values, err := callback.Call(ctx, NumberValue(13))
	if err != nil {
		t.Fatal(err)
	}
	if !seenContext {
		t.Fatal("callback host global did not receive Callback.Call context")
	}
	if len(values) != 1 || values[0] != NumberValue(13) {
		t.Fatalf("Callback.Call values = %#v, want [13]", values)
	}
}

func newBlockingMachineCallback(t *testing.T) (*Runtime, Callback, <-chan struct{}, func()) {
	t.Helper()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	runtime, callback := captureMachineRuntimeCallbackWithGlobals(t, `function() block() end`, map[string]Value{
		"block": ContextHostFuncValue(func(context.Context, []Value) ([]Value, error) {
			entered <- struct{}{}
			<-release
			return nil, nil
		}),
	})
	return runtime, callback, entered, closeOnce(release)
}

func captureMachineRuntimeCallback(t *testing.T, callbackExpression string) (*Runtime, Callback) {
	return captureMachineRuntimeCallbackWithGlobals(t, callbackExpression, nil)
}

func captureMachineRuntimeCallbackWithGlobals(t *testing.T, callbackExpression string, extraGlobals map[string]Value) (*Runtime, Callback) {
	t.Helper()
	t.Setenv(runtimeEngineEnvironment, "machine")
	entry := LogicalModule("machine/callback")
	program, _, err := LoadProgram(context.Background(), machineRuntimeTestLoader{
		entry.String(): `return { startup = function() capture(` + callbackExpression + `) end }`,
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: entry}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}

	var callback Callback
	runtime, err := program.NewRuntime(RuntimeOptions{Host: RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
		globals := copyGlobals(extraGlobals)
		if globals == nil {
			globals = make(map[string]Value)
		}
		globals["capture"] = ContextHostFuncValue(func(ctx context.Context, args []Value) ([]Value, error) {
			var err error
			callback, err = CaptureCallback(ctx, args[0])
			return nil, err
		})
		return globals, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		_ = runtime.Close()
		t.Fatal(err)
	}
	return runtime, callback
}
