package ember

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestMachineCoroutineStatusNamesMatchOldVM(t *testing.T) {
	for _, pair := range []struct {
		machine machineCoroutineStatus
		old     vmCoroutineStatus
	}{
		{machineCoroutineSuspended, vmCoroutineSuspended},
		{machineCoroutineRunning, vmCoroutineRunning},
		{machineCoroutineNormal, vmCoroutineNormal},
		{machineCoroutineDead, vmCoroutineDead},
	} {
		if got := pair.machine.String(); got != string(pair.old) {
			t.Fatalf("Machine status %d=%q, old VM=%q", pair.machine, got, pair.old)
		}
	}
}

func TestMachineCoroutineIdentityAndTableKeysMatchVM(t *testing.T) {
	assertMachineOwnerDispatchMatchesVM(t, `
local first = coroutine.create(function() end)
local second = coroutine.create(function() end)
local keyed = {}
keyed[first] = 41
return first == first, first == second, keyed[first], keyed[second]
`, nil)
}

func TestMachineCoroutineProtectedLimitEscapesResume(t *testing.T) {
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`
local co = coroutine.create(function()
    local total = 0
    for i = 1, 1000 do total = total + i end
    return total
end)
return coroutine.resume(co)
`}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 25})
	if err != nil {
		t.Fatal(err)
	}
	err = owner.executeRoot(0, controller)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitInstructions {
		t.Fatalf("resume error = %v, want instruction LimitError", err)
	}
}

func TestMachineCoroutineProtectedRuntimeObjectLimitEscapesResume(t *testing.T) {
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`
local co = coroutine.create(function()
    local first = {}
    local second = {}
    return first, second
end)
return coroutine.resume(co)
`}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxRuntimeObjects: 1})
	if err != nil {
		t.Fatal(err)
	}
	err = owner.executeRoot(0, controller)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitRuntimeObjects {
		t.Fatalf("resume error = %v, want runtime-object LimitError", err)
	}
}

func TestMachineCoroutineProtectedCallDepthLimitEscapesBeforeResume(t *testing.T) {
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`
local co = coroutine.create(function() return 1 end)
return coroutine.resume(co)
`}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxCallDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	err = owner.executeRoot(0, controller)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitCallDepth {
		t.Fatalf("pre-resume error = %v, want call-depth LimitError", err)
	}
}

