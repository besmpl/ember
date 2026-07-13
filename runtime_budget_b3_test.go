package ember

import (
	"context"
	"strings"
	"testing"
)

func TestInstructionLimitIsSharedAcrossRequire(t *testing.T) {
	mainProto := compileB3TestProto(t, `
return {
	startup = function()
		return require("child")
	end,
}
`)
	childProto := compileB3TestProto(t, `
local total = 0
for i = 1, 8 do
	total = total + i
end
return total
`)
	mainKey := moduleKey{kind: moduleKeyLogical, path: "main"}
	childKey := moduleKey{kind: moduleKeyLogical, path: "child"}
	program := &Program{
		entrypoints: []programEntrypoint{{name: "main", key: mainKey}},
		protos: map[moduleKey]*Proto{
			mainKey:  mainProto,
			childKey: childProto,
		},
	}
	runtime, err := program.NewRuntime(RuntimeOptions{Limits: ExecutionLimits{MaxInstructions: 30}})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err == nil || !strings.Contains(err.Error(), "instruction budget") {
		t.Fatalf("RunHook error = %v, want shared instruction budget exhaustion", err)
	}
}

func TestInstructionLimitIsSharedAcrossEntrypoints(t *testing.T) {
	program := newB3Program(t, []b3EntrypointSource{
		{name: "first", path: "first", source: b3LoopEntrypointSource()},
		{name: "second", path: "second", source: b3LoopEntrypointSource()},
	})
	runtime, err := program.NewRuntime(RuntimeOptions{Limits: ExecutionLimits{MaxInstructions: 30}})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err == nil || !strings.Contains(err.Error(), "instruction budget") {
		t.Fatalf("RunHook error = %v, want shared instruction budget exhaustion", err)
	}
}

func TestLoadedModuleDoesNotResetInstructionLimit(t *testing.T) {
	childInitializations := 0
	program := newB3Program(t, []b3EntrypointSource{
		{name: "first", path: "first", source: b3RequireEntrypointSource()},
		{name: "second", path: "second", source: b3RequireEntrypointSource()},
	}, b3ModuleSource{name: "child", source: `
markChild()
local total = 0
for i = 1, 8 do total = total + i end
return total
`})
	host := RuntimeHostFunc(func(_ context.Context, _ HostCall) (map[string]Value, error) {
		return map[string]Value{
			"markChild": HostFuncValue(func([]Value) ([]Value, error) {
				childInitializations++
				return nil, nil
			}),
		}, nil
	})
	runtime, err := program.NewRuntime(RuntimeOptions{Host: host, Limits: ExecutionLimits{MaxInstructions: 60}})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}
	if childInitializations != 1 {
		t.Fatalf("child initialization count = %d, want 1", childInitializations)
	}
}

func TestCallbackCallGetsFreshInstructionLimit(t *testing.T) {
	var callback Callback
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
return { startup = function()
	local setup = 0
	for i = 1, 8 do setup = setup + i end
	capture(function()
	local total = 0
	for i = 1, 8 do total = total + i end
	return total
end) end }
`}})
	host := RuntimeHostFunc(func(_ context.Context, call HostCall) (map[string]Value, error) {
		return map[string]Value{
			"capture": ContextHostFuncValue(func(ctx context.Context, args []Value) ([]Value, error) {
				var err error
				callback, err = CaptureCallback(ctx, args[0])
				return nil, err
			}),
		}, nil
	})
	runtime, err := program.NewRuntime(RuntimeOptions{Host: host, Limits: ExecutionLimits{MaxInstructions: 45}})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("RunHook returned error: %v", err)
	}
	defer callback.Close()
	values, err := callback.Call(context.Background())
	if err != nil {
		t.Fatalf("Callback.Call returned error: %v", err)
	}
	if len(values) != 1 || values[0] != NumberValue(36) {
		t.Fatalf("Callback.Call values = %#v, want [36]", values)
	}
}

func TestNestedScriptCallsShareInstructionLimit(t *testing.T) {
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
local function child()
	local total = 0
	for i = 1, 8 do total = total + i end
	return total
end
return { startup = function() return child() end }
`}})
	runtime, err := program.NewRuntime(RuntimeOptions{Limits: ExecutionLimits{MaxInstructions: 25}})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err == nil || !strings.Contains(err.Error(), "instruction budget") {
		t.Fatalf("RunHook error = %v, want shared instruction budget exhaustion", err)
	}
}

func compileB3TestProto(t *testing.T, source string) *Proto {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	return proto
}

type b3EntrypointSource struct {
	name   string
	path   string
	source string
}

type b3ModuleSource struct {
	name   string
	source string
}

func newB3Program(t *testing.T, entrypoints []b3EntrypointSource, modules ...b3ModuleSource) *Program {
	t.Helper()
	program := &Program{
		entrypoints: make([]programEntrypoint, 0, len(entrypoints)),
		protos:      make(map[moduleKey]*Proto, len(entrypoints)+len(modules)),
	}
	for _, entrypoint := range entrypoints {
		key := moduleKey{kind: moduleKeyLogical, path: entrypoint.path}
		program.entrypoints = append(program.entrypoints, programEntrypoint{name: entrypoint.name, key: key})
		program.protos[key] = compileB3TestProto(t, entrypoint.source)
	}
	for _, module := range modules {
		key := moduleKey{kind: moduleKeyLogical, path: module.name}
		program.protos[key] = compileB3TestProto(t, module.source)
	}
	return program
}

func b3LoopEntrypointSource() string {
	return `return { startup = function()
	local total = 0
	for i = 1, 8 do total = total + i end
	return total
end }`
}

func b3RequireEntrypointSource() string {
	return `return { startup = function() return require("child") end }`
}

func b3LoopModuleSource() string {
	return `local total = 0
for i = 1, 8 do total = total + i end
return total`
}
