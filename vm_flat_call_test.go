package ember

import (
	"context"
	"testing"
)

func TestNestedMarkedLocalCallsUseIterativeTrampoline(t *testing.T) {
	proto, err := Compile(`
local function outer(value)
	local function step(input)
		return input + 1
	end
	local first = step(value)
	local second = step(first)
	return second
end
return outer(0)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) == 0 {
		t.Fatal("compiled nested program has no outer prototype")
	}

	thread := newVMThread(runtimeGlobals(nil))
	var snapshot directFrameMechanismSnapshot
	thread.directFrameInstrumented = true
	thread.directFramePICCounts = &snapshot.picCounts
	thread.directFrameOpcodeCounts = &snapshot.opcodeCounts
	results, err := thread.run(proto.prototypes[0], []Value{NumberValue(0)}, nil)
	if err != nil {
		t.Fatalf("nested fixed-call run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("nested fixed-call run returned %d results, want one", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 2 {
		t.Fatalf("nested fixed-call result is %v (%t), want 2", results[0], ok)
	}
	if snapshot.picCounts.fixedCallTrampolineEntries < 2 {
		t.Fatalf("iterative trampoline entries = %d, want at least 2", snapshot.picCounts.fixedCallTrampolineEntries)
	}
	if snapshot.picCounts.fixedCallArgCopies != 0 || snapshot.picCounts.fixedCallFrameMaterializations != 0 {
		t.Fatalf("nested fixed-call copies/materializations = %d/%d, want zero", snapshot.picCounts.fixedCallArgCopies, snapshot.picCounts.fixedCallFrameMaterializations)
	}
}

func TestHostCallbacksReceiveOwnedArgumentSlices(t *testing.T) {
	var hostRetained [][]Value
	host := HostFuncValue(func(args []Value) ([]Value, error) {
		hostRetained = append(hostRetained, args)
		args[0] = NumberValue(99)
		return nil, nil
	})
	first := []Value{NumberValue(1)}
	if _, ok, err := directFrameNonYieldingCallIsland(host, nil, first); err != nil || !ok {
		t.Fatalf("HostFunc call = ok %t err %v, want successful call", ok, err)
	}
	if got, _ := first[0].Number(); got != 1 {
		t.Fatalf("HostFunc mutated borrowed source to %v, want 1", got)
	}
	second := []Value{NumberValue(2)}
	if _, ok, err := directFrameNonYieldingCallIsland(host, nil, second); err != nil || !ok {
		t.Fatalf("second HostFunc call = ok %t err %v, want successful call", ok, err)
	}
	if got, _ := hostRetained[0][0].Number(); got != 99 {
		t.Fatalf("retained HostFunc args changed to %v, want 99", got)
	}

	var contextRetained [][]Value
	contextHost := ContextHostFuncValue(func(_ context.Context, args []Value) ([]Value, error) {
		contextRetained = append(contextRetained, args)
		args[0] = NumberValue(77)
		return nil, nil
	})
	third := []Value{NumberValue(3)}
	if _, ok, err := directFrameNonYieldingCallIsland(contextHost, nil, third); err != nil || !ok {
		t.Fatalf("ContextHostFunc call = ok %t err %v, want successful call", ok, err)
	}
	if got, _ := third[0].Number(); got != 3 {
		t.Fatalf("ContextHostFunc mutated borrowed source to %v, want 3", got)
	}
	if got, _ := contextRetained[0][0].Number(); got != 77 {
		t.Fatalf("retained ContextHostFunc args changed to %v, want 77", got)
	}
}

func TestOverriddenFastCallHostReceivesOwnedArguments(t *testing.T) {
	proto, err := Compile(`
local left = 4
local right = 2
local first = math.min(left, right)
local second = math.min(left + 1, right + 1)
return first + second, left + right
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	var retained [][]Value
	mathTable := NewTable()
	if err := mathTable.Set(StringValue("min"), HostFuncValue(func(args []Value) ([]Value, error) {
		retained = append(retained, args)
		value, _ := args[1].Number()
		args[0] = NumberValue(99)
		return []Value{NumberValue(value)}, nil
	})); err != nil {
		t.Fatalf("set overridden math.min: %v", err)
	}
	results, err := RunWithGlobals(proto, map[string]Value{"math": TableValue(mathTable)})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 5 {
		t.Fatalf("host result sum is %v (%t), want 5", got, ok)
	}
	if got, ok := results[1].Number(); !ok || got != 6 {
		t.Fatalf("host mutation changed script registers: %v (%t), want 6", got, ok)
	}
	if len(retained) != 2 {
		t.Fatalf("host retained %d argument slices, want 2", len(retained))
	}
	if got, _ := retained[0][1].Number(); got != 2 {
		t.Fatalf("first retained arguments changed after second call: %v, want 2", got)
	}
}

func TestRecursiveSelfCallUsesBorrowedTrampolineWhenSafe(t *testing.T) {
	proto, err := Compile(`
local function sum(n)
	if n == 0 then
		return 0
	end
	return n + sum(n - 1)
end
return sum(8)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("recursive run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 36 {
		t.Fatalf("recursive result is %v (%t), want 36", results[0], ok)
	}
	if snapshot.picCounts.fixedCallTrampolineEntries == 0 {
		t.Fatal("recursive call used no iterative trampoline entries")
	}
}

func TestMarkedGenericOneResultCallUsesBorrowedTrampoline(t *testing.T) {
	callee := newProto(nil, []instruction{{op: opReturnOne, a: 0}}, nil, nil, 1, 1, false)
	caller := newProto(
		[]Value{NumberValue(41)},
		[]instruction{
			{op: opClosure, a: 0, b: 0},
			{op: opLoadConst, a: 1, b: 0},
			{op: opCallOne, a: 0, b: 0, c: encodeFixedCallCount(1, true), d: 1},
			{op: opReturnOne, a: 0},
		},
		[]*Proto{callee}, nil, 2, 0, false,
	)
	results, snapshot, err := runWithDirectFrameMechanismCounters(caller, nil)
	if err != nil {
		t.Fatalf("marked generic fixed call returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 41 {
		t.Fatalf("marked generic fixed call result is %v (%t), want 41", results[0], ok)
	}
	if snapshot.picCounts.fixedCallTrampolineEntries == 0 {
		t.Fatalf("generic fixed-call trampoline entries = %d, want at least one", snapshot.picCounts.fixedCallTrampolineEntries)
	}
	if snapshot.picCounts.fixedCallArgCopies != 0 || snapshot.picCounts.fixedCallFrameMaterializations != 0 {
		t.Fatalf("generic fixed-call copies/materializations = %d/%d, want zero", snapshot.picCounts.fixedCallArgCopies, snapshot.picCounts.fixedCallFrameMaterializations)
	}
}
