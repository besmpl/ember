package ember

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMachineOwnerDispatchMatchesVMForP2Operations(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{
		{
			name: "numeric string coercion",
			source: `local total = 0
for i = "1", "3", "1" do
    total = total + i
end
return "40" + 2, -"3", "10" < "2", total`,
		},
		{
			name: "generated strings",
			source: `local value = 12
return tostring(value), "a" .. 1 .. "b", tostring(true), (1 .. 2)`,
		},
		{
			name: "fused string field index and iteration",
			source: `local root = {child = {}}
local key = "answer"
root.child[key] = 7
local value = root.child[key]
local arrayTotal = 0
for _, item in {10, 20, 30} do
    arrayTotal = arrayTotal + item
end
local fieldTotal = 0
for _, item in {left = 2, right = 3} do
    fieldTotal = fieldTotal + item
end
return value, arrayTotal, fieldTotal`,
		},
		{
			name: "guarded intrinsics",
			source: `local values = {3, 1}
table.insert(values, 2, 2)
local removed = table.remove(values, 1)
local function count(...)
    return select("#", ...)
end
local selected, trailing = select(2, 10, 20, 30)
return math.min(9, 4, 7), rawlen("abc"), rawlen(values), removed, values[1], values[2], count(1, nil, 3), selected, trailing`,
		},
		{
			name: "metatable index",
			source: `local fallback = {hp = 7, shield = 3}
local player = setmetatable({shield = 5}, {__index = fallback})
local total = 0
for i = 1, 90 do
    total = total + player.hp + player.shield
end
return total`,
		},
		{
			name: "method calls",
			source: `local counter = {value = 0}
function counter:add(amount)
    self.value = self.value + amount
    return self.value
end
local total = 0
for i = 1, 70 do
    total = total + counter:add(i % 5)
end
return total`,
		},
		{
			name: "prototype fallback",
			source: `local prototype = {hp = 20, mana = 5, armor = 2}
local misses = 0
local mt = {
    __index = function(_, key)
        misses = misses + 1
        if key == "power" then return prototype.hp + prototype.mana end
        return prototype[key] or 0
    end,
}
local actors = {
    setmetatable({hp = 80}, mt),
    setmetatable({mana = 15, armor = 4}, mt),
    setmetatable({hp = 45, power = 9}, mt),
}
local total = 0
for tick = 1, 80 do
    for _, actor in actors do
        local value = actor.hp + actor.mana + actor.power
        if actor.armor > 3 then actor.hp = actor.hp + tick % 3 - actor.armor
        else actor.mana = actor.mana + tick % 4 end
        total = total + value + actor.hp + actor.mana
    end
end
return total + misses`,
		},
		{
			name: "dirty metatable writes",
			source: `local dirty = {}
local backing = {hp = 100, mana = 30, xp = 0, gold = 5, flags = 1}
local tracked = setmetatable({}, {
    __index = function(_, key) return backing[key] or 0 end,
    __newindex = function(_, key, value)
        dirty[key] = (dirty[key] or 0) + 1
        backing[key] = value
    end,
})
local keys = {"hp", "mana", "xp", "gold", "flags"}
local total = 0
for tick = 1, 100 do
    local key = keys[tick % rawlen(keys) + 1]
    tracked[key] = tracked[key] + tick % 9
    if tick % 7 == 0 then tracked[key] = tracked[key] - tracked.hp % 3 end
    total = total + tracked[key] + dirty[key]
end
return total + tracked.hp + tracked.mana + tracked.xp + tracked.gold + tracked.flags`,
		},
		{
			name: "protected getmetatable",
			source: `local object = {}
setmetatable(object, {__metatable = "locked"})
return getmetatable(object)`,
		},
		{
			name: "protected setmetatable error",
			source: `local object = {}
setmetatable(object, {__metatable = "locked"})
return setmetatable(object, {})`,
		},
		{
			name: "fused callable index read",
			source: `local child = {answer = 42}
local root = setmetatable({}, {__index = function(_, key)
    if key == "child" then return child end
end})
local key = "answer"
return root.child[key]`,
		},
		{
			name: "fused callable index write",
			source: `local child = {}
local root = setmetatable({}, {__index = function(_, key)
    if key == "child" then return child end
end})
local key = "answer"
root.child[key] = 42
return child.answer`,
		},
		{
			name: "nested table call",
			source: `local first = {}
local second = {}
setmetatable(first, {__call = second})
setmetatable(second, {__call = function(secondArg, firstArg, amount)
    if secondArg == second and firstArg == first then return amount + 1 end
    return -1
end})
return first(41)`,
		},
		{
			name: "cyclic table call error",
			source: `local object = {}
setmetatable(object, {__call = object})
return object()`,
		},
		{
			name: "callable iterator",
			source: `local object = setmetatable({}, {__iter = function()
    return next, {left = 2, right = 3}, nil
end})
local total = 0
for _, value in object do total = total + value end
return total`,
		},
		{
			name: "dirty metatable protection write",
			source: `local metatable = {}
local object = setmetatable({}, metatable)
metatable.__metatable = "locked"
return getmetatable(object)`,
		},
		{
			name: "clear metatable",
			source: `local object = setmetatable({}, {__index = {answer = 42}})
setmetatable(object, nil)
return getmetatable(object), object.answer`,
		},
		{
			name: "callable method lookup",
			source: `local object = setmetatable({base = 40}, {__index = function(_, key)
    if key == "add" then
        return function(self, amount) return self.base + amount end
    end
end})
return object:add(2)`,
		},
		{
			name: "table method call",
			source: `local handler = {}
setmetatable(handler, {__call = function(callable, receiver, amount)
    if callable == handler then return receiver.base + amount end
    return -1
end})
local object = {base = 40, add = handler}
return object:add(2)`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertMachineOwnerDispatchMatchesVM(t, test.source, nil)
		})
	}
}

