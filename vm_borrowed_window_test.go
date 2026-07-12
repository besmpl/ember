package ember

import "testing"

func TestOpenResultRangeUsesSharedOwnerAndClearsLogicalTop(t *testing.T) {
	thread := newVMThread(runtimeGlobals(nil))
	proto := newProto(nil, []instruction{{op: opReturn, a: 0, b: -1}}, nil, nil, 2, 0, true)
	frame := thread.newFrame(proto, nil, nil)
	thread.pushFrame(frame)
	owner := thread.stackOwner
	logicalTop := len(owner.values)
	frame.openResultStart = 0
	if !frame.publishOpenVarargRange(&thread) {
		t.Fatal("publishOpenVarargRange returned false")
	}
	if got, want := frame.openRangeBase, logicalTop; got != want {
		t.Fatalf("open range base = %d, want logical top %d", got, want)
	}
	if got, want := frame.openRangeCount, 1; got != want {
		t.Fatalf("empty open range count = %d, want adjusted count %d", got, want)
	}
	if !frame.openResultAt(0).IsNil() {
		t.Fatalf("empty open result = %v, want nil", frame.openResultAt(0))
	}
	if len(thread.stack) != len(owner.values) {
		t.Fatalf("thread stack length %d differs from owner length %d", len(thread.stack), len(owner.values))
	}

	frame.clearOpenResultState()
	if got := len(owner.values); got != logicalTop {
		t.Fatalf("cleared owner length = %d, want %d", got, logicalTop)
	}
	if got := len(thread.stack); got != logicalTop {
		t.Fatalf("cleared thread stack length = %d, want %d", got, logicalTop)
	}
}

func TestOpenResultRangeRebindsAfterStackGrowth(t *testing.T) {
	thread := newVMThread(runtimeGlobals(nil))
	proto := newProto(nil, []instruction{{op: opReturn, a: 0, b: -1}}, nil, nil, 2, 0, true)
	frame := thread.newFrame(proto, []Value{NumberValue(1), NumberValue(2), NumberValue(3)}, nil)
	thread.pushFrame(frame)
	oldRegisters := frame.registers
	thread.growStack(cap(thread.stack) + 32)
	if &oldRegisters[0] == &frame.registers[0] && cap(oldRegisters) == cap(frame.registers) {
		t.Fatal("stack growth did not force a rebind")
	}
	frame.openResultStart = 0
	if !frame.publishOpenVarargRange(&thread) {
		t.Fatal("publishOpenVarargRange returned false after growth")
	}
	values := frame.openResultRangeValues()
	if len(values) != 3 {
		t.Fatalf("open range length = %d, want 3", len(values))
	}
	if got, ok := values[1].Number(); !ok || got != 2 {
		t.Fatalf("rebound open range second value = %v (%t), want 2", values[1], ok)
	}
	frame.clearOpenResultState()
}

func TestFixedResultOverwriteClearsOwnerBackedOpenRange(t *testing.T) {
	thread := newVMThread(runtimeGlobals(nil))
	proto := newProto(nil, []instruction{{op: opReturnOne, a: 0}}, nil, nil, 2, 0, false)
	frame := thread.newFrame(proto, nil, nil)
	thread.pushFrame(frame)
	owner := thread.stackOwner
	logicalTop := len(owner.values)
	frame.openResultStart = 0
	if !frame.publishOpenResultRange(&thread, []Value{NumberValue(1), NumberValue(2)}) {
		t.Fatal("publishOpenResultRange returned false")
	}
	rangeBase, rangeCount := frame.openRangeBase, frame.openRangeCount
	if got := len(owner.values); got <= logicalTop {
		t.Fatalf("owner length = %d, want range above logical top %d", got, logicalTop)
	}
	frame.applySingleFrameResult(0, vmFrameResult{window: vmSingleResultWindow(NumberValue(9))})
	if got := len(owner.values); got != logicalTop {
		t.Fatalf("fixed result left owner length %d, want logical top %d", got, logicalTop)
	}
	backing := owner.values[:cap(owner.values)]
	for index := 0; index < rangeCount; index++ {
		if !backing[rangeBase+index].IsNil() {
			t.Fatalf("cleared owner range slot %d = %v, want nil", rangeBase+index, backing[rangeBase+index])
		}
	}
	if frame.openResultStart != -1 || frame.openRangeOwner != nil || frame.openRangeBase != -1 || frame.openRangeCount != 0 || frame.openRangeLogicalTop != -1 {
		t.Fatalf("fixed result left stale open state: start=%d owner=%p base=%d count=%d top=%d", frame.openResultStart, frame.openRangeOwner, frame.openRangeBase, frame.openRangeCount, frame.openRangeLogicalTop)
	}
	if got, ok := frame.register(0).Number(); !ok || got != 9 {
		t.Fatalf("fixed result register = %v (%t), want 9", frame.register(0), ok)
	}
}