func TestMachineCoroutineDeadRecordsAreReusedAcrossCallbackRuns(t *testing.T) {
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`
return function()
    local co = coroutine.create(function() end)
    coroutine.resume(co)
    local holder = {[co] = true}
    local keep = function() return co end
    holder = nil
    keep = nil
    return 1
end
`}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	if err := owner.executeRoot(0, nil); err != nil {
		t.Fatal(err)
	}
	callback, err := owner.resultAt(0)
	if err != nil {
		t.Fatal(err)
	}
	for range 128 {
		if err := owner.executeClosure(callback, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	owner.coroutines.mu.Lock()
	records := len(owner.coroutines.arena.records)
	owner.coroutines.mu.Unlock()
	if records > 2 {
		t.Fatalf("coroutine record count = %d after repeated callback runs, want bounded reuse", records)
	}
}

func TestMachineCoroutineDeadReclamationHonorsReachabilityAndPublicPins(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	closure, err := owner.closures.createClosureStopped(0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	makeDead := func() (machineCoroutineHandle, slot) {
		handle, createErr := owner.coroutines.createStopped(machineCoroutineRoot{module: 0, proto: 0, closure: closure})
		if createErr != nil {
			t.Fatal(createErr)
		}
		owner.coroutines.mu.Lock()
		_, _, _, beginErr := owner.coroutines.arena.beginResumeStopped(handle)
		_, completeErr := owner.coroutines.arena.completeStopped(handle)
		owner.coroutines.mu.Unlock()
		if beginErr != nil || completeErr != nil {
			t.Fatalf("complete dead coroutine: begin=%v complete=%v", beginErr, completeErr)
		}
		value, packErr := slotPackHandle(slotTagCoroutine, handle.index, handle.generation)
		if packErr != nil {
			t.Fatal(packErr)
		}
		return handle, value
	}

	reachableHandle, reachableValue := makeDead()
	tableID, err := owner.tables.newTableStopped(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.tables.setStringStopped(tableID, owner.indexName, reachableValue); err != nil {
		t.Fatal(err)
	}
	tableValue, err := slotPackHandle(slotTagTable, uint32(tableID), 1)
	if err != nil {
		t.Fatal(err)
	}
	owner.registers = []slot{tableValue}
	if err := owner.reclaimDeadCoroutinesStopped(); err != nil {
		t.Fatal(err)
	}
	if status, err := owner.coroutines.status(reachableHandle); err != nil || status != machineCoroutineDead {
		t.Fatalf("reachable dead handle status=%v err=%v", status, err)
	}
	if err := owner.tables.setStringStopped(tableID, owner.indexName, slotNil); err != nil {
		t.Fatal(err)
	}
	owner.registers = nil
	if err := owner.reclaimDeadCoroutinesStopped(); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.coroutines.status(reachableHandle); !errors.Is(err, errMachineCoroutineIndex) {
		t.Fatalf("unreachable dead handle status error = %v, want released record", err)
	}

	capturedHandle, capturedValue := makeDead()
	keeper, err := owner.closures.createClosureStopped(0, 0, []machineCaptureDescriptor{{mode: machineCaptureByValue, value: capturedValue}})
	if err != nil {
		t.Fatal(err)
	}
	keeperValue, err := slotPackHandle(slotTagClosure, keeper.index, keeper.generation)
	if err != nil {
		t.Fatal(err)
	}
	owner.registers = []slot{keeperValue}
	if err := owner.reclaimDeadCoroutinesStopped(); err != nil {
		t.Fatal(err)
	}
	if status, err := owner.coroutines.status(capturedHandle); err != nil || status != machineCoroutineDead {
		t.Fatalf("closure-captured dead handle status=%v err=%v", status, err)
	}
	owner.registers = nil
	if err := owner.reclaimDeadCoroutinesStopped(); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.coroutines.status(capturedHandle); !errors.Is(err, errMachineCoroutineIndex) {
		t.Fatalf("unreachable closure capture status error = %v, want released record", err)
	}

	_, pinnedValue := makeDead()
	public, err := owner.coroutines.exportValueStopped(pinnedValue)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.reclaimDeadCoroutinesStopped(); err != nil {
		t.Fatal(err)
	}
	if roundTrip, err := owner.coroutines.importValueStopped(public); err != nil || roundTrip != pinnedValue {
		t.Fatalf("pinned public handle round trip=%#x err=%v", roundTrip, err)
	}
}

func TestMachineCoroutineControllerHandoffAndActions(t *testing.T) {
	var coroutines machineCoroutineController
	if err := coroutines.bindStopped(71, 0); err != nil {
		t.Fatal(err)
	}
	handle, err := coroutines.createStopped(machineCoroutineRoot{
		module: 1, proto: 2,
		closure: machineClosureHandle{owner: 71, index: 3, generation: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), machineCoroutineTestContextKey{}, "current")
	execution, err := newExecutionController(ctx, ExecutionLimits{MaxCallDepth: 3})
	if err != nil {
		t.Fatal(err)
	}
	action, err := coroutines.beginResumeStopped(machineCoroutineHandle{}, handle, execution, machineRunEffects{ctx: ctx}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if action.kind != machineCoroutineActionStart || action.callDepth != 1 || execution.callDepth != 1 {
		t.Fatalf("start action=%#v callDepth=%d", action, execution.callDepth)
	}
	if effects, err := coroutines.activeEffectsStopped(handle); err != nil || effects.ctx.Value(machineCoroutineTestContextKey{}) != "current" {
		t.Fatalf("active effects=%#v err=%v", effects, err)
	}
	snapshot := machineCoroutineSnapshot{
		frame:      machineCoroutineFrameState{module: 1, proto: 2, pc: 10, resumeRegister: 4, resumeCount: 1, callDepth: 1},
		registers:  []slot{slotBool(true)},
		numberBits: []uint64{0},
	}
	exit, err := coroutines.yieldStopped(handle, snapshot, []machineTransferValue{{value: slot(7), numberBits: 7, isNumber: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if exit.action.kind != machineCoroutineActionYield || len(exit.values) != 1 || exit.values[0].value != slot(7) || execution.callDepth != 0 {
		t.Fatalf("yield exit=%#v callDepth=%d", exit, execution.callDepth)
	}
	if _, err := coroutines.activeEffectsStopped(handle); !errors.Is(err, errMachineCoroutineNotRunning) {
		t.Fatalf("suspended coroutine retained active effects: %v", err)
	}

	action, err = coroutines.beginResumeStopped(machineCoroutineHandle{}, handle, execution, machineRunEffects{ctx: context.Background()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if action.kind != machineCoroutineActionResume || action.frame.pc != 10 || execution.callDepth != 1 {
		t.Fatalf("resume action=%#v callDepth=%d", action, execution.callDepth)
	}
	var restored machineCoroutineSnapshot
	if err := coroutines.takeSnapshotStopped(handle, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.frame.pc != 10 || len(restored.registers) != 1 {
		t.Fatalf("restored snapshot=%#v", restored)
	}
	exit, err = coroutines.returnStopped(handle, []machineTransferValue{{value: slot(9), numberBits: 9, isNumber: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if exit.action.kind != machineCoroutineActionReturn || execution.callDepth != 0 {
		t.Fatalf("return exit=%#v callDepth=%d", exit, execution.callDepth)
	}
	if status, err := coroutines.status(handle); err != nil || status != machineCoroutineDead {
		t.Fatalf("final status=%v err=%v", status, err)
	}
}

func TestMachineCoroutineControllerStatusIsRaceSafe(t *testing.T) {
	var coroutines machineCoroutineController
	if err := coroutines.bindStopped(91, 0); err != nil {
		t.Fatal(err)
	}
	handle, err := coroutines.createStopped(machineCoroutineRoot{
		proto: 1, closure: machineClosureHandle{owner: 91, index: 1, generation: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				if status, err := coroutines.status(handle); err != nil || status != machineCoroutineSuspended {
					t.Errorf("status=%v err=%v", status, err)
					return
				}
			}
		}()
	}
	wait.Wait()
	coroutines.close()
}

func TestMachineOwnerCoroutineYieldMatchesVM(t *testing.T) {
	assertMachineOwnerDispatchMatchesVM(t, `
local co = coroutine.create(function(limit)
    local total = 0
    for i = 1, limit do
        total = total + i
        if i % 10 == 0 then
            coroutine.yield(total)
        end
    end
    return total
end)
local total = 0
local ok, value = coroutine.resume(co, 45)
while coroutine.status(co) ~= "dead" do
    total = total + value
    ok, value = coroutine.resume(co)
end
return total + value
`, nil)
}

func TestMachineOwnerCoroutineResumeSemanticsMatchVM(t *testing.T) {
	tests := map[string]string{
		"resume arguments become yield results": `
local co = coroutine.create(function(seed)
    local left, right = coroutine.yield(seed, seed + 1)
    return left * 10 + right
end)
local ok1, first, second = coroutine.resume(co, 4)
local ok2, final = coroutine.resume(co, 7, 8)
return ok1, first, second, ok2, final, coroutine.status(co)
`,
		"fixed and open results": `
local first = coroutine.create(function()
    coroutine.yield(1, 2, 3)
end)
local ok1, one = coroutine.resume(first)
local second = coroutine.create(function()
    return 4, 5, 6
end)
local values = {coroutine.resume(second)}
return ok1, one, values[1], values[2], values[3], values[4]
`,
		"nested status transitions": `
local outer = nil
local inner = coroutine.create(function()
    return coroutine.status(outer)
end)
outer = coroutine.create(function()
    local before = coroutine.status(inner)
    local ok, parent = coroutine.resume(inner)
    return before, ok, parent, coroutine.status(inner)
end)
local ok, before, innerOK, parent, after = coroutine.resume(outer)
return ok, before, innerOK, parent, after, coroutine.status(outer)
`,
		"error and dead resume": `
local co = coroutine.create(function()
    error("boom")
end)
local ok1, message1 = coroutine.resume(co)
local ok2, message2 = coroutine.resume(co)
return ok1, type(message1), ok2, message2, coroutine.status(co)
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			assertMachineOwnerDispatchMatchesVM(t, source, nil)
		})
	}
}

func TestMachineRuntimePersistsCoroutineAcrossPublicHookLifecycle(t *testing.T) {
	t.Setenv(runtimeEngineEnvironment, "machine")
	entry := LogicalModule("machine/coroutine")
	program, _, err := LoadProgram(context.Background(), machineRuntimeTestLoader{
		entry.String(): `
local co = nil
return {
    startup = function()
        co = coroutine.create(function(seed)
            local resumed = coroutine.yield(seed + 1)
            return resumed + 2
        end)
        local ok, value = coroutine.resume(co, 40)
        observe(ok, value, coroutine.status(co))
    end,
    update = function(value)
        local ok, result = coroutine.resume(co, value)
        observe(ok, result, coroutine.status(co))
    end,
}
`,
	}, ProgramOptions{Entrypoints: []Entrypoint{{Name: "main", Module: entry}}, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	var observed []string
	runtime, err := program.NewRuntime(RuntimeOptions{
		Limits: ExecutionLimits{MaxCoroutines: 1},
		Host: RuntimeHostFunc(func(context.Context, HostCall) (map[string]Value, error) {
			return map[string]Value{"observe": ContextHostFuncValue(func(_ context.Context, args []Value) ([]Value, error) {
				observed = append(observed, valuesDiagnostic(args))
				return nil, nil
			})}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	execution := runtime.execution.(*machineRuntimeExecution)
	if _, err := runtime.RunHook(context.Background(), "startup"); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.RunHook(context.Background(), "update", NumberValue(7)); err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(observed), `[[bool(true), number(41), string("suspended")] [bool(true), number(9), string("dead")]]`; got != want {
		t.Fatalf("observed = %s, want %s", got, want)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if !execution.owner.coroutines.arena.closed || execution.owner.coroutines.arena.live != 0 {
		t.Fatalf("coroutine arena after close = closed:%t live:%d", execution.owner.coroutines.arena.closed, execution.owner.coroutines.arena.live)
	}
}

func TestMachineRuntimeAllowsNonReturningCoroutinePrototype(t *testing.T) {
	entry := LogicalModule("machine/non-returning-coroutine")
	loader := machineRuntimeTestLoader{
		entry.String(): `
local co = nil
return {
    startup = function()
        co = coroutine.create(function()
            while true do
                coroutine.yield(1)
            end
        end)
        return coroutine.resume(co)
    end,
    update = function()
        local ok, value = coroutine.resume(co)
        return ok, value, coroutine.status(co)
    end,
}
`,
	}

	for _, engine := range []string{"vm", "machine"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv(runtimeEngineEnvironment, engine)
			program, _, err := LoadProgram(context.Background(), loader, ProgramOptions{
				Entrypoints: []Entrypoint{{Name: "main", Module: entry}},
				Parallelism: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := program.NewRuntime(RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			startup, err := runtime.Invoke(context.Background(), Invocation{Module: entry, Export: "startup"})
			if err != nil {
				t.Fatal(err)
			}
			if got, want := valuesDiagnostic(startup), `[bool(true), number(1)]`; got != want {
				t.Fatalf("startup results = %s, want %s", got, want)
			}
			for index := 0; index < 2; index++ {
				values, err := runtime.Invoke(context.Background(), Invocation{Module: entry, Export: "update"})
				if err != nil {
					t.Fatal(err)
				}
				if got, want := valuesDiagnostic(values), `[bool(true), number(1), string("suspended")]`; got != want {
					t.Fatalf("update %d results = %s, want %s", index, got, want)
				}
			}
		})
	}
}

func TestMachineCoroutineLimitsReleaseAndNestedDepthBalances(t *testing.T) {
	t.Run("completed coroutine releases live limit", func(t *testing.T) {
		owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`
local first = coroutine.create(function() return 1 end)
local ok1, firstValue = coroutine.resume(first)
local second = coroutine.create(function() return 2 end)
local ok2, secondValue = coroutine.resume(second)
return ok1, firstValue, ok2, secondValue
`}))
		if err != nil {
			t.Fatal(err)
		}
		defer owner.close()
		owner.coroutines.arena.limit = 1
		if err := owner.executeRoot(0, nil); err != nil {
			t.Fatal(err)
		}
		if owner.coroutines.arena.live != 0 {
			t.Fatalf("live coroutines = %d, want 0", owner.coroutines.arena.live)
		}
	})

	t.Run("nested suspended depth is charged once", func(t *testing.T) {
		owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`
local function inner()
    local value = coroutine.yield(1)
    return value
end
local co = coroutine.create(function()
    return inner()
end)
local ok1, first = coroutine.resume(co)
local ok2, second = coroutine.resume(co, 9)
return ok1, first, ok2, second
`}))
		if err != nil {
			t.Fatal(err)
		}
		defer owner.close()
		controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxCallDepth: 3})
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.executeRoot(0, controller); err != nil {
			t.Fatal(err)
		}
		if controller.callDepth != 0 {
			t.Fatalf("call depth after nested resume = %d, want 0", controller.callDepth)
		}
	})

	t.Run("live limit rejects second suspended coroutine", func(t *testing.T) {
		owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`
local first = coroutine.create(function() coroutine.yield(1) end)
local second = coroutine.create(function() coroutine.yield(2) end)
return first, second
`}))
		if err != nil {
			t.Fatal(err)
		}
		defer owner.close()
		owner.coroutines.arena.limit = 1
		err = owner.executeRoot(0, nil)
		var limit *LimitError
		if !errors.As(err, &limit) || limit.Kind != LimitCoroutines || limit.Limit != 1 || limit.Used != 2 {
			t.Fatalf("second coroutine error = %v, want coroutine limit used 2", err)
		}
	})
}

func TestMachineCoroutineBoundaryValuesPreserveBoxedNumberPayload(t *testing.T) {
	payload := math.Float64frombits(0x7ff8000000000042)
	machine := scalarMachine{
		registers:  make([]slot, 1),
		numberBits: make([]uint64, 1),
	}
	machine.setNumber(0, payload)
	if slotTagOf(machine.registers[0]) != slotTagBoxedNumber {
		t.Fatalf("payload slot tag = %d, want boxed number", slotTagOf(machine.registers[0]))
	}

	values, err := captureMachineCoroutineValuesStopped(&machine, nil, machine.registers)
	if err != nil {
		t.Fatal(err)
	}
	machine.registers[0] = slotNil
	machine.numberBits[0] = 0
	if err := applyMachineCoroutineValuesStopped(&machine, 0, 1, values); err != nil {
		t.Fatal(err)
	}
	got, err := machine.number(machine.registers[0])
	if err != nil {
		t.Fatal(err)
	}
	if math.Float64bits(got) != math.Float64bits(payload) {
		t.Fatalf("restored payload bits = %x, want %x", math.Float64bits(got), math.Float64bits(payload))
	}
}

func TestMachineCoroutineCaptureCommitsExecutionWindow(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	machine := &owner.scalarMachine
	machine.image = owner.image.modules[0].code
	machine.activeProto = 0
	machine.registers = []slot{slotBool(true)}
	machine.numberBits = make([]uint64, 1)
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 2})
	if err != nil {
		t.Fatal(err)
	}
	machine.window = newExecutionWindow(controller)
	if err := machine.window.stepInstruction(); err != nil {
		t.Fatal(err)
	}
	if controller.remaining != 2 {
		t.Fatalf("controller remaining before capture = %d, want unflushed 2", controller.remaining)
	}

	var snapshot machineCoroutineSnapshot
	if err := captureMachineCoroutineStopped(machine, machineCoroutineFrameState{pc: 1}, &snapshot); err != nil {
		t.Fatal(err)
	}
	if controller.remaining != 1 {
		t.Fatalf("controller remaining after capture = %d, want committed 1", controller.remaining)
	}
	if machine.window.controller != nil {
		t.Fatal("captured Machine retained execution controller")
	}
}

func TestMachineCoroutineCaptureFailureLeavesOpenCellsAttached(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	machine := &owner.scalarMachine
	machine.image = owner.image.modules[0].code
	machine.activeProto = 0
	machine.registers = []slot{slotBool(true), slotBool(false)}
	machine.numberBits = make([]uint64, 2)
	first, err := machine.closures.openCellStopped(0, slotNil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := machine.closures.openCellStopped(1, slotNil)
	if err != nil {
		t.Fatal(err)
	}
	machine.closures.cells[second.index-1].live = 0
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 2})
	if err != nil {
		t.Fatal(err)
	}
	machine.window = newExecutionWindow(controller)
	if err := machine.window.stepInstruction(); err != nil {
		t.Fatal(err)
	}

	snapshot := machineCoroutineSnapshot{
		frame:     machineCoroutineFrameState{pc: 99},
		registers: []slot{slotBool(false)},
	}
	if err := captureMachineCoroutineStopped(machine, machineCoroutineFrameState{pc: 1}, &snapshot); !errors.Is(err, errMachineCoroutineSnapshot) {
		t.Fatalf("capture error = %v, want invalid snapshot", err)
	}
	firstRecord := machine.closures.cells[first.index-1]
	if firstRecord.openRegister != 1 || firstRecord.value != slotNil {
		t.Fatalf("first cell after failed capture = %#v, want original open cell", firstRecord)
	}
	if len(machine.closures.openCells) != 2 {
		t.Fatalf("open cells after failed capture = %d, want 2", len(machine.closures.openCells))
	}
	if machine.window.controller != controller || controller.remaining != 2 {
		t.Fatalf("window after failed capture = %#v remaining=%d, want attached and uncommitted", machine.window, controller.remaining)
	}
	if snapshot.frame.pc != 99 || len(snapshot.registers) != 1 || snapshot.registers[0] != slotBool(false) {
		t.Fatalf("destination changed after failed capture: %#v", snapshot)
	}
}

func TestMachineCoroutineRestoreFailureLeavesMachineStateUntouched(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	machine := &owner.scalarMachine
	machine.image = owner.image.modules[0].code
	machine.activeProto = 0
	machine.registers = []slot{slotBool(false)}
	machine.numberBits = []uint64{99}
	first, err := machine.closures.newCellStopped(slotBool(true))
	if err != nil {
		t.Fatal(err)
	}
	second, err := machine.closures.newCellStopped(slotBool(false))
	if err != nil {
		t.Fatal(err)
	}
	machine.closures.cells[second.index-1].live = 0
	snapshot := machineCoroutineSnapshot{
		frame:      machineCoroutineFrameState{proto: 0, pc: 1, callDepth: 1},
		registers:  []slot{slotNil, slotNil},
		numberBits: []uint64{1, 2},
		openCells: []machineCoroutineOpenCell{
			{register: 1, cell: machineCellID(first.index), generation: first.generation},
			{register: 2, cell: machineCellID(second.index), generation: second.generation},
		},
	}

	if err := restoreMachineCoroutineStopped(machine, snapshot); !errors.Is(err, errMachineCoroutineSnapshot) {
		t.Fatalf("restore error = %v, want invalid snapshot", err)
	}
	if len(machine.registers) != 1 || machine.registers[0] != slotBool(false) || len(machine.numberBits) != 1 || machine.numberBits[0] != 99 {
		t.Fatalf("Machine state changed after failed restore: registers=%#v numberBits=%#v", machine.registers, machine.numberBits)
	}
	if machine.closures.cells[first.index-1].openRegister != 0 || len(machine.closures.openCells) != 0 {
		t.Fatal("failed restore partially reopened the first cell")
	}
}

func TestMachineCoroutineSnapshotRetainsSemanticResume(t *testing.T) {
	owner := newMachineIntrinsicTestOwner(t)
	defer owner.close()
	machine := &owner.scalarMachine
	machine.image = owner.image.modules[0].code
	machine.activeProto = 0
	machine.registers = []slot{slotBool(true)}
	machine.numberBits = make([]uint64, 1)
	want := machineSemanticResume{
		kind:         machineResumeSetStringFieldIndex,
		temporaryReg: 3,
		stackLength:  5,
	}
	machine.resume = want

	var snapshot machineCoroutineSnapshot
	if err := captureMachineCoroutineStopped(machine, machineCoroutineFrameState{pc: 1}, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.frame.resume != want {
		t.Fatalf("captured semantic resume = %#v, want %#v", snapshot.frame.resume, want)
	}
	machine.resume = machineSemanticResume{}
	if err := restoreMachineCoroutineStopped(machine, snapshot); err != nil {
		t.Fatal(err)
	}
	if machine.resume != want {
		t.Fatalf("restored semantic resume = %#v, want %#v", machine.resume, want)
	}
}

type machineCoroutineTestContextKey struct{}
