package ember

import (
	"context"
	"errors"
	"testing"
)

func TestB8ControllerResourceLimits(t *testing.T) {
	c, err := newExecutionController(context.Background(), ExecutionLimits{
		MaxCallDepth:             2,
		MaxModuleInitializations: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.enterCall(); err != nil {
		t.Fatal(err)
	}
	if err := c.enterCall(); err != nil {
		t.Fatal(err)
	}
	err = c.enterCall()
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitCallDepth || limit.Limit != 2 || limit.Used != 3 {
		t.Fatalf("call limit = %#v", err)
	}
	c.leaveCall()
	c.leaveCall()
	if err := c.chargeModuleInitialization(); err != nil {
		t.Fatal(err)
	}
	err = c.chargeModuleInitialization()
	if !errors.As(err, &limit) || limit.Kind != LimitModuleInitializations || limit.Used != 2 {
		t.Fatalf("module limit = %#v", err)
	}
}

func TestB8RuntimeRecursionLimit(t *testing.T) {
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
local function loop() return loop() end
return { startup = function() return loop() end }
`}})
	runtime, err := program.NewRuntime(RuntimeOptions{Limits: ExecutionLimits{MaxCallDepth: 4}})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, err = runtime.RunHook(context.Background(), "startup")
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitCallDepth {
		t.Fatalf("RunHook error = %#v", err)
	}
}

func TestB8OwnerCoroutineLimitReleases(t *testing.T) {
	owner := newRuntimeOwner()
	owner.coroutineLimit = 1
	first := &vmCoroutine{owner: owner}
	if err := owner.retainCoroutine(first); err != nil {
		t.Fatal(err)
	}
	second := &vmCoroutine{owner: owner}
	err := owner.retainCoroutine(second)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitCoroutines || limit.Used != 2 {
		t.Fatalf("retain error = %#v", err)
	}
	owner.releaseCoroutine(first)
	if err := owner.retainCoroutine(second); err != nil {
		t.Fatal(err)
	}
}

func TestB8ModuleInitializationCache(t *testing.T) {
	program := newB3Program(t, []b3EntrypointSource{
		{name: "a", path: "a", source: `return { startup = function() return require("child") end }`},
		{name: "b", path: "b", source: `return { startup = function() return require("child") end }`},
	}, b3ModuleSource{name: "child", source: `return 7`})
	runtime, err := program.NewRuntime(RuntimeOptions{Limits: ExecutionLimits{MaxModuleInitializations: 3}})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("cached module run = %v", err)
	}
}

func TestB8ModuleInitializationLimit(t *testing.T) {
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `return { startup = function() return require("one"), require("two") end }`}},
		b3ModuleSource{name: "one", source: `return 1`}, b3ModuleSource{name: "two", source: `return 2`})
	runtime, err := program.NewRuntime(RuntimeOptions{Limits: ExecutionLimits{MaxModuleInitializations: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, err = runtime.RunHook(context.Background(), "startup")
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitModuleInitializations {
		t.Fatalf("module limit error = %#v", err)
	}
}

func TestB8PublicCoroutineCapacity(t *testing.T) {
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
return { startup = function()
  local f = function() coroutine.yield() end
  local a = coroutine.create(f)
  local b = coroutine.create(f)
  return a, b
end }
`}})
	runtime, err := program.NewRuntime(RuntimeOptions{Limits: ExecutionLimits{MaxCoroutines: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, err = runtime.RunHook(context.Background(), "startup")
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitCoroutines {
		t.Fatalf("capacity error = %#v", err)
	}
}

func TestB8CoroutineResumeDepthReattach(t *testing.T) {
	globals := runtimeGlobals(nil)
	proto := compileB5Proto(t, `local function nested() coroutine.yield("pause") end nested() return 1`)
	coroutine := newVMCoroutine(globals, &closure{proto: proto})
	parent := newVMThread(globals)
	globals.thread = &parent
	controllerA, err := newExecutionController(context.Background(), ExecutionLimits{MaxCallDepth: 4})
	if err != nil {
		t.Fatal(err)
	}
	parent.controller = controllerA
	first, err := baseCoroutineResume(globals, []Value{UserDataValue(coroutine.userdata)})
	if err != nil || len(first) == 0 {
		t.Fatalf("first resume = %#v, %v", first, err)
	}
	controllerB, err := newExecutionController(context.Background(), ExecutionLimits{MaxCallDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := controllerB.enterCall(); err != nil {
		t.Fatal(err)
	}
	parent.controller = controllerB
	second, err := baseCoroutineResume(globals, []Value{UserDataValue(coroutine.userdata)})
	if err != nil || len(second) < 2 {
		t.Fatalf("second resume = %#v, %v", second, err)
	}
	ok, _ := second[0].Bool()
	if ok {
		t.Fatalf("second resume = %#v, want depth failure", second)
	}
	if controllerB.callDepth != 1 {
		t.Fatalf("parent call depth = %d, want 1", controllerB.callDepth)
	}
	controllerB.leaveCall()
}

func TestB8CoroutineCloseAndCompletionReleaseCapacity(t *testing.T) {
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
local function done() return 1 end
local function paused() coroutine.yield() end
local co = nil
local startup = function()
  co = coroutine.create(paused)
  coroutine.close(co)
  co = coroutine.create(done)
  coroutine.resume(co)
  co = coroutine.create(paused)
  coroutine.close(co)
end
return { startup = startup }
`}})
	runtime, err := program.NewRuntime(RuntimeOptions{Limits: ExecutionLimits{MaxCoroutines: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("release run = %v", err)
	}
}

func TestB8SuspendedCoroutineConsumesAcrossRuns(t *testing.T) {
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
local function paused() coroutine.yield() end
local co = nil
local startup = function() co = coroutine.create(paused) end
return { startup = startup,
again = function() coroutine.create(paused) end }
`}})
	runtime, err := program.NewRuntime(RuntimeOptions{Limits: ExecutionLimits{MaxCoroutines: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RunHook(context.Background(), "again")
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitCoroutines {
		t.Fatalf("cross-run limit = %#v", err)
	}
}

func TestB8FailedCreateDoesNotLeakCapacity(t *testing.T) {
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
local function paused() coroutine.yield() end
return { startup = function()
  local first = coroutine.create(paused)
  local ok = pcall(function() coroutine.create(paused) end)
  coroutine.close(first)
  local second = coroutine.create(paused)
  return ok, second ~= nil
end }
`}})
	runtime, err := program.NewRuntime(RuntimeOptions{Limits: ExecutionLimits{MaxCoroutines: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatalf("failed-create run = %v", err)
	}
}

func TestB8CompactCallDepthLimit(t *testing.T) {
	proto := compileB5Proto(t, `local function add(left, right) return left + right end return add(add(1, 2), add(3, 4))`)
	if proto.compact == nil {
		t.Fatal("fixture did not build compact program")
	}
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxCallDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, handled, err := runSlotExecutionWithController(proto, nil, controller)
	var limit *LimitError
	if !handled || !errors.As(err, &limit) || limit.Kind != LimitCallDepth {
		t.Fatalf("compact result = %t, %v", handled, err)
	}
}

func TestB8BorrowedRecordCallDepthLimit(t *testing.T) {
	proto, err := Compile(`local function loop(x) return loop(x + 1) end return loop(1)`)
	if err != nil {
		t.Fatal(err)
	}
	markFirstLocalCallBorrowHint(t, proto, true)
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxCallDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	thread.controller = controller
	_, err = thread.run(proto, nil, nil)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitCallDepth {
		t.Fatalf("borrowed result = %v", err)
	}
}

func TestB8MetamethodCallDepthLimit(t *testing.T) {
	proto := compileB5Proto(t, `
local mt = { __add = function(a, b) return a + b end }
local value = setmetatable({}, mt)
return value + 1
`)
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxCallDepth: 3})
	if err != nil {
		t.Fatal(err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	thread.controller = controller
	_, err = thread.run(proto, nil, nil)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitCallDepth {
		t.Fatalf("metamethod result = %v", err)
	}
}
