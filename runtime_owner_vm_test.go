package ember

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestExecuteProtoBindsRuntimeOwnerThreadAndReleasesIt(t *testing.T) {
	owner := newRuntimeOwner()
	seen := false
	globals := runtimeGlobals(map[string]Value{
		"observe": nativeFuncValue(func(env *globalEnv, _ []Value) ([]Value, error) {
			seen = true
			if env == nil || env.thread == nil {
				t.Fatal("owner observation ran without an active VM thread")
			}
			if env.thread.owner != owner {
				t.Fatalf("thread owner = %p, want %p", env.thread.owner, owner)
			}
			if got := owner.threadCount(); got != 1 {
				t.Fatalf("owner thread count during execution = %d, want 1", got)
			}
			return nil, nil
		}),
	})
	globals.owner = owner
	proto, err := Compile("observe()\nreturn 7")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := executeProto(context.Background(), proto, globals, executeOptions{maxInstructions: -1})
	if err != nil {
		t.Fatalf("executeProto returned error: %v", err)
	}
	if !seen {
		t.Fatal("owner observation did not run")
	}
	if len(results) != 1 || results[0] != NumberValue(7) {
		t.Fatalf("executeProto results = %v, want [7]", results)
	}
	if got := owner.threadCount(); got != 0 {
		t.Fatalf("owner thread count after execution = %d, want 0", got)
	}
}

