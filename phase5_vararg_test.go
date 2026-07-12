package ember

import "testing"

func TestVariadicCalleeUsesRecordOnlyOwnerTail(t *testing.T) {
	first := NewTable()
	second := NewTable()
	proto, err := Compile(`
local function apply(head, ...)
	return head, select("#", ...)
end
local a, b = apply(0, first, second)
return a, b
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(map[string]Value{
		"first":  TableValue(first),
		"second": TableValue(second),
	}))
	var snapshot directFrameMechanismSnapshot
	thread.directFrameInstrumented = true
	thread.directFramePICCounts = &snapshot.picCounts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("thread.run returned %d results, want two", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 0 {
		t.Fatalf("head result = %v (%t), want 0", results[0], ok)
	}
	if got, ok := results[1].Number(); !ok || got != 2 {
		t.Fatalf("vararg count result = %v (%t), want 2", results[1], ok)
	}
	if thread.maxFrames != 1 || thread.maxFrameRecords < 1 {
		t.Fatalf("variadic callee frames/records = %d/%d, want one physical frame and a compact record", thread.maxFrames, thread.maxFrameRecords)
	}
	if snapshot.picCounts.fixedCallTrampolineEntries == 0 || snapshot.picCounts.fixedCallFrameMaterializations != 0 || snapshot.picCounts.fixedCallArgCopies != 0 {
		t.Fatalf("variadic callee trampoline/materializations/copies = %d/%d/%d, want record-only entry", snapshot.picCounts.fixedCallTrampolineEntries, snapshot.picCounts.fixedCallFrameMaterializations, snapshot.picCounts.fixedCallArgCopies)
	}
	if owner := thread.stackOwner; owner != nil {
		for index, value := range owner.values[:cap(owner.values)] {
			if table, ok := value.Table(); ok && (table == first || table == second) {
				t.Fatalf("owner slot %d retained variadic table argument", index)
			}
		}
	}
}

func TestVariadicCoroutineRetainsArgumentsAcrossYield(t *testing.T) {
	proto, err := Compile(`
local co = coroutine.create(function(...)
	local before = (...)
	coroutine.yield()
	return before, (...)
end)
local firstOK = coroutine.resume(co, 11)
local secondOK, before, after = coroutine.resume(co, 22)
return firstOK, secondOK, before, after
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("Run returned %d results, want 4", len(results))
	}
	for index, want := range []float64{1, 1, 11, 11} {
		if index < 2 {
			got, ok := results[index].Bool()
			if !ok || (got != (want == 1)) {
				t.Fatalf("result %d is %v (%t), want boolean %t", index, results[index], ok, want == 1)
			}
			continue
		}
		got, ok := results[index].Number()
		if !ok || got != want {
			t.Fatalf("result %d is %v (%t), want number %v", index, results[index], ok, want)
		}
	}
}

func TestVarargFrameClearsOwnerTailOnRelease(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturnOne, a: 0}}, nil, nil, 1, 1, true)
	thread := newVMThread(nil)
	frame := thread.newFrame(proto, []Value{StringValue("head"), TableValue(newTableWithCapacity(0, 0))}, nil)
	owner := frame.varargOwner
	if owner == nil || frame.varargLen() != 1 {
		t.Fatalf("vararg owner/count = %p/%d, want owner and one value", owner, frame.varargLen())
	}
	tail := frame.varargBase
	extended := owner.values
	thread.pushFrame(frame)
	thread.popFrame()
	if len(owner.values) != 0 {
		t.Fatalf("released owner length = %d, want 0", len(owner.values))
	}
	if tail >= len(extended) {
		t.Fatalf("released vararg tail index %d outside retained owner backing length %d", tail, len(extended))
	}
	if !extended[tail].IsNil() {
		t.Fatalf("released vararg slot retained %v", extended[tail])
	}
}
