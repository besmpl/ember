package ember

import (
	"context"
	"errors"
	"strings"
	"testing"
)

var benchmarkRuntimeRequireValue Value

// BenchmarkRuntimeRequireEnvironment includes the per-invocation environment
// and global-map setup around the cached require adapter.
func BenchmarkRuntimeRequireEnvironment(b *testing.B) {
	runtime := &Runtime{execution: vmRuntimeExecution{}, owner: newRuntimeOwner(), requireAdapters: make(map[moduleKey]Value)}
	call := runtime.newInvocationScope(context.Background(), moduleKey{kind: moduleKeyLogical, path: "game/init"}, nil, nil)
	_ = call.envWithRequire()
	b.ReportAllocs()
	for range b.N {
		env := call.envWithRequire()
		benchmarkRuntimeRequireValue, _ = env.get("require")
	}
}

func BenchmarkRuntimeRequireAdapterCacheLookup(b *testing.B) {
	runtime := &Runtime{execution: vmRuntimeExecution{}, owner: newRuntimeOwner(), requireAdapters: make(map[moduleKey]Value)}
	from := moduleKey{kind: moduleKeyLogical, path: "game/init"}
	runtime.requireAdapter(from)
	b.ReportAllocs()
	for range b.N {
		benchmarkRuntimeRequireValue = runtime.requireAdapter(from)
	}
}

func TestRetainedRequirePreservesModuleOriginAndUsesActiveScope(t *testing.T) {
	type contextKey struct{}
	key := contextKey{}
	ctxA := context.WithValue(context.Background(), key, "origin")
	ctxB := context.WithValue(context.Background(), key, "caller")

	moduleA := moduleKey{kind: moduleKeyLogical, path: "game/server/a"}
	moduleAChild := moduleKey{kind: moduleKeyLogical, path: "game/server/child"}
	moduleB := moduleKey{kind: moduleKeyLogical, path: "game/client/b"}
	moduleBChild := moduleKey{kind: moduleKeyLogical, path: "game/client/child"}
	compile := func(source string) *Proto {
		t.Helper()
		proto, err := Compile(source)
		if err != nil {
			t.Fatalf("compile %q: %v", source, err)
		}
		return proto
	}
	runtime := &Runtime{
		execution: vmRuntimeExecution{},
		owner:     newRuntimeOwner(),
		program: &Program{protos: map[moduleKey]*Proto{
			moduleA:      compile("return require"),
			moduleAChild: compile(`return probe()`),
			moduleB:      compile("return 1"),
			moduleBChild: compile(`return "client"`),
		}},
		loaded: make(map[moduleKey]Value),
		active: make(map[moduleKey]bool),
	}
	defer runtime.Close()

	originGlobals := map[string]Value{
		"probe": ContextHostFuncValue(func(ctx context.Context, _ []Value) ([]Value, error) {
			return []Value{StringValue("origin:" + ctx.Value(key).(string))}, nil
		}),
	}
	results, err := runtime.runModuleWithContextGlobalsController(ctxA, moduleA, originGlobals, nil, nil)
	if err != nil {
		t.Fatalf("load origin module: %v", err)
	}
	if len(results) != 1 || !callableValue(results[0]) {
		t.Fatalf("origin module result = %#v, want retained require function", results)
	}
	retainedRequire := results[0]
	if _, ok := runtime.loaded[moduleA]; !ok {
		t.Fatal("origin module was not cached after it returned")
	}
	if runtime.active[moduleA] {
		t.Fatal("origin module remained active after it returned")
	}
	cachedRequire, ok := runtime.newInvocationScope(ctxA, moduleA, originGlobals, nil).envWithRequire().get("require")
	if !ok || cachedRequire.ref != retainedRequire.ref {
		t.Fatal("require adapter was recreated for the same runtime/module origin")
	}

	callerGlobals := map[string]Value{
		"probe": ContextHostFuncValue(func(ctx context.Context, _ []Value) ([]Value, error) {
			value, ok := ctx.Value(key).(string)
			if !ok {
				return nil, errors.New("caller context value missing")
			}
			return []Value{StringValue("caller:" + value)}, nil
		}),
	}
	callerController, err := newExecutionController(ctxB, ExecutionLimits{MaxInstructions: 100})
	if err != nil {
		t.Fatalf("new caller controller: %v", err)
	}
	callerScope := runtime.newInvocationScope(ctxB, moduleB, callerGlobals, callerController)
	callerEnv := callerScope.envWithRequire()
	child, err := callValueWithContextController(ctxB, retainedRequire, callerEnv, []Value{StringValue("./child")}, callerController)
	if err != nil {
		t.Fatalf("call retained require: %v", err)
	}
	if len(child) != 1 || child[0].Kind() != StringKind || child[0].stringText() != "caller:caller" {
		got := "<missing>"
		if len(child) == 1 && child[0].Kind() == StringKind {
			got = child[0].stringText()
		}
		t.Fatalf("retained require result = %#v (%s), want caller context from server child", child, got)
	}

	// The retained function came from server/a, so it must not resolve the
	// same relative request from client/b's origin.
	if child[0].stringText() == "client" {
		t.Fatal("retained require resolved relative to the invoking module")
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("close runtime: %v", err)
	}
	if runtime.requireAdapters != nil {
		t.Fatal("runtime close retained require adapters")
	}
	closedEnv := runtimeGlobalsWithInvocation(callerGlobals, runtime.owner, callerScope)
	if _, err := callValueWithContextController(ctxB, retainedRequire, closedEnv, []Value{StringValue("./child")}, callerController); err == nil {
		t.Fatal("retained require succeeded after runtime close")
	} else if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("retained require after close error = %v, want closed", err)
	}
}

func TestRuntimeRequireAdapterRejectsRuntimeMismatch(t *testing.T) {
	runtimeA := &Runtime{execution: vmRuntimeExecution{}, owner: newRuntimeOwner()}
	defer runtimeA.Close()
	runtimeB := &Runtime{execution: vmRuntimeExecution{}, owner: newRuntimeOwner()}
	defer runtimeB.Close()
	from := moduleKey{kind: moduleKeyLogical, path: "game/a"}
	adapter := runtimeA.requireAdapter(from)
	ctx := context.Background()
	scope := runtimeB.newInvocationScope(ctx, moduleKey{kind: moduleKeyLogical, path: "game/b"}, nil, nil)
	env := scope.envWithRequire()
	if _, err := callValueWithContextController(ctx, adapter, env, []Value{StringValue("./child")}, nil); err == nil {
		t.Fatal("runtime-mismatched require adapter succeeded")
	} else if !strings.Contains(err.Error(), "runtime mismatch") {
		t.Fatalf("runtime-mismatched require error = %v, want runtime mismatch", err)
	}
}

func TestRuntimeCollectImportsRequireAdapters(t *testing.T) {
	runtime := &Runtime{execution: vmRuntimeExecution{}, owner: newRuntimeOwner(), requireAdapters: make(map[moduleKey]Value)}
	from := moduleKey{kind: moduleKeyLogical, path: "game/a"}
	value := runtime.requireAdapter(from)
	if _, err := runtime.collect(); err != nil {
		t.Fatalf("collect with cached require adapter: %v", err)
	}
	handle, found, _ := runtime.owner.heap.lookupExistingValue(value)
	if !found {
		t.Fatal("cached require adapter was not imported into the owner heap")
	}
	if err := runtime.owner.heap.validateSlot(handle); err != nil {
		t.Fatalf("cached require adapter handle after collect: %v", err)
	}
}
