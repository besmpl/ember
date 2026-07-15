package ember

import (
	"context"
	"errors"
	"testing"
	"time"
)

type invocationScopeTestLoader map[string]string

func (loader invocationScopeTestLoader) LoadModule(_ context.Context, id ModuleID) (Source, error) {
	text, ok := loader[id.String()]
	if !ok {
		return Source{}, errors.New("missing module " + id.String())
	}
	return Source{Name: id.String(), Text: text}, nil
}

func TestDirectContextHostHookUsesInvocationContext(t *testing.T) {
	type contextKey struct{}
	key := contextKey{}
	deadline := time.Now().Add(time.Minute)
	base := context.WithValue(context.Background(), key, "direct-hook")
	ctx, cancel := context.WithDeadline(base, deadline)
	defer cancel()

	var observed context.Context
	program, _, err := LoadProgram(ctx, invocationScopeTestLoader{
		"logical:test/init": `return {startup = checkContext}`,
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: LogicalModule("test/init")}}})
	if err != nil {
		t.Fatalf("LoadProgram: %v", err)
	}
	runtime, err := program.NewRuntime(RuntimeOptions{
		Host: RuntimeHostFunc(func(_ context.Context, _ HostCall) (map[string]Value, error) {
			return map[string]Value{
				"checkContext": ContextHostFuncValue(func(got context.Context, _ []Value) ([]Value, error) {
					observed = got
					return nil, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer runtime.Close()
	if _, err := runtime.RunHook(ctx, "startup"); err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if observed == nil || observed.Value(key) != "direct-hook" {
		t.Fatalf("direct hook context value = %#v, want direct-hook", observed)
	}
	gotDeadline, ok := observed.Deadline()
	if !ok || !gotDeadline.Equal(deadline) {
		t.Fatalf("direct hook deadline = (%v, %t), want (%v, true)", gotDeadline, ok, deadline)
	}
}

func TestCoroutineDoesNotRetainInvocationScopeAcrossLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := newRuntimeOwner()
	runtime := &Runtime{execution: vmRuntimeExecution{}, owner: owner}
	controller, err := newExecutionController(ctx, ExecutionLimits{MaxInstructions: 100})
	if err != nil {
		t.Fatalf("new controller: %v", err)
	}
	scope := invocationScope{
		runtime:    runtime,
		ctx:        ctx,
		from:       moduleKey{kind: moduleKeyLogical, path: "test"},
		globals:    map[string]Value{"state": TableValue(NewTable())},
		controller: controller,
	}
	env := runtimeGlobalsWithInvocation(scope.globals, owner, scope)
	parent := newVMThread(env)
	parent.ctx = ctx
	parent.scope = scope
	parent.hasScope = true
	parent.controller = controller
	env.thread = &parent
	// A real VM entry consumes the static scope after binding it to the active
	// thread. Coroutines created by that script share this env, so they must
	// observe the transient/static split rather than retain the call capability.
	env.clearInvocationScope()

	newCoroutine := func(t *testing.T, source string) *vmCoroutine {
		t.Helper()
		proto, err := Compile(source)
		if err != nil {
			t.Fatalf("compile coroutine: %v", err)
		}
		coroutine, err := newVMCoroutineChecked(env, &closure{proto: proto})
		if err != nil {
			t.Fatalf("create coroutine: %v", err)
		}
		if coroutine.thread.globals == nil || coroutine.thread.globals != env || coroutine.thread.globals.hasScope {
			t.Fatal("new coroutine did not preserve shared globals without invocation scope")
		}
		if coroutine.thread.hasScope || coroutine.thread.scope.ctx != nil || coroutine.thread.scope.controller != nil {
			t.Fatal("new coroutine retained dynamic invocation scope")
		}
		return coroutine
	}

	t.Run("yield", func(t *testing.T) {
		coroutine := newCoroutine(t, "coroutine.yield()")
		_, err := baseCoroutineResume(env, []Value{UserDataValue(coroutine.userdata)})
		if err != nil {
			t.Fatalf("resume yielded coroutine: %v", err)
		}
		if coroutine.thread.hasScope || coroutine.suspended.hasScope || coroutine.suspended.scope.ctx != nil || coroutine.suspended.scope.controller != nil {
			t.Fatal("yielded coroutine retained invocation scope")
		}
		if coroutine.thread.controller != nil || coroutine.suspended.controller != nil {
			t.Fatal("yielded coroutine retained execution controller")
		}
		if _, err := baseCoroutineClose([]Value{UserDataValue(coroutine.userdata)}); err != nil {
			t.Fatalf("close yielded coroutine: %v", err)
		}
		if coroutine.thread.globals != nil || coroutine.thread.hasScope || coroutine.thread.scope.ctx != nil {
			t.Fatal("closed yielded coroutine retained invocation scope")
		}
	})

	t.Run("dead", func(t *testing.T) {
		coroutine := newCoroutine(t, "return 1")
		_, err := baseCoroutineResume(env, []Value{UserDataValue(coroutine.userdata)})
		if err != nil {
			t.Fatalf("resume dead coroutine: %v", err)
		}
		if coroutine.thread.globals != nil || coroutine.thread.hasScope || coroutine.thread.scope.ctx != nil || coroutine.thread.scope.controller != nil {
			t.Fatal("dead coroutine retained invocation scope")
		}
	})

	t.Run("close-before-resume", func(t *testing.T) {
		coroutine := newCoroutine(t, "return 1")
		if _, err := baseCoroutineClose([]Value{UserDataValue(coroutine.userdata)}); err != nil {
			t.Fatalf("close fresh coroutine: %v", err)
		}
		if coroutine.thread.globals != nil || coroutine.thread.hasScope || coroutine.thread.scope.ctx != nil || coroutine.thread.scope.controller != nil {
			t.Fatal("closed fresh coroutine retained invocation scope")
		}
	})
}

func TestCoroutineContextHostUsesActiveThreadScopeForCapture(t *testing.T) {
	type contextKey struct{}
	key := contextKey{}
	ctx := context.WithValue(context.Background(), key, "coroutine")
	owner := newRuntimeOwner()
	runtime := &Runtime{execution: vmRuntimeExecution{}, owner: owner}
	var callback Callback
	globals := map[string]Value{
		"capture": ContextHostFuncValue(func(got context.Context, args []Value) ([]Value, error) {
			if got.Value(key) != "coroutine" {
				return nil, errors.New("coroutine context scope was not active")
			}
			var err error
			callback, err = CaptureCallback(got, args[0])
			return nil, err
		}),
	}
	scope := invocationScope{runtime: runtime, ctx: ctx, globals: globals}
	env := runtimeGlobalsWithInvocation(globals, owner, scope)
	parent := newVMThread(env)
	parent.ctx = ctx
	parent.scope = scope
	parent.hasScope = true
	env.thread = &parent
	env.clearInvocationScope()

	proto, err := Compile("capture(function() return 7 end)\ncoroutine.yield()")
	if err != nil {
		t.Fatalf("compile coroutine: %v", err)
	}
	coroutine, err := newVMCoroutineChecked(env, &closure{proto: proto})
	if err != nil {
		t.Fatalf("create coroutine: %v", err)
	}
	if _, err := baseCoroutineResume(env, []Value{UserDataValue(coroutine.userdata)}); err != nil {
		t.Fatalf("resume coroutine: %v", err)
	}
	if callback.target == nil {
		t.Fatal("context host callback did not capture callback from coroutine")
	}
	values, err := callback.Call(context.Background())
	if err != nil || len(values) != 1 || values[0] != NumberValue(7) {
		t.Fatalf("captured coroutine callback = %#v, err %v; want [7]", values, err)
	}
	if err := callback.Close(); err != nil {
		t.Fatalf("close callback: %v", err)
	}
	if _, err := baseCoroutineClose([]Value{UserDataValue(coroutine.userdata)}); err != nil {
		t.Fatalf("close coroutine: %v", err)
	}
}

func TestCoroutineWriteInvalidatesParentGlobalSlotCache(t *testing.T) {
	globals := map[string]Value{"value": NumberValue(1)}
	env := runtimeGlobals(globals)
	if got, ok, _ := env.getSlot(0, "value"); !ok || got != NumberValue(1) {
		t.Fatalf("parent cached global = (%#v, %t), want 1", got, ok)
	}
	parent := newVMThread(env)
	env.thread = &parent
	proto, err := Compile("value = 2")
	if err != nil {
		t.Fatalf("compile coroutine: %v", err)
	}
	coroutine, err := newVMCoroutineChecked(env, &closure{proto: proto})
	if err != nil {
		t.Fatalf("create coroutine: %v", err)
	}
	if _, err := baseCoroutineResume(env, []Value{UserDataValue(coroutine.userdata)}); err != nil {
		t.Fatalf("resume coroutine: %v", err)
	}
	if got, ok, _ := env.getSlot(0, "value"); !ok || got != NumberValue(2) {
		t.Fatalf("parent global after coroutine write = (%#v, %t), want 2", got, ok)
	}
}