func TestMachineOwnerFastCallGuardFallsBackAndBaseSnapshotRestores(t *testing.T) {
	const source = `return math.min(9, 2)`
	override := NewTable()
	if err := override.Set(StringValue("min"), HostFuncValue(func([]Value) ([]Value, error) {
		return []Value{NumberValue(99)}, nil
	})); err != nil {
		t.Fatal(err)
	}
	assertMachineOwnerDispatchMatchesVM(t, source, map[string]Value{"math": TableValue(override)})

	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{source}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	base := append([]slot(nil), owner.baseGlobals...)
	if err := owner.importGlobalsStopped(map[string]Value{"math": TableValue(override)}); err != nil {
		t.Fatal(err)
	}
	if err := owner.importGlobalsStopped(nil); err != nil {
		t.Fatal(err)
	}
	if err := owner.executeRoot(0, nil); err != nil {
		t.Fatal(err)
	}
	got, err := owner.exportResults()
	if err != nil {
		t.Fatal(err)
	}
	want, err := executeProto(context.Background(), proto, nil, executeOptions{instrumented: true})
	if err != nil {
		t.Fatal(err)
	}
	if !valuesSliceEquivalent(want, got) {
		t.Fatalf("restored base values differ: VM=%s Machine=%s", valuesDiagnostic(want), valuesDiagnostic(got))
	}
	for dense := range base {
		if owner.basePresent[dense] != 0 && owner.globals.values[dense] != base[dense] {
			t.Fatalf("global %d did not restore its persistent base slot", dense)
		}
	}
}

func TestMachineOwnerHostMethodReceivesReceiver(t *testing.T) {
	object := NewTable()
	method := HostFuncValue(func(args []Value) ([]Value, error) {
		if len(args) != 2 || args[0].Kind() != TableKind {
			return nil, errors.New("host method did not receive its table receiver")
		}
		amount, ok := args[1].Number()
		if !ok {
			return nil, errors.New("host method amount is not a number")
		}
		return []Value{NumberValue(amount + 37)}, nil
	})
	if err := object.Set(StringValue("score"), method); err != nil {
		t.Fatal(err)
	}
	assertMachineOwnerDispatchMatchesVM(t, `return object:score(5)`, map[string]Value{"object": TableValue(object)})
}

func TestMachineOwnerGeneratedStringsChargeOnlyNewBytes(t *testing.T) {
	const source = `return tostring(12345), tostring(12345)`
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{source}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxGeneratedStringBytes: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.executeRoot(0, controller); err != nil {
		t.Fatalf("reused generated string charged twice: %v", err)
	}

	limited, err := newMachineOwner(machineOwnerProgramImage(t, []string{`local function join(value)
    return "a" .. value
end
return join(12345)`}))
	if err != nil {
		t.Fatal(err)
	}
	defer limited.close()
	controller, err = newExecutionController(context.Background(), ExecutionLimits{MaxGeneratedStringBytes: 5})
	if err != nil {
		t.Fatal(err)
	}
	err = limited.executeRoot(0, controller)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitGeneratedStringBytes || limit.Used != 6 {
		t.Fatalf("concat limit error = %v, want generated-string limit used 6", err)
	}
}

