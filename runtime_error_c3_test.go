package ember

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestProtectedCallsCatchOrdinaryScriptAndHostErrors(t *testing.T) {
	proto, err := Compile(`return pcall(function() return missing.value end)`)
	if err != nil {
		t.Fatal(err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("script pcall returned error: %v", err)
	}
	if len(results) != 2 || results[0] != BoolValue(false) {
		t.Fatalf("script pcall results = %#v, want false and message", results)
	}

	proto, err = Compile("return pcall(host)")
	if err != nil {
		t.Fatal(err)
	}
	results, err = RunWithGlobals(proto, map[string]Value{
		"host": HostFuncValue(func([]Value) ([]Value, error) { return nil, errors.New("ordinary host failure") }),
	})
	if err != nil {
		t.Fatalf("host pcall returned error: %v", err)
	}
	if len(results) != 2 || results[0] != BoolValue(false) {
		t.Fatalf("host pcall results = %#v, want false and message", results)
	}

	proto, err = Compile(`return xpcall(host, function(message) return message end)`)
	if err != nil {
		t.Fatal(err)
	}
	results, err = RunWithGlobals(proto, map[string]Value{
		"host": HostFuncValue(func([]Value) ([]Value, error) { return nil, errors.New("xpcall ordinary failure") }),
	})
	if err != nil || len(results) != 2 || results[0] != BoolValue(false) {
		t.Fatalf("ordinary xpcall = %#v, err %v; want handled false result", results, err)
	}

	proto, err = Compile("return (function() return missing.value end)()")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(proto)
	var runtimeErr *RuntimeError
	if err == nil || !errors.As(err, &runtimeErr) {
		t.Fatalf("uncaught script error = %v, want RuntimeError", err)
	}
}

func TestProtectedCallsPropagateCancellationAndEveryLimitKind(t *testing.T) {
	proto, err := Compile("return pcall(host)")
	if err != nil {
		t.Fatal(err)
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		results, callErr := RunWithGlobals(proto, map[string]Value{
			"host": HostFuncValue(func([]Value) ([]Value, error) { return nil, cause }),
		})
		if results != nil || !errors.Is(callErr, cause) {
			t.Fatalf("pcall %v returned results %#v, err %v; want propagated cause", cause, results, callErr)
		}
	}
	kinds := []LimitKind{
		LimitInstructions, LimitSourceBytes, LimitTokens, LimitNesting, LimitSyntaxNodes,
		LimitModules, LimitTotalSourceBytes, LimitCallDepth, LimitModuleInitializations,
		LimitCoroutines, LimitGeneratedStringBytes, LimitTableEntriesPerTable, LimitRuntimeObjects,
	}
	for _, kind := range kinds {
		want := &LimitError{Kind: kind, Limit: 1, Used: 2}
		results, callErr := RunWithGlobals(proto, map[string]Value{
			"host": HostFuncValue(func([]Value) ([]Value, error) { return nil, want }),
		})
		var got *LimitError
		if results != nil || !errors.Is(callErr, ErrLimitExceeded) || !errors.As(callErr, &got) || got != want {
			t.Fatalf("pcall %s returned results %#v, err %v; want exact LimitError propagation", kind, results, callErr)
		}
	}
}

func TestInstructionLimitInsidePCallEscapesToGo(t *testing.T) {
	proto, err := Compile("return pcall(function() while true do end end)")
	if err != nil {
		t.Fatal(err)
	}
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 1})
	if err != nil {
		t.Fatal(err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	thread.controller = controller
	_, err = thread.run(proto, nil, nil)
	var limitErr *LimitError
	if err == nil || !errors.Is(err, ErrLimitExceeded) || !errors.As(err, &limitErr) {
		t.Fatalf("pcall instruction limit error = %v, want propagated LimitError", err)
	}
}

func TestXPCallHandlerBoundaryErrorPropagates(t *testing.T) {
	proto, err := Compile("return xpcall(fail, handler)")
	if err != nil {
		t.Fatal(err)
	}
	original := errors.New("original failure")
	causes := []error{context.Canceled, &LimitError{Kind: LimitRuntimeObjects, Limit: 1, Used: 2}}
	for _, want := range causes {
		_, err = RunWithGlobals(proto, map[string]Value{
			"fail":    HostFuncValue(func([]Value) ([]Value, error) { return nil, original }),
			"handler": HostFuncValue(func([]Value) ([]Value, error) { return nil, want }),
		})
		var limitErr *LimitError
		if !errors.Is(err, want) || errors.Is(err, original) {
			t.Fatalf("xpcall error = %v, want handler boundary cause and not original failure", err)
		}
		if wantLimit, ok := want.(*LimitError); ok && (!errors.As(err, &limitErr) || limitErr != wantLimit) {
			t.Fatalf("xpcall limit error = %v, want exact handler limit", err)
		}
	}
}

func TestUncaughtHostErrorIsRuntimeErrorWithCause(t *testing.T) {
	want := errors.New("host sentinel")
	proto, err := Compile("return host()")
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunWithGlobals(proto, map[string]Value{
		"host": HostFuncValue(func([]Value) ([]Value, error) { return nil, fmt.Errorf("adapter: %w", want) }),
	})
	var runtimeErr *RuntimeError
	if !errors.Is(err, want) || !errors.As(err, &runtimeErr) {
		t.Fatalf("error = %v, want RuntimeError preserving host cause", err)
	}
}

func TestRunHookAndCallbackWrapRuntimeErrorsWithoutFlattening(t *testing.T) {
	hostErr := errors.New("hook host sentinel")
	program, _, err := LoadProgram(context.Background(), runtimeErrorModuleLoader{
		"logical:game/main": "return {startup = function() return host() end}",
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: LogicalModule("game/main")}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := program.NewRuntime(RuntimeOptions{Host: RuntimeHostFunc(func(_ context.Context, call HostCall) (map[string]Value, error) {
		if call.Hook == "" {
			return nil, nil
		}
		return map[string]Value{"host": HostFuncValue(func([]Value) ([]Value, error) { return nil, hostErr })}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RunHook(context.Background(), "startup")
	var runtimeErr *RuntimeError
	if !errors.Is(err, hostErr) || !errors.As(err, &runtimeErr) {
		t.Fatalf("RunHook error = %v, want wrapped RuntimeError preserving cause", err)
	}

	var callback Callback
	callbackErr := errors.New("callback host sentinel")
	program, _, err = LoadProgram(context.Background(), runtimeErrorModuleLoader{
		"logical:game/main": "return {startup = function() connect(function() return host() end) end}",
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: LogicalModule("game/main")}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err = program.NewRuntime(RuntimeOptions{Host: RuntimeHostFunc(func(_ context.Context, call HostCall) (map[string]Value, error) {
		if call.Hook == "" {
			return nil, nil
		}
		return map[string]Value{
			"connect": ContextHostFuncValue(func(ctx context.Context, args []Value) ([]Value, error) {
				callback, err = CaptureCallback(ctx, args[0])
				return nil, err
			}),
			"host": HostFuncValue(func([]Value) ([]Value, error) { return nil, callbackErr }),
		}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("callback capture hook returned error: %v", err)
	}
	defer callback.Close()
	_, err = callback.Call(context.Background())
	if !errors.Is(err, callbackErr) || !errors.As(err, &runtimeErr) {
		t.Fatalf("Callback.Call error = %v, want wrapped RuntimeError preserving cause", err)
	}
}