func TestOpenVarargRangePrefixForwardingParity(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []Value
	}{
		{
			name: "empty",
			source: `local function identity(...) return ... end
return identity()`,
			want: []Value{NilValue()},
		},
		{
			name: "many",
			source: `local function identity(...) return ... end
return identity(1, 2, 3)`,
			want: []Value{NumberValue(1), NumberValue(2), NumberValue(3)},
		},
		{
			name: "prefix",
			source: `local function source(...) return 7, ... end
local function forward(...) return source(...) end
return forward(1, 2, 3)`,
			want: []Value{NumberValue(7), NumberValue(1), NumberValue(2), NumberValue(3)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proto, err := Compile(test.source)
			if err != nil {
				t.Fatalf("Compile returned error: %v", err)
			}
			got, err := Run(proto)
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			instrumented, _, err := runWithDirectFrameMechanismCounters(proto, nil)
			if err != nil {
				t.Fatalf("instrumented Run returned error: %v", err)
			}
			modes := []struct {
				name    string
				results []Value
			}{
				{name: "production", results: got},
				{name: "instrumented", results: instrumented},
			}
			for _, mode := range modes {
				results := mode.results
				if len(results) != len(test.want) {
					t.Fatalf("%s result count = %d, want %d (%v)", mode.name, len(results), len(test.want), results)
				}
				for index := range test.want {
					if results[index] != test.want[index] {
						t.Fatalf("%s result %d = %v, want %v", mode.name, index, results[index], test.want[index])
					}
				}
			}
		})
	}
}

func rebuildProtoExecutionCodeForTest(t *testing.T, proto *Proto, code []instruction) {
	t.Helper()
	if err := encodeProtoWords(proto, code); err != nil {
		t.Fatalf("encode marked prototype: %v", err)
	}
}

func markFirstLocalCallBorrowHint(t *testing.T, proto *Proto, borrow bool) {
	t.Helper()
	code, err := decodeWordcode(proto.words)
	if err != nil {
		t.Fatalf("decode prototype wordcode: %v", err)
	}
	for pc, ins := range code {
		if ins.op == opCall {
			count := ins.c
			if count < 0 {
				continue
			}
			ins.op = opCallLocalOne
			ins.c = ins.b + 1
			ins.d = encodeFixedCallCount(count, borrow)
			code[pc] = ins
			rebuildProtoExecutionCodeForTest(t, proto, code)
			return
		}
		if ins.op != opCallLocalOne {
			continue
		}
		count, _ := decodeFixedCallCount(ins.d)
		code[pc].d = encodeFixedCallCount(count, borrow)
		rebuildProtoExecutionCodeForTest(t, proto, code)
		return
	}
	t.Fatalf("compiled prototype has no CALL_LOCAL_ONE: %v", disassembleProto(proto))
}