func TestExecuteProtoReleasesRuntimeOwnerThreadOnError(t *testing.T) {
	owner := newRuntimeOwner()
	globals := runtimeGlobals(map[string]Value{
		"fail": nativeFuncValue(func(*globalEnv, []Value) ([]Value, error) {
			return nil, errors.New("expected failure")
		}),
	})
	globals.owner = owner
	proto, err := Compile(`fail()`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	if _, err := executeProto(context.Background(), proto, globals, executeOptions{maxInstructions: -1}); err == nil {
		t.Fatal("executeProto succeeded, want host failure")
	}
	if got := owner.threadCount(); got != 0 {
		t.Fatalf("owner thread count after failed execution = %d, want 0", got)
	}
}

func TestRuntimeOwnerThreadPoolHandoffClearsOwner(t *testing.T) {
	owner := newRuntimeOwner()
	globals := runtimeGlobalsWithOwner(nil, owner)
	thread := acquireVMThread(context.Background(), globals)
	if err := thread.bindOwner(owner); err != nil {
		t.Fatalf("bindOwner returned error: %v", err)
	}
	if thread.owner != owner {
		t.Fatalf("acquired thread owner = %p, want %p", thread.owner, owner)
	}
	if got := owner.threadCount(); got != 1 {
		t.Fatalf("owner thread count after acquire = %d, want 1", got)
	}

	thread.resetForPool()
	if got := owner.threadCount(); got != 0 {
		t.Fatalf("owner thread count after reset = %d, want 0", got)
	}
	if thread.owner != nil || thread.ownerBound {
		t.Fatalf("reset thread retained owner: owner=%p bound=%v", thread.owner, thread.ownerBound)
	}
	vmThreadPool.Put(thread)
}

func TestRuntimeOwnerCacheBundlesStayWithTheirOwner(t *testing.T) {
	ownerA := newRuntimeOwner()
	ownerB := newRuntimeOwner()
	proto, err := Compile(`return 1`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	instanceA := &vmFunctionInstance{}
	boxA := &stringBox{text: "owner-a"}

	thread := newVMThread(nil)
	thread.resetForRun(context.Background(), runtimeGlobalsWithOwner(nil, ownerA))
	if err := thread.bindOwner(ownerA); err != nil {
		t.Fatalf("bind owner A: %v", err)
	}
	thread.functionInstances = map[*Proto]*vmFunctionInstance{proto: instanceA}
	thread.stringIntern = map[string]*stringBox{"owner-a": boxA}
	thread.resetForPool()
	if thread.functionInstances != nil || thread.stringIntern != nil {
		t.Fatal("reset thread retained owner A caches")
	}

	thread.resetForRun(context.Background(), runtimeGlobalsWithOwner(nil, ownerB))
	if err := thread.bindOwner(ownerB); err != nil {
		t.Fatalf("bind owner B: %v", err)
	}
	if thread.functionInstances != nil || thread.stringIntern != nil {
		t.Fatal("owner B observed owner A caches")
	}
	thread.resetForPool()

	thread.resetForRun(context.Background(), runtimeGlobalsWithOwner(nil, ownerA))
	if err := thread.bindOwner(ownerA); err != nil {
		t.Fatalf("rebind owner A: %v", err)
	}
	if thread.functionInstances[proto] != instanceA || thread.stringIntern["owner-a"] != boxA {
		t.Fatal("owner A did not recover its warm cache bundle")
	}
	thread.resetForPool()
	if len(ownerA.idleVMCaches) != 1 {
		t.Fatalf("owner A idle cache bundles = %d, want 1", len(ownerA.idleVMCaches))
	}
	if err := ownerA.close(); err != nil {
		t.Fatalf("close owner A: %v", err)
	}
	if len(ownerA.idleVMCaches) != 0 {
		t.Fatalf("closed owner retained %d cache bundles", len(ownerA.idleVMCaches))
	}
}

func TestVMThreadPoolResetDropsFrameReferences(t *testing.T) {
	owner := newRuntimeOwner()
	table := NewTable()
	stackOwner := &vmStackOwner{values: []Value{TableValue(table)}}
	frame := &vmFrame{
		registers:   []Value{TableValue(table)},
		cells:       []*cell{{value: TableValue(table)}},
		varargOwner: stackOwner,
		varargCount: 1,
	}
	thread := newVMThread(runtimeGlobalsWithOwner(nil, owner))
	if err := thread.bindOwner(owner); err != nil {
		t.Fatalf("bind owner: %v", err)
	}
	thread.frameSlots = []*vmFrame{frame}
	thread.resetForPool()
	if frame.registers != nil {
		t.Fatalf("reset frame retained register window: %v", frame.registers)
	}
	if frame.varargOwner != nil || frame.varargBase != 0 || frame.varargCount != 0 {
		t.Fatalf("reset frame retained vararg storage: owner=%p base=%d count=%d", frame.varargOwner, frame.varargBase, frame.varargCount)
	}
	if !stackOwner.values[0].IsNil() {
		t.Fatalf("reset frame retained vararg value: %v", stackOwner.values[0])
	}
	for i, cell := range frame.cells {
		if cell != nil {
			t.Fatalf("reset frame cell %d retained %p", i, cell)
		}
	}
}

func TestNestedScriptCallsReuseRuntimeOwnerThread(t *testing.T) {
	owner := newRuntimeOwner()
	var first *vmThread
	var calls int
	globals := runtimeGlobals(map[string]Value{
		"observe": nativeFuncValue(func(env *globalEnv, _ []Value) ([]Value, error) {
			if env == nil || env.thread == nil {
				return nil, errors.New("missing active thread")
			}
			if env.thread.owner != owner {
				return nil, errors.New("nested call lost runtime owner")
			}
			if got := owner.threadCount(); got != 1 {
				return nil, fmt.Errorf("owner thread count during nested call = %d, want 1", got)
			}
			if first == nil {
				first = env.thread
			} else if first != env.thread {
				return nil, errors.New("nested script call switched VM thread")
			}
			calls++
			return nil, nil
		}),
	})
	globals.owner = owner
	proto, err := Compile("local inner = function() observe() end\nobserve()\ninner()\nreturn 9")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := executeProto(context.Background(), proto, globals, executeOptions{maxInstructions: -1})
	if err != nil {
		t.Fatalf("executeProto returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("observe calls = %d, want 2", calls)
	}
	if len(results) != 1 || results[0] != NumberValue(9) {
		t.Fatalf("executeProto results = %v, want [9]", results)
	}
	if got := owner.threadCount(); got != 0 {
		t.Fatalf("owner thread count after nested calls = %d, want 0", got)
	}
}

func TestRuntimeOwnerReachesModuleRequireAndHookThreads(t *testing.T) {
	mainKey := moduleKey{kind: moduleKeyLogical, path: "game/main"}
	childKey := moduleKey{kind: moduleKeyLogical, path: "game/child"}
	mainProto, err := Compile(`
local child = require("./child")
return {
	startup = function()
		observe("hook")
	end,
}
`)
	if err != nil {
		t.Fatalf("Compile main returned error: %v", err)
	}
	childProto, err := Compile(`
observe("child")
return {}
`)
	if err != nil {
		t.Fatalf("Compile child returned error: %v", err)
	}
	program := &Program{
		entrypoints: []programEntrypoint{{name: "main", key: mainKey}},
		protos: map[moduleKey]*Proto{
			mainKey:  mainProto,
			childKey: childProto,
		},
	}

	var runtime *Runtime
	var events []string
	runtime, err = program.NewRuntime(RuntimeOptions{
		Host: RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
			return map[string]Value{
				"observe": nativeFuncValue(func(env *globalEnv, args []Value) ([]Value, error) {
					if env == nil || env.thread == nil {
						return nil, errors.New("owner observation ran without an active VM thread")
					}
					if env.thread.owner != runtime.owner {
						return nil, errors.New("runtime owner was not propagated")
					}
					if got := runtime.owner.threadCount(); got == 0 {
						return nil, errors.New("runtime owner has no active thread")
					}
					if len(args) != 1 {
						return nil, fmt.Errorf("observe received %d args, want 1", len(args))
					}
					name, ok := args[0].String()
					if !ok {
						return nil, fmt.Errorf("observe argument is %s, want string", args[0].Kind())
					}
					events = append(events, name)
					return nil, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}
	if len(events) != 2 || events[0] != "child" || events[1] != "hook" {
		t.Fatalf("owner observations = %v, want [child hook]", events)
	}
	if got := runtime.owner.threadCount(); got != 0 {
		t.Fatalf("runtime owner thread count after RunHook = %d, want 0", got)
	}
}

func TestRuntimeOwnerReachesHookLookupMetamethod(t *testing.T) {
	key := moduleKey{kind: moduleKeyLogical, path: "game/main"}
	proto, err := Compile(`
local hook = function()
	observe("hook")
end
return setmetatable({}, {
	__index = function(_, key)
		if key == "startup" then
			observe("lookup")
			return hook
		end
		return nil
	end,
})
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	program := &Program{
		entrypoints: []programEntrypoint{{name: "main", key: key}},
		protos:      map[moduleKey]*Proto{key: proto},
	}
	var runtime *Runtime
	var events []string
	runtime, err = program.NewRuntime(RuntimeOptions{
		Host: RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
			return map[string]Value{
				"observe": nativeFuncValue(func(env *globalEnv, args []Value) ([]Value, error) {
					if env == nil || env.thread == nil || env.thread.owner != runtime.owner {
						return nil, errors.New("hook lookup lost runtime owner")
					}
					if len(args) != 1 {
						return nil, fmt.Errorf("observe received %d args, want 1", len(args))
					}
					name, ok := args[0].String()
					if !ok {
						return nil, fmt.Errorf("observe argument is %s, want string", args[0].Kind())
					}
					events = append(events, name)
					return nil, nil
				}),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}
	if len(events) != 2 || events[0] != "lookup" || events[1] != "hook" {
		t.Fatalf("owner observations = %v, want [lookup hook]", events)
	}
	if got := runtime.owner.threadCount(); got != 0 {
		t.Fatalf("runtime owner thread count after hook lookup = %d, want 0", got)
	}
}
