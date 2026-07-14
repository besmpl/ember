package ember

import (
	"context"
	"errors"
	"testing"
)

func h4SoakRounds() int {
	if allocationInstrumentedTest() {
		return 32
	}
	return 256
}

func h4CancellationRounds() int {
	if allocationInstrumentedTest() {
		return 8
	}
	return 48
}

func TestH4SoakPersistentHooks(t *testing.T) {
	observed := 0
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
local count = 0
return {
    startup = function()
        count = count + 1
        observe(count)
    end,
    update = function()
        count = count + 1
        observe(count)
    end,
}
`}})
	runtime, err := program.NewRuntime(RuntimeOptions{Host: RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
		return map[string]Value{
			"observe": HostFuncValue(func(args []Value) ([]Value, error) {
				if len(args) != 1 || args[0] != NumberValue(float64(observed+1)) {
					return nil, errors.New("persistent hook counter changed unexpectedly")
				}
				observed++
				return nil, nil
			}),
		}, nil
	})})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	defer runtime.Close()
	rounds := h4SoakRounds()
	for i := 0; i < rounds; i++ {
		hook := "update"
		if i == 0 {
			hook = "startup"
		}
		if _, err := runtime.RunHook(context.Background(), hook); err != nil {
			t.Fatalf("RunHook(%q) round %d: %v", hook, i, err)
		}
	}
	if observed != rounds {
		t.Fatalf("persistent hook observations = %d, want %d", observed, rounds)
	}
}

func TestH4SoakCallbackCaptureCallClose(t *testing.T) {
	var callback Callback
	hasCallback := false
	captured := 0
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
return { startup = function() capture(function(value) return value + 1 end) end }
`}})
	runtime, err := program.NewRuntime(RuntimeOptions{Host: RuntimeHostFunc(func(_ context.Context, call HostCall) (map[string]Value, error) {
		if call.Hook == "" {
			return nil, nil
		}
		return map[string]Value{
			"capture": ContextHostFuncValue(func(ctx context.Context, args []Value) ([]Value, error) {
				if hasCallback {
					if err := callback.Close(); err != nil {
						return nil, err
					}
				}
				var err error
				callback, err = CaptureCallback(ctx, args[0])
				if err == nil {
					hasCallback = true
					captured++
				}
				return nil, err
			}),
		}, nil
	})})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	defer runtime.Close()
	rounds := h4SoakRounds()
	for i := 0; i < rounds; i++ {
		if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
			t.Fatalf("capture round %d: %v", i, err)
		}
		values, err := callback.Call(context.Background(), NumberValue(float64(i)))
		if err != nil {
			t.Fatalf("callback call round %d: %v", i, err)
		}
		if len(values) != 1 || values[0] != NumberValue(float64(i+1)) {
			t.Fatalf("callback result round %d = %#v, want %d", i, values, i+1)
		}
	}
	if err := callback.Close(); err != nil {
		t.Fatalf("final callback close: %v", err)
	}
	if _, err := callback.Call(context.Background()); err == nil {
		t.Fatal("callback call after close succeeded")
	}
	if captured != rounds {
		t.Fatalf("callbacks captured = %d, want %d", captured, rounds)
	}
}

func TestH4SoakCoroutineCreateYieldResumeClose(t *testing.T) {
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
return { startup = function()
    local co = coroutine.create(function(value)
        coroutine.yield(value)
        return value + 1
    end)
    local ok, value = coroutine.resume(co, 41)
    if not ok or value ~= 41 then error("unexpected coroutine yield") end
    local closed = coroutine.close(co)
    if not closed then error("coroutine close failed") end
end }
`}})
	runtime, err := program.NewRuntime(RuntimeOptions{Limits: ExecutionLimits{MaxCoroutines: 1}})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	defer runtime.Close()
	rounds := h4SoakRounds()
	for i := 0; i < rounds; i++ {
		if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
			t.Fatalf("coroutine round %d: %v", i, err)
		}
	}
}

func TestH4SoakModuleLoadingAndCachedExports(t *testing.T) {
	loads := 0
	bumps := 0
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
local shared = require("child")
return {
    startup = function()
        observe(shared.bump())
    end,
    update = function()
        observe(shared.bump())
    end,
}
`}}, b3ModuleSource{name: "child", source: `
observe("load")
local count = 0
return {
    bump = function()
        count = count + 1
        return count
    end,
}
`})
	runtime, err := program.NewRuntime(RuntimeOptions{Host: RuntimeHostFunc(func(_ context.Context, call HostCall) (map[string]Value, error) {
		return map[string]Value{
			"observe": HostFuncValue(func(args []Value) ([]Value, error) {
				if call.Hook == "" {
					if len(args) == 1 {
						if text, ok := args[0].String(); ok && text == "load" {
							loads++
						}
					}
					return nil, nil
				}
				if len(args) != 1 || args[0] != NumberValue(float64(bumps+1)) {
					return nil, errors.New("cached module export counter changed unexpectedly")
				}
				bumps++
				return nil, nil
			}),
		}, nil
	})})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	defer runtime.Close()
	rounds := h4SoakRounds()
	for i := 0; i < rounds; i++ {
		hook := "update"
		if i == 0 {
			hook = "startup"
		}
		if _, err := runtime.RunHook(context.Background(), hook); err != nil {
			t.Fatalf("module hook %q round %d: %v", hook, i, err)
		}
	}
	if loads != 1 {
		t.Fatalf("child module loads = %d, want 1", loads)
	}
	if bumps != rounds {
		t.Fatalf("cached export calls = %d, want %d", bumps, rounds)
	}
}