func TestMarkedLocalFixedCallBorrowsRegisterWindow(t *testing.T) {
	proto, err := Compile(`
local function add(a, b)
	return a + b
end
return add(2, 3)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	markFirstLocalCallBorrowHint(t, proto, true)

	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("marked run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("marked run returned %d results, want one", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 5 {
		t.Fatalf("marked result is %v (%t), want 5", results[0], ok)
	}
	if snapshot.picCounts.fixedCallArgCopies != 0 {
		t.Fatalf("marked fixed-call argument copies = %d, want zero", snapshot.picCounts.fixedCallArgCopies)
	}
	if snapshot.picCounts.fixedCallFrameMaterializations != 0 {
		t.Fatalf("marked fixed-call materializations = %d, want zero", snapshot.picCounts.fixedCallFrameMaterializations)
	}
}

func TestUnmarkedLocalFixedCallKeepsCopiedFallback(t *testing.T) {
	proto, err := Compile(`
local function add(a, b)
	return a + b
end
return add(2, 3)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	markFirstLocalCallBorrowHint(t, proto, false)

	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("unmarked run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("unmarked run returned %d results, want one", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 5 {
		t.Fatalf("unmarked result is %v (%t), want 5", results[0], ok)
	}
	if snapshot.picCounts.fixedCallFrameMaterializations == 0 {
		t.Fatal("unmarked fixed-call materializations = 0, want copied fallback")
	}
}

func TestMarkedUpvalueFixedCallBorrowsRegisterWindow(t *testing.T) {
	callee := newProto(nil, []instruction{{op: opReturnOne, a: 0}}, nil, nil, 1, 1, false)
	caller := newProto(
		[]Value{NumberValue(41)},
		[]instruction{
			{op: opLoadConst, a: 1, b: 0},
			{op: opCallUpvalueOne, a: 0, b: 0, c: 1, d: encodeFixedCallCount(1, true)},
			{op: opReturnOne, a: 0},
		},
		nil,
		[]upvalueDesc{{local: false, index: 0}},
		2,
		0,
		false,
	)
	thread := newVMThread(runtimeGlobals(nil))
	var snapshot directFrameMechanismSnapshot
	thread.directFrameInstrumented = true
	thread.directFramePICCounts = &snapshot.picCounts
	thread.directFrameOpcodeCounts = &snapshot.opcodeCounts
	results, err := thread.run(caller, nil, []*cell{{value: functionValue(callee, nil)}})
	if err != nil {
		t.Fatalf("marked upvalue run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("marked upvalue run returned %d results, want one", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 41 {
		t.Fatalf("marked upvalue result is %v (%t), want 41", results[0], ok)
	}
	if snapshot.picCounts.fixedCallArgCopies != 0 || snapshot.picCounts.fixedCallFrameMaterializations != 0 {
		t.Fatalf("marked upvalue copies/materializations = %d/%d, want zero", snapshot.picCounts.fixedCallArgCopies, snapshot.picCounts.fixedCallFrameMaterializations)
	}
	if snapshot.picCounts.fixedCallFrameReuses != 0 {
		t.Fatalf("marked upvalue pooled frame reuses = %d, want zero", snapshot.picCounts.fixedCallFrameReuses)
	}
	if snapshot.picCounts.fixedCallTrampolineEntries == 0 || snapshot.picCounts.fixedCallRecursiveEntries != 0 {
		t.Fatalf("marked upvalue trampoline/recursive entries = %d/%d, want iterative only", snapshot.picCounts.fixedCallTrampolineEntries, snapshot.picCounts.fixedCallRecursiveEntries)
	}
}

func TestMarkedFixedCallOneRuntimeNativeUsesDecodedCount(t *testing.T) {
	proto := newProto(
		[]Value{StringValue("rawlen")},
		[]instruction{
			{op: opLoadGlobal, a: 0, b: 0},
			{op: opNewTable, a: 1},
			{op: opCallOne, a: 0, b: 0, c: encodeFixedCallCount(1, true), d: 1},
			{op: opReturnOne, a: 0},
		},
		nil,
		nil,
		2,
		0,
		false,
	)
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("marked native call returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("marked native call returned %d results, want one", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 0 {
		t.Fatalf("marked native result is %v (%t), want 0", results[0], ok)
	}
	if snapshot.picCounts.fixedCallFrameMaterializations != 0 {
		t.Fatalf("marked native materializations = %d, want zero", snapshot.picCounts.fixedCallFrameMaterializations)
	}
}

func TestMarkedMethodFixedCallBorrowsRegisterWindow(t *testing.T) {
	proto, err := Compile(`
local object = {value = 10}
function object:add(amount)
	self.value = self.value + amount
	return self.value
end
local value = object:add(5)
return value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	var marked bool
	code, err := decodeWordcode(proto.words)
	if err != nil {
		t.Fatalf("decode prototype wordcode: %v", err)
	}
	for pc, ins := range code {
		if ins.op != opCallMethodOne {
			continue
		}
		count, _ := decodeFixedCallCount(ins.d)
		code[pc].d = encodeFixedCallCount(count, true)
		marked = true
		break
	}
	if !marked {
		t.Fatalf("compiled method program has no CALL_METHOD_ONE: %v", disassembleProto(proto))
	}
	rebuildProtoExecutionCodeForTest(t, proto, code)
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("marked method run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("marked method run returned %d results, want one", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 15 {
		t.Fatalf("marked method result is %v (%t), want 15", results[0], ok)
	}
	if snapshot.picCounts.fixedCallArgCopies != 0 {
		t.Fatalf("marked method fixed-call argument copies = %d, want zero", snapshot.picCounts.fixedCallArgCopies)
	}
	if snapshot.picCounts.fixedCallFrameMaterializations != 0 {
		t.Fatalf("marked method fixed-call materializations = %d, want zero", snapshot.picCounts.fixedCallFrameMaterializations)
	}
	if got := snapshot.picCounts.fixedCallFrameReuses; got != 0 {
		t.Fatalf("marked method pooled frame reuses = %d, want zero", got)
	}
	if snapshot.picCounts.fixedCallTrampolineEntries == 0 || snapshot.picCounts.fixedCallRecursiveEntries != 0 {
		t.Fatalf("marked method trampoline/recursive entries = %d/%d, want iterative only", snapshot.picCounts.fixedCallTrampolineEntries, snapshot.picCounts.fixedCallRecursiveEntries)
	}
}

func TestMarkedMethodFixedCallFallsBackForRuntimeNative(t *testing.T) {
	proto, err := Compile(`
local object = {}
object.add = table.insert
object:add(1)
return #object
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	var marked bool
	code, err := decodeWordcode(proto.words)
	if err != nil {
		t.Fatalf("decode prototype wordcode: %v", err)
	}
	for pc, ins := range code {
		if ins.op != opCallMethodOne {
			continue
		}
		count, _ := decodeFixedCallCount(ins.d)
		code[pc].d = encodeFixedCallCount(count, true)
		marked = true
		break
	}
	if !marked {
		t.Fatalf("compiled native method program has no CALL_METHOD_ONE: %v", disassembleProto(proto))
	}
	rebuildProtoExecutionCodeForTest(t, proto, code)
	results, _, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("marked native method run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("marked native method run returned %d results, want one", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 1 {
		t.Fatalf("marked native method result is %v (%t), want 1", results[0], ok)
	}
}

func TestCompiledNestedFixedCallsUseCompactRecords(t *testing.T) {
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
	if got := snapshot.picCounts.fixedCallFrameReuses; got != 0 {
		t.Fatalf("nested fixed-call pooled frame reuses = %d, want zero", got)
	}
	if got, wantAtLeast := snapshot.picCounts.fixedCallTrampolineEntries, uint64(2); got < wantAtLeast {
		t.Fatalf("nested fixed-call record entries = %d, want at least %d", got, wantAtLeast)
	}
	if snapshot.picCounts.fixedCallArgCopies != 0 || snapshot.picCounts.fixedCallFrameMaterializations != 0 {
		t.Fatalf("nested fixed-call copies/materializations = %d/%d, want zero", snapshot.picCounts.fixedCallArgCopies, snapshot.picCounts.fixedCallFrameMaterializations)
	}
}

func TestCompiledFixedCallLoopKeepsCopiesFlat(t *testing.T) {
	proto, err := Compile(`
local function inc(value)
	return value + 1
end
local total = 0
for i = 1, 1000 do
	total = inc(total)
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("fixed-call loop returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("fixed-call loop returned %d results, want one", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 1000 {
		t.Fatalf("fixed-call loop result is %v (%t), want 1000", results[0], ok)
	}
	if got := snapshot.picCounts.fixedCallFrameReuses; got != 0 {
		t.Fatalf("fixed-call loop pooled frame reuses = %d, want zero", got)
	}
	if got, wantAtLeast := snapshot.picCounts.fixedCallTrampolineEntries, uint64(1000); got < wantAtLeast {
		t.Fatalf("fixed-call loop record entries = %d, want at least %d", got, wantAtLeast)
	}
	if snapshot.picCounts.fixedCallArgCopies != 0 || snapshot.picCounts.fixedCallFrameMaterializations != 0 {
		t.Fatalf("fixed-call loop copies/materializations = %d/%d, want zero", snapshot.picCounts.fixedCallArgCopies, snapshot.picCounts.fixedCallFrameMaterializations)
	}
}