func TestMachineOwnerGeneratedStringProvenanceResetsPerInvocation(t *testing.T) {
	const source = `local source = "12345"
return tostring(12345), source`
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{source}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	for run := 0; run < 2; run++ {
		controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxGeneratedStringBytes: 5})
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.executeRoot(0, controller); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if controller.generatedStringBytes != 5 {
			t.Fatalf("run %d generated bytes = %d, want 5 despite the source/prior-run intern", run, controller.generatedStringBytes)
		}
	}
}

func TestMachineOwnerOrdinaryTableSetsEnforceEntryLimit(t *testing.T) {
	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{`local value = {}
value.first = 1
value[2] = 2
return value`}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxTableEntriesPerTable: 1})
	if err != nil {
		t.Fatal(err)
	}
	err = owner.executeRoot(0, controller)
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Kind != LimitTableEntriesPerTable || limit.Limit != 1 || limit.Used != 2 {
		t.Fatalf("ordinary table set limit error = %v, want table-entry limit used 2", err)
	}
}

func TestMachineImageAcceptsCoreCoroutineGlobal(t *testing.T) {
	proto, err := Compile(`local thread = coroutine.create(function() end) return coroutine.resume(thread)`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if !image.eligible {
		t.Fatalf("core coroutine source is ineligible: %s", image.rejectReason)
	}
}

func TestMachineImageRejectsUnprovenCoreCoroutineAccess(t *testing.T) {
	for name, source := range map[string]string{
		"table alias": `
local c = coroutine
local co = c.create(function() end)
return c.status(co)
`,
		"dynamic field": `
local name = "create"
return coroutine[name](function() end)
`,
		"unknown field":   `return coroutine.wrap(function() end)`,
		"table escape":    `return coroutine`,
		"function escape": `return coroutine.create`,
	} {
		t.Run(name, func(t *testing.T) {
			proto, err := Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := proto.preparedCodeImage()
			if err != nil {
				t.Fatal(err)
			}
			if image.eligible {
				t.Fatalf("unproven coroutine access remained Machine-eligible:\n%s", strings.Join(disassembleProto(proto), "\n"))
			}
		})
	}
}

func TestMachineImageRejectsCoroutineFastCallAfterGlobalRebinding(t *testing.T) {
	proto, err := Compile(`
coroutine = {resume = function() return true, 42 end}
return coroutine.resume()
`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if image.eligible {
		t.Fatalf("rebound coroutine FAST_CALL remained Machine-eligible:\n%s", strings.Join(disassembleProto(proto), "\n"))
	}
}

func assertMachineOwnerDispatchMatchesVM(t *testing.T, source string, globals map[string]Value) {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if !image.eligible {
		t.Fatalf("prepared image is ineligible: %s", image.rejectReason)
	}

	const instructionBudget = 100_000
	vmController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: instructionBudget})
	if err != nil {
		t.Fatal(err)
	}
	var env *globalEnv
	if globals != nil {
		env = runtimeGlobals(globals)
	}
	want, vmErr := executeProto(context.Background(), proto, env, executeOptions{
		controller:   vmController,
		instrumented: true,
	})

	owner, err := newMachineOwner(machineOwnerProgramImage(t, []string{source}))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	if err := owner.importGlobalsStopped(globals); err != nil {
		t.Fatal(err)
	}
	machineController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: instructionBudget})
	if err != nil {
		t.Fatal(err)
	}
	machineErr := owner.executeRoot(0, machineController)
	got, exportErr := owner.exportResults()
	if machineErr == nil {
		machineErr = exportErr
	}
	if difference := errorsEquivalent(vmErr, machineErr); difference != "" {
		t.Fatalf("errors differ: %s; VM=%v Machine=%v", difference, vmErr, machineErr)
	}
	if !valuesSliceEquivalent(want, got) {
		t.Fatalf("values differ: VM=%s Machine=%s", valuesDiagnostic(want), valuesDiagnostic(got))
	}
	if vmController.remaining != machineController.remaining {
		t.Fatalf("instruction accounting differs: VM=%d Machine=%d operations=%#v", vmController.remaining, machineController.remaining, image.operations)
	}
}
