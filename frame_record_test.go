package ember

import (
	"reflect"
	"testing"
	"unsafe"
)

func TestVMFrameRecordFitsCompactCallStateBudget(t *testing.T) {
	typeOfRecord := reflect.TypeOf(vmFrameRecord{})
	if got, want := unsafe.Sizeof(vmFrameRecord{}), uintptr(48); got != want {
		t.Fatalf("vmFrameRecord size = %d bytes, want exactly %d", got, want)
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
	record := vmFrameRecord{
		proto:             proto,
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

	if record.proto != proto {
		t.Fatal("vmFrameRecord lost its proto reference")
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
