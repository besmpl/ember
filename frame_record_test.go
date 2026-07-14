package ember

import (
	"reflect"
	"testing"
	"unsafe"
)

func TestVMFrameRecordFitsCompactCallStateBudget(t *testing.T) {
	typeOfRecord := reflect.TypeOf(vmFrameRecord{})
	if got, want := unsafe.Sizeof(vmFrameRecord{}), expectedArchitectureLayoutSize(44, 48); got != want {
		t.Fatalf("vmFrameRecord size = %d bytes, want exactly %d for this pointer width", got, want)
	}

	pointers := 0
	for fieldIndex := 0; fieldIndex < typeOfRecord.NumField(); fieldIndex++ {
		if typeOfRecord.Field(fieldIndex).Type.Kind() == reflect.Pointer {
			pointers++
		}
	}
	if pointers > 1 {
		t.Fatalf("vmFrameRecord has %d pointer fields, want at most one", pointers)
	}

	proto := &Proto{}
	identity := &closure{proto: proto}
	record := vmFrameRecord{
		closure:           identity,
		returnPC:          0x10203040,
		base:              17,
		top:               31,
		resultDestination: 5,
		resultCount:       0xffff_ffff,
		argumentBase:      8,
		argumentCount:     13,
		varargBase:        21,
		varargCount:       34,
		protectedDepth:    7,
		frameDepth:        4,
		flags:             vmFrameRecordFlagProtected | vmFrameRecordFlagOpenResults,
	}

	if record.closure != identity || record.closure.proto != proto {
		t.Fatal("vmFrameRecord lost its closure identity")
	}
	if record.returnPC != 0x10203040 || record.base != 17 || record.top != 31 {
		t.Fatalf("vmFrameRecord control state = %#v", record)
	}
	if record.resultDestination != 5 || record.resultCount != 0xffff_ffff {
		t.Fatalf("vmFrameRecord result state = %#v", record)
	}
	if record.argumentBase != 8 || record.argumentCount != 13 ||
		record.varargBase != 21 || record.varargCount != 34 {
		t.Fatalf("vmFrameRecord argument state = %#v", record)
	}
	if record.protectedDepth != 7 || record.frameDepth != 4 || record.flags != vmFrameRecordFlagProtected|vmFrameRecordFlagOpenResults {
		t.Fatalf("vmFrameRecord protection state = %#v", record)
	}
	if _, ok := reflect.TypeOf(vmFrame{}).FieldByName("caller"); ok {
		t.Fatal("vmFrame retains a pointer-linked caller field")
	}
}

func TestBorrowedFixedCallRecordRoundTrips(t *testing.T) {
	proto, err := Compile(`
local function outer(value)
	local function step(input)
		return input + 1
	end
	local first = step(value)
	return step(first)
end
return outer(0)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	thread := newVMThread(runtimeGlobals(nil))
	if len(proto.prototypes) == 0 {
		t.Fatal("compiled program has no outer prototype")
	}
	results, err := thread.run(proto.prototypes[0], []Value{NumberValue(0)}, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("thread.run returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 2 {
		t.Fatalf("thread.run result is %v (%t), want number 2", got, ok)
	}
	if thread.maxFrameRecords < 1 {
		t.Fatalf("max frame-record depth is %d, want at least 1", thread.maxFrameRecords)
	}
	if len(thread.frameRecords) != 0 {
		t.Fatalf("thread kept %d frame records after return, want empty stack", len(thread.frameRecords))
	}
}

func TestBorrowedFixedCallRecordDropsOnError(t *testing.T) {
	proto, err := Compile(`
local function outer(value)
	local function fail(input)
		return input + true
	end
	local failed = fail(value)
	return failed + 1
end
return outer(0)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) == 0 {
		t.Fatal("compiled program has no outer prototype")
	}

	thread := newVMThread(runtimeGlobals(nil))
	if _, err := thread.run(proto.prototypes[0], []Value{NumberValue(0)}, nil); err == nil {
		t.Fatal("thread.run unexpectedly accepted number + boolean")
	}
	if thread.maxFrameRecords < 1 {
		t.Fatalf("max frame-record depth is %d, want at least 1", thread.maxFrameRecords)
	}
	if len(thread.frameRecords) != 0 {
		t.Fatalf("thread kept %d frame records after error, want empty stack", len(thread.frameRecords))
	}
}