func TestH4SoakCancellationUnderLoad(t *testing.T) {
	rounds := h4CancellationRounds()
	for round := 0; round < rounds; round++ {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
return { startup = function() while true do tick() end end }
`}})
		runtime, err := program.NewRuntime(RuntimeOptions{Host: RuntimeHostFunc(func(_ context.Context, call HostCall) (map[string]Value, error) {
			if call.Hook == "" {
				return nil, nil
			}
			return map[string]Value{"tick": ContextHostFuncValue(func(context.Context, []Value) ([]Value, error) {
				calls++
				if calls == 24 {
					cancel()
				}
				return nil, nil
			})}, nil
		})})
		if err != nil {
			cancel()
			t.Fatalf("NewRuntime round %d: %v", round, err)
		}
		_, runErr := runtime.RunHook(ctx, "startup")
		cancel()
		if !errors.Is(runErr, context.Canceled) {
			t.Fatalf("cancellation round %d error = %v, want context.Canceled", round, runErr)
		}
		if err := runtime.Close(); err != nil {
			t.Fatalf("Close round %d: %v", round, err)
		}
	}
}

func TestH4SoakRepeatedLimitFailuresRecover(t *testing.T) {
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
return {
    startup = function()
        local total = 0
        for i = 1, 64 do total = total + i end
    end,
    healthy = function() return 7 end,
}
`}})
	runtime, err := program.NewRuntime(RuntimeOptions{Limits: ExecutionLimits{MaxInstructions: 48}})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	defer runtime.Close()
	rounds := h4SoakRounds()
	for i := 0; i < rounds; i++ {
		if _, err := runtime.RunHook(context.Background(), "startup"); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("limit failure round %d = %v, want ErrLimitExceeded", i, err)
		}
	}
	for i := 0; i < rounds; i++ {
		report, err := runtime.RunHook(context.Background(), "healthy")
		if err != nil {
			t.Fatalf("healthy call round %d: %v", i, err)
		}
		if len(report.Calls) != 1 || !report.Calls[0].Called {
			t.Fatalf("healthy report round %d = %#v", i, report)
		}
	}
}

func TestH4SoakTableCreationMutationCollection(t *testing.T) {
	var boundaryTable *Table
	program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: `
return { startup = function()
    local scriptTable = {}
    local hostTable = makeTable()
    for i = 1, 24 do
        scriptTable[i] = i
        scriptTable["field" .. i] = i
        hostTable[i] = i
        hostTable["field" .. i] = i
    end
    if scriptTable[24] ~= 24 or hostTable[24] ~= 24 then error("table mutation failed") end
end }
`}})
	runtime, err := program.NewRuntime(RuntimeOptions{Host: RuntimeHostFunc(func(_ context.Context, call HostCall) (map[string]Value, error) {
		if call.Hook == "" {
			return nil, nil
		}
		return map[string]Value{
			"makeTable": HostFuncValue(func([]Value) ([]Value, error) {
				boundaryTable = NewTable()
				return []Value{TableValue(boundaryTable)}, nil
			}),
		}, nil
	})})
	if err != nil {
		t.Fatalf("NewRuntime returned error: %v", err)
	}
	defer runtime.Close()
	var reclaimed uint64
	rounds := h4SoakRounds()
	for i := 0; i < rounds; i++ {
		if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
			t.Fatalf("table round %d: %v", i, err)
		}
		if boundaryTable == nil {
			t.Fatalf("table round %d created no host-boundary table", i)
		}
		value, err := boundaryTable.Get(NumberValue(24))
		if err != nil || value != NumberValue(24) {
			t.Fatalf("table mutation round %d value = %v, %v; want 24", i, value, err)
		}
		pin, err := runtime.owner.pin(TableValue(boundaryTable))
		if err != nil {
			t.Fatalf("pin mutated table round %d: %v", i, err)
		}
		handle := pin.handle
		pin.release()
		stats, err := runtime.collect()
		if err != nil {
			t.Fatalf("collect round %d: %v", i, err)
		}
		if stats.reclaimed == 0 {
			t.Fatalf("collect round %d reclaimed no table handles", i)
		}
		if err := runtime.owner.heap.validateSlot(handle); err == nil {
			t.Fatalf("collect round %d retained the unreachable boundary table", i)
		}
		reclaimed += stats.reclaimed
		boundaryTable = nil
	}
	if reclaimed == 0 {
		t.Fatal("table soak reclaimed no heap entries")
	}
}

func TestH4SoakRuntimeCloseWithAndWithoutPins(t *testing.T) {
	closeRounds := 12
	if allocationInstrumentedTest() {
		closeRounds = 4
	}
	for _, withPin := range []bool{false, true} {
		t.Run(map[bool]string{false: "without-pin", true: "with-pin"}[withPin], func(t *testing.T) {
			for round := 0; round < closeRounds; round++ {
				program := newB3Program(t, []b3EntrypointSource{{name: "main", path: "main", source: "return {}"}})
				runtime, err := program.NewRuntime(RuntimeOptions{})
				if err != nil {
					t.Fatalf("NewRuntime round %d: %v", round, err)
				}
				var pin *runtimePin
				if withPin {
					pin, err = runtime.owner.pin(TableValue(NewTable()))
					if err != nil {
						t.Fatalf("pin round %d: %v", round, err)
					}
				}
				if err := runtime.Close(); err != nil {
					t.Fatalf("Close round %d: %v", round, err)
				}
				if err := runtime.Close(); err != nil {
					t.Fatalf("repeated Close round %d: %v", round, err)
				}
				if pin != nil {
					if _, err := pin.value(); err != nil {
						t.Fatalf("pinned value after Close round %d: %v", round, err)
					}
					pin.release()
				}
			}
		})
	}
}