func TestRecursiveZeroCaptureCallsUseOnlyCompactFrameRecords(t *testing.T) {
	proto, err := Compile(`
function sum(n)
	if n == 0 then
		return 0
	end
	return n + sum(n - 1)
end
local result = sum(64)
return result
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("thread.run returned %d results, want %d", got, want)
	}
	if got, ok := results[0].Number(); !ok || got != 2080 {
		t.Fatalf("thread.run result is %v (%t), want number 2080", results[0], ok)
	}
	if got, want := thread.maxFrames, 1; got != want {
		t.Fatalf("thread max physical frame depth is %d, want %d", got, want)
	}
	if got, wantAtLeast := thread.maxFrameRecords, 65; got < wantAtLeast {
		t.Fatalf("thread max compact frame-record depth is %d, want at least %d", got, wantAtLeast)
	}
	if len(thread.frames) != 0 || len(thread.frameRecords) != 0 {
		t.Fatalf("thread retained %d physical frames and %d records after return", len(thread.frames), len(thread.frameRecords))
	}
	if !allocationInstrumentedTest() {
		allocs := testing.AllocsPerRun(50, func() {
			results, runErr := thread.run(proto, nil, nil)
			if runErr != nil {
				t.Fatalf("warm record-only run returned error: %v", runErr)
			}
			if got, ok := results[0].Number(); !ok || got != 2080 {
				t.Fatalf("warm record-only result is %v (%t), want number 2080", results[0], ok)
			}
		})
		if allocs > 2 {
			t.Fatalf("warm record-only recursion allocated %.0f times per run, want boundary allocations only", allocs)
		}
	}

	_, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("instrumented run returned error: %v", err)
	}
	if got, wantAtLeast := snapshot.picCounts.fixedCallTrampolineEntries, uint64(65); got < wantAtLeast {
		t.Fatalf("fixed-call record entries = %d, want at least %d", got, wantAtLeast)
	}
	if got := snapshot.picCounts.fixedCallFrameReuses; got != 0 {
		t.Fatalf("fixed-call pooled frame reuses = %d, want zero", got)
	}
}

func TestRecursiveCapturedCallsUseOnePhysicalFrameAndRestoreClosureState(t *testing.T) {
	proto, err := Compile(`
function makeCounter()
	local count = 0
	local function recurse(n)
		if n == 0 then
			return count
		end
		count = count + 1
		local nested = recurse(n - 1) + 0
		return nested
	end
	local result = recurse(64)
	return result + count
end
local result = makeCounter()
return result
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("thread.run returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 128 {
		t.Fatalf("result is %v (%t), want number 128", results[0], ok)
	}
	if got, want := thread.maxFrames, 1; got != want {
		t.Fatalf("thread max physical frame depth is %d, want %d", got, want)
	}
	if got, wantAtLeast := thread.maxFrameRecords, 65; got < wantAtLeast {
		t.Fatalf("thread max compact frame-record depth is %d, want at least %d", got, wantAtLeast)
	}
	if len(thread.frames) != 0 || len(thread.frameRecords) != 0 || len(thread.openUpvalues) != 0 {
		t.Fatalf("thread retained %d frames, %d records, and %d open upvalues", len(thread.frames), len(thread.frameRecords), len(thread.openUpvalues))
	}
	if len(thread.rootClosureSlots) == 0 || thread.rootClosureSlots[0] == nil {
		t.Fatal("thread did not retain its pooled root closure slot")
	}
	root := thread.rootClosureSlots[0]
	if root.proto != nil || root.upvalues != nil || root.upvalueValues != nil || root.upvalueValueOK != nil {
		t.Fatalf("pooled root closure retained live state: %#v", root)
	}
	if !allocationInstrumentedTest() {
		allocs := testing.AllocsPerRun(50, func() {
			results, runErr := thread.run(proto, nil, nil)
			if runErr != nil {
				t.Fatalf("warm captured record-only run returned error: %v", runErr)
			}
			if got, ok := results[0].Number(); !ok || got != 128 {
				t.Fatalf("warm captured record-only result is %v (%t), want number 128", results[0], ok)
			}
		})
		if allocs > 8 {
			t.Fatalf("warm captured record-only recursion allocated %.0f times per run, want boundary allocations only", allocs)
		}
	}
}

func TestFixedMultiResultCallsUseOneRecordOnlyFrame(t *testing.T) {
	proto, err := Compile(`
local function pair(value)
	return value, value + 1
end
local first, second = pair(41)
return first, second
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	firstWords := append([]wordcodeWord(nil), proto.words...)
	if err := finalizeProtoExecutionArtifact(proto); err != nil {
		t.Fatalf("re-finalize compiled prototype: %v", err)
	}
	if len(proto.words) != len(firstWords) {
		t.Fatalf("re-finalized word count = %d, want stable %d", len(proto.words), len(firstWords))
	}
	for index := range firstWords {
		if proto.words[index] != firstWords[index] {
			t.Fatalf("re-finalized word %d = %#x, want stable %#x", index, proto.words[index], firstWords[index])
		}
	}
	code, err := decodeWordcode(proto.words)
	if err != nil {
		t.Fatalf("decode compiled wordcode: %v", err)
	}
	marked := false
	for _, ins := range code {
		if ins.op != opCall {
			continue
		}
		if count, borrow := decodeFixedMultiResultCount(ins.d, proto.registers); borrow && count == 2 {
			marked = true
		}
	}
	if !marked {
		t.Fatalf("compiled pair call has no fixed-multi borrow marker: %v", disassembleProto(proto))
	}
	thread := newVMThread(runtimeGlobals(nil))
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("thread.run returned %d results, want 2", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 41 {
		t.Fatalf("first result is %v (%t), want 41", results[0], ok)
	}
	if got, ok := results[1].Number(); !ok || got != 42 {
		t.Fatalf("second result is %v (%t), want 42", results[1], ok)
	}
	if got, want := thread.maxFrames, 1; got != want {
		t.Fatalf("thread max physical frame depth is %d, want %d", got, want)
	}
	if thread.maxFrameRecords < 1 {
		t.Fatalf("thread max compact frame-record depth is %d, want at least 1", thread.maxFrameRecords)
	}
	if len(thread.frames) != 0 || len(thread.frameRecords) != 0 {
		t.Fatalf("thread retained %d physical frames and %d records after return", len(thread.frames), len(thread.frameRecords))
	}
	if !allocationInstrumentedTest() {
		allocs := testing.AllocsPerRun(50, func() {
			results, runErr := thread.run(proto, nil, nil)
			if runErr != nil || len(results) != 2 {
				t.Fatalf("warm fixed-multi run returned %v (%d results)", runErr, len(results))
			}
		})
		if allocs > 2 {
			t.Fatalf("warm fixed-multi record call allocated %.0f times per run, want boundary allocations only", allocs)
		}
	}

	_, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("instrumented run returned error: %v", err)
	}
	if snapshot.picCounts.fixedCallTrampolineEntries < 1 {
		t.Fatalf("fixed-call record entries = %d, want at least 1", snapshot.picCounts.fixedCallTrampolineEntries)
	}
}

func TestFixedMultiResultCallNilFillsShortReturn(t *testing.T) {
	proto, err := Compile(`
local function short()
	return 7
end
local first, second = short()
return first, second
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("thread.run returned %d results, want 2", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 7 {
		t.Fatalf("first result is %v (%t), want 7", results[0], ok)
	}
	if !results[1].IsNil() {
		t.Fatalf("second result is %v, want nil", results[1])
	}
	if thread.maxFrames != 1 || thread.maxFrameRecords < 1 {
		t.Fatalf("short multi call used maxFrames=%d maxFrameRecords=%d, want 1 and record-only", thread.maxFrames, thread.maxFrameRecords)
	}
}

func TestFixedMultiResultRecordForwardsOpenReturn(t *testing.T) {
	proto, err := Compile(`
local function pair(value)
	return value, value + 1
end
local function forward(value)
	return pair(value)
end
local first, second = forward(9)
return first, second
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("thread.run returned %d results, want 2", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 9 {
		t.Fatalf("first result is %v (%t), want 9", results[0], ok)
	}
	if got, ok := results[1].Number(); !ok || got != 10 {
		t.Fatalf("second result is %v (%t), want 10", results[1], ok)
	}
	if thread.maxFrameRecords < 1 {
		t.Fatalf("forward multi call used maxFrameRecords=%d, want record-only outer call", thread.maxFrameRecords)
	}
}

func TestFixedMultiResultCapturedClosureState(t *testing.T) {
	proto, err := Compile(`
local function makePair()
	local state = 0
	local function pair()
		state = state + 1
		return state, state + 10
	end
	local first, second = pair()
	local third, fourth = pair()
	return first, second, third, fourth
end
return makePair()
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	want := []float64{1, 11, 2, 12}
	if len(results) != len(want) {
		t.Fatalf("thread.run returned %d results, want %d", len(results), len(want))
	}
	for index, expected := range want {
		got, ok := results[index].Number()
		if !ok || got != expected {
			t.Fatalf("result %d is %v (%t), want %g", index, results[index], ok, expected)
		}
	}
}

func TestRecursiveFixedMultiResultCallsUseCompactRecords(t *testing.T) {
	proto, err := Compile(`
function recurse(n)
	if n == 0 then
		return 0, 0
	end
	local first, second = recurse(n - 1)
	return first + n, second + n
end
local first, second = recurse(8)
return first, second
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("thread.run returned %d results, want 2", len(results))
	}
	for index, value := range results {
		got, ok := value.Number()
		if !ok || got != 36 {
			t.Fatalf("result %d is %v (%t), want 36", index, value, ok)
		}
	}
	if thread.maxFrames != 1 || thread.maxFrameRecords < 9 {
		t.Fatalf("recursive multi call used maxFrames=%d maxFrameRecords=%d, want 1 and at least 9 records", thread.maxFrames, thread.maxFrameRecords)
	}
}
