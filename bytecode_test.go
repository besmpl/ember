package ember

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestDisassembleProtoNamesInstructions(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(0, NumberValue(2))
	builder.emitLoadConst(1, NumberValue(3))
	builder.emit(instruction{op: opAdd, a: 2, b: 0, c: 1})
	builder.emit(instruction{op: opReturn, a: 2, b: 1})
	proto := builder.proto(nil, 3, 0, false)

	got := disassembleProto(proto)
	want := []string{
		"0000 LOAD_CONST r0 k0(number 2)",
		"0001 LOAD_CONST r1 k1(number 3)",
		"0002 ADD r2 r0 r1",
		"0003 RETURN r2 1",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("disassembleProto() = %#v, want %#v", got, want)
	}
}

func TestInstructionSizeBudget(t *testing.T) {
	if got, want := reflect.TypeOf(packedInstruction{}).Size(), uintptr(16); got > want {
		t.Fatalf("instruction size is %d bytes, want at most %d", got, want)
	}
}

func TestFixedCallCountEncodingBoundaries(t *testing.T) {
	for _, count := range []int{0, 1, 32767} {
		for _, borrow := range []bool{false, true} {
			raw := encodeFixedCallCount(count, borrow)
			decoded, gotBorrow := decodeFixedCallCount(raw)
			if decoded != count || gotBorrow != borrow {
				t.Fatalf("fixed call count (%d, %t) encoded as %d and decoded as (%d, %t)", count, borrow, raw, decoded, gotBorrow)
			}
			if _, err := packInstruction(instruction{op: opCallOne, a: 0, b: 0, c: raw}); err != nil {
				t.Fatalf("fixed call count (%d, %t) did not fit packed operand: %v", count, borrow, err)
			}
		}
	}
	if got := encodeFixedCallCount(2, false); got < 0 {
		t.Fatalf("ordinary fixed call count encoded as negative %d", got)
	}
	if got, borrow := decodeFixedCallCount(-32769); got != 32768 || !borrow {
		t.Fatalf("corrupt negative count decoded as (%d, %t), want (32768, true)", got, borrow)
	}
	if _, _, err := verifyFixedCallCount(-32769, "fixed one-result call"); err == nil {
		t.Fatal("verifier accepted a negative count outside the packed int16 range")
	}
	if _, _, err := verifyFixedCallCount(32768, "fixed one-result call"); err == nil {
		t.Fatal("verifier accepted a positive count outside the packed int16 range")
	}
}

func TestDisassembleFixedCallBorrowMarker(t *testing.T) {
	proto := &Proto{code: []instruction{{op: opCallLocalOne, a: 0, b: 1, c: 2, d: encodeFixedCallCount(3, true)}}}
	got := disassembleProto(proto)
	if len(got) != 1 || !strings.Contains(got[0], "CALL_LOCAL_ONE r0 r1 r2 3 borrow") {
		t.Fatalf("fixed-call borrow disassembly = %#v", got)
	}
}

func TestPackedInstructionRoundTripsAllOpcodes(t *testing.T) {
	for _, op := range allOpcodes {
		ins := instruction{op: op, a: 1, b: 2, c: 3, d: 4}
		packed, err := packInstruction(ins)
		if err != nil {
			t.Fatalf("packInstruction(%s) returned error: %v", opcodeName(op), err)
		}
		if got := packed.unpack(); got != ins {
			t.Fatalf("packed %s round trip = %#v, want %#v", opcodeName(op), got, ins)
		}
	}
}

func TestFinalizeProtoRejectsPackedInstructionOperandOverflow(t *testing.T) {
	proto := newProto(
		[]Value{NumberValue(1)},
		[]instruction{
			{op: opLoadConst, a: 32768, b: 0},
			{op: opReturnOne, a: 0},
		},
		nil,
		nil,
		1,
		0,
		false,
	)
	if proto.verifyErr == nil {
		t.Fatal("newProto accepted an instruction operand outside the packed int16 range")
	}
	if got := proto.verifyErr.Error(); !strings.Contains(got, "instruction 0 LOAD_CONST") || !strings.Contains(got, "operand a value 32768 out of int16 range") {
		t.Fatalf("packed operand overflow error is %q", got)
	}
}

func TestValueSizeBudgetSafeLayout(t *testing.T) {
	if got, want := reflect.TypeOf(Value{}).Size(), uintptr(16); got != want {
		t.Fatalf("Value size is %d bytes, want exactly %d", got, want)
	}
}

func TestValueRoundTripsAllKinds(t *testing.T) {
	table := NewTable()
	userdata := NewUserData("payload")
	proto := newProto(nil, []instruction{{op: opReturn}}, nil, nil, 0, 0, false)
	closureValue := functionValue(proto, nil)
	hostFn := func(args []Value) ([]Value, error) { return args, nil }
	nativeValue := nativeFuncValueWithID(baseRawLenNative, nativeFuncRawLen)

	if !NilValue().IsNil() {
		t.Fatal("NilValue did not round-trip nil kind")
	}
	if got, ok := BoolValue(true).Bool(); !ok || !got {
		t.Fatalf("BoolValue round trip = %v, %t; want true, true", got, ok)
	}
	if got, ok := NumberValue(12.5).Number(); !ok || got != 12.5 {
		t.Fatalf("NumberValue round trip = %v, %t; want 12.5, true", got, ok)
	}
	if got, ok := StringValue("ember").String(); !ok || got != "ember" {
		t.Fatalf("StringValue round trip = %q, %t; want ember, true", got, ok)
	}
	if got, ok := TableValue(table).Table(); !ok || got != table {
		t.Fatalf("TableValue round trip = %p, %t; want %p, true", got, ok, table)
	}
	if got, ok := UserDataValue(userdata).UserData(); !ok || got != userdata {
		t.Fatalf("UserDataValue round trip = %p, %t; want %p, true", got, ok, userdata)
	}
	if got, ok := closureValue.scriptFunction(); !ok || got == nil || got.proto != proto {
		t.Fatalf("functionValue round trip = %#v, %t; want closure for proto", got, ok)
	}
	if got, ok := HostFuncValue(hostFn).hostFunction(); !ok || got == nil {
		t.Fatalf("HostFuncValue round trip = %v, %t; want host function", got, ok)
	}
	if got, ok := nativeValue.nativeFunction(); !ok || got == nil {
		t.Fatalf("nativeFuncValueWithID round trip = %v, %t; want native function", got, ok)
	}
}

func TestStringValuesCompareAndHashAcrossBoxingBoundaries(t *testing.T) {
	left := StringValue("ember")
	right := StringValue(strings.Join([]string{"em", "ber"}, ""))
	if !valuesEqual(left, right) {
		t.Fatalf("boxed strings with equal text did not compare equal: %#v %#v", left, right)
	}
	leftKey, leftOK := tableKeyFromValue(left)
	rightKey, rightOK := tableKeyFromValue(right)
	if !leftOK || !rightOK {
		t.Fatalf("tableKeyFromValue ok = %t, %t; want true, true", leftOK, rightOK)
	}
	if !tableKeysEqual(leftKey, rightKey) {
		t.Fatalf("table keys from separately boxed strings are not equal: %#v != %#v", leftKey, rightKey)
	}
	table := NewTable()
	if err := table.Set(left, NumberValue(7)); err != nil {
		t.Fatalf("table.Set returned error: %v", err)
	}
	got, err := table.Get(right)
	if err != nil {
		t.Fatalf("table.Get returned error: %v", err)
	}
	if number, ok := got.Number(); !ok || number != 7 {
		t.Fatalf("table lookup across string boxes = %v (%t), want 7", got, ok)
	}
}

func TestValueConstructorsDoNotAllocateForScalars(t *testing.T) {
	var sink Value
	allocs := testing.AllocsPerRun(1000, func() {
		sink = NilValue()
		sink = BoolValue(true)
		sink = NumberValue(1)
		sink = nativeFuncValueWithID(baseRawLenNative, nativeFuncRawLen)
	})
	if allocs != 0 {
		t.Fatalf("scalar value constructors allocated %.2f times, want 0", allocs)
	}
	_ = sink
}

func TestRunMinimalScriptAllocationBudget(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`return 1`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if results, err := Run(proto); err != nil {
		t.Fatalf("warm Run returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 1 {
		t.Fatalf("warm Run result is %v (%t), want number 1", results[0], ok)
	}

	allocs := testing.AllocsPerRun(1000, func() {
		results, err := Run(proto)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 1 {
			t.Fatalf("Run result is %v (%t), want number 1", results[0], ok)
		}
	})
	if allocs > 1 {
		t.Fatalf("minimal Run allocated %.0f times, want only the public result slice allocation", allocs)
	}
}

func TestRunWithGlobalsDoesNotCopyHostMapPerRun(t *testing.T) {
	proto, err := Compile(`return target`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	globals := make(map[string]Value, 512)
	for i := 0; i < 512; i++ {
		globals[fmt.Sprintf("unused_%03d", i)] = NumberValue(float64(i))
	}
	globals["target"] = NumberValue(42)

	bytes := measuredRunWithGlobalsAllocBytes(t, proto, globals, 42, 40)
	if bytes > 8192 {
		t.Fatalf("RunWithGlobals allocated %d bytes per run with a large host map, want no per-run host map copy", bytes)
	}
}

func TestGlobalReadsDoNotAllocateOrRehashPerAccess(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 80 do
	total = total + score
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, map[string]Value{
		"score": NumberValue(3),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 240 {
		t.Fatalf("RunWithGlobals result is %v (%t), want 240", results[0], ok)
	}
	if got := snapshot.opcodeCounts.count(opLoadGlobal); got < 80 {
		t.Fatalf("LOAD_GLOBAL executed %d times, want repeated global reads in the loop", got)
	}
	if got := snapshot.picCounts.globalSlotMisses; got != 1 {
		t.Fatalf("global slot misses = %d, want one name resolution", got)
	}
	if got := snapshot.picCounts.globalSlotHits; got < 79 {
		t.Fatalf("global slot hits = %d, want repeated reads to use the resolved slot", got)
	}
}

func TestConcatChainAllocatesOnceForRawOperands(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local left = "hp"
local current = 25
local max = 100
return left .. ":" .. current .. "/" .. max
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if joined := strings.Join(disassembleProto(proto), "\n"); !strings.Contains(joined, "CONCAT_CHAIN") {
		t.Fatalf("compiled concat program is missing CONCAT_CHAIN:\n%s", joined)
	}
	if results, err := Run(proto); err != nil {
		t.Fatalf("warm Run returned error: %v", err)
	} else if got, ok := results[0].String(); !ok || got != "hp:25/100" {
		t.Fatalf("warm Run result is %v (%t), want hp:25/100", results[0], ok)
	}

	allocs := testing.AllocsPerRun(1000, func() {
		results, err := Run(proto)
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if got, ok := results[0].String(); !ok || got != "hp:25/100" {
			t.Fatalf("Run result is %v (%t), want hp:25/100", results[0], ok)
		}
	})
	if allocs > 2 {
		t.Fatalf("raw concat chain allocated %.0f times per run, want result slice plus one final string allocation", allocs)
	}
}

func TestTostringSmallIntegerDoesNotAllocate(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	globals := runtimeGlobals(nil)
	thread := newVMThread(globals)
	restore := thread.activate()
	defer restore()

	if result, err := baseToStringValue(globals, NumberValue(25)); err != nil {
		t.Fatalf("warm baseToStringValue returned error: %v", err)
	} else if got, ok := result.String(); !ok || got != "25" {
		t.Fatalf("warm baseToStringValue result is %v (%t), want 25", result, ok)
	}

	allocs := testing.AllocsPerRun(1000, func() {
		result, err := baseToStringValue(globals, NumberValue(25))
		if err != nil {
			t.Fatalf("baseToStringValue returned error: %v", err)
		}
		if got, ok := result.String(); !ok || got != "25" {
			t.Fatalf("baseToStringValue result is %v (%t), want 25", result, ok)
		}
	})
	if allocs != 0 {
		t.Fatalf("tostring small integer allocated %.0f times, want static formatting and warmed string intern", allocs)
	}
}

func TestLoopTableLiteralAllocationBudget(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local total = 0
for i = 1, 80 do
	local values = {i, i + 1, hp = i + 2, mp = i + 3}
	total = total + values[1] + values[2] + values.hp + values.mp
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if joined := strings.Join(disassembleProto(proto), "\n"); !strings.Contains(joined, "NEW_TABLE") {
		t.Fatalf("compiled loop literal program is missing NEW_TABLE:\n%s", joined)
	}

	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()
	if results, err := thread.runScript(proto, nil, nil); err != nil {
		t.Fatalf("warm thread.runScript returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 13440 {
		t.Fatalf("warm result is %v (%t), want 13440", results[0], ok)
	}

	allocs := testing.AllocsPerRun(100, func() {
		results, err := thread.runScript(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.runScript returned error: %v", err)
		}
		if got, ok := results[0].Number(); !ok || got != 13440 {
			t.Fatalf("thread.runScript result is %v (%t), want 13440", results[0], ok)
		}
	})
	if allocs > 90 {
		t.Fatalf("loop table literals allocated %.0f times per run, want one table allocation per iteration plus run-boundary allocations", allocs)
	}
}

func measuredRunWithGlobalsAllocBytes(t *testing.T, proto *Proto, globals map[string]Value, want float64, runs int) uint64 {
	t.Helper()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < runs; i++ {
		results, err := RunWithGlobals(proto, globals)
		if err != nil {
			t.Fatalf("RunWithGlobals returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != want {
			t.Fatalf("RunWithGlobals result is %v (%t), want number %v", results[0], ok, want)
		}
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / uint64(runs)
}

func TestValueUnsafeLayoutSizeBudget(t *testing.T) {
	if got, want := reflect.TypeOf(Value{}).Size(), uintptr(16); got != want {
		t.Fatalf("unsafe Value size is %d bytes, want exactly %d", got, want)
	}
}

func TestTableHeaderSizeBudget(t *testing.T) {
	if got, want := reflect.TypeOf(Table{}).Size(), uintptr(128); got > want {
		t.Fatalf("Table size is %d bytes, want at most %d", got, want)
	}
}

func TestTableGenericKeyLookupDoesNotAllocate(t *testing.T) {
	table := NewTable()
	key := BoolValue(true)
	if err := table.rawSet(key, NumberValue(42)); err != nil {
		t.Fatalf("rawSet returned error: %v", err)
	}

	var sink Value
	allocs := testing.AllocsPerRun(1000, func() {
		value, err := table.rawGet(key)
		if err != nil {
			t.Fatalf("rawGet returned error: %v", err)
		}
		sink = value
	})
	if allocs != 0 {
		t.Fatalf("generic key lookup allocated %.2f times, want 0", allocs)
	}
	if got, ok := sink.Number(); !ok || got != 42 {
		t.Fatalf("generic key lookup result = %v (%t), want 42", got, ok)
	}
}

func TestValueUnsafeAccessorsRoundTripAllKinds(t *testing.T) {
	TestValueRoundTripsAllKinds(t)
}

func TestValueUnsafeLayoutMatchesSafeSemantics(t *testing.T) {
	table := NewTable()
	userdata := NewUserData("payload")
	proto := newProto(nil, []instruction{{op: opReturn}}, nil, nil, 0, 0, false)
	closureValue := functionValue(proto, nil)
	hostValue := HostFuncValue(func(args []Value) ([]Value, error) { return args, nil })

	if got, ok := TableValue(table).Table(); !ok || got != table {
		t.Fatalf("unsafe table accessor = %p, %t; want %p, true", got, ok, table)
	}
	if got, ok := UserDataValue(userdata).UserData(); !ok || got != userdata {
		t.Fatalf("unsafe userdata accessor = %p, %t; want %p, true", got, ok, userdata)
	}
	if got, ok := closureValue.scriptFunction(); !ok || got == nil || got.proto != proto {
		t.Fatalf("unsafe closure accessor = %#v, %t; want closure for proto", got, ok)
	}
	if got, ok := hostValue.hostFunction(); !ok || got == nil {
		t.Fatalf("unsafe host accessor = %v, %t; want host function", got, ok)
	}
}

func TestSmallTableStringFieldsUseInlineStorage(t *testing.T) {
	var sink *Table
	allocs := testing.AllocsPerRun(1000, func() {
		table := newTableWithCapacity(0, 0)
		table.setRawStringField("a", NumberValue(1))
		table.setRawStringField("b", NumberValue(2))
		sink = table
	})
	if allocs > 1 {
		t.Fatalf("small table with inline string fields allocated %.2f times, want only table allocation", allocs)
	}
	if sink == nil {
		t.Fatal("sink table is nil")
	}
	if sink.hasStringOverflow() {
		t.Fatal("small table used string field map, want inline string fields")
	}
	const wantInlineStringFieldCapacity = 2
	if got := cap(sink.stringFields); got != wantInlineStringFieldCapacity {
		t.Fatalf("small table inline string field capacity = %d, want %d", got, wantInlineStringFieldCapacity)
	}
}

func TestBytecodeFinalizerReturnsVerifiedProto(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(0, NumberValue(2))
	builder.emit(instruction{op: opReturn, a: 0, b: 1})

	proto, err := builder.finalizeProto(nil, 1, 0, false)
	if err != nil {
		t.Fatalf("finalizeProto returned error: %v", err)
	}
	if proto.verifyErr != nil {
		t.Fatalf("finalized proto has verifyErr %v, want nil", proto.verifyErr)
	}
}

func TestExecutionArtifactFinalizerRebuildsDerivedProtoFacts(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(0, NumberValue(2))
	builder.emit(instruction{op: opReturnOne, a: 0})
	proto := builder.proto(nil, 1, 0, false)

	proto.constantKeys = nil
	proto.constantKeyOK = nil
	proto.constantNumbers = nil
	proto.constantNumberOK = nil
	proto.capturedLocals = []bool{true}
	proto.entryNilRegisters = []int{99}
	proto.verifyErr = fmt.Errorf("stale")

	if err := finalizeProtoExecutionArtifact(proto); err != nil {
		t.Fatalf("finalizeProtoExecutionArtifact returned error: %v", err)
	}
	if proto.verifyErr != nil {
		t.Fatalf("finalized proto verifyErr = %v, want nil", proto.verifyErr)
	}
	if proto.constantKeys == nil || proto.constantKeyOK == nil {
		t.Fatal("finalized proto did not rebuild constant key facts")
	}
	if proto.constantNumbers == nil || proto.constantNumberOK == nil {
		t.Fatal("finalized proto did not rebuild constant number facts")
	}
	if len(proto.capturedLocals) != 0 {
		t.Fatalf("capturedLocals = %#v, want rebuilt empty facts", proto.capturedLocals)
	}
	if len(proto.entryNilRegisters) != 0 {
		t.Fatalf("entryNilRegisters = %#v, want rebuilt empty facts", proto.entryNilRegisters)
	}
}

func TestBytecodeFinalizerRejectsInvalidCompilerProto(t *testing.T) {
	var builder bytecodeBuilder
	builder.emit(instruction{op: opJump, b: 99})

	proto, err := builder.finalizeProto(nil, 1, 0, false)
	if err == nil {
		t.Fatal("finalizeProto succeeded, want invalid finalized prototype error")
	}
	if proto != nil {
		t.Fatalf("finalizeProto returned proto %#v, want nil", proto)
	}
	if !strings.Contains(err.Error(), "invalid finalized prototype") {
		t.Fatalf("finalizeProto error is %q, want invalid finalized prototype", err)
	}
	if !strings.Contains(err.Error(), "jump target 99 out of range") {
		t.Fatalf("finalizeProto error is %q, want jump target detail", err)
	}
}

func TestBytecodeFinalizerRejectsNonStringGlobalName(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(0, NumberValue(1))
	builder.emit(instruction{op: opLoadGlobal, a: 0, b: 0})

	_, err := builder.finalizeProto(nil, 1, 0, false)
	if err == nil {
		t.Fatal("finalizeProto succeeded, want non-string global name error")
	}
	if !strings.Contains(err.Error(), "invalid finalized prototype") {
		t.Fatalf("finalizeProto error is %q, want invalid finalized prototype", err)
	}
	if !strings.Contains(err.Error(), "constant index 0 is number, want string") {
		t.Fatalf("finalizeProto error is %q, want non-string global detail", err)
	}
}

func TestBytecodeFinalizerRejectsInvalidFieldConstantOperand(t *testing.T) {
	var builder bytecodeBuilder
	builder.emit(instruction{op: opNewTable, a: 0})
	builder.emitLoadConst(1, NumberValue(2))
	builder.emit(instruction{op: opSetField, a: 0, b: 99, c: 1})

	_, err := builder.finalizeProto(nil, 2, 0, false)
	if err == nil {
		t.Fatal("finalizeProto succeeded, want invalid field constant operand error")
	}
	if !strings.Contains(err.Error(), "invalid finalized prototype") {
		t.Fatalf("finalizeProto error is %q, want invalid finalized prototype", err)
	}
	if !strings.Contains(err.Error(), "constant index 99 out of range") {
		t.Fatalf("finalizeProto error is %q, want constant range detail", err)
	}
}

func TestBytecodeFinalizerRejectsInvalidStringFieldNumericBranchConstants(t *testing.T) {
	t.Run("field", func(t *testing.T) {
		var builder bytecodeBuilder
		field := builder.addConstant(NumberValue(1))
		value := builder.addConstant(NumberValue(0))
		builder.emit(instruction{op: opGetStringField, a: 1, b: 0, c: field})
		builder.emit(instruction{op: opJumpIfNotGreaterK, a: 1, b: value, d: 2})

		_, err := builder.finalizeProto(nil, 2, 0, false)
		if err == nil {
			t.Fatal("finalizeProto succeeded, want non-string field error")
		}
		if !strings.Contains(err.Error(), "constant index 0 is number, want string") {
			t.Fatalf("finalizeProto error is %q, want non-string field detail", err)
		}
	})

}

func TestBytecodeFinalizerRejectsInvalidCanonicalFieldConstant(t *testing.T) {
	var builder bytecodeBuilder
	field := builder.addConstant(NumberValue(1))
	builder.emit(instruction{op: opSetStringField, a: 0, b: field, c: 1})

	_, err := builder.finalizeProto(nil, 2, 0, false)
	if err == nil {
		t.Fatal("finalizeProto succeeded, want non-string field error")
	}
	if !strings.Contains(err.Error(), "constant index 0 is number, want string") {
		t.Fatalf("finalizeProto error is %q, want non-string field detail", err)
	}
}

func TestBytecodeFinalizerRejectsInvalidArithmeticRegister(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(0, NumberValue(1))
	builder.emit(instruction{op: opAdd, a: 0, b: 0, c: 99})

	_, err := builder.finalizeProto(nil, 1, 0, false)
	if err == nil {
		t.Fatal("finalizeProto succeeded, want invalid arithmetic register error")
	}
	if !strings.Contains(err.Error(), "invalid finalized prototype") {
		t.Fatalf("finalizeProto error is %q, want invalid finalized prototype", err)
	}
	if !strings.Contains(err.Error(), "register index 99 out of range") {
		t.Fatalf("finalizeProto error is %q, want register range detail", err)
	}
}

func TestBytecodeFinalizerRejectsInvalidCallArgumentSpan(t *testing.T) {
	var builder bytecodeBuilder
	builder.emit(instruction{op: opCall, a: 0, b: 1, c: 1, d: 1})

	_, err := builder.finalizeProto(nil, 2, 0, false)
	if err == nil {
		t.Fatal("finalizeProto succeeded, want invalid call argument span error")
	}
	if !strings.Contains(err.Error(), "invalid finalized prototype") {
		t.Fatalf("finalizeProto error is %q, want invalid finalized prototype", err)
	}
	if !strings.Contains(err.Error(), "call argument register range out of range") {
		t.Fatalf("finalizeProto error is %q, want call argument range detail", err)
	}
}

func TestCallValueNativeDoesNotAllocateCycleMap(t *testing.T) {
	fn := nativeFuncValue(func(_ *globalEnv, _ []Value) ([]Value, error) {
		return nil, nil
	})

	allocs := testing.AllocsPerRun(100, func() {
		if _, err := callValue(fn, nil, nil); err != nil {
			t.Fatalf("callValue returned error: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("native call allocated %.0f times, want no cycle-map allocation", allocs)
	}
}

func TestMetatableWalkCommonCaseDoesNotAllocate(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	fallback := NewTable()
	if err := fallback.Set(StringValue("hp"), NumberValue(25)); err != nil {
		t.Fatalf("fallback.Set returned error: %v", err)
	}
	index := NewTable()
	if err := index.Set(StringValue("__index"), TableValue(fallback)); err != nil {
		t.Fatalf("index.Set returned error: %v", err)
	}
	object := NewTable()
	object.setMetatable(index)

	access := publicTableAccess()
	key := StringValue("hp")
	allocs := testing.AllocsPerRun(100, func() {
		value, err := access.get(object, key)
		if err != nil {
			t.Fatalf("table access returned error: %v", err)
		}
		got, ok := value.Number()
		if !ok || got != 25 {
			t.Fatalf("table access returned %v (%t), want number 25", value, ok)
		}
	})
	if allocs != 0 {
		t.Fatalf("metatable walk allocated %.0f times, want no common-case allocation", allocs)
	}
}

func TestMetatableWalkStillRejectsCycles(t *testing.T) {
	left := NewTable()
	right := NewTable()
	leftMeta := NewTable()
	rightMeta := NewTable()
	if err := leftMeta.Set(StringValue("__index"), TableValue(right)); err != nil {
		t.Fatalf("leftMeta.Set returned error: %v", err)
	}
	if err := rightMeta.Set(StringValue("__index"), TableValue(left)); err != nil {
		t.Fatalf("rightMeta.Set returned error: %v", err)
	}
	left.setMetatable(leftMeta)
	right.setMetatable(rightMeta)

	_, err := publicTableAccess().get(left, StringValue("missing"))
	if err == nil {
		t.Fatal("table access succeeded, want cyclic __index error")
	}
	if !strings.Contains(err.Error(), "cyclic __index chain") {
		t.Fatalf("table access error is %q, want cyclic __index detail", err)
	}
}

func TestFunctionIndexFallbackResolvesOncePerShape(t *testing.T) {
	first := nativeFuncValueWithID(baseToString, nativeFuncToString)
	second := nativeFuncValueWithID(baseRawLenNative, nativeFuncRawLen)
	metatable := NewTable()
	metatable.setRawStringField("__index", first)
	object := NewTable()
	object.setMetatable(metatable)

	index, ok, err := object.cachedIndexFallback()
	if err != nil {
		t.Fatalf("cachedIndexFallback returned error: %v", err)
	}
	if !ok || valueNativeID(index) != nativeFuncToString {
		t.Fatalf("cachedIndexFallback = %#v (%t), want first function", index, ok)
	}
	index, ok, err = object.cachedIndexFallback()
	if err != nil {
		t.Fatalf("cachedIndexFallback second call returned error: %v", err)
	}
	if !ok || valueNativeID(index) != nativeFuncToString {
		t.Fatalf("cachedIndexFallback second call = %#v (%t), want cached first function", index, ok)
	}

	metatable.setRawStringField("__index", second)
	index, ok, err = object.cachedIndexFallback()
	if err != nil {
		t.Fatalf("cachedIndexFallback after mutation returned error: %v", err)
	}
	if !ok || valueNativeID(index) != nativeFuncRawLen {
		t.Fatalf("cachedIndexFallback after mutation = %#v (%t), want refreshed second function", index, ok)
	}
}

func TestNewindexFallbackChainMatchesLuauOrder(t *testing.T) {
	proto, err := Compile(`
local log = {}
local root = {}
local middle = {}
setmetatable(root, {__newindex = middle})
setmetatable(middle, {__newindex = function(self, key, value)
	log[#log + 1] = self == middle
	log[#log + 1] = key
	log[#log + 1] = value
end})

root.hp = 25
return log[1], log[2], log[3], rawget(root, "hp"), rawget(middle, "hp")
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("Run returned %d results, want 5", len(results))
	}
	if got, ok := results[0].Bool(); !ok || !got {
		t.Fatalf("first result is %v (%t), want true", results[0], ok)
	}
	if got, ok := results[1].String(); !ok || got != "hp" {
		t.Fatalf("second result is %v (%t), want hp", results[1], ok)
	}
	if got, ok := results[2].Number(); !ok || got != 25 {
		t.Fatalf("third result is %v (%t), want 25", results[2], ok)
	}
	if !results[3].IsNil() {
		t.Fatalf("fourth result is %s, want nil", results[3].Kind())
	}
	if !results[4].IsNil() {
		t.Fatalf("fifth result is %s, want nil", results[4].Kind())
	}
}

func TestBytecodeFinalizerRejectsInvalidClosureUpvalue(t *testing.T) {
	child := newProto(
		nil,
		[]instruction{{op: opReturn, a: 0, b: 1}},
		nil,
		[]upvalueDesc{{local: true, index: 2}},
		1,
		0,
		false,
	)
	var builder bytecodeBuilder
	prototype := builder.addPrototype(child)
	builder.emit(instruction{op: opClosure, a: 0, b: prototype})
	builder.emit(instruction{op: opReturn, a: 0, b: 1})

	_, err := builder.finalizeProto(nil, 1, 0, false)
	if err == nil {
		t.Fatal("finalizeProto succeeded, want invalid closure upvalue error")
	}
	if !strings.Contains(err.Error(), "invalid finalized prototype") {
		t.Fatalf("finalizeProto error is %q, want invalid finalized prototype", err)
	}
	if !strings.Contains(err.Error(), "upvalue 0 local register index 2 out of range") {
		t.Fatalf("finalizeProto error is %q, want closure upvalue range detail", err)
	}
}

func TestBytecodeVerifierRejectsStaleEntryNilRegisters(t *testing.T) {
	proto := newProto(
		nil,
		[]instruction{{op: opReturnOne, a: 1}},
		nil,
		nil,
		2,
		0,
		false,
	)
	proto.entryNilRegisters = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale entry nil register error")
	}
	if !strings.Contains(err.Error(), "entry nil registers [] do not match finalized plan [1]") {
		t.Fatalf("verifyProto error is %q, want entry nil register detail", err)
	}
}

func TestRunDirectFrameArrayNextJumpUsesInlineArrayIterator(t *testing.T) {
	proto, err := Compile(`
local values = {1, 2, 3, 4}
local total = 0
for _, value in values do
	total = total + value * 2 + value % 2
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameInstrumented = true
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 22 {
		t.Fatalf("thread.run result is %v (%t), want 22", got, ok)
	}
	if counts.arrayIteratorFastSteps == 0 {
		t.Fatalf("array iterator fast steps = 0, want direct array iterator handling")
	}
}

func TestRunDirectFrameArrayRowLoopMutationSideExitsBeforeMismatchedSlot(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{cooldown = 2},
	{other = 99, cooldown = 3},
	{cooldown = 1},
}
local total = 0
for _, row in rows do
	if row.cooldown > 0 then
		row.cooldown = row.cooldown - 1
	end
	total = total + row.cooldown
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameInstrumented = true
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 3 {
		t.Fatalf("thread.run result is %v (%t), want 3", got, ok)
	}
}

func TestVMFrameAllocatesCellsOnlyForCapturedLocals(t *testing.T) {
	child := newProto(
		nil,
		[]instruction{{op: opReturn, a: 0, b: 1}},
		nil,
		[]upvalueDesc{{local: true, index: 1}},
		1,
		0,
		false,
	)
	proto := newProto(
		nil,
		[]instruction{
			{op: opClosure, a: 2, b: 0},
			{op: opReturn, a: 0, b: 1},
		},
		[]*Proto{child},
		nil,
		3,
		0,
		false,
	)

	frame := newVMFrame(proto, []Value{NumberValue(7)}, nil)
	if got, want := len(frame.registers), 3; got != want {
		t.Fatalf("frame has %d value registers, want %d", got, want)
	}
	if got, want := len(frame.cells), 3; got != want {
		t.Fatalf("frame has %d capture cell slots, want %d", got, want)
	}
	if frame.cells[0] != nil {
		t.Fatalf("register 0 has cell %#v, want ordinary value slot", frame.cells[0])
	}
	if frame.cells[1] == nil {
		t.Fatal("register 1 has nil cell, want captured local cell")
	}
	if frame.cells[2] != nil {
		t.Fatalf("register 2 has cell %#v, want ordinary value slot", frame.cells[2])
	}

	frame.setRegister(1, NumberValue(9))
	got, ok := frame.cells[1].get().Number()
	if !ok || got != 9 {
		t.Fatalf("captured register cell is %v (%t), want number 9", got, ok)
	}
}

func TestVMFrameAppliesDirectFixedResultDestinations(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturn, a: 0, b: 1}}, nil, nil, 3, 0, false)
	frame := newVMFrame(proto, nil, nil)

	frame.applyResultDestination(vmResultDestination{register: 1, count: 2}, []Value{NumberValue(7)})
	first, firstOK := frame.registers[1].Number()
	if !firstOK || first != 7 {
		t.Fatalf("first fixed result is %v (%t), want number 7", first, firstOK)
	}
	if !frame.registers[2].IsNil() {
		t.Fatalf("second fixed result is %s, want nil padding", frame.registers[2].Kind())
	}

	frame.applyInlineResultDestination(
		vmResultDestination{register: 0, count: 1},
		[2]Value{NumberValue(11), NumberValue(13)},
		0,
	)
	if !frame.registers[0].IsNil() {
		t.Fatalf("zero inline result is %s, want nil padding", frame.registers[0].Kind())
	}
}

func TestVMFrameBorrowsVarargArgumentWindow(t *testing.T) {
	proto := newProto(
		nil,
		[]instruction{{op: opReturn, a: 0, b: 1}},
		nil,
		nil,
		1,
		1,
		true,
	)
	args := []Value{StringValue("head"), NumberValue(1), NumberValue(2)}

	frame := newVMFrame(proto, args, nil)
	args[1] = NumberValue(99)

	got, ok := frame.varargs[0].Number()
	if !ok || got != 99 {
		t.Fatalf("vararg frame copied argument value %v (%t), want borrowed number 99", got, ok)
	}
}

func TestRunVarargWindowPreservesNilFillAndCount(t *testing.T) {
	proto, err := Compile(`
local function collect(...)
	local a, b, c, d = ...
	return a, b, c, d, select("#", ...)
end
return collect(1, nil, 3)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 1 {
		t.Fatalf("first result is %v (%t), want number 1", got, ok)
	}
	if !results[1].IsNil() {
		t.Fatalf("second result is %s, want nil", results[1].Kind())
	}
	if got, ok := results[2].Number(); !ok || got != 3 {
		t.Fatalf("third result is %v (%t), want number 3", got, ok)
	}
	if !results[3].IsNil() {
		t.Fatalf("fourth result is %s, want nil fill", results[3].Kind())
	}
	if got, ok := results[4].Number(); !ok || got != 3 {
		t.Fatalf("fifth result is %v (%t), want vararg count 3", got, ok)
	}
}

func TestBaseLibraryTablesPreallocateInlineStringFields(t *testing.T) {
	tests := []struct {
		name  string
		table *Table
		want  int
	}{
		{name: "math", table: baseMath(), want: 5},
		{name: "table", table: baseTable(), want: 8},
		{name: "coroutine", table: baseCoroutine(), want: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.table.hasStringOverflow() {
				t.Fatalf("%s base table used string field map, want inline fields", tt.name)
			}
			if got := len(tt.table.stringFields); got != tt.want {
				t.Fatalf("%s base table has %d string fields, want %d", tt.name, got, tt.want)
			}
			if got := cap(tt.table.stringFields); got != tt.want {
				t.Fatalf("%s base table string field capacity is %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestTableInlineStringFieldSlotsAreLayoutVersionGuarded(t *testing.T) {
	table := NewTable()
	table.setRawStringField("hp", NumberValue(10))
	table.setRawStringField("regen", NumberValue(2))

	hpSlot, ok := table.rawStringFieldSlot("hp")
	if !ok {
		t.Fatal("rawStringFieldSlot(hp) failed, want inline slot")
	}
	regenSlot, ok := table.rawStringFieldSlot("regen")
	if !ok {
		t.Fatal("rawStringFieldSlot(regen) failed, want inline slot")
	}

	hp, ok := table.rawStringFieldAtSlot(hpSlot, "hp")
	if !ok {
		t.Fatal("rawStringFieldAtSlot(hp) failed, want value")
	}
	if got, ok := hp.Number(); !ok || got != 10 {
		t.Fatalf("hp slot value is %v (%t), want number 10", hp, ok)
	}
	regen, ok := table.rawStringFieldAtSlot(regenSlot, "regen")
	if !ok {
		t.Fatal("rawStringFieldAtSlot(regen) failed, want value")
	}
	if got, ok := regen.Number(); !ok || got != 2 {
		t.Fatalf("regen slot value is %v (%t), want number 2", regen, ok)
	}

	if !table.setRawStringFieldAtSlot(hpSlot, "hp", NumberValue(9)) {
		t.Fatal("setRawStringFieldAtSlot(hp) failed, want guarded update")
	}
	if _, ok := table.rawStringFieldAtSlot(regenSlot, "regen"); !ok {
		t.Fatal("rawStringFieldAtSlot(regen) failed after value-only hp update")
	}
	updated, ok := table.rawStringField("hp")
	if !ok {
		t.Fatal("rawStringField(hp) failed after slot update")
	}
	if got, ok := updated.Number(); !ok || got != 9 {
		t.Fatalf("updated hp is %v (%t), want number 9", updated, ok)
	}
	table.setRawStringField("hp", NilValue())
	if _, ok := table.rawStringFieldAtSlot(regenSlot, "regen"); ok {
		t.Fatal("rawStringFieldAtSlot(regen) used stale slot after layout change")
	}
}

func TestTableMapStringFieldSlotsAreLayoutVersionGuarded(t *testing.T) {
	table := NewTable()
	for i := 0; i < maxInlineStringFields; i++ {
		key := fmt.Sprintf("field%d", i)
		table.setRawStringField(key, NumberValue(float64(len(key))))
	}
	for _, key := range []string{"target"} {
		table.setRawStringField(key, NumberValue(float64(len(key))))
	}
	if !table.hasStringOverflow() {
		t.Fatal("table did not promote to string field map, want map-backed slot coverage")
	}

	slot, ok := table.rawStringFieldSlot("target")
	if !ok {
		t.Fatal("rawStringFieldSlot(target) failed, want map-backed slot")
	}
	value, ok := table.rawStringFieldAtSlot(slot, "target")
	if !ok {
		t.Fatal("rawStringFieldAtSlot(target) failed, want map-backed value")
	}
	if got, ok := value.Number(); !ok || got != 6 {
		t.Fatalf("target slot value is %v (%t), want number 6", value, ok)
	}
	if !table.setRawStringFieldAtSlot(slot, "target", NumberValue(42)) {
		t.Fatal("setRawStringFieldAtSlot(target) failed, want guarded map update")
	}
	updated, ok := table.rawStringField("target")
	if !ok {
		t.Fatal("rawStringField(target) failed after map slot update")
	}
	if got, ok := updated.Number(); !ok || got != 42 {
		t.Fatalf("updated target is %v (%t), want number 42", updated, ok)
	}

	table.setRawStringField("field0", NumberValue(100))
	if _, ok := table.rawStringFieldAtSlot(slot, "target"); !ok {
		t.Fatal("rawStringFieldAtSlot(target) failed after unrelated map value update")
	}
	table.setRawStringField("target", NilValue())
	if _, ok := table.rawStringFieldAtSlot(slot, "target"); ok {
		t.Fatal("rawStringFieldAtSlot(target) used stale map slot after delete")
	}
}

func TestTableShapeTokenSplitsStringLayoutAndValueChanges(t *testing.T) {
	table := NewTable()
	table.setRawStringField("hp", NumberValue(10))

	initial := table.shapeToken()
	table.setRawStringField("hp", NumberValue(9))
	valueUpdate := table.shapeToken()
	if !initial.sameStringLayout(valueUpdate) {
		t.Fatalf("string layout token changed after value update: %#v -> %#v", initial, valueUpdate)
	}
	if initial.sameStringValues(valueUpdate) {
		t.Fatalf("string value token did not change after value update: %#v", valueUpdate)
	}

	table.setRawStringField("hp", NilValue())
	deleted := table.shapeToken()
	if valueUpdate.sameStringLayout(deleted) {
		t.Fatalf("string layout token did not change after delete: %#v", deleted)
	}
}

func TestTableShapeTokenKeepsUnrelatedEpochsIndependent(t *testing.T) {
	table := NewTable()
	table.setRawStringField("name", StringValue("ember"))
	if err := table.rawSet(NumberValue(1), NumberValue(10)); err != nil {
		t.Fatalf("rawSet array seed returned error: %v", err)
	}
	if err := table.rawSet(BoolValue(true), StringValue("generic")); err != nil {
		t.Fatalf("rawSet generic seed returned error: %v", err)
	}

	initial := table.shapeToken()
	if err := table.rawSet(NumberValue(1), NumberValue(11)); err != nil {
		t.Fatalf("rawSet array update returned error: %v", err)
	}
	arrayUpdated := table.shapeToken()
	if !initial.sameStringLayout(arrayUpdated) {
		t.Fatalf("string layout token changed after array value update: %#v -> %#v", initial, arrayUpdated)
	}
	if !initial.sameStringValues(arrayUpdated) {
		t.Fatalf("string value token changed after array value update: %#v -> %#v", initial, arrayUpdated)
	}
	if initial.sameArrayValues(arrayUpdated) {
		t.Fatalf("array value token did not change after array update: %#v", arrayUpdated)
	}

	table.setRawStringField("name", StringValue("codex"))
	stringUpdated := table.shapeToken()
	if !arrayUpdated.sameArrayLayout(stringUpdated) {
		t.Fatalf("array layout token changed after string value update: %#v -> %#v", arrayUpdated, stringUpdated)
	}
	if !arrayUpdated.sameArrayValues(stringUpdated) {
		t.Fatalf("array value token changed after string value update: %#v -> %#v", arrayUpdated, stringUpdated)
	}
	if !arrayUpdated.sameGenericLayout(stringUpdated) {
		t.Fatalf("generic layout token changed after string value update: %#v -> %#v", arrayUpdated, stringUpdated)
	}
	if !arrayUpdated.sameGenericValues(stringUpdated) {
		t.Fatalf("generic value token changed after string value update: %#v -> %#v", arrayUpdated, stringUpdated)
	}

	if err := table.rawSet(BoolValue(true), StringValue("changed")); err != nil {
		t.Fatalf("rawSet generic update returned error: %v", err)
	}
	genericUpdated := table.shapeToken()
	if !stringUpdated.sameStringLayout(genericUpdated) {
		t.Fatalf("string layout token changed after generic value update: %#v -> %#v", stringUpdated, genericUpdated)
	}
	if !stringUpdated.sameStringValues(genericUpdated) {
		t.Fatalf("string value token changed after generic value update: %#v -> %#v", stringUpdated, genericUpdated)
	}
	if stringUpdated.sameGenericValues(genericUpdated) {
		t.Fatalf("generic value token did not change after generic update: %#v", genericUpdated)
	}
}

func TestTableShapeTokenTracksMutationVersionRules(t *testing.T) {
	table := NewTable()
	table.setRawStringField("a", NumberValue(1))
	table.setRawStringField("b", NumberValue(2))
	table.setRawStringField("c", NumberValue(3))
	table.setRawStringField("d", NumberValue(4))
	table.setRawStringField("e", NumberValue(5))
	table.setRawStringField("f", NumberValue(6))
	table.setRawStringField("g", NumberValue(7))
	table.setRawStringField("h", NumberValue(8))
	inline := table.shapeToken()
	table.setRawStringField("i", NumberValue(9))
	promoted := table.shapeToken()
	if inline.sameStringLayout(promoted) {
		t.Fatalf("string layout token did not change after string-map promotion: %#v", promoted)
	}

	if err := table.rawSet(NumberValue(1), NumberValue(10)); err != nil {
		t.Fatalf("rawSet array append returned error: %v", err)
	}
	arrayFilled := table.shapeToken()
	if promoted.sameArrayLayout(arrayFilled) {
		t.Fatalf("array layout token did not change after array append: %#v", arrayFilled)
	}
	if promoted.sameArrayValues(arrayFilled) {
		t.Fatalf("array value token did not change after array append: %#v", arrayFilled)
	}
	if err := table.rawSet(NumberValue(1), NumberValue(11)); err != nil {
		t.Fatalf("rawSet array value returned error: %v", err)
	}
	arrayValueUpdate := table.shapeToken()
	if !arrayFilled.sameArrayLayout(arrayValueUpdate) {
		t.Fatalf("array layout token changed after value-only update: %#v -> %#v", arrayFilled, arrayValueUpdate)
	}
	if arrayFilled.sameArrayValues(arrayValueUpdate) {
		t.Fatalf("array value token did not change after value update: %#v", arrayValueUpdate)
	}
	if err := table.rawSet(NumberValue(1), NilValue()); err != nil {
		t.Fatalf("rawSet array delete returned error: %v", err)
	}
	arrayDeleted := table.shapeToken()
	if arrayValueUpdate.sameArrayLayout(arrayDeleted) {
		t.Fatalf("array layout token did not change after array hole/delete: %#v", arrayDeleted)
	}

	genericKey := BoolValue(true)
	if err := table.rawSet(genericKey, StringValue("yes")); err != nil {
		t.Fatalf("rawSet generic add returned error: %v", err)
	}
	genericAdded := table.shapeToken()
	if arrayDeleted.sameGenericLayout(genericAdded) {
		t.Fatalf("generic layout token did not change after add: %#v", genericAdded)
	}
	if err := table.rawSet(genericKey, StringValue("still")); err != nil {
		t.Fatalf("rawSet generic update returned error: %v", err)
	}
	genericUpdated := table.shapeToken()
	if !genericAdded.sameGenericLayout(genericUpdated) {
		t.Fatalf("generic layout token changed after value-only update: %#v -> %#v", genericAdded, genericUpdated)
	}
	if genericAdded.sameGenericValues(genericUpdated) {
		t.Fatalf("generic value token did not change after value update: %#v", genericUpdated)
	}
	if err := table.rawSet(genericKey, NilValue()); err != nil {
		t.Fatalf("rawSet generic delete returned error: %v", err)
	}
	genericDeleted := table.shapeToken()
	if genericUpdated.sameGenericLayout(genericDeleted) {
		t.Fatalf("generic layout token did not change after delete: %#v", genericDeleted)
	}

	metatable := NewTable()
	table.setMetatable(metatable)
	withMetatable := table.shapeToken()
	if genericDeleted.sameMetatable(withMetatable) {
		t.Fatalf("metatable token did not change after set: %#v", withMetatable)
	}
	table.setMetatable(nil)
	withoutMetatable := table.shapeToken()
	if withMetatable.sameMetatable(withoutMetatable) {
		t.Fatalf("metatable token did not change after clear: %#v", withoutMetatable)
	}
}

func TestTableRawHelpersPreserveStorageSemantics(t *testing.T) {
	table := NewTable()
	if err := table.rawSet(NumberValue(1), StringValue("first")); err != nil {
		t.Fatalf("rawSet array value returned error: %v", err)
	}
	if err := table.rawSet(BoolValue(true), StringValue("generic")); err != nil {
		t.Fatalf("rawSet generic value returned error: %v", err)
	}
	table.setRawStringField("name", StringValue("ember"))

	arrayValue, ok := table.rawArrayValue(1)
	if !ok {
		t.Fatal("rawArrayValue failed, want first array value")
	}
	if got, ok := arrayValue.String(); !ok || got != "first" {
		t.Fatalf("rawArrayValue is %q (%t), want first", got, ok)
	}
	if _, ok := table.rawArrayValue(2); ok {
		t.Fatal("rawArrayValue found missing array value")
	}

	genericValue, ok := table.rawGenericField(tableKey{kind: BoolKind, bool: true})
	if !ok {
		t.Fatal("rawGenericField failed, want generic value")
	}
	if got, ok := genericValue.String(); !ok || got != "generic" {
		t.Fatalf("rawGenericField is %q (%t), want generic", got, ok)
	}
	if _, ok := table.rawGenericField(tableKey{kind: BoolKind, bool: false}); ok {
		t.Fatal("rawGenericField found missing generic key")
	}

	slot, ok := table.rawStringFieldSlot("name")
	if !ok {
		t.Fatal("rawStringFieldSlot failed, want inline string slot")
	}
	name, ok := table.rawStringFieldAtIndex(slot.index, "name")
	if !ok {
		t.Fatal("rawStringFieldAtIndex failed, want string field")
	}
	if got, ok := name.String(); !ok || got != "ember" {
		t.Fatalf("rawStringFieldAtIndex is %q (%t), want ember", got, ok)
	}
	table.setRawStringField("name", NilValue())
	if _, ok := table.rawStringFieldAtIndex(slot.index, "name"); ok {
		t.Fatal("rawStringFieldAtIndex used stale string slot after delete")
	}
}

func TestTableRowStringSlotReferenceFallsBackThroughShapeChanges(t *testing.T) {
	table := NewTable()
	table.setRawStringField("drop", NumberValue(1))
	table.setRawStringField("keep", NumberValue(7))
	slot, ok := table.rawStringFieldSlot("keep")
	if !ok {
		t.Fatal("rawStringFieldSlot(keep) failed, want inline slot")
	}
	ref := rowStringFieldSlotRef{index: slot.index}

	value, ok := table.rawRowStringField(ref, "keep")
	if !ok {
		t.Fatal("rawRowStringField failed, want slot-backed value")
	}
	if got, ok := value.Number(); !ok || got != 7 {
		t.Fatalf("rawRowStringField is %v (%t), want number 7", got, ok)
	}

	table.setRawStringField("drop", NilValue())
	value, ok = table.rawRowStringField(ref, "keep")
	if !ok {
		t.Fatal("rawRowStringField failed after layout change, want key fallback")
	}
	if got, ok := value.Number(); !ok || got != 7 {
		t.Fatalf("rawRowStringField after layout change is %v (%t), want number 7", got, ok)
	}

	table.setRawRowStringField(ref, "keep", NumberValue(9))
	value, ok = table.rawStringField("keep")
	if !ok {
		t.Fatal("rawStringField(keep) failed after row slot write")
	}
	if got, ok := value.Number(); !ok || got != 9 {
		t.Fatalf("row slot write stored %v (%t), want number 9", got, ok)
	}
}

func TestDynamicStringIndexCacheRetainsFourStringKeys(t *testing.T) {
	table := NewTable()
	for index, key := range []string{"wood", "ore", "herb", "gem"} {
		table.setRawStringField(key, NumberValue(float64(index+1)))
	}
	var cache dynamicStringIndexCache
	for _, key := range []string{"wood", "ore", "herb", "gem"} {
		slot, ok := table.rawStringFieldSlot(key)
		if !ok {
			t.Fatalf("rawStringFieldSlot(%s) failed, want inline slot", key)
		}
		cache.store(table, key, slot)
	}
	for index, key := range []string{"wood", "ore", "herb", "gem"} {
		value, ok := cache.get(table, key)
		if !ok {
			t.Fatalf("cache.get(%s) missed, want finite-key PIC hit", key)
		}
		if got, ok := value.Number(); !ok || got != float64(index+1) {
			t.Fatalf("cache.get(%s) = %v (%t), want number %d", key, got, ok, index+1)
		}
	}
}

func TestStringFieldSymbolCacheFallsBackForDynamicKeys(t *testing.T) {
	table := NewTable()
	table.setRawStringField("wood", NumberValue(4))
	slot, ok := table.rawStringFieldSlot("wood")
	if !ok {
		t.Fatal("rawStringFieldSlot(wood) failed, want inline slot")
	}

	var cache dynamicStringIndexCache
	cache.storeSymbol(table, "wood", 0, slot)

	var counts directFramePICCounts
	value, ok := cache.getSymbolCounted(table, "wood", 99, &counts)
	if !ok {
		t.Fatal("symbol cache missed dynamic key, want string fallback hit")
	}
	if got, ok := value.Number(); !ok || got != 4 {
		t.Fatalf("cache.getSymbolCounted(wood) = %v (%t), want number 4", value, ok)
	}
	if !cache.writeSymbolCounted(table, "wood", 99, NumberValue(8), &counts) {
		t.Fatal("symbol cache write missed dynamic key, want string fallback hit")
	}
	updated, ok := table.rawStringField("wood")
	if !ok {
		t.Fatal("rawStringField(wood) missing after dynamic fallback write")
	}
	if got, ok := updated.Number(); !ok || got != 8 {
		t.Fatalf("rawStringField(wood) = %v (%t), want number 8", updated, ok)
	}
}

func TestDynamicStringIndexCacheEvictsAndRejectsStaleShapes(t *testing.T) {
	table := NewTable()
	for index, key := range []string{"wood", "ore", "herb", "gem", "coin"} {
		table.setRawStringField(key, NumberValue(float64(index+1)))
	}

	var cache dynamicStringIndexCache
	for _, key := range []string{"wood", "ore", "herb", "gem"} {
		slot, ok := table.rawStringFieldSlot(key)
		if !ok {
			t.Fatalf("rawStringFieldSlot(%s) failed, want inline slot", key)
		}
		cache.store(table, key, slot)
	}
	slot, ok := table.rawStringFieldSlot("coin")
	if !ok {
		t.Fatal("rawStringFieldSlot(coin) failed, want inline slot")
	}
	cache.store(table, "coin", slot)
	if _, ok := cache.get(table, "wood"); ok {
		t.Fatal("cache.get(wood) hit after fifth key, want oldest entry evicted")
	}
	value, ok := cache.get(table, "coin")
	if !ok {
		t.Fatal("cache.get(coin) missed, want newest entry")
	}
	if got, ok := value.Number(); !ok || got != 5 {
		t.Fatalf("cache.get(coin) = %v (%t), want number 5", got, ok)
	}

	table.setRawStringField("other", NumberValue(6))
	if _, ok := cache.get(table, "coin"); ok {
		t.Fatal("cache.get(coin) hit after layout mutation, want stale shape rejected")
	}
}

func TestDynamicStringIndexCacheWritesFourStringKeys(t *testing.T) {
	table := NewTable()
	for index, key := range []string{"wood", "ore", "herb", "gem"} {
		table.setRawStringField(key, NumberValue(float64(index+1)))
	}

	var cache dynamicStringIndexCache
	for _, key := range []string{"wood", "ore", "herb", "gem"} {
		slot, ok := table.rawStringFieldSlot(key)
		if !ok {
			t.Fatalf("rawStringFieldSlot(%s) failed, want inline slot", key)
		}
		cache.store(table, key, slot)
	}
	for index, key := range []string{"wood", "ore", "herb", "gem"} {
		if !cache.write(table, key, NumberValue(float64((index+1)*10))) {
			t.Fatalf("cache.write(%s) missed, want finite-key write PIC hit", key)
		}
	}
	for index, key := range []string{"wood", "ore", "herb", "gem"} {
		value, ok := table.rawStringField(key)
		if !ok {
			t.Fatalf("rawStringField(%s) missed after cache write", key)
		}
		if got, ok := value.Number(); !ok || got != float64((index+1)*10) {
			t.Fatalf("rawStringField(%s) = %v (%t), want number %d", key, got, ok, (index+1)*10)
		}
	}
}

func TestDynamicStringIndexCacheUsesMapBackedSlots(t *testing.T) {
	table := NewTable()
	for i := 0; i < maxInlineStringFields; i++ {
		key := fmt.Sprintf("field%d", i)
		table.setRawStringField(key, NumberValue(float64(len(key))))
	}
	for _, key := range []string{"target"} {
		table.setRawStringField(key, NumberValue(float64(len(key))))
	}
	if !table.hasStringOverflow() {
		t.Fatal("table did not promote to string field map, want map-backed cache coverage")
	}
	slot, ok := table.rawStringFieldSlot("target")
	if !ok {
		t.Fatal("rawStringFieldSlot(target) failed, want map-backed slot")
	}

	var cache dynamicStringIndexCache
	cache.store(table, "target", slot)
	var counts directFramePICCounts
	value, ok := cache.getCounted(table, "target", &counts)
	if !ok {
		t.Fatal("cache.getCounted(target) missed, want map-backed slot hit")
	}
	if got, ok := value.Number(); !ok || got != 6 {
		t.Fatalf("cache.getCounted(target) = %v (%t), want number 6", value, ok)
	}
	if !cache.writeCounted(table, "target", NumberValue(77), &counts) {
		t.Fatal("cache.writeCounted(target) missed, want map-backed slot write hit")
	}
	updated, ok := table.rawStringField("target")
	if !ok {
		t.Fatal("rawStringField(target) missing after cache write")
	}
	if got, ok := updated.Number(); !ok || got != 77 {
		t.Fatalf("rawStringField(target) = %v (%t), want number 77", updated, ok)
	}

	table.setRawStringField("other", NumberValue(1))
	if _, ok := cache.getCounted(table, "target", &counts); ok {
		t.Fatal("cache.getCounted(target) hit after map layout mutation, want stale shape miss")
	}
}

func TestDynamicStringIndexCacheWriteRejectsNilAndStaleShape(t *testing.T) {
	table := NewTable()
	table.setRawStringField("wood", NumberValue(1))
	slot, ok := table.rawStringFieldSlot("wood")
	if !ok {
		t.Fatal("rawStringFieldSlot(wood) failed, want inline slot")
	}

	var cache dynamicStringIndexCache
	cache.store(table, "wood", slot)
	if cache.write(table, "wood", NilValue()) {
		t.Fatal("cache.write nil hit, want nil write to use raw delete path")
	}
	value, ok := table.rawStringField("wood")
	if !ok {
		t.Fatal("rawStringField(wood) missing after rejected nil cache write")
	}
	if got, ok := value.Number(); !ok || got != 1 {
		t.Fatalf("rawStringField(wood) = %v (%t), want number 1", got, ok)
	}

	table.setRawStringField("other", NumberValue(2))
	if cache.write(table, "wood", NumberValue(3)) {
		t.Fatal("cache.write hit after layout mutation, want stale shape rejected")
	}
}

func TestDynamicStringIndexCacheCountsHitsAndMisses(t *testing.T) {
	table := NewTable()
	for index, key := range []string{"wood", "ore"} {
		table.setRawStringField(key, NumberValue(float64(index+1)))
	}

	var cache dynamicStringIndexCache
	for _, key := range []string{"wood", "ore"} {
		slot, ok := table.rawStringFieldSlot(key)
		if !ok {
			t.Fatalf("rawStringFieldSlot(%s) failed, want inline slot", key)
		}
		cache.store(table, key, slot)
	}

	var counts directFramePICCounts
	if _, ok := cache.getCounted(table, "wood", &counts); !ok {
		t.Fatal("cache.getCounted(wood) missed, want monomorphic hit")
	}
	if _, ok := cache.getCounted(table, "ore", &counts); !ok {
		t.Fatal("cache.getCounted(ore) missed, want polymorphic hit")
	}
	if _, ok := cache.getCounted(table, "missing", &counts); ok {
		t.Fatal("cache.getCounted(missing) hit, want key miss")
	}
	table.setRawStringField("gem", NumberValue(3))
	if _, ok := cache.getCounted(table, "ore", &counts); ok {
		t.Fatal("cache.getCounted(ore) hit after layout mutation, want shape miss")
	}
	if cache.writeCounted(table, "wood", NilValue(), &counts) {
		t.Fatal("cache.writeCounted nil hit, want nil write fallback")
	}

	if counts.monomorphicHits != 1 {
		t.Fatalf("monomorphicHits = %d, want 1", counts.monomorphicHits)
	}
	if counts.polymorphicHits != 1 {
		t.Fatalf("polymorphicHits = %d, want 1", counts.polymorphicHits)
	}
	if counts.keyMisses != 1 {
		t.Fatalf("keyMisses = %d, want 1", counts.keyMisses)
	}
	if counts.shapeMisses != 1 {
		t.Fatalf("shapeMisses = %d, want 1", counts.shapeMisses)
	}
	if counts.nilWriteFallbacks != 1 {
		t.Fatalf("nilWriteFallbacks = %d, want 1", counts.nilWriteFallbacks)
	}
}

func TestTableIndexCacheInvalidatesWhenMetatableIndexChanges(t *testing.T) {
	first := NewTable()
	first.setRawStringField("hp", NumberValue(10))
	second := NewTable()
	second.setRawStringField("hp", NumberValue(20))
	metatable := NewTable()
	metatable.setRawStringField("__index", TableValue(first))
	object := NewTable()
	object.setMetatable(metatable)

	index, ok, err := object.cachedIndexTable()
	if err != nil {
		t.Fatalf("cachedIndexTable returned error: %v", err)
	}
	if !ok || index != first {
		t.Fatalf("cachedIndexTable returned %#v (%t), want first table", index, ok)
	}

	metatable.setRawStringField("__index", TableValue(second))
	index, ok, err = object.cachedIndexTable()
	if err != nil {
		t.Fatalf("cachedIndexTable after mutation returned error: %v", err)
	}
	if !ok || index != second {
		t.Fatalf("cachedIndexTable after mutation returned %#v (%t), want second table", index, ok)
	}
}

func TestRepeatedCallsReuseWarmFieldCaches(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{hp = 1},
	{hp = 2},
	{hp = 3},
}

local function read(key)
	local total = 0
	for i = 1, 3 do
		total = total + rows[i][key]
	end
	return total
end

local total = 0
for i = 1, 20 do
	total = total + read("hp")
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 120 {
		t.Fatalf("result is %v (%t), want number 120", got, ok)
	}
	if got := snapshot.picCounts.keyMisses; got > 2 {
		t.Fatalf("key misses = %d, want warmed field caches to survive repeated calls; %s", got, summarizeDirectFrameMechanisms(snapshot))
	}
	if got := snapshot.picCounts.monomorphicHits + snapshot.picCounts.polymorphicHits; got == 0 {
		t.Fatalf("PIC hits = 0, want repeated dynamic string indexes to hit warmed field caches; %s", summarizeDirectFrameMechanisms(snapshot))
	}
}

func TestFrameResetNoLongerScalesWithCodeLength(t *testing.T) {
	shortProto := compileDynamicIndexProgram(t, 4)
	longProto := compileDynamicIndexProgram(t, 160)

	shortBytes := measuredFreshThreadRunAllocBytes(t, shortProto, 4, 40)
	longBytes := measuredFreshThreadRunAllocBytes(t, longProto, 160, 40)
	if delta := int64(longBytes) - int64(shortBytes); delta > 8192 {
		t.Fatalf("fresh run allocated %d more bytes for long dynamic-index code (%d vs %d), want frame reset cost not to scale with code length", delta, longBytes, shortBytes)
	}
}

func compileDynamicIndexProgram(t *testing.T, reads int) *Proto {
	t.Helper()
	var source strings.Builder
	source.WriteString(`
local row = {hp = 1}
local key = "hp"
local total = 0
`)
	for i := 0; i < reads; i++ {
		source.WriteString("total = total + row[key]\n")
	}
	source.WriteString("return total\n")
	proto, err := Compile(source.String())
	if err != nil {
		t.Fatalf("Compile(%d reads) returned error: %v", reads, err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled %d-read dynamic-index program is not direct-frame eligible:\n%s", reads, strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	return proto
}

func measuredFreshThreadRunAllocBytes(t *testing.T, proto *Proto, want float64, runs int) uint64 {
	t.Helper()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < runs; i++ {
		thread := newVMThread(runtimeGlobals(nil))
		results, err := thread.run(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.run returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != want {
			t.Fatalf("thread.run result is %v (%t), want number %v", got, ok, want)
		}
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	return (after.TotalAlloc - before.TotalAlloc) / uint64(runs)
}

func TestTableFastArrayFrontRemoveKeepsSequenceStorage(t *testing.T) {
	table := NewTable()
	table.fastArrayAppend(NumberValue(1))
	table.fastArrayAppend(NumberValue(2))
	table.fastArrayAppend(NumberValue(3))

	removed := table.fastArrayRemove(1)
	if got, ok := removed.Number(); !ok || got != 1 {
		t.Fatalf("removed value is %v (%t), want number 1", got, ok)
	}
	table.fastArrayAppend(NumberValue(4))

	length, err := table.rawLen()
	if err != nil {
		t.Fatalf("rawLen returned error: %v", err)
	}
	if length != 3 {
		t.Fatalf("rawLen after front remove/append is %d, want 3", length)
	}
	if table.hashFieldCount() != 0 {
		t.Fatalf("fast array spilled %d hash fields, want none", table.hashFieldCount())
	}
	for index, want := range []float64{2, 3, 4} {
		got, ok := table.array[index].Number()
		if !ok || got != want {
			t.Fatalf("array[%d] is %v (%t), want %v", index, got, ok, want)
		}
	}
}

func TestVMThreadUsesExplicitFrameStackForRecursiveScriptCalls(t *testing.T) {
	proto, err := Compile(`
local function sum(n)
	if n == 0 then
		return 0
	end
	return n + sum(n - 1)
end
return sum(4)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameInstrumented = true
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("thread.run returned %d results, want %d", got, want)
	}
	got, ok := results[0].Number()
	if !ok || got != 10 {
		t.Fatalf("thread.run result is %v (%t), want number 10", got, ok)
	}
	if thread.maxFrames < 6 {
		t.Fatalf("thread max frame depth is %d, want recursive script calls on explicit stack", thread.maxFrames)
	}
	if len(thread.frames) != 0 {
		t.Fatalf("thread kept %d frames after return, want empty stack", len(thread.frames))
	}
}

func TestVMThreadKeepsRecursiveScriptCallAllocationsBounded(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local function sum(n)
	if n == 0 then
		return 0
	end
	return n + sum(n - 1)
end
return sum(12)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	allocs := testing.AllocsPerRun(20, func() {
		thread := newVMThread(runtimeGlobals(nil))
		results, err := thread.run(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.run returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 78 {
			t.Fatalf("thread.run result is %v (%t), want number 78", got, ok)
		}
	})
	if allocs > 80 {
		t.Fatalf("recursive script call allocated %.0f times per run, want at most 80", allocs)
	}
}

func TestScriptCallFixedArityDoesNotAllocatePerCall(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local function add(a, b)
	return a + b
end

local total = 0
for i = 1, 100 do
	total = total + add(i, 1)
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()
	if results, err := thread.runScript(proto, nil, nil); err != nil {
		t.Fatalf("warm thread.runScript returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 5150 {
		t.Fatalf("warm result is %v (%t), want number 5150", got, ok)
	}

	allocs := testing.AllocsPerRun(100, func() {
		results, err := thread.runScript(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.runScript returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 5150 {
			t.Fatalf("thread.runScript result is %v (%t), want number 5150", got, ok)
		}
	})
	if allocs > 2 {
		t.Fatalf("fixed-arity script calls allocated %.0f times per run, want constant run-boundary allocations only", allocs)
	}
}

func TestDeepRecursionGrowsStackWithoutCorruption(t *testing.T) {
	proto, err := Compile(`
local function sum(n, acc)
	if n == 0 then
		return acc
	end
	return sum(n - 1, acc + n)
end
return sum(256, 0)
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
	got, ok := results[0].Number()
	if !ok || got != 32896 {
		t.Fatalf("thread.run result is %v (%t), want number 32896", got, ok)
	}
	if thread.maxFrames < 250 {
		t.Fatalf("thread max frame depth is %d, want deep recursion to grow frame stack", thread.maxFrames)
	}
	if len(thread.frames) != 0 {
		t.Fatalf("thread kept %d frames after return, want empty stack", len(thread.frames))
	}
	if len(thread.stack) != 0 {
		t.Fatalf("thread kept %d stack values after return, want empty stack", len(thread.stack))
	}
}

func TestVMThreadKeepsRecursiveFibonacciAllocationsBounded(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local function fib(n)
	if n < 2 then
		return n
	end
	return fib(n - 1) + fib(n - 2)
end
return fib(10)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	allocs := testing.AllocsPerRun(5, func() {
		thread := newVMThread(runtimeGlobals(nil))
		results, err := thread.run(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.run returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 55 {
			t.Fatalf("thread.run result is %v (%t), want number 55", got, ok)
		}
	})
	if allocs > 120 {
		t.Fatalf("recursive fibonacci allocated %.0f times per run, want at most 120", allocs)
	}
}

func TestMultiReturnAdjustmentDoesNotAllocatePerCall(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local function pair(a, b)
	return a, b
end

local total = 0
for i = 1, 80 do
	local a, b = pair(i, i + 1)
	total = total + a + b
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()
	if results, err := thread.runScript(proto, nil, nil); err != nil {
		t.Fatalf("warm thread.runScript returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 6560 {
		t.Fatalf("warm result is %v (%t), want number 6560", got, ok)
	}

	allocs := testing.AllocsPerRun(100, func() {
		results, err := thread.runScript(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.runScript returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 6560 {
			t.Fatalf("thread.runScript result is %v (%t), want number 6560", got, ok)
		}
	})
	if allocs > 2 {
		t.Fatalf("internal fixed multi-return script calls allocated %.0f times per run, want constant run-boundary allocations only", allocs)
	}
}

func TestOpenReturnPrefixDoesNotAllocatePerCall(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local function route(...)
	return 1, 2, select("#", ...)
end

local total = 0
for i = 1, 80 do
	local a, b, c = route(i, i + 1, i + 2)
	total = total + a + b + c
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()
	if results, err := thread.runScript(proto, nil, nil); err != nil {
		t.Fatalf("warm thread.runScript returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 480 {
		t.Fatalf("warm result is %v (%t), want number 480", got, ok)
	}

	allocs := testing.AllocsPerRun(100, func() {
		results, err := thread.runScript(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.runScript returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 480 {
			t.Fatalf("thread.runScript result is %v (%t), want number 480", got, ok)
		}
	})
	if allocs > 2 {
		t.Fatalf("open return with prefix allocated %.0f times per run, want constant run-boundary allocations only", allocs)
	}
}

func TestVarargForwardingDoesNotCopyPerAccess(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local function sum(a, b, c, d)
	return a + b + c + d
end

local function forward(...)
	local total = 0
	for i = 1, 80 do
		total = total + sum(...)
	end
	return total
end

return forward(1, 2, 3, 4)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()
	if results, err := thread.runScript(proto, nil, nil); err != nil {
		t.Fatalf("warm thread.runScript returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 800 {
		t.Fatalf("warm result is %v (%t), want number 800", got, ok)
	}

	allocs := testing.AllocsPerRun(100, func() {
		results, err := thread.runScript(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.runScript returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 800 {
			t.Fatalf("thread.runScript result is %v (%t), want number 800", got, ok)
		}
	})
	if allocs > 8 {
		t.Fatalf("vararg forwarding allocated %.0f times per run, want constant run-boundary allocations only", allocs)
	}
}

func TestFunctionIndexMetamethodCallDoesNotAllocatePerHit(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local object = {base = 20}
setmetatable(object, {__index = function(self, key)
	return self.base + key
end})

local total = 0
for i = 1, 80 do
	total = total + object[5]
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()
	if results, err := thread.runScript(proto, nil, nil); err != nil {
		t.Fatalf("warm thread.runScript returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 2000 {
		t.Fatalf("warm result is %v (%t), want number 2000", got, ok)
	}

	allocs := testing.AllocsPerRun(100, func() {
		results, err := thread.runScript(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.runScript returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 2000 {
			t.Fatalf("thread.runScript result is %v (%t), want number 2000", got, ok)
		}
	})
	if allocs > 8 {
		t.Fatalf("function __index hits allocated %.0f times per run, want constant run-boundary allocations only", allocs)
	}
}

func TestNewindexMetamethodWriteDoesNotAllocatePerHit(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local log = {hp = 0}
local object = {}
setmetatable(object, {__newindex = function(_, key, value)
	log[key] = value + 1
end})

for i = 1, 80 do
	object.hp = i
end
return log.hp, object.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()
	if results, err := thread.runScript(proto, nil, nil); err != nil {
		t.Fatalf("warm thread.runScript returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 81 {
		t.Fatalf("warm first result is %v (%t), want number 81", got, ok)
	} else if !results[1].IsNil() {
		t.Fatalf("warm second result is %s, want nil", results[1].Kind())
	}

	allocs := testing.AllocsPerRun(100, func() {
		results, err := thread.runScript(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.runScript returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 81 {
			t.Fatalf("thread.runScript first result is %v (%t), want number 81", got, ok)
		}
		if !results[1].IsNil() {
			t.Fatalf("thread.runScript second result is %s, want nil", results[1].Kind())
		}
	})
	if allocs > 8 {
		t.Fatalf("function __newindex hits allocated %.0f times per run, want constant run-boundary allocations only", allocs)
	}
}

func TestArithmeticComparisonMetamethodsDoNotAllocatePerHit(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local values = {left = 4, right = 6}
setmetatable(values, {
	__add = function(a, b)
		return a.left + b.right
	end,
	__lt = function(a, b)
		return a.left < b.right
	end,
})

local total = 0
for i = 1, 80 do
	if values < values then
		total = total + (values + values)
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()
	if results, err := thread.runScript(proto, nil, nil); err != nil {
		t.Fatalf("warm thread.runScript returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 800 {
		t.Fatalf("warm result is %v (%t), want number 800", got, ok)
	}

	allocs := testing.AllocsPerRun(100, func() {
		results, err := thread.runScript(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.runScript returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 800 {
			t.Fatalf("thread.runScript result is %v (%t), want number 800", got, ok)
		}
	})
	if allocs > 8 {
		t.Fatalf("arithmetic/comparison metamethod hits allocated %.0f times per run, want constant run-boundary allocations only", allocs)
	}
}

func TestTostringMetamethodDoesNotAllocatePerHit(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local object = {label = "ready"}
setmetatable(object, {
	__tostring = function(self)
		return self.label
	end,
})

local total = 0
for i = 1, 80 do
	if tostring(object) == "ready" then
		total = total + 1
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()
	if results, err := thread.runScript(proto, nil, nil); err != nil {
		t.Fatalf("warm thread.runScript returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 80 {
		t.Fatalf("warm result is %v (%t), want number 80", got, ok)
	}

	allocs := testing.AllocsPerRun(100, func() {
		results, err := thread.runScript(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.runScript returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 80 {
			t.Fatalf("thread.runScript result is %v (%t), want number 80", got, ok)
		}
	})
	if allocs > 8 {
		t.Fatalf("tostring metamethod hits allocated %.0f times per run, want constant run-boundary allocations only", allocs)
	}
}

func TestCallMetamethodDoesNotAllocatePerHit(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local object = {base = 7}
setmetatable(object, {
	__call = function(self, amount)
		return self.base + amount
	end,
})

local total = 0
for i = 1, 80 do
	total = total + object(5)
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()
	if results, err := thread.runScript(proto, nil, nil); err != nil {
		t.Fatalf("warm thread.runScript returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 960 {
		t.Fatalf("warm result is %v (%t), want number 960", got, ok)
	}

	allocs := testing.AllocsPerRun(100, func() {
		results, err := thread.runScript(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.runScript returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 960 {
			t.Fatalf("thread.runScript result is %v (%t), want number 960", got, ok)
		}
	})
	if allocs > 8 {
		t.Fatalf("__call metamethod hits allocated %.0f times per run, want constant run-boundary allocations only", allocs)
	}
}

func TestRunPublicResultsRemainStableAfterReturnWindowReuse(t *testing.T) {
	proto, err := Compile(`
local function many(seed)
	return seed, seed + 1, seed + 2
end

local a, b, c = many(3)
local d, e, f = many(20)
return a, b, c, d, e, f
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	thread := newVMThread(runtimeGlobals(nil))
	first, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("first thread.run returned error: %v", err)
	}
	second, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("second thread.run returned error: %v", err)
	}

	want := []float64{3, 4, 5, 20, 21, 22}
	for run, results := range [][]Value{first, second} {
		if got, wantLen := len(results), len(want); got != wantLen {
			t.Fatalf("run %d returned %d values, want %d", run+1, got, wantLen)
		}
		for i, value := range results {
			got, ok := value.Number()
			if !ok || got != want[i] {
				t.Fatalf("run %d result[%d] is %v (%t), want number %v", run+1, i, value, ok, want[i])
			}
		}
	}
}

func TestZeroCaptureClosureIdentityIsPreserved(t *testing.T) {
	proto, err := Compile(`
local function make()
	return function()
		return 17
	end
end

local first = make()
local second = make()
return first == second, first(), second()
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Bool(); !ok || got {
		t.Fatalf("first == second is %v (%t), want false for repeated closure creation", got, ok)
	}
	for i := 1; i <= 2; i++ {
		got, ok := results[i].Number()
		if !ok || got != 17 {
			t.Fatalf("result[%d] is %v (%t), want number 17", i, results[i], ok)
		}
	}
}

func TestImmutableCaptureAvoidsCellAllocation(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local total = 0
for i = 1, 80 do
	local base = i
	local add = function(delta)
		return base + delta
	end
	total = total + add(1)
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()
	if results, err := thread.runScript(proto, nil, nil); err != nil {
		t.Fatalf("warm thread.runScript returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 3320 {
		t.Fatalf("warm result is %v (%t), want number 3320", got, ok)
	}

	allocs := testing.AllocsPerRun(100, func() {
		results, err := thread.runScript(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.runScript returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 3320 {
			t.Fatalf("thread.runScript result is %v (%t), want number 3320", got, ok)
		}
	})
	if allocs > 85 {
		t.Fatalf("immutable captures allocated %.0f times per run, want closure allocations without capture cells", allocs)
	}
}

func TestZeroCaptureImmediateClosureDoesNotAllocatePerCreation(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	proto, err := Compile(`
local total = 0
for i = 1, 80 do
	total = total + (function()
		return 1
	end)()
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()
	if results, err := thread.runScript(proto, nil, nil); err != nil {
		t.Fatalf("warm thread.runScript returned error: %v", err)
	} else if got, ok := results[0].Number(); !ok || got != 80 {
		t.Fatalf("warm result is %v (%t), want number 80", got, ok)
	}

	allocs := testing.AllocsPerRun(100, func() {
		results, err := thread.runScript(proto, nil, nil)
		if err != nil {
			t.Fatalf("thread.runScript returned error: %v", err)
		}
		got, ok := results[0].Number()
		if !ok || got != 80 {
			t.Fatalf("thread.runScript result is %v (%t), want number 80", got, ok)
		}
	})
	if allocs > 8 {
		t.Fatalf("immediate zero-capture closures allocated %.0f times per run, want constant run-boundary allocations only", allocs)
	}
}

func TestMutableCaptureStillSharesCell(t *testing.T) {
	proto, err := Compile(`
local value = 1
local function inc()
	value = value + 1
	return value
end
local function get()
	return value
end
return inc(), get(), inc(), get()
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	want := []float64{2, 2, 3, 3}
	if got, wantLen := len(results), len(want); got != wantLen {
		t.Fatalf("Run returned %d values, want %d", got, wantLen)
	}
	for i, value := range results {
		got, ok := value.Number()
		if !ok || got != want[i] {
			t.Fatalf("result[%d] is %v (%t), want number %v", i, value, ok, want[i])
		}
	}
}

func TestVMThreadUsesExplicitFrameStackForScriptIndexMetamethod(t *testing.T) {
	proto, err := Compile(`
local object = setmetatable({}, {
	__index = function(self, key)
		local function hop(n)
			if n == 0 then
				return 20
			end
			return hop(n - 1)
		end
		return hop(3)
	end,
})
return object.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameInstrumented = true
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("thread.run returned %d results, want %d", got, want)
	}
	got, ok := results[0].Number()
	if !ok || got != 20 {
		t.Fatalf("thread.run result is %v (%t), want number 20", got, ok)
	}
	if thread.maxFrames < 6 {
		t.Fatalf("thread max frame depth is %d, want script metamethod calls on explicit stack", thread.maxFrames)
	}
	if len(thread.frames) != 0 {
		t.Fatalf("thread kept %d frames after return, want empty stack", len(thread.frames))
	}
}

func TestVMFrameResultStatesNameReturnAndScriptCall(t *testing.T) {
	returnProto := newProto(
		[]Value{NumberValue(5)},
		[]instruction{
			{op: opLoadConst, a: 0, b: 0},
			{op: opReturn, a: 0, b: 1},
		},
		nil,
		nil,
		1,
		0,
		false,
	)
	thread := newVMThread(runtimeGlobals(nil))
	result, err := thread.runFrame(newVMFrame(returnProto, nil, nil))
	if err != nil {
		t.Fatalf("runFrame returned error: %v", err)
	}
	if result.state != vmCallStateReturned {
		t.Fatalf("runFrame state is %v, want returned", result.state)
	}
	values := result.values()
	got, ok := values[0].Number()
	if !ok || got != 5 {
		t.Fatalf("runFrame result is %v (%t), want number 5", got, ok)
	}

	child := newProto(
		[]Value{NumberValue(9)},
		[]instruction{
			{op: opLoadConst, a: 0, b: 0},
			{op: opReturn, a: 0, b: 1},
		},
		nil,
		nil,
		1,
		0,
		false,
	)
	callProto := newProto(
		nil,
		[]instruction{
			{op: opClosure, a: 0, b: 0},
			{op: opCall, a: 0, b: 0, c: 0, d: 1},
			{op: opReturn, a: 0, b: 1},
		},
		[]*Proto{child},
		nil,
		1,
		0,
		false,
	)
	callResult, err := thread.runFrame(newVMFrame(callProto, nil, nil))
	if err != nil {
		t.Fatalf("runFrame returned error: %v", err)
	}
	if callResult.state != vmCallStateReturned {
		t.Fatalf("runFrame state is %v, want returned", callResult.state)
	}
	values = callResult.values()
	if got, want := len(values), 1; got != want {
		t.Fatalf("runFrame returned %d values, want %d", got, want)
	}
	got, ok = values[0].Number()
	if !ok || got != 9 {
		t.Fatalf("runFrame result is %v (%t), want number 9", got, ok)
	}
}

func TestVMSuspendedFramesResumeWithoutRebuildingFrames(t *testing.T) {
	proto, err := Compile(`
local function value()
	return 7
end
return value()
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	thread := newVMThread(runtimeGlobals(nil))
	restore := thread.activate()
	defer restore()

	parent := newVMFrame(proto, nil, nil)
	parent.pc = len(proto.code) - 1
	returnRegister := proto.code[parent.pc].a
	parent.pendingCall = vmPendingCall{
		destination: vmResultDestination{
			register: returnRegister,
			count:    1,
		},
	}
	parent.hasPendingCall = true
	thread.pushFrame(parent)
	child := newVMFrame(proto.prototypes[0], nil, nil)
	thread.pushFrame(child)
	stackSlot := &thread.frames[0]

	suspended := thread.suspendFrames()
	if len(thread.frames) != 0 {
		t.Fatalf("thread kept %d frames after suspend, want none", len(thread.frames))
	}
	if got, want := len(suspended.frames), 2; got != want {
		t.Fatalf("suspended frame count is %d, want %d", got, want)
	}
	if suspended.frames[0] != parent {
		t.Fatal("suspended parent frame was rebuilt, want same frame")
	}
	if suspended.frames[1] != child {
		t.Fatal("suspended child frame was rebuilt, want same frame")
	}
	if &suspended.frames[0] != stackSlot {
		t.Fatal("suspended frame slice was copied, want ownership transfer")
	}
	if !parent.hasPendingCall {
		t.Fatal("parent pending call is missing, want preserved result placement")
	}

	resumed := newVMThread(nil)
	resumed.resumeFrames(suspended)
	if len(resumed.frames) == 0 || &resumed.frames[0] != &suspended.frames[0] {
		t.Fatal("resumed frame slice was copied, want ownership transfer")
	}
	restoreResumed := resumed.activate()
	defer restoreResumed()

	results, err := resumed.runUntilDepth(0)
	if err != nil {
		t.Fatalf("resumed runUntilDepth returned error: %v", err)
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("resumed returned %d results, want %d", got, want)
	}
	got, ok := results[0].Number()
	if !ok || got != 7 {
		t.Fatalf("resumed result is %v (%t), want number 7", got, ok)
	}
	if len(resumed.frames) != 0 {
		t.Fatalf("resumed thread kept %d frames after return, want empty stack", len(resumed.frames))
	}
}

func TestCoroutineSingleYieldUsesInlineValueBuffer(t *testing.T) {
	globals := runtimeGlobals(nil)
	coroutine := newVMCoroutine(globals, &closure{proto: newProto(nil, []instruction{{op: opReturnOne}}, nil, nil, 1, 0, false)})
	coroutine.status = vmCoroutineRunning
	globals.thread = &coroutine.thread
	coroutine.thread.coroutine = coroutine

	_, err := baseCoroutineYield(globals, []Value{NumberValue(42)})
	if _, ok := err.(vmYieldRequest); !ok {
		t.Fatalf("baseCoroutineYield error is %v, want vmYieldRequest", err)
	}
	if got, want := len(coroutine.yieldedValues), 1; got != want {
		t.Fatalf("yielded value count is %d, want %d", got, want)
	}
	if &coroutine.yieldedValues[0] != &coroutine.yieldedInline[0] {
		t.Fatal("single yielded value used heap slice, want inline buffer")
	}
	got, ok := coroutine.yieldedValues[0].Number()
	if !ok || got != 42 {
		t.Fatalf("yielded value is %v (%t), want number 42", got, ok)
	}
}

func TestVMFrameReturnsHostInterruptWhenInstructionBudgetExpires(t *testing.T) {
	proto := newProto(
		[]Value{NumberValue(1)},
		[]instruction{
			{op: opLoadConst, a: 0, b: 0},
			{op: opReturn, a: 0, b: 1},
		},
		nil,
		nil,
		1,
		0,
		false,
	)
	thread := newVMThread(runtimeGlobals(nil))
	thread.instructionBudget = 1
	result, err := thread.runFrame(newVMFrame(proto, nil, nil))
	if err != nil {
		t.Fatalf("runFrame returned error: %v", err)
	}
	if result.state != vmCallStateHostInterrupt {
		t.Fatalf("runFrame state is %v, want host interrupt", result.state)
	}
}

func TestInstructionBudgetInterruptsFastExecution(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 100 do
	total = total + i
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled budget program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	var counts directFrameOpcodeCounts
	var pic directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.instructionBudget = 7
	thread.directFrameInstrumented = true
	thread.directFrameOpcodeCounts = &counts
	thread.directFramePICCounts = &pic

	result, err := thread.runFrame(newVMFrame(proto, nil, nil))
	if err != nil {
		t.Fatalf("runFrame returned error: %v", err)
	}
	if result.state != vmCallStateHostInterrupt {
		t.Fatalf("runFrame state is %v, want host interrupt", result.state)
	}
	if counts.count(opNumericForLoop) == 0 && counts.count(opAdd) == 0 && counts.count(opAddK) == 0 {
		t.Fatalf("direct opcode counts show no loop/body execution: %#v", counts.ranked())
	}
	if got := pic.sideExitCount(directFrameSideExitReasonBudget); got != 0 {
		t.Fatalf("budget side exits = %d, want budget handled inside fast loop", got)
	}
}

func TestVMThreadReturnsErrorWhenInstructionBudgetExpires(t *testing.T) {
	proto := newProto(
		[]Value{NumberValue(1)},
		[]instruction{
			{op: opLoadConst, a: 0, b: 0},
			{op: opReturn, a: 0, b: 1},
		},
		nil,
		nil,
		1,
		0,
		false,
	)
	thread := newVMThread(runtimeGlobals(nil))
	thread.instructionBudget = 1

	_, err := thread.run(proto, nil, nil)
	if err == nil {
		t.Fatal("thread.run returned nil error, want instruction budget error")
	}
	if !strings.Contains(err.Error(), "instruction budget exhausted") {
		t.Fatalf("thread.run error is %q, want instruction budget detail", err)
	}
}

func TestVMCountDebugHookRunsAtInstructionBoundariesNonYieldably(t *testing.T) {
	proto, err := Compile(`
local co = coroutine.create(function()
	local before = coroutine.isyieldable()
	local after = coroutine.isyieldable()
	return before, after
end)
return coroutine.resume(co)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	thread := newVMThread(runtimeGlobals(nil))
	hookCalls := 0
	hookSawYieldable := true
	thread.debugHook = func(globals *globalEnv, event vmDebugEvent) error {
		if event.kind != vmDebugEventCount {
			return nil
		}
		hookCalls++
		if globals.thread.isYieldable() {
			hookSawYieldable = true
		} else {
			hookSawYieldable = false
		}
		return nil
	}
	thread.debugCountInterval = 1

	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if hookCalls == 0 {
		t.Fatal("count debug hook was not called")
	}
	if hookSawYieldable {
		t.Fatal("count debug hook ran yieldably, want non-yieldable hook execution")
	}
	if got, want := len(results), 3; got != want {
		t.Fatalf("thread.run returned %d results, want %d", got, want)
	}
	if ok, boolOK := results[0].Bool(); !boolOK || !ok {
		t.Fatalf("coroutine.resume ok is %#v, want true", results[0])
	}
	if before, boolOK := results[1].Bool(); !boolOK || !before {
		t.Fatalf("coroutine isyieldable before hook is %#v, want true", results[1])
	}
	if after, boolOK := results[2].Bool(); !boolOK || !after {
		t.Fatalf("coroutine isyieldable after hook is %#v, want true", results[2])
	}
}

func TestVMCountDebugHookCanReportRuntimeError(t *testing.T) {
	proto, err := Compile("return 1")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	thread := newVMThread(runtimeGlobals(nil))
	thread.debugCountInterval = 1
	thread.debugHook = func(_ *globalEnv, event vmDebugEvent) error {
		if event.kind != vmDebugEventCount {
			return nil
		}
		return errDebugHookTest("debug hook failed")
	}

	_, err = thread.run(proto, nil, nil)
	if err == nil {
		t.Fatal("thread.run returned nil error, want debug hook error")
	}
	if !strings.Contains(err.Error(), "debug hook failed") {
		t.Fatalf("thread.run error is %q, want debug hook failure", err)
	}
}

func TestVMCountDebugHookCanReportHostInterrupt(t *testing.T) {
	proto, err := Compile(`
local ok, value = pcall(function()
	return 1
end)
return ok, value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	thread := newVMThread(runtimeGlobals(nil))
	thread.debugCountInterval = 1
	thread.debugHook = func(_ *globalEnv, event vmDebugEvent) error {
		if event.kind != vmDebugEventCount {
			return nil
		}
		return vmHostInterrupt{}
	}

	_, err = thread.run(proto, nil, nil)
	if err == nil {
		t.Fatal("thread.run returned nil error, want host interrupt")
	}
	if !strings.Contains(err.Error(), "instruction budget exhausted") {
		t.Fatalf("thread.run error is %q, want host interrupt detail", err)
	}
}

func TestVMLineDebugHookReportsSourceLineChanges(t *testing.T) {
	proto, err := Compile("local value = 1\nreturn value + 2\n")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	thread := newVMThread(runtimeGlobals(nil))
	var lines []int
	thread.debugLineHook = true
	thread.debugHook = func(_ *globalEnv, event vmDebugEvent) error {
		if event.kind != vmDebugEventLine {
			return nil
		}
		lines = append(lines, event.line)
		return nil
	}

	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("thread.run returned %d results, want %d", got, want)
	}
	got, ok := results[0].Number()
	if !ok || got != 3 {
		t.Fatalf("thread.run result is %v (%t), want number 3", got, ok)
	}
	wantLines := []int{2}
	if !reflect.DeepEqual(lines, wantLines) {
		t.Fatalf("line hook lines are %#v, want %#v", lines, wantLines)
	}
}

func TestVMCallAndReturnDebugHooksReportScriptFrames(t *testing.T) {
	proto, err := Compile(`
local function add(value)
	return value + 1
end
return add(2)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	thread := newVMThread(runtimeGlobals(nil))
	var events []vmDebugEventKind
	thread.debugCallHook = true
	thread.debugReturnHook = true
	thread.debugHook = func(_ *globalEnv, event vmDebugEvent) error {
		if event.kind == vmDebugEventCall || event.kind == vmDebugEventReturn {
			events = append(events, event.kind)
		}
		return nil
	}

	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("thread.run returned %d results, want %d", got, want)
	}
	got, ok := results[0].Number()
	if !ok || got != 3 {
		t.Fatalf("thread.run result is %v (%t), want number 3", got, ok)
	}
	wantEvents := []vmDebugEventKind{
		vmDebugEventCall,
		vmDebugEventCall,
		vmDebugEventReturn,
		vmDebugEventReturn,
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("debug hook events are %#v, want %#v", events, wantEvents)
	}
}

func TestVMLineDebugHookContinuesAcrossCoroutineResume(t *testing.T) {
	proto, err := Compile("local co = coroutine.create(function()\n\tcoroutine.yield(\"pause\")\n\treturn \"done\"\nend)\nlocal ok1, label = coroutine.resume(co)\nlocal ok2, done = coroutine.resume(co)\nreturn ok1, label, ok2, done\n")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	thread := newVMThread(runtimeGlobals(nil))
	var lines []int
	thread.debugLineHook = true
	thread.debugHook = func(_ *globalEnv, event vmDebugEvent) error {
		if event.kind == vmDebugEventLine {
			lines = append(lines, event.line)
		}
		return nil
	}

	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, want := len(results), 4; got != want {
		t.Fatalf("thread.run returned %d results, want %d", got, want)
	}
	if ok, boolOK := results[0].Bool(); !boolOK || !ok {
		t.Fatalf("first resume ok is %#v, want true", results[0])
	}
	if label, stringOK := results[1].String(); !stringOK || label != "pause" {
		t.Fatalf("first resume label is %q, want pause", label)
	}
	if ok, boolOK := results[2].Bool(); !boolOK || !ok {
		t.Fatalf("second resume ok is %#v, want true", results[2])
	}
	if done, stringOK := results[3].String(); !stringOK || done != "done" {
		t.Fatalf("second resume value is %q, want done", done)
	}
	if !lineSequenceContains(lines, []int{2, 3}) {
		t.Fatalf("line hook lines are %#v, want coroutine lines 2 then 3 across resume", lines)
	}
}

func TestTableCommonArrayWritesUseArrayPart(t *testing.T) {
	table := NewTable()
	for i := 1; i <= 4; i++ {
		if err := table.rawSet(NumberValue(float64(i)), NumberValue(float64(i*10))); err != nil {
			t.Fatalf("rawSet index %d returned error: %v", i, err)
		}
	}

	if got, want := len(table.array), 4; got != want {
		t.Fatalf("array part length is %d, want %d", got, want)
	}
	if table.hashFieldCount() != 0 {
		t.Fatalf("hash fields has %d entries, want 0 for contiguous array writes", table.hashFieldCount())
	}
	length, err := table.rawLen()
	if err != nil {
		t.Fatalf("rawLen returned error: %v", err)
	}
	if length != 4 {
		t.Fatalf("rawLen returned %d, want 4", length)
	}
}

func TestTableSparseArrayKeysPromoteWhenContiguous(t *testing.T) {
	table := NewTable()
	if err := table.rawSet(NumberValue(3), StringValue("third")); err != nil {
		t.Fatalf("rawSet sparse returned error: %v", err)
	}
	if got, want := len(table.array), 0; got != want {
		t.Fatalf("array length after sparse write is %d, want %d", got, want)
	}
	if got, want := table.hashFieldCount(), 1; got != want {
		t.Fatalf("hash fields after sparse write is %d, want %d", got, want)
	}
	if err := table.rawSet(NumberValue(1), StringValue("first")); err != nil {
		t.Fatalf("rawSet first returned error: %v", err)
	}
	if err := table.rawSet(NumberValue(2), StringValue("second")); err != nil {
		t.Fatalf("rawSet second returned error: %v", err)
	}
	if got, want := len(table.array), 3; got != want {
		t.Fatalf("array length after promotion is %d, want %d", got, want)
	}
	if got, want := table.hashFieldCount(), 0; got != want {
		t.Fatalf("hash fields after promotion is %d, want %d", got, want)
	}
	length, err := table.rawLen()
	if err != nil {
		t.Fatalf("rawLen returned error: %v", err)
	}
	if length != 3 {
		t.Fatalf("rawLen returned %d, want 3", length)
	}
}

func TestTableRawNextIncludesArrayAndHashKeysInDeterministicInsertionOrder(t *testing.T) {
	table := NewTable()
	if err := table.rawSet(StringValue("name"), StringValue("ember")); err != nil {
		t.Fatalf("rawSet name returned error: %v", err)
	}
	if err := table.rawSet(NumberValue(2), StringValue("second")); err != nil {
		t.Fatalf("rawSet second returned error: %v", err)
	}
	if err := table.rawSet(NumberValue(1), StringValue("first")); err != nil {
		t.Fatalf("rawSet first returned error: %v", err)
	}

	firstKey, firstValue, err := table.rawNext(NilValue())
	if err != nil {
		t.Fatalf("rawNext nil returned error: %v", err)
	}
	if text, ok := firstKey.String(); !ok || text != "name" {
		t.Fatalf("first next key is %q (%t), want name", text, ok)
	}
	if text, ok := firstValue.String(); !ok || text != "ember" {
		t.Fatalf("first next value is %q (%t), want ember", text, ok)
	}

	secondKey, secondValue, err := table.rawNext(firstKey)
	if err != nil {
		t.Fatalf("rawNext first returned error: %v", err)
	}
	if number, ok := secondKey.Number(); !ok || number != 2 {
		t.Fatalf("second next key is %v (%t), want number 2", number, ok)
	}
	if text, ok := secondValue.String(); !ok || text != "second" {
		t.Fatalf("second next value is %q (%t), want second", text, ok)
	}

	thirdKey, thirdValue, err := table.rawNext(secondKey)
	if err != nil {
		t.Fatalf("rawNext second returned error: %v", err)
	}
	if number, ok := thirdKey.Number(); !ok || number != 1 {
		t.Fatalf("third next key is %v (%t), want number 1", number, ok)
	}
	if text, ok := thirdValue.String(); !ok || text != "first" {
		t.Fatalf("third next value is %q (%t), want first", text, ok)
	}
}

func TestTableRawNextMixedTableDoesNotAllocatePerStep(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	table := NewTable()
	for _, item := range []struct {
		key   Value
		value Value
	}{
		{StringValue("name"), StringValue("ember")},
		{NumberValue(3), StringValue("third")},
		{NumberValue(1), StringValue("first")},
		{TableValue(NewTable()), StringValue("object")},
	} {
		if err := table.rawSet(item.key, item.value); err != nil {
			t.Fatalf("rawSet returned error: %v", err)
		}
	}
	firstKey, _, err := table.rawNext(NilValue())
	if err != nil {
		t.Fatalf("rawNext nil returned error: %v", err)
	}

	var nextKey Value
	var nextValue Value
	allocs := testing.AllocsPerRun(1000, func() {
		nextKey, nextValue, err = table.rawNext(firstKey)
		if err != nil {
			t.Fatalf("rawNext first returned error: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("rawNext allocated %.2f times per step, want 0", allocs)
	}
	if nextKey.IsNil() || nextValue.IsNil() {
		t.Fatal("rawNext returned nil key/value during allocation check")
	}
}

func TestTableRawNextRejectsInvalidResumptionKey(t *testing.T) {
	table := NewTable()
	if err := table.rawSet(StringValue("present"), NumberValue(1)); err != nil {
		t.Fatalf("rawSet returned error: %v", err)
	}
	if _, _, err := table.rawNext(StringValue("missing")); err == nil {
		t.Fatal("rawNext accepted missing resumption key, want invalid key error")
	}
}

func TestTableRawNextPreservesPositionAcrossUpdateDeleteAndReinsert(t *testing.T) {
	table := NewTable()
	for _, key := range []string{"a", "b", "c"} {
		if err := table.rawSet(StringValue(key), StringValue(key+"1")); err != nil {
			t.Fatalf("rawSet %s returned error: %v", key, err)
		}
	}
	if err := table.rawSet(StringValue("c"), StringValue("c2")); err != nil {
		t.Fatalf("rawSet c update returned error: %v", err)
	}
	if err := table.rawSet(StringValue("b"), NilValue()); err != nil {
		t.Fatalf("rawSet b delete returned error: %v", err)
	}
	if err := table.rawSet(StringValue("b"), StringValue("b2")); err != nil {
		t.Fatalf("rawSet b reinsert returned error: %v", err)
	}

	var got []string
	for key, value, err := table.rawNext(NilValue()); !key.IsNil(); key, value, err = table.rawNext(key) {
		if err != nil {
			t.Fatalf("rawNext returned error: %v", err)
		}
		keyText, keyOK := key.String()
		valueText, valueOK := value.String()
		if !keyOK || !valueOK {
			t.Fatalf("rawNext returned key/value %v/%v, want strings", key, value)
		}
		got = append(got, keyText+"="+valueText)
	}
	want := []string{"a=a1", "b=b2", "c=c2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rawNext order = %v, want %v", got, want)
	}
}

func TestTableIterationJournalCompactsOnlyPastTombstoneThreshold(t *testing.T) {
	table := NewTable()
	for i := 0; i < 40; i++ {
		if err := table.rawSet(StringValue(fmt.Sprintf("k%02d", i)), NumberValue(float64(i))); err != nil {
			t.Fatalf("rawSet seed %d returned error: %v", i, err)
		}
	}
	if table.iteration == nil {
		t.Fatal("mixed string-map table has no iteration journal")
	}
	if got := len(table.iteration.keys); got != 40 {
		t.Fatalf("journal key count after seed = %d, want 40", got)
	}
	for i := 0; i < 20; i++ {
		if err := table.rawSet(StringValue(fmt.Sprintf("k%02d", i)), NilValue()); err != nil {
			t.Fatalf("rawSet delete %d returned error: %v", i, err)
		}
	}
	if got := len(table.iteration.keys); got != 40 {
		t.Fatalf("journal compacted at half tombstones; key count = %d, want 40", got)
	}
	if got := table.iteration.tombstones; got != 20 {
		t.Fatalf("journal tombstones = %d, want 20 before threshold is crossed", got)
	}
	if err := table.rawSet(StringValue("k20"), NilValue()); err != nil {
		t.Fatalf("rawSet threshold delete returned error: %v", err)
	}
	if got := len(table.iteration.keys); got != 19 {
		t.Fatalf("journal key count after compaction = %d, want 19", got)
	}
	if got := table.iteration.tombstones; got != 0 {
		t.Fatalf("journal tombstones after compaction = %d, want 0", got)
	}
}

func TestTableObjectKeysUseCreationIDsForStableOrder(t *testing.T) {
	firstTable := NewTable()
	secondTable := NewTable()
	if !(tableKey{kind: TableKind, table: firstTable}).less(tableKey{kind: TableKind, table: secondTable}) {
		t.Fatal("first table key does not sort before later table key")
	}

	firstUserData := NewUserData("first")
	secondUserData := NewUserData("second")
	if !(tableKey{kind: UserDataKind, userdata: firstUserData}).less(tableKey{kind: UserDataKind, userdata: secondUserData}) {
		t.Fatal("first userdata key does not sort before later userdata key")
	}
}

func TestTableRawNextObjectKeysAvoidPointerFormattingAllocation(t *testing.T) {
	table := NewTable()
	if err := table.rawSet(TableValue(NewTable()), NumberValue(1)); err != nil {
		t.Fatalf("rawSet table key returned error: %v", err)
	}
	if err := table.rawSet(UserDataValue(NewUserData("payload")), NumberValue(2)); err != nil {
		t.Fatalf("rawSet userdata key returned error: %v", err)
	}

	var key Value
	var value Value
	var err error
	allocs := testing.AllocsPerRun(1000, func() {
		key, value, err = table.rawNext(NilValue())
		if err != nil {
			t.Fatalf("rawNext returned error: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("rawNext object key step allocated %.2f times, want 0", allocs)
	}
	if key.IsNil() || value.IsNil() {
		t.Fatal("rawNext returned nil key/value during allocation check")
	}
}

func lineSequenceContains(lines []int, want []int) bool {
	if len(want) == 0 {
		return true
	}
	next := 0
	for _, line := range lines {
		if line == want[next] {
			next++
			if next == len(want) {
				return true
			}
		}
	}
	return false
}

func TestVMProtectedRecoveryDoesNotCatchHostInterrupt(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturn, a: 0, b: 1}}, nil, nil, 1, 0, false)
	frame := newVMFrame(proto, nil, nil)
	frame.pendingCall = vmPendingCall{
		destination: vmResultDestination{
			register: 0,
			count:    1,
		},
		protected: &vmProtectedCall{},
	}
	frame.hasPendingCall = true
	thread := newVMThread(runtimeGlobals(nil))
	thread.pushFrame(frame)

	if thread.recoverProtectedError(vmHostInterrupt{}) {
		t.Fatal("protected recovery caught host interrupt, want it to propagate")
	}
}

func TestVMYieldableHostCallResumesWithCoroutineArguments(t *testing.T) {
	proto, err := Compile(`
local co = coroutine.create(function()
	local label, total = yieldHost(4)
	return label, total
end)

local ok1, yielded, first = coroutine.resume(co)
local ok2, label, total = coroutine.resume(co, 8)
return ok1, yielded, first, ok2, label, total, coroutine.status(co)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := RunWithGlobals(proto, map[string]Value{
		"yieldHost": yieldableHostFuncValue(func(_ *globalEnv, args []Value) vmHostCallResult {
			seed, ok := args[0].Number()
			if !ok {
				return vmHostCallResult{err: errHostYieldTest("missing numeric seed")}
			}
			return vmHostCallResult{
				yield: &vmHostYield{
					values: []Value{StringValue("host-yield"), NumberValue(seed + 1)},
					continuation: func(_ *globalEnv, resumeArgs []Value) vmHostCallResult {
						resumed, ok := resumeArgs[0].Number()
						if !ok {
							return vmHostCallResult{err: errHostYieldTest("missing numeric resume value")}
						}
						return vmHostCallResult{
							values: []Value{StringValue("host-done"), NumberValue(resumed + seed)},
						}
					},
				},
			}
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}

	if len(results) != 7 {
		t.Fatalf("RunWithGlobals returned %d results, want 7", len(results))
	}
	if ok, _ := results[0].Bool(); !ok {
		t.Fatalf("first resume ok is %#v, want true", results[0])
	}
	if yielded, _ := results[1].String(); yielded != "host-yield" {
		t.Fatalf("yielded value is %q, want host-yield", yielded)
	}
	if first, _ := results[2].Number(); first != 5 {
		t.Fatalf("yielded number is %v, want 5", first)
	}
	if ok, _ := results[3].Bool(); !ok {
		t.Fatalf("second resume ok is %#v, want true", results[3])
	}
	if label, _ := results[4].String(); label != "host-done" {
		t.Fatalf("resumed label is %q, want host-done", label)
	}
	if total, _ := results[5].Number(); total != 12 {
		t.Fatalf("resumed total is %v, want 12", total)
	}
	if status, _ := results[6].String(); status != "dead" {
		t.Fatalf("coroutine status is %q, want dead", status)
	}
}

func TestVMYieldableHostCallCanYieldRepeatedly(t *testing.T) {
	proto, err := Compile(`
local co = coroutine.create(function()
	return yieldTwice()
end)

local ok1, first = coroutine.resume(co)
local ok2, second = coroutine.resume(co, "resume-one")
local ok3, final = coroutine.resume(co, "resume-two")
return ok1, first, ok2, second, ok3, final, coroutine.status(co)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := RunWithGlobals(proto, map[string]Value{
		"yieldTwice": yieldableHostFuncValue(func(_ *globalEnv, _ []Value) vmHostCallResult {
			return vmHostCallResult{
				yield: &vmHostYield{
					values: []Value{StringValue("host-yield-one")},
					continuation: func(_ *globalEnv, resumeArgs []Value) vmHostCallResult {
						resumed, ok := resumeArgs[0].String()
						if !ok {
							return vmHostCallResult{err: errHostYieldTest("missing first resume value")}
						}
						return vmHostCallResult{
							yield: &vmHostYield{
								values: []Value{StringValue("host-yield-two:" + resumed)},
								continuation: func(_ *globalEnv, resumeArgs []Value) vmHostCallResult {
									resumed, ok := resumeArgs[0].String()
									if !ok {
										return vmHostCallResult{err: errHostYieldTest("missing second resume value")}
									}
									return vmHostCallResult{values: []Value{StringValue("host-done:" + resumed)}}
								},
							},
						}
					},
				},
			}
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}

	wants := []string{"host-yield-one", "host-yield-two:resume-one", "host-done:resume-two", "dead"}
	if len(results) != 7 {
		t.Fatalf("RunWithGlobals returned %d results, want 7", len(results))
	}
	if ok, _ := results[0].Bool(); !ok {
		t.Fatalf("first resume ok is %#v, want true", results[0])
	}
	if first, _ := results[1].String(); first != wants[0] {
		t.Fatalf("first yield is %q, want %q", first, wants[0])
	}
	if ok, _ := results[2].Bool(); !ok {
		t.Fatalf("second resume ok is %#v, want true", results[2])
	}
	if second, _ := results[3].String(); second != wants[1] {
		t.Fatalf("second yield is %q, want %q", second, wants[1])
	}
	if ok, _ := results[4].Bool(); !ok {
		t.Fatalf("third resume ok is %#v, want true", results[4])
	}
	if final, _ := results[5].String(); final != wants[2] {
		t.Fatalf("final value is %q, want %q", final, wants[2])
	}
	if status, _ := results[6].String(); status != wants[3] {
		t.Fatalf("coroutine status is %q, want %q", status, wants[3])
	}
}

func TestVMYieldableHostContinuationErrorStopsCoroutine(t *testing.T) {
	proto, err := Compile(`
local co = coroutine.create(function()
	return yieldThenError()
end)

local ok1, yielded = coroutine.resume(co)
local ok2, message = coroutine.resume(co)
return ok1, yielded, ok2, message, coroutine.status(co)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := RunWithGlobals(proto, map[string]Value{
		"yieldThenError": yieldableHostFuncValue(func(_ *globalEnv, _ []Value) vmHostCallResult {
			return vmHostCallResult{
				yield: &vmHostYield{
					values: []Value{StringValue("before-error")},
					continuation: func(_ *globalEnv, _ []Value) vmHostCallResult {
						return vmHostCallResult{err: errHostYieldTest("host continuation failed")}
					},
				},
			}
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}

	if len(results) != 5 {
		t.Fatalf("RunWithGlobals returned %d results, want 5", len(results))
	}
	if ok, _ := results[0].Bool(); !ok {
		t.Fatalf("first resume ok is %#v, want true", results[0])
	}
	if yielded, _ := results[1].String(); yielded != "before-error" {
		t.Fatalf("yielded value is %q, want before-error", yielded)
	}
	if ok, _ := results[2].Bool(); ok {
		t.Fatalf("second resume ok is %#v, want false", results[2])
	}
	message, _ := results[3].String()
	if !strings.Contains(message, "host continuation failed") {
		t.Fatalf("second resume message is %q, want host continuation failure", message)
	}
	if status, _ := results[4].String(); status != "dead" {
		t.Fatalf("coroutine status is %q, want dead", status)
	}
}

func TestVMYieldableHostContinuationErrorCanBeProtected(t *testing.T) {
	proto, err := Compile(`
local co = coroutine.create(function()
	return pcall(yieldThenError)
end)

local ok1, yielded = coroutine.resume(co)
local ok2, protectedOK, message = coroutine.resume(co)
return ok1, yielded, ok2, protectedOK, message, coroutine.status(co)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := RunWithGlobals(proto, map[string]Value{
		"yieldThenError": yieldableHostFuncValue(func(_ *globalEnv, _ []Value) vmHostCallResult {
			return vmHostCallResult{
				yield: &vmHostYield{
					values: []Value{StringValue("before-protected-error")},
					continuation: func(_ *globalEnv, _ []Value) vmHostCallResult {
						return vmHostCallResult{err: errHostYieldTest("protected host continuation failed")}
					},
				},
			}
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}

	if len(results) != 6 {
		t.Fatalf("RunWithGlobals returned %d results, want 6", len(results))
	}
	if ok, _ := results[0].Bool(); !ok {
		t.Fatalf("first resume ok is %#v, want true", results[0])
	}
	if yielded, _ := results[1].String(); yielded != "before-protected-error" {
		t.Fatalf("yielded value is %q, want before-protected-error", yielded)
	}
	if ok, _ := results[2].Bool(); !ok {
		t.Fatalf("second resume ok is %#v, want true", results[2])
	}
	if protectedOK, _ := results[3].Bool(); protectedOK {
		t.Fatalf("protected ok is %#v, want false", results[3])
	}
	message, _ := results[4].String()
	if !strings.Contains(message, "protected host continuation failed") {
		t.Fatalf("protected message is %q, want host continuation failure", message)
	}
	if status, _ := results[5].String(); status != "dead" {
		t.Fatalf("coroutine status is %q, want dead", status)
	}
}

func TestVMYieldableHostInterruptBypassesProtectedCall(t *testing.T) {
	proto, err := Compile(`
local ok, value = pcall(interruptHost)
return ok, value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	_, err = RunWithGlobals(proto, map[string]Value{
		"interruptHost": yieldableHostFuncValue(func(_ *globalEnv, _ []Value) vmHostCallResult {
			return vmHostCallResult{interrupt: true}
		}),
	})
	if err == nil {
		t.Fatal("RunWithGlobals succeeded, want host interrupt")
	}
	if !strings.Contains(err.Error(), "instruction budget exhausted") {
		t.Fatalf("RunWithGlobals error is %q, want host interrupt detail", err)
	}
}

func TestVMYieldableHostContinuationInterruptBypassesProtectedCall(t *testing.T) {
	proto, err := Compile(`
local co = coroutine.create(function()
	return pcall(yieldThenInterrupt)
end)

local ok1, yielded = coroutine.resume(co)
local ok2, message = coroutine.resume(co)
return ok1, yielded, ok2, message, coroutine.status(co)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := RunWithGlobals(proto, map[string]Value{
		"yieldThenInterrupt": yieldableHostFuncValue(func(_ *globalEnv, _ []Value) vmHostCallResult {
			return vmHostCallResult{
				yield: &vmHostYield{
					values: []Value{StringValue("before-interrupt")},
					continuation: func(_ *globalEnv, _ []Value) vmHostCallResult {
						return vmHostCallResult{interrupt: true}
					},
				},
			}
		}),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}

	if len(results) != 5 {
		t.Fatalf("RunWithGlobals returned %d results, want 5", len(results))
	}
	if ok, _ := results[0].Bool(); !ok {
		t.Fatalf("first resume ok is %#v, want true", results[0])
	}
	if yielded, _ := results[1].String(); yielded != "before-interrupt" {
		t.Fatalf("yielded value is %q, want before-interrupt", yielded)
	}
	if ok, _ := results[2].Bool(); ok {
		t.Fatalf("second resume ok is %#v, want false", results[2])
	}
	message, _ := results[3].String()
	if !strings.Contains(message, "instruction budget exhausted") {
		t.Fatalf("second resume message is %q, want host interrupt detail", message)
	}
	if status, _ := results[4].String(); status != "dead" {
		t.Fatalf("coroutine status is %q, want dead", status)
	}
}

type errHostYieldTest string

func (err errHostYieldTest) Error() string {
	return string(err)
}

type errDebugHookTest string

func (err errDebugHookTest) Error() string {
	return string(err)
}

func TestVMFrameRecordsCallMetadataForFutureControlFlow(t *testing.T) {
	parentProto := newProto(nil, []instruction{{op: opReturn, a: 0, b: 1}}, nil, nil, 3, 0, false)
	childProto := newProto(nil, []instruction{{op: opReturn, a: 0, b: 1}}, nil, nil, 2, 0, false)
	parent := newVMFrame(parentProto, nil, nil)
	child := newVMFrame(childProto, nil, nil)
	thread := newVMThread(runtimeGlobals(nil))

	thread.pushFrame(parent)
	thread.pushFrame(child)

	if parent.registerBase != 0 {
		t.Fatalf("parent register base is %d, want 0", parent.registerBase)
	}
	if parent.registerCount != 3 {
		t.Fatalf("parent register count is %d, want 3", parent.registerCount)
	}
	if parent.debugLine != -1 {
		t.Fatalf("parent debug line is %d, want -1 placeholder", parent.debugLine)
	}
	if child.caller != parent {
		t.Fatal("child caller is not parent frame")
	}

	child.pendingCall = vmPendingCall{
		destination: vmResultDestination{register: 1, count: 2},
	}
	child.hasPendingCall = true
	if child.pendingCall.destination.register != 1 {
		t.Fatalf("result destination register is %d, want 1", child.pendingCall.destination.register)
	}
	if child.pendingCall.destination.count != 2 {
		t.Fatalf("result destination count is %d, want 2", child.pendingCall.destination.count)
	}
}

func TestBytecodeBuilderRecordsExplicitIROperands(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(0, NumberValue(2))
	builder.emit(instruction{op: opAdd, a: 2, b: 0, c: 1})

	if got, want := len(builder.ir), 2; got != want {
		t.Fatalf("builder recorded %d IR instructions, want %d", got, want)
	}
	load := builder.ir[0]
	if load.operands.a.kind != bytecodeOperandRegister || load.operands.a.value != 0 {
		t.Fatalf("load const target operand is %#v, want register 0", load.operands.a)
	}
	if load.operands.b.kind != bytecodeOperandConstant || load.operands.b.value != 0 {
		t.Fatalf("load const value operand is %#v, want constant 0", load.operands.b)
	}
	add := builder.ir[1]
	if add.operands.a.kind != bytecodeOperandRegister ||
		add.operands.b.kind != bytecodeOperandRegister ||
		add.operands.c.kind != bytecodeOperandRegister {
		t.Fatalf("add operands are %#v, want register operands", add.operands)
	}
}

func TestBytecodeBuilderPatchesIRJumpTargets(t *testing.T) {
	var builder bytecodeBuilder
	jump := builder.emitJumpIfFalse(0)
	builder.emitLoadConst(1, NumberValue(2))
	builder.patchJump(jump, builder.pc())

	if got := builder.ir[jump].operands.b; got.kind != bytecodeOperandJumpTarget || got.value != 2 {
		t.Fatalf("jump target operand is %#v, want jump target 2", got)
	}
	proto := builder.proto(nil, 2, 0, false)
	got := disassembleProto(proto)
	want := []string{
		"0000 JUMP_IF_FALSE r0 2",
		"0001 LOAD_CONST r1 k0(number 2)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("disassembleProto() = %#v, want %#v", got, want)
	}
}

func TestDisassembleBytecodeIRBeforeProtoConstruction(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(0, NumberValue(2))
	builder.emitLoadConst(1, NumberValue(3))
	builder.emit(instruction{op: opAdd, a: 2, b: 0, c: 1})
	builder.emit(instruction{op: opReturn, a: 2, b: 1})

	got := disassembleBytecodeIR(builder.constants, builder.ir)
	want := []string{
		"0000 LOAD_CONST r0 k0(number 2)",
		"0001 LOAD_CONST r1 k1(number 3)",
		"0002 ADD r2 r0 r1",
		"0003 RETURN r2 1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("disassembleBytecodeIR() = %#v, want %#v", got, want)
	}
}

func TestDisassembleProtoFactsShowsOptimizedArtifactShape(t *testing.T) {
	child := newProto(
		nil,
		[]instruction{{op: opReturnOne, a: 0}},
		nil,
		[]upvalueDesc{{local: true, index: 1}},
		1,
		0,
		false,
	)
	proto := newProto(
		[]Value{StringValue("hp"), NumberValue(3)},
		[]instruction{
			{op: opClosure, a: 2, b: 0},
			{op: opReturnOne, a: 2},
		},
		[]*Proto{child},
		nil,
		3,
		0,
		false,
	)

	got := disassembleProtoFacts(proto)
	want := []string{
		"direct_frame_dispatch true",
		"captured_locals r1",
		"entry_nil none",
		"constant_key k0 string \"hp\"",
		"constant_number k1 3",
		"constant_kind k0 string",
		"constant_kind k1 number",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("disassembleProtoFacts() = %#v, want %#v", got, want)
	}
}

func TestBytecodeIRRecordsSourceMetadata(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitWithSource(instruction{op: opReturn, a: 0, b: 1}, sourceRange{start: 7, end: 13})

	got := builder.ir[0].source
	if got.start != 7 || got.end != 13 {
		t.Fatalf("IR source range is [%d,%d), want [7,13)", got.start, got.end)
	}
	lines := disassembleBytecodeIRWithSource(builder.constants, builder.ir)
	want := []string{"0000 [7,13) RETURN r0 1"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("disassembleBytecodeIRWithSource() = %#v, want %#v", lines, want)
	}
}

func TestCompilerAttachesExpressionSourceMetadataToBytecodeIR(t *testing.T) {
	source := "return 12 + 3"
	artifact := parseSourceForBytecodeIRTest(t, source)
	compiler := compilerForBytecodeIRTest(artifact, compilerOptions{
		optimizations: optimizationOptions{
			disabledCategories: map[optimizationCategory]bool{
				optimizationHIRSimplify: true,
			},
		},
	})

	if err := compiler.compileStatements(artifact.program.statements); err != nil {
		t.Fatalf("compileStatements returned error: %v", err)
	}
	add, ok := findBytecodeIRInstruction(compiler.ir, opAdd)
	if !ok {
		add, ok = findBytecodeIRInstruction(compiler.ir, opAddK)
	}
	if !ok {
		t.Fatalf("compiled IR is missing ADD instruction: %#v", disassembleBytecodeIR(compiler.constants, compiler.ir))
	}
	if got := source[add.source.start:add.source.end]; got != "12 + 3" {
		t.Fatalf("ADD source range points at %q, want %q", got, "12 + 3")
	}
}

func TestCompilerEmitsFusedNumericForLoop(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 5, 2 do
	total = total + i
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	lines := disassembleProto(proto)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "NUMERIC_FOR_CHECK") {
		t.Fatalf("compiled numeric for is missing NUMERIC_FOR_CHECK:\n%s", joined)
	}
	if !strings.Contains(joined, "NUMERIC_FOR_LOOP") {
		t.Fatalf("compiled numeric for is missing NUMERIC_FOR_LOOP:\n%s", joined)
	}
	if strings.Contains(joined, "ADD r1 r1 r3\n") && strings.Contains(joined, "JUMP 4") {
		t.Fatalf("compiled numeric for kept separate increment and back-jump:\n%s", joined)
	}
	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "numeric_for") {
		t.Fatalf("compiled numeric for is missing numeric loop descriptor:\n%s", facts)
	}
	if !strings.Contains(facts, "increment") {
		t.Fatalf("compiled numeric for descriptor is missing increment pc:\n%s", facts)
	}
}

func TestRunFusedNumericForMatchesStepSemantics(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 5, 2 do
	total = total + i
end
for i = 5, 1, -2 do
	total = total + i * 10
end
for i = 1.5, 2.5, 0.5 do
	total = total + i * 100
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if joined := strings.Join(disassembleProto(proto), "\n"); !strings.Contains(joined, "NUMERIC_FOR_LOOP") {
		t.Fatalf("compiled numeric for is missing NUMERIC_FOR_LOOP:\n%s", joined)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 699 {
		t.Fatalf("Run result is %v (%t), want number 699", got, ok)
	}
}

func TestCompilerReusesConstantZeroForNumericForCoercions(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 5, 2 do
	total = total + i
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, max := proto.registers, 5; got > max {
		t.Fatalf("compiled numeric for uses %d registers, want at most %d", got, max)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, oldCoercion := range []string{"ADD r1 r1 r4", "ADD r2 r2 r4", "ADD r3 r3 r4"} {
		if strings.Contains(joined, oldCoercion) {
			t.Fatalf("compiled numeric for kept register-form zero coercion %q:\n%s", oldCoercion, joined)
		}
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 9 {
		t.Fatalf("Run result is %v (%t), want number 9", got, ok)
	}
}

func TestCompilerUpdatesSingleLocalAssignmentInPlace(t *testing.T) {
	proto, err := Compile(`
local total = 0
total = total + 1
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	lines := disassembleProto(proto)
	for _, line := range lines {
		if strings.Contains(line, "MOVE r0 ") {
			t.Fatalf("compiled single local assignment copies back into r0, want in-place update:\n%s", strings.Join(lines, "\n"))
		}
	}
}

func TestCompilerRunsNumericAddModExpressionWithoutFusedOpcode(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 5 do
	total = total + ((i * 3 - i // 2) % 17)
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if strings.Contains(joined, "ADD_NUMERIC_MOD_K") {
		t.Fatalf("compiled numeric update regrew fused ADD_NUMERIC_MOD_K:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 39 {
		t.Fatalf("Run result is %v (%t), want number 39", got, ok)
	}
}

func TestCompilerReturnsSingleLocalInPlace(t *testing.T) {
	proto, err := Compile(`
local value = 7
return value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	lines := disassembleProto(proto)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "RETURN_ONE r0") {
		t.Fatalf("compiled return does not return local r0 directly:\n%s", joined)
	}
	if strings.Contains(joined, "MOVE r1 r0") {
		t.Fatalf("compiled return copies r0 before returning:\n%s", joined)
	}
}

func TestFinalizedProtoMarksDirectFrameDispatch(t *testing.T) {
	direct, err := Compile("return 1")
	if err != nil {
		t.Fatalf("Compile direct returned error: %v", err)
	}
	if !protoSupportsDirectFrame(direct) {
		t.Fatal("direct prototype is not marked for direct-frame dispatch")
	}

	captured, err := Compile(`
local value = 1
local function get()
	return value
end
return get()
`)
	if err != nil {
		t.Fatalf("Compile captured returned error: %v", err)
	}
	if !protoSupportsDirectFrame(captured) {
		t.Fatal("capturing parent prototype is not marked for direct-frame dispatch")
	}
	if !protoSupportsDirectFrame(captured.prototypes[0]) {
		t.Fatal("non-capturing child frame should still use direct-frame dispatch")
	}
}

func TestRunDirectFrameScalarLoopPreservesValues(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 10 do
	total = total + ((i * 3 - i // 2) % 7)
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatal("compiled scalar loop is not marked for direct-frame dispatch")
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	got, ok := results[0].Number()
	if !ok || got != 35 {
		t.Fatalf("Run result is %v (%t), want number 35", got, ok)
	}
}

func TestAssemblerRemovesJumpToNextInstruction(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(0, NumberValue(41))
	jump := builder.emitJump()
	builder.emit(instruction{op: opReturnOne, a: 0})
	builder.patchJump(jump, jump+1)

	got := builder.assembledCode()
	want := []instruction{
		{op: opLoadConst, a: 0, b: 0},
		{op: opReturnOne, a: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("assembled bytecode = %#v, want %#v", got, want)
	}
}

func TestRunProductionLoopHasNoInstrumentationSideEffects(t *testing.T) {
	proto, err := Compile(`
local row = {value = 5}
local total = 0
for i = 1, 4 do
	total = total + math.min(row.value, i)
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled scalar program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	var opcodeCounts directFrameOpcodeCounts
	var picCounts directFramePICCounts
	pcCounts := make(map[*Proto][]uint64)
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &opcodeCounts
	thread.directFramePICCounts = &picCounts
	thread.directFramePCCounts = pcCounts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 10 {
		t.Fatalf("result is %v (%t), want number 10", results[0], ok)
	}
	if got := len(opcodeCounts.ranked()); got != 0 {
		t.Fatalf("production direct-frame opcode counters recorded %d opcodes without opt-in", got)
	}
	if got := picCounts.totalMechanismActivity(); got != 0 {
		t.Fatalf("production direct-frame PIC counters recorded %d events without opt-in", got)
	}
	if got := pcCounts[proto]; len(got) != 0 {
		t.Fatalf("production direct-frame pc counters recorded %v without opt-in", got)
	}
}

func TestRunDirectFrameClosureUpvaluesStayEligible(t *testing.T) {
	proto, err := Compile(`
local counter = 1
local function nextValue()
	counter = counter + 1
	return counter
end
return nextValue(), nextValue()
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) != 1 {
		t.Fatalf("compiled %d child prototypes, want 1", len(proto.prototypes))
	}
	child := proto.prototypes[0]
	if !protoSupportsDirectFrame(child) {
		t.Fatalf("closure with upvalue reads/writes is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(child), "\n"))
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.opcodeCounts.count(opGetUpvalue); got == 0 {
		t.Fatal("direct-frame GET_UPVALUE count is 0, want captured reads handled directly")
	}
	if got := snapshot.opcodeCounts.count(opSetUpvalue); got == 0 {
		t.Fatal("direct-frame SET_UPVALUE count is 0, want captured writes handled directly")
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("generic side exits = %d, want closure upvalue body to stay direct", got)
	}
	if got, ok := results[0].Number(); !ok || got != 2 {
		t.Fatalf("first result is %v (%t), want 2", results[0], ok)
	}
	if got, ok := results[1].Number(); !ok || got != 3 {
		t.Fatalf("second result is %v (%t), want 3", results[1], ok)
	}
}

func TestRunDirectFrameCapturedParentWritesUpdateUpvalueCells(t *testing.T) {
	proto, err := Compile(`
local value = 1
local function get()
	return value
end
value = value + 1
return get(), value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("capturing parent is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("generic side exits = %d, want captured parent to stay direct", got)
	}
	first, ok := results[0].Number()
	if !ok || first != 2 {
		t.Fatalf("closure result is %v (%t), want 2", results[0], ok)
	}
	second, ok := results[1].Number()
	if !ok || second != 2 {
		t.Fatalf("parent result is %v (%t), want 2", results[1], ok)
	}
}

func TestRunDirectFrameUpvalueCallOneStaysEligible(t *testing.T) {
	proto, err := Compile(`
local function makeCaller()
	local function inc(value)
		return value + 1
	end
	return function(value)
		local result = inc(value)
		return result
	end
end
local caller = makeCaller()
return caller(41)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	var callerProto *Proto
	var dump strings.Builder
	var findCaller func(*Proto)
	findCaller = func(proto *Proto) {
		if proto == nil || callerProto != nil {
			return
		}
		dump.WriteString(strings.Join(disassembleProto(proto), "\n"))
		dump.WriteString("\n---\n")
		joined := strings.Join(disassembleProto(proto), "\n")
		if strings.Contains(joined, "CALL_UPVALUE_ONE") {
			callerProto = proto
			return
		}
		for _, child := range proto.prototypes {
			findCaller(child)
		}
	}
	findCaller(proto)
	if callerProto == nil {
		t.Fatalf("compiled program is missing CALL_UPVALUE_ONE:\n%s", dump.String())
	}
	if !protoSupportsDirectFrame(callerProto) {
		t.Fatalf("upvalue-call child is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(callerProto), "\n"))
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.opcodeCounts.count(opCallUpvalueOne); got == 0 {
		t.Fatal("direct-frame CALL_UPVALUE_ONE count is 0, want upvalue call handled directly")
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("generic side exits = %d, want upvalue call body to stay direct", got)
	}
	if got, ok := results[0].Number(); !ok || got != 42 {
		t.Fatalf("upvalue call result is %v (%t), want 42", results[0], ok)
	}
}

func TestRunDirectFrameSetGlobalPreservesExpressionValue(t *testing.T) {
	proto, err := Compile(`
local value = 12 + 3
answer = value
return value, answer
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled global-write program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.opcodeCounts.count(opSetGlobal); got == 0 {
		t.Fatal("direct-frame SET_GLOBAL count is 0, want global writes handled directly")
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("generic side exits = %d, want direct SET_GLOBAL execution", got)
	}
	for index, result := range results {
		got, ok := result.Number()
		if !ok || got != 15 {
			t.Fatalf("result %d is %v (%t), want 15", index, result, ok)
		}
	}
}

func TestRunDirectFrameVarargFunctionStaysEligible(t *testing.T) {
	proto, err := Compile(`
local function collect(...)
	local count = select("#", ...)
	local first, second = ...
	return count, first, second
end
return collect(7, 8, 9)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) != 1 {
		t.Fatalf("compiled %d child prototypes, want 1", len(proto.prototypes))
	}
	child := proto.prototypes[0]
	if !protoSupportsDirectFrame(child) {
		t.Fatalf("vararg child is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(child), "\n"))
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.opcodeCounts.count(opFastCall); got == 0 {
		t.Fatal("direct-frame FAST_CALL count is 0, want vararg count handled directly")
	}
	if got := snapshot.opcodeCounts.count(opVararg); got == 0 {
		t.Fatal("direct-frame VARARG count is 0, want vararg reads handled directly")
	}
	want := []float64{3, 7, 8}
	for index, want := range want {
		got, ok := results[index].Number()
		if !ok || got != want {
			t.Fatalf("result %d is %v (%t), want %v", index, results[index], ok, want)
		}
	}
}

func TestRunDirectFrameMethodCallOneStaysEligible(t *testing.T) {
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
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "CALL_METHOD_ONE") {
		t.Fatalf("compiled method call is missing CALL_METHOD_ONE:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("method-call program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.opcodeCounts.count(opCallMethodOne); got == 0 {
		t.Fatal("direct-frame CALL_METHOD_ONE count is 0, want raw method call handled directly")
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("generic side exits = %d, want raw method call to stay direct", got)
	}
	if got, ok := results[0].Number(); !ok || got != 15 {
		t.Fatalf("method result is %v (%t), want 15", results[0], ok)
	}
}

func TestRunDirectFrameCoroutineResumeSideExitsLocally(t *testing.T) {
	proto, err := Compile(`
local ok, value = coroutine.resume(co)
return ok, value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "COROUTINE_RESUME") {
		t.Fatalf("compiled coroutine resume is missing COROUTINE_RESUME:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("coroutine-resume program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	body, err := Compile(`return 41`)
	if err != nil {
		t.Fatalf("Compile coroutine body returned error: %v", err)
	}
	coroutine := newVMCoroutine(runtimeGlobals(nil), &closure{proto: body})
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, map[string]Value{
		"co": UserDataValue(coroutine.userdata),
	})
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.opcodeCounts.count(opFastCall); got == 0 {
		t.Fatal("direct-frame FAST_CALL count is 0, want local coroutine side-exit point")
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonYield); got == 0 {
		t.Fatal("coroutine resume had 0 yield side exits, want local side exit")
	}
	if got, ok := results[0].Bool(); !ok || !got {
		t.Fatalf("resume ok result is %v (%t), want true", results[0], ok)
	}
	if got, ok := results[1].Number(); !ok || got != 41 {
		t.Fatalf("resume value result is %v (%t), want 41", results[1], ok)
	}
}

func TestRunDirectFrameSetupOpcodesPreserveValues(t *testing.T) {
	proto, err := Compile(`
local named = {hp = 10, alive = true}
local keyed = {[true] = 2}
local function child()
	return 3
end
return 4
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !strings.Contains(strings.Join(disassembleProto(proto), "\n"), "NEW_TABLE") {
		t.Fatalf("compiled setup program is missing NEW_TABLE:\n%s", strings.Join(disassembleProto(proto), "\n"))
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled setup program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 4 {
		t.Fatalf("Run result is %v (%t), want number 4", got, ok)
	}
}

func TestRunDirectFrameOwnStringFieldAccessPreservesMissingAndDeletion(t *testing.T) {
	proto, err := Compile(`
local row = {hp = 10, alive = true}
local first = row.hp
local missing = row.missing
row.hp = nil
local deleted = row.hp
if missing == nil and deleted == nil then
	return first
end
return 0
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "GET_STRING_FIELD") {
		t.Fatalf("compiled field access is missing GET_STRING_FIELD:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled field access program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 10 {
		t.Fatalf("Run result is %v (%t), want number 10", got, ok)
	}
}

func TestRunDirectFrameDynamicIndexPreservesStringNumberAndMissingKeys(t *testing.T) {
	proto, err := Compile(`
local row = {hp = 10, alive = true}
local values = {3, 5}
local hp = row["hp"]
local second = values[2]
local missing = row["missing"]
if missing == nil then
	return hp + second
end
return 0
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "GET_INDEX") {
		t.Fatalf("compiled dynamic index program is missing GET_INDEX:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled dynamic index program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 15 {
		t.Fatalf("Run result is %v (%t), want number 15", got, ok)
	}
}

func TestRunDirectFrameDynamicIndexStorePreservesStringNumberAndNilKeys(t *testing.T) {
	proto, err := Compile(`
local row = {hp = 10}
local values = {3}
row["hp"] = 12
values[2] = 5
row["missing"] = nil
if row["missing"] == nil then
	return row.hp + values[1] + values[2]
end
return 0
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "SET_INDEX") || !strings.Contains(joined, "GET_INDEX") {
		t.Fatalf("compiled dynamic index store program is missing index opcodes:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled dynamic index store program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 20 {
		t.Fatalf("Run result is %v (%t), want number 20", got, ok)
	}
}

func TestRunDirectFrameDynamicIndexPICCountsFallbackClasses(t *testing.T) {
	proto, err := Compile(`
local row = {hp = 10}
local values = {3}
local missing = row["missing"]
row["hp"] = nil
local numeric = values[1]
local metatable = proxy["anything"]
if missing == nil and row.hp == nil then
	return numeric + metatable
end
return 0
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "GET_INDEX") || !strings.Contains(joined, "SET_INDEX") {
		t.Fatalf("compiled dynamic index accounting program is missing index opcodes:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled dynamic index accounting program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	backing := NewTable()
	backing.setRawStringField("anything", NumberValue(4))
	metatable := NewTable()
	metatable.setRawStringField("__index", TableValue(backing))
	proxy := NewTable()
	proxy.setMetatable(metatable)

	thread := newVMThread(runtimeGlobals(map[string]Value{
		"proxy": TableValue(proxy),
	}))
	counts := &directFramePICCounts{}
	thread.directFrameInstrumented = true
	thread.directFramePICCounts = counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 7 {
		t.Fatalf("thread.run result is %v (%t), want number 7", got, ok)
	}

	if counts.metatableMisses != 1 {
		t.Fatalf("metatableMisses = %d, want 1", counts.metatableMisses)
	}
	if counts.missingKeyFallbacks != 1 {
		t.Fatalf("missingKeyFallbacks = %d, want 1", counts.missingKeyFallbacks)
	}
	if counts.nilWriteFallbacks != 1 {
		t.Fatalf("nilWriteFallbacks = %d, want 1", counts.nilWriteFallbacks)
	}
	if counts.invalidKeyFallbacks != 0 {
		t.Fatalf("invalidKeyFallbacks = %d, want numeric array index to avoid invalid-key fallback", counts.invalidKeyFallbacks)
	}
	if counts.numericArrayIndexHits != 1 {
		t.Fatalf("numericArrayIndexHits = %d, want 1", counts.numericArrayIndexHits)
	}
}

func TestRunDirectFrameNestedStringFieldIndexPathsPreserveValues(t *testing.T) {
	proto, err := Compile(`
local market = {stock = {wood = 10, ore = 5}}
local good = "wood"
local before = market.stock[good]
market.stock[good] = before - 3
return before, market.stock[good], market.stock.ore
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD_INDEX", "SET_STRING_FIELD_INDEX"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled nested field-index program is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled nested field-index program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 10 {
		t.Fatalf("first result is %v (%t), want number 10", got, ok)
	}
	if got, ok := results[1].Number(); !ok || got != 7 {
		t.Fatalf("second result is %v (%t), want number 7", got, ok)
	}
	if got, ok := results[2].Number(); !ok || got != 5 {
		t.Fatalf("third result is %v (%t), want number 5", got, ok)
	}
}

func TestStringFieldIndexPathsUseMetatableSemantics(t *testing.T) {
	proto, err := Compile(`
local stockBacking = {wood = 2}
local stockProxy = {}
setmetatable(stockProxy, {
	__index = stockBacking,
	__newindex = stockBacking,
})
local market = {}
setmetatable(market, {
	__index = {stock = stockProxy},
})
local good = "wood"
local before = market.stock[good]
market.stock[good] = before + 3
return before, stockBacking.wood
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD_INDEX", "SET_STRING_FIELD_INDEX"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled nested field-index metatable program is missing %s:\n%s", want, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 2 {
		t.Fatalf("first result is %v (%t), want number 2", got, ok)
	}
	if got, ok := results[1].Number(); !ok || got != 5 {
		t.Fatalf("second result is %v (%t), want number 5", got, ok)
	}
}

func TestRunDirectFrameTableAccessIslandResumesAfterIndexMetatable(t *testing.T) {
	proto, err := Compile(`
return proxy.value + 3
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD", "ADD_K"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled table island program is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled table island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	backing := NewTable()
	backing.setRawStringField("value", NumberValue(4))
	metatable := NewTable()
	metatable.setRawStringField("__index", TableValue(backing))
	proxy := NewTable()
	proxy.setMetatable(metatable)

	var counts directFrameOpcodeCounts
	thread := newVMThread(runtimeGlobals(map[string]Value{"proxy": TableValue(proxy)}))
	thread.directFrameInstrumented = true
	thread.directFrameOpcodeCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 7 {
		t.Fatalf("thread.run result is %v (%t), want number 7", got, ok)
	}
	if counts.count(opAddK) == 0 {
		t.Fatalf("direct-frame ADDK count is 0, want table island to resume direct-frame execution")
	}
}

func TestRunDirectFrameTableAccessIslandResumesAfterNewIndexMetatable(t *testing.T) {
	proto, err := Compile(`
proxy.value = 4
local value = seed
return value + 2
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"SET_STRING_FIELD", "ADD_K"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled newindex island program is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled newindex island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	backing := NewTable()
	metatable := NewTable()
	metatable.setRawStringField("__newindex", TableValue(backing))
	proxy := NewTable()
	proxy.setMetatable(metatable)

	var counts directFrameOpcodeCounts
	thread := newVMThread(runtimeGlobals(map[string]Value{"proxy": TableValue(proxy), "seed": NumberValue(1)}))
	thread.directFrameInstrumented = true
	thread.directFrameOpcodeCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 3 {
		t.Fatalf("thread.run result is %v (%t), want number 3", got, ok)
	}
	if value, ok := backing.rawStringField("value"); !ok || valueNumber(value) != 4 {
		t.Fatalf("backing value is %#v (%t), want number 4", value, ok)
	}
	if counts.count(opAddK) == 0 {
		t.Fatalf("direct-frame ADDK count is 0, want table island to resume direct-frame execution")
	}
}

func TestRunDirectFrameTableAccessIslandResumesAfterDynamicIndexMetatable(t *testing.T) {
	proto, err := Compile(`
local key = "value"
return proxy[key] + 3
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_INDEX", "ADD_K"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled dynamic index island program is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled dynamic index island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	backing := NewTable()
	backing.setRawStringField("value", NumberValue(4))
	metatable := NewTable()
	metatable.setRawStringField("__index", TableValue(backing))
	proxy := NewTable()
	proxy.setMetatable(metatable)

	var counts directFrameOpcodeCounts
	thread := newVMThread(runtimeGlobals(map[string]Value{"proxy": TableValue(proxy)}))
	thread.directFrameInstrumented = true
	thread.directFrameOpcodeCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 7 {
		t.Fatalf("thread.run result is %v (%t), want number 7", got, ok)
	}
	if counts.count(opAddK) == 0 {
		t.Fatalf("direct-frame ADDK count is 0, want dynamic table island to resume direct-frame execution")
	}
}

func TestRunDirectFrameTableAccessIslandResumesAfterDynamicNewIndexMetatable(t *testing.T) {
	proto, err := Compile(`
local key = "value"
proxy[key] = 4
local value = seed
return value + 2
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"SET_INDEX", "ADD_K"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled dynamic newindex island program is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled dynamic newindex island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	backing := NewTable()
	metatable := NewTable()
	metatable.setRawStringField("__newindex", TableValue(backing))
	proxy := NewTable()
	proxy.setMetatable(metatable)

	var counts directFrameOpcodeCounts
	thread := newVMThread(runtimeGlobals(map[string]Value{"proxy": TableValue(proxy), "seed": NumberValue(1)}))
	thread.directFrameInstrumented = true
	thread.directFrameOpcodeCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 3 {
		t.Fatalf("thread.run result is %v (%t), want number 3", got, ok)
	}
	if value, ok := backing.rawStringField("value"); !ok || valueNumber(value) != 4 {
		t.Fatalf("backing value is %#v (%t), want number 4", value, ok)
	}
	if counts.count(opAddK) == 0 {
		t.Fatalf("direct-frame ADDK count is 0, want dynamic table island to resume direct-frame execution")
	}
}

func TestRunDirectFrameIntrinsicIslandResumesAfterOverriddenMathMin(t *testing.T) {
	proto, err := Compile(`
return math.min(5, 2) + 3
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"MATH_MIN", "ADD_K"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled intrinsic island program is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled intrinsic island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	mathTable := NewTable()
	mathTable.setRawStringField("min", HostFuncValue(func(args []Value) ([]Value, error) {
		if len(args) != 2 {
			t.Fatalf("math.min override received %d args, want 2", len(args))
		}
		return []Value{NumberValue(4)}, nil
	}))

	var counts directFrameOpcodeCounts
	thread := newVMThread(runtimeGlobals(map[string]Value{"math": TableValue(mathTable)}))
	thread.directFrameInstrumented = true
	thread.directFrameOpcodeCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 7 {
		t.Fatalf("thread.run result is %v (%t), want number 7", got, ok)
	}
	if counts.count(opAddK) == 0 {
		t.Fatalf("direct-frame ADDK count is 0, want intrinsic island to resume direct-frame execution")
	}
}

func TestRunDirectFrameSideExitCountersRecordTableAndIntrinsicIslands(t *testing.T) {
	tableProto, err := Compile(`
return proxy.value + 3
`)
	if err != nil {
		t.Fatalf("Compile table program returned error: %v", err)
	}
	backing := NewTable()
	backing.setRawStringField("value", NumberValue(4))
	metatable := NewTable()
	metatable.setRawStringField("__index", TableValue(backing))
	proxy := NewTable()
	proxy.setMetatable(metatable)

	var tableCounts directFramePICCounts
	tableThread := newVMThread(runtimeGlobals(map[string]Value{"proxy": TableValue(proxy)}))
	tableThread.directFrameInstrumented = true
	tableThread.directFramePICCounts = &tableCounts
	if _, err := tableThread.run(tableProto, nil, nil); err != nil {
		t.Fatalf("table thread.run returned error: %v", err)
	}
	if got := tableCounts.sideExitCount(directFrameSideExitReasonTable); got == 0 {
		t.Fatalf("table side exits = %d, want at least one", got)
	}

	intrinsicProto, err := Compile(`
return math.min(5, 2) + 3
`)
	if err != nil {
		t.Fatalf("Compile intrinsic program returned error: %v", err)
	}
	mathTable := NewTable()
	mathTable.setRawStringField("min", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{NumberValue(4)}, nil
	}))

	var intrinsicCounts directFramePICCounts
	intrinsicThread := newVMThread(runtimeGlobals(map[string]Value{"math": TableValue(mathTable)}))
	intrinsicThread.directFrameInstrumented = true
	intrinsicThread.directFramePICCounts = &intrinsicCounts
	if _, err := intrinsicThread.run(intrinsicProto, nil, nil); err != nil {
		t.Fatalf("intrinsic thread.run returned error: %v", err)
	}
	if got := intrinsicCounts.sideExitCount(directFrameSideExitReasonIntrinsic); got == 0 {
		t.Fatalf("intrinsic side exits = %d, want at least one", got)
	}
}

func TestRunDirectFrameHandlesDebugAndBudgetWithoutWholeFrameDemotion(t *testing.T) {
	proto, err := Compile(`return 1`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled block counter program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	var debugOpcodes directFrameOpcodeCounts
	var debugCounts directFramePICCounts
	debugThread := newVMThread(runtimeGlobals(nil))
	debugThread.directFrameInstrumented = true
	debugThread.directFrameOpcodeCounts = &debugOpcodes
	debugThread.directFramePICCounts = &debugCounts
	debugThread.debugHook = func(_ *globalEnv, _ vmDebugEvent) error { return nil }
	if _, err := debugThread.run(proto, nil, nil); err != nil {
		t.Fatalf("debug thread.run returned error: %v", err)
	}
	if got := debugCounts.sideExitCount(directFrameSideExitReasonDebug); got != 0 {
		t.Fatalf("debug side exits = %d, want debug-capable fast loop", got)
	}
	if got := debugOpcodes.count(opReturnOne) + debugOpcodes.count(opReturn); got == 0 {
		t.Fatalf("debug opcode counts recorded no return, want direct execution")
	}

	var budgetOpcodes directFrameOpcodeCounts
	var budgetCounts directFramePICCounts
	budgetThread := newVMThread(runtimeGlobals(nil))
	budgetThread.directFrameInstrumented = true
	budgetThread.directFrameOpcodeCounts = &budgetOpcodes
	budgetThread.directFramePICCounts = &budgetCounts
	budgetThread.instructionBudget = 10
	if _, err := budgetThread.run(proto, nil, nil); err != nil {
		t.Fatalf("budget thread.run returned error: %v", err)
	}
	if got := budgetCounts.sideExitCount(directFrameSideExitReasonBudget); got != 0 {
		t.Fatalf("budget side exits = %d, want budget-capable fast loop", got)
	}
	if got := budgetOpcodes.count(opReturnOne) + budgetOpcodes.count(opReturn); got == 0 {
		t.Fatalf("budget opcode counts recorded no return, want direct execution")
	}
}

func TestRunDirectFrameUnaryNumericNegationPreservesValues(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 10 do
	local delta = i - 7
	if delta < 0 then
		delta = -delta
	end
	total = total + delta
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "NEG") {
		t.Fatalf("compiled unary negation program is missing NEG:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled unary negation program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 27 {
		t.Fatalf("Run result is %v (%t), want number 27", got, ok)
	}
}

func TestRunDirectFrameTableInsertRemoveIntrinsicsPreserveValues(t *testing.T) {
	proto, err := Compile(`
local values = {1, 3}
table.insert(values, 2, 2)
local removed = table.remove(values, 1)
return removed, values[1], values[2]
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"TABLE_INSERT", "TABLE_REMOVE"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled table intrinsic program is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled table intrinsic program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	wants := []float64{1, 2, 3}
	for i, want := range wants {
		got, ok := results[i].Number()
		if !ok || got != want {
			t.Fatalf("result %d is %v (%t), want number %v", i, results[i], ok, want)
		}
	}
}

func TestCompilerUsesMixedTableNextJumpForGenericFor(t *testing.T) {
	proto, err := Compile(`
local values = {}
values.name = 2
values[2] = 3
values.ready = 4
local total = 0
for key, value in values do
	total = total + value
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"PREPARE_ITER", "ARRAY_NEXT_JUMP2"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled mixed-table loop is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled mixed-table loop is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
}

func TestRunDirectFrameMixedTableIterationMatchesPairs(t *testing.T) {
	proto, err := Compile(`
local values = {}
values.name = 2
values[2] = 3
values.ready = 4
local direct = 0
for key, value in values do
	direct = direct + value
end
local viaPairs = 0
for key, value in pairs(values) do
	viaPairs = viaPairs + value
end
return direct, viaPairs
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled mixed-table loop is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("direct-frame generic side exits = %d, want 0 for mixed-table raw iteration", got)
	}
	if got := snapshot.opcodeCounts.count(opArrayNextJump2); got == 0 {
		t.Fatal("direct-frame ARRAY_NEXT_JUMP2 count is 0, want mixed-table iteration to stay in direct frame")
	}
	direct, ok := results[0].Number()
	if !ok {
		t.Fatalf("first result is %s, want number", results[0].Kind())
	}
	viaPairs, ok := results[1].Number()
	if !ok {
		t.Fatalf("second result is %s, want number", results[1].Kind())
	}
	if direct != viaPairs || direct != 9 {
		t.Fatalf("direct result %v and pairs result %v, want matching total 9", direct, viaPairs)
	}
}

func TestRunDirectFrameConcatLenPowRawFastPaths(t *testing.T) {
	proto, err := Compile(`
	local function compute(sep, ready, suffix, base)
		local values = {10, 20, 30}
		local label = "hp" .. sep .. ready
		local length = #values + #suffix
		local power = base ^ 5
		return label, length, power
	end
	return compute(":", "ready", "ab", 2)
	`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) != 1 {
		t.Fatalf("compiled raw fast-path program has %d child prototypes, want 1", len(proto.prototypes))
	}
	compute := proto.prototypes[0]
	joined := strings.Join(disassembleProto(compute), "\n")
	for _, want := range []string{"CONCAT_CHAIN", "LEN", "POW"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled raw fast-path program is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(compute) {
		t.Fatalf("compiled raw fast-path function is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(compute), "\n"))
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("direct-frame generic side exits = %d, want 0 for raw CONCAT/LEN/POW", got)
	}
	if got := snapshot.opcodeCounts.count(opConcatChain); got == 0 {
		t.Fatal("direct-frame CONCAT_CHAIN count is 0, want raw concat handled directly")
	}
	if got := snapshot.opcodeCounts.count(opLen); got == 0 {
		t.Fatal("direct-frame LEN count is 0, want raw length handled directly")
	}
	if got := snapshot.opcodeCounts.count(opPow); got == 0 {
		t.Fatal("direct-frame POW count is 0, want raw power handled directly")
	}
	label, ok := results[0].String()
	if !ok || label != "hp:ready" {
		t.Fatalf("label result is %v (%t), want hp:ready", results[0], ok)
	}
	length, ok := results[1].Number()
	if !ok || length != 5 {
		t.Fatalf("length result is %v (%t), want 5", results[1], ok)
	}
	power, ok := results[2].Number()
	if !ok || power != 32 {
		t.Fatalf("power result is %v (%t), want 32", results[2], ok)
	}
}

func TestCompilerEmitsConcatChainForAssociativeRawConcat(t *testing.T) {
	proto, err := Compile(`
local suffix = "ready"
local label = "hp" .. ":" .. 25 .. "/" .. suffix
return label
	`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "CONCAT_CHAIN") {
		t.Fatalf("compiled concat chain is missing CONCAT_CHAIN:\n%s", joined)
	}
	if strings.Count(joined, "CONCAT ") != 0 {
		t.Fatalf("compiled concat chain kept pairwise CONCAT:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].String()
	if !ok || got != "hp:25/ready" {
		t.Fatalf("Run result is %v (%t), want hp:25/ready", results[0], ok)
	}
}

func TestConcatChainPreservesMetamethodFallbackOrder(t *testing.T) {
	proto, err := Compile(`
return "a" .. left .. right .. "d"
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if joined := strings.Join(disassembleProto(proto), "\n"); !strings.Contains(joined, "CONCAT_CHAIN") {
		t.Fatalf("compiled concat chain is missing CONCAT_CHAIN:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled concat chain is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	var calls []string
	left := NewTable()
	leftMeta := NewTable()
	leftMeta.setRawStringField("__concat", HostFuncValue(func(args []Value) ([]Value, error) {
		calls = append(calls, "left")
		prefix, ok := args[0].String()
		if !ok || prefix != "a" {
			return nil, fmt.Errorf("left __concat first arg is %s, want string a", args[0].Kind())
		}
		return []Value{StringValue("ab")}, nil
	}))
	left.setMetatable(leftMeta)

	right := NewTable()
	rightMeta := NewTable()
	rightMeta.setRawStringField("__concat", HostFuncValue(func(args []Value) ([]Value, error) {
		calls = append(calls, "right")
		prefix, ok := args[0].String()
		if !ok || prefix != "ab" {
			return nil, fmt.Errorf("right __concat first arg is %s, want string ab", args[0].Kind())
		}
		return []Value{StringValue("abc")}, nil
	}))
	right.setMetatable(rightMeta)

	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, map[string]Value{
		"left":  TableValue(left),
		"right": TableValue(right),
	})
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonMetatable); got == 0 {
		t.Fatal("metatable side exits = 0, want concat chain cold island")
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("generic-frame side exits = %d, want concat chain cold island to stay local", got)
	}
	got, ok := results[0].String()
	if !ok || got != "abcd" {
		t.Fatalf("Run result is %v (%t), want abcd", results[0], ok)
	}
	if !reflect.DeepEqual(calls, []string{"left", "right"}) {
		t.Fatalf("concat metamethod calls are %#v, want left then right", calls)
	}
}

func TestRunDirectFrameConcatLenPowSideExitForMetamethods(t *testing.T) {
	proto, err := Compile(`
return #lenObject, concatObject .. "-vm", powObject ^ 3
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled metamethod side-exit program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	lenObject := NewTable()
	lenMetatable := NewTable()
	lenMetatable.setRawStringField("__len", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{NumberValue(4)}, nil
	}))
	lenObject.setMetatable(lenMetatable)

	concatObject := NewTable()
	concatMetatable := NewTable()
	concatMetatable.setRawStringField("__concat", HostFuncValue(func(args []Value) ([]Value, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("__concat got %d args, want 2", len(args))
		}
		right, ok := args[1].String()
		if !ok {
			return nil, fmt.Errorf("__concat right arg is %s, want string", args[1].Kind())
		}
		return []Value{StringValue("ember" + right)}, nil
	}))
	concatObject.setMetatable(concatMetatable)

	powObject := NewTable()
	powMetatable := NewTable()
	powMetatable.setRawStringField("__pow", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{NumberValue(27)}, nil
	}))
	powObject.setMetatable(powMetatable)

	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, map[string]Value{
		"lenObject":    TableValue(lenObject),
		"concatObject": TableValue(concatObject),
		"powObject":    TableValue(powObject),
	})
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonMetatable); got == 0 {
		t.Fatal("metamethod program had 0 metatable side exits, want local side exit")
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("generic-frame side exits = %d, want local metatable islands to resume fast loop", got)
	}
	if got, ok := results[0].Number(); !ok || got != 4 {
		t.Fatalf("length result is %v (%t), want 4", results[0], ok)
	}
	if got, ok := results[1].String(); !ok || got != "ember-vm" {
		t.Fatalf("concat result is %v (%t), want ember-vm", results[1], ok)
	}
	if got, ok := results[2].Number(); !ok || got != 27 {
		t.Fatalf("power result is %v (%t), want 27", results[2], ok)
	}
}

func TestFastLoopResumesAfterColdIsland(t *testing.T) {
	proto, err := Compile(`return #lenObject + 3`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled cold-island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	lenObject := NewTable()
	metatable := NewTable()
	metatable.setRawStringField("__len", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{NumberValue(4)}, nil
	}))
	lenObject.setMetatable(metatable)

	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, map[string]Value{
		"lenObject": TableValue(lenObject),
	})
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonMetatable); got == 0 {
		t.Fatal("metatable side exits = 0, want cold island")
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("generic-frame side exits = %d, want fast loop resume", got)
	}
	if got := snapshot.opcodeCounts.count(opAddK); got == 0 {
		t.Fatalf("ADD_K count = 0, want fast loop to resume after cold island")
	}
	if got, ok := results[0].Number(); !ok || got != 7 {
		t.Fatalf("Run result is %v (%t), want 7", results[0], ok)
	}
}

func TestUnsupportedOpcodeSideExitsPerInstruction(t *testing.T) {
	proto, err := Compile(`
local sum = left + right
return sum + 3
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled unsupported-op island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	left := NewTable()
	right := NewTable()
	metatable := NewTable()
	metatable.setRawStringField("__add", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{NumberValue(4)}, nil
	}))
	left.setMetatable(metatable)

	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, map[string]Value{
		"left":  TableValue(left),
		"right": TableValue(right),
	})
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonMetatable); got == 0 {
		t.Fatal("metatable side exits = 0, want unsupported ADD cold island")
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("generic-frame side exits = %d, want per-instruction cold island", got)
	}
	if got := snapshot.opcodeCounts.count(opAddK); got == 0 {
		t.Fatalf("ADD_K count = 0, want fast loop to resume after ADD cold island")
	}
	if got, ok := results[0].Number(); !ok || got != 7 {
		t.Fatalf("Run result is %v (%t), want 7", results[0], ok)
	}
}

func TestGenericColdIslandResumesAfterHostCall(t *testing.T) {
	proto, err := Compile(`
local value = f(1, 2)
return value + 3
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled host-call island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, map[string]Value{
		"f": HostFuncValue(func(args []Value) ([]Value, error) {
			if len(args) != 2 {
				t.Fatalf("host call received %d args, want 2", len(args))
			}
			return []Value{NumberValue(4)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.opcodeCounts.count(opAddK); got == 0 {
		t.Fatalf("ADD_K count = 0, want fast loop to resume after host-call cold island")
	}
	if got, ok := results[0].Number(); !ok || got != 7 {
		t.Fatalf("Run result is %v (%t), want 7", results[0], ok)
	}
}

func TestFastLoopResumesAfterArithmeticColdIslands(t *testing.T) {
	proto, err := Compile(`
local a = object - 1
local b = object * 1
local c = object / 1
local d = object % 1
local e = object // 1
local f = -object
local g = object + 1
return a + b + c + d + e + f + g + 3
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled arithmetic-island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	object := NewTable()
	metatable := NewTable()
	metatable.setRawStringField("__sub", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{NumberValue(2)}, nil
	}))
	metatable.setRawStringField("__mul", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{NumberValue(3)}, nil
	}))
	metatable.setRawStringField("__div", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{NumberValue(4)}, nil
	}))
	metatable.setRawStringField("__mod", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{NumberValue(5)}, nil
	}))
	metatable.setRawStringField("__idiv", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{NumberValue(6)}, nil
	}))
	metatable.setRawStringField("__unm", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{NumberValue(7)}, nil
	}))
	metatable.setRawStringField("__add", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{NumberValue(8)}, nil
	}))
	object.setMetatable(metatable)

	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, map[string]Value{
		"object": TableValue(object),
	})
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonMetatable); got < 7 {
		t.Fatalf("metatable side exits = %d, want arithmetic cold islands", got)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("generic-frame side exits = %d, want per-instruction arithmetic islands", got)
	}
	if got := snapshot.opcodeCounts.count(opReturnOne) + snapshot.opcodeCounts.count(opReturn); got == 0 {
		t.Fatalf("return opcode count = 0, want fast loop to reach return")
	}
	if got, ok := results[0].Number(); !ok || got != 38 {
		t.Fatalf("Run result is %v (%t), want 38", results[0], ok)
	}
}

func TestFastLoopResumesAfterComparisonColdIslands(t *testing.T) {
	proto, err := Compile(`
local eq = left == right
local ne = left ~= right
local lt = left < right
local le = left <= right
local gt = left > right
local ge = left >= right
if eq and not ne and lt and le and gt and ge then
	return 7
end
return 0
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled comparison-island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	left := NewTable()
	right := NewTable()
	metatable := NewTable()
	metatable.setRawStringField("__eq", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{BoolValue(true)}, nil
	}))
	metatable.setRawStringField("__lt", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{BoolValue(true)}, nil
	}))
	metatable.setRawStringField("__le", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{BoolValue(true)}, nil
	}))
	left.setMetatable(metatable)
	right.setMetatable(metatable)

	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, map[string]Value{
		"left":  TableValue(left),
		"right": TableValue(right),
	})
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonMetatable); got < 6 {
		t.Fatalf("metatable side exits = %d, want comparison cold islands", got)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("generic-frame side exits = %d, want per-instruction comparison islands", got)
	}
	if got, ok := results[0].Number(); !ok || got != 7 {
		t.Fatalf("Run result is %v (%t), want 7", results[0], ok)
	}
}

func TestFastLoopResumesAfterComparisonBranchColdIslands(t *testing.T) {
	proto, err := Compile(`
local score = 0
if left < right then
	score = score + 1
end
if left > right then
	score = score + 2
end
if left == right then
	score = score + 4
end
return score + 3
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled comparison-branch island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	left := NewTable()
	right := NewTable()
	metatable := NewTable()
	metatable.setRawStringField("__lt", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{BoolValue(true)}, nil
	}))
	metatable.setRawStringField("__eq", HostFuncValue(func(_ []Value) ([]Value, error) {
		return []Value{BoolValue(true)}, nil
	}))
	left.setMetatable(metatable)
	right.setMetatable(metatable)

	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, map[string]Value{
		"left":  TableValue(left),
		"right": TableValue(right),
	})
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonMetatable); got < 3 {
		t.Fatalf("metatable side exits = %d, want comparison branch cold islands", got)
	}
	if got := snapshot.picCounts.sideExitCount(directFrameSideExitReasonGenericFrame); got != 0 {
		t.Fatalf("generic-frame side exits = %d, want local comparison branch islands", got)
	}
	if got, ok := results[0].Number(); !ok || got != 10 {
		t.Fatalf("Run result is %v (%t), want 10", results[0], ok)
	}
}

func TestRunDirectFrameRawLenGlobalPreservesValues(t *testing.T) {
	proto, err := Compile(`
local values = {1, 2, 3}
local total = 0
for i = 1, 4 do
	total = total + rawlen(values)
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "RAW_LEN") {
		t.Fatalf("compiled rawlen program is missing RAW_LEN intrinsic:\n%s", joined)
	}
	if strings.Contains(joined, "LOAD_GLOBAL") || strings.Contains(joined, "CALL_ONE") {
		t.Fatalf("compiled rawlen program still uses global call shape:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled rawlen program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 12 {
		t.Fatalf("Run result is %v (%t), want number 12", got, ok)
	}
	if snapshot.picCounts.intrinsicGuardHits == 0 {
		t.Fatalf("rawlen intrinsic guard hits = 0, want guard reuse after first resolution:\n%s", summarizeDirectFrameMechanisms(snapshot))
	}
}

func TestRunDirectFrameArrayIterationPreservesRowOrderAndNilTermination(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{value = 2},
	{value = 3},
}
local total = 0
for _, row in rows do
	total = total + row.value
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "PREPARE_ITER") || !strings.Contains(joined, "ARRAY_NEXT") {
		t.Fatalf("compiled array iteration is missing iterator setup/call:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled array iteration is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 5 {
		t.Fatalf("Run result is %v (%t), want number 5", got, ok)
	}
}

func TestCompilerUsesArrayNextJumpForTwoResultArrayIteration(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{value = 2},
	{value = 3},
}
local total = 0
for i, row in rows do
	total = total + row.value + i
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "ARRAY_NEXT_JUMP2") {
		t.Fatalf("compiled two-result array iteration is missing ARRAY_NEXT_JUMP2:\n%s", joined)
	}
	if strings.Contains(joined, "NOT_EQUAL") {
		t.Fatalf("compiled two-result array iteration kept separate nil branch:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled two-result array iteration is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 8 {
		t.Fatalf("Run result is %v (%t), want number 8", got, ok)
	}
}

func TestCompileRunIteratorDCEPreservesEffects(t *testing.T) {
	proto, err := Compile(`
local rows = {1, 2, 3}
local total = 0
for i, value in rows do
	local unused = 99
	total = total + i + value
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "PREPARE_ITER") || !strings.Contains(joined, "ARRAY_NEXT_JUMP2") {
		t.Fatalf("compiled iterator program is missing iterator opcodes:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 12 {
		t.Fatalf("Run result is %v (%t), want number 12", got, ok)
	}
}

func TestArrayNextIteratorOpcodePreservesMetatableIteratorFallback(t *testing.T) {
	proto, err := Compile(`
local object = {}
setmetatable(object, {
	__iter = function()
		local i = 0
		return function()
			i = i + 1
			if i > 3 then
				return nil
			end
			return i, i * 2
		end
	end,
})
local total = 0
for _, value in object do
	total = total + value
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "ARRAY_NEXT") {
		t.Fatalf("compiled custom iterator program is missing ARRAY_NEXT:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 12 {
		t.Fatalf("Run result is %v (%t), want number 12", got, ok)
	}
}

func TestRunDirectFrameStringFieldBranchPredicatesPreserveSemantics(t *testing.T) {
	proto, err := Compile(`
local item = {kind = "gem", shield = 3, alive = true, hp = 0}
local score = 0
if item.alive then
	score = score + 1
end
if item.kind == "gem" or item.kind == "key" then
	score = score + 10
end
if item.shield > 0 then
	score = score + 100
end
if item.hp <= 0 then
	score = score + 1000
end
return score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"GET_STRING_FIELD",
		"JUMP_IF_FALSE",
		"JUMP_IF_NOT_EQUAL_K",
		"JUMP_IF_NOT_GREATER_K",
		"JUMP_IF_GREATER_K",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled branch program is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled branch program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 1111 {
		t.Fatalf("Run result is %v (%t), want number 1111", got, ok)
	}
}

func TestScenarioProgramsDoNotEmitBenchmarkNamedArtifacts(t *testing.T) {
	sources := []string{
		`
local entities = {
	{hp = 120, shield = 12, regen = 2, damage = 13, alive = true},
	{hp = 95, shield = 24, regen = 1, damage = 8, alive = true},
}
local score = 0
for tick = 1, 3 do
	for _, entity in entities do
		if entity.alive then
			local incoming = entity.damage + tick % 5
			if entity.shield > 0 then
				local absorbed = math.min(entity.shield, incoming)
				entity.shield = entity.shield - absorbed
				incoming = incoming - absorbed
			end
			entity.hp = entity.hp - incoming + entity.regen
			score = score + entity.hp + entity.shield
		end
	end
end
return score
`,
		`
local inventory = {
	{kind = "ore", count = 12, value = 5, rarity = 1},
	{kind = "gem", count = 3, value = 40, rarity = 4},
}
local score = 0
for day = 1, 3 do
	for _, item in inventory do
		local bonus = item.rarity * (day % 4 + 1)
		if item.kind == "gem" or item.kind == "key" then
			score = score + item.count * (item.value + bonus)
		else
			score = score + item.count * item.value + bonus
		end
	end
end
return score
`,
		`
local self = {hp = 72, energy = 40, threat = 9}
local targets = {{hp = 30, distance = 4, threat = 7, armor = 2}}
local actions = {{kind = "attack", cost = 8, base = 20, range = 5}}
local total = 0
for tick = 1, 3 do
	local best = -9999
	for _, action in actions do
		for _, target in targets do
			local score = action.base + self.threat - target.armor
			if action.kind == "attack" then
				score = score + (100 - target.hp) // 4
			end
			best = score
		end
	end
	total = total + best
end
return total
`,
	}
	for _, source := range sources {
		proto, err := Compile(source)
		if err != nil {
			t.Fatalf("Compile returned error: %v", err)
		}
		if _, err := Run(proto); err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		artifact := strings.Join(append(disassembleProto(proto), disassembleProtoFacts(proto)...), "\n")
		for _, forbidden := range []string{
			"INVENTORY_VALUE_STEP",
			"COMBAT_TICK_STEP",
			"EVENT_DISPATCH_STEP",
			"AI_UTILITY_SCORE_STEP",
			"ABILITY_RESOLUTION_STEP",
			"BUFF_STACK_TICK_STEP",
			"ECONOMY_MARKET_TICK_STEP",
			"scenario_loop_region",
			"typed_row_slot",
			"mutation_slot",
			"intrinsic_guard",
			"handler_cache",
			"no_yield_handler",
		} {
			if strings.Contains(artifact, forbidden) {
				t.Fatalf("compiled artifact contains forbidden benchmark artifact %s:\n%s", forbidden, artifact)
			}
		}
	}
}

func TestCompilerUsesConstantArithmeticOperands(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 3 do
	total = total + ((i * 3 - i // 2) % 17)
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if strings.Contains(joined, "ADD_NUMERIC_MOD_K") {
		t.Fatalf("compiled arithmetic still uses removed ADD_NUMERIC_MOD_K:\n%s", joined)
	}
	for _, want := range []string{"number 3", "number 2", "number 17"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled arithmetic is missing %s:\n%s", want, joined)
		}
	}
}

func TestCompilerUsesRegisterNumericLessBranch(t *testing.T) {
	proto, err := Compile(`
local limits = {5, 3, 9}
local total = 0
for i = 1, 6 do
	local candidate = i + (i % 2)
	local limit = limits[(i % 3) + 1]
	if candidate < limit then
		total = total + candidate
	else
		total = total - limit
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "JUMP_IF_NOT_LESS") {
		t.Fatalf("compiled numeric branch is missing register branch opcode:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled numeric branch program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 6 {
		t.Fatalf("Run result is %v (%t), want number 6", got, ok)
	}
}

func TestRegisterNumericLessBranchFallsBackToStringComparison(t *testing.T) {
	proto, err := Compile(`
local function compare(left, right)
	if left < right then
		return 7
	end
	return 0
end
return compare("apple", "pear")
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) != 1 {
		t.Fatalf("compiled comparison program has %d child prototypes, want 1", len(proto.prototypes))
	}
	joined := strings.Join(disassembleProto(proto.prototypes[0]), "\n")
	if !strings.Contains(joined, "JUMP_IF_NOT_LESS") {
		t.Fatalf("compiled string comparison branch is missing register branch opcode:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 7 {
		t.Fatalf("Run result is %v (%t), want number 7", got, ok)
	}
}

func TestCompilerUsesRegisterNumericGreaterBranch(t *testing.T) {
	proto, err := Compile(`
local scores = {3, 8, 5, 12}
local best = -999
for _, score in scores do
	if score > best then
		best = score
	end
end
return best
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "JUMP_IF_NOT_GREATER") {
		t.Fatalf("compiled numeric greater branch is missing register branch opcode:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled numeric greater branch program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 12 {
		t.Fatalf("Run result is %v (%t), want number 12", got, ok)
	}
}

func TestCompilerFusesGenericLessThanBranch(t *testing.T) {
	proto, err := Compile(`
local index = 3
local depth = 0
local total = 0
while index > 0 and depth < 4 do
	total = total + index + depth
	index = index - 1
	depth = depth + 1
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "JUMP_IF_NOT_GREATER") {
		t.Fatalf("compiled greater-than branch is missing fused register branch:\n%s", joined)
	}
	if !strings.Contains(joined, "JUMP_IF_NOT_LESS_K") {
		t.Fatalf("compiled less-than constant branch is missing fused constant branch:\n%s", joined)
	}
	for _, line := range disassembleProto(proto) {
		if strings.Contains(line, "GREATER r") || strings.Contains(line, "LESS r") {
			t.Fatalf("compiled loop materialized comparison before branch:\n%s", joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 9 {
		t.Fatalf("Run result is %v (%t), want number 9", got, ok)
	}
}

func TestCompareBranchFusionPreservesMetamethodCallOrder(t *testing.T) {
	proto, err := Compile(`
local seen = "none"
local object = {}
object = setmetatable(object, {
	__lt = function(left, right)
		if type(left) == "number" and right == object then
			seen = "number-object"
		else
			seen = "wrong-order"
		end
		return true
	end,
})
if object > 3 then
	if seen == "number-object" then
		return 7
	end
	return 1
end
return 0
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "JUMP_IF_NOT_GREATER_K") {
		t.Fatalf("compiled greater-than constant branch is missing fused constant branch:\n%s", joined)
	}
	for _, line := range disassembleProto(proto) {
		if strings.Contains(line, "GREATER r") {
			t.Fatalf("compiled metamethod branch materialized comparison before branch:\n%s", joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 7 {
		t.Fatalf("Run result is %v (%t), want number 7", got, ok)
	}
}

func TestCompilerFusesLessEqualAndGreaterEqualBranches(t *testing.T) {
	proto, err := Compile(`
local i = 1
local total = 0
while i <= 3 do
	total = total + i
	i = i + 1
end
if total >= 6 then
	return total
end
return 0
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "JUMP_IF_GREATER_K") {
		t.Fatalf("compiled less-equal branch is missing fused greater-than constant branch:\n%s", joined)
	}
	if !strings.Contains(joined, "JUMP_IF_LESS_K") {
		t.Fatalf("compiled greater-equal branch is missing fused less-than constant branch:\n%s", joined)
	}
	for _, line := range disassembleProto(proto) {
		if strings.Contains(line, "LESS_EQUAL r") || strings.Contains(line, "GREATER_EQUAL r") {
			t.Fatalf("compiled branch materialized relational comparison before branch:\n%s", joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 6 {
		t.Fatalf("Run result is %v (%t), want number 6", got, ok)
	}
}

func TestRunDirectFrameSquaredDistanceBlockPreservesLiveScratchRegisters(t *testing.T) {
	proto, err := Compile(`
local projectile = {x = 3, y = 4}
local target = {x = 0, y = 0, radius = 5}
local dx = projectile.x - target.x
local dy = projectile.y - target.y
if dx * dx + dy * dy <= target.radius * target.radius then
	return dx + dy + target.radius
end
return 0
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 12 {
		t.Fatalf("Run result is %v (%t), want number 12", results[0], ok)
	}
}

func TestCompilerRejectsAllCompleteReductionWithCallInMutationBody(t *testing.T) {
	proto, err := Compile(`
local objectives = {
	{have = 1, need = 2},
}
local complete = true
local touched = 0
local function touch()
	touched = touched + 1
end
for _, objective in objectives do
	if objective.have < objective.need then
		touch()
		complete = false
	end
end
return complete, touched
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if strings.Contains(facts, "kind all_complete") {
		t.Fatalf("compiled side-effectful all-complete branch unexpectedly emitted reduction fact:\n%s\nbytecode:\n%s", facts, strings.Join(disassembleProto(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	if got, ok := results[0].Bool(); !ok || got {
		t.Fatalf("first result is %v (%t), want false", results[0], ok)
	}
	if got, ok := results[1].Number(); !ok || got != 1 {
		t.Fatalf("second result is %v (%t), want number 1", results[1], ok)
	}
}

func TestCompilerRejectsPairedRowDiffReductionAfterPairMutation(t *testing.T) {
	proto, err := Compile(`
local before = {
	{hp = 10},
}
local after = {
	{hp = 13},
}
local total = 0
for i, left in before do
	local right = after[i]
	right.hp = right.hp + 1
	local delta = left.hp - right.hp
	if delta < 0 then
		delta = -delta
	end
	total = total + delta
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if strings.Contains(facts, "kind paired_row_diff") {
		t.Fatalf("compiled pair mutation branch unexpectedly emitted paired-row reduction fact:\n%s\nbytecode:\n%s", facts, strings.Join(disassembleProto(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 4 {
		t.Fatalf("result is %v (%t), want number 4", results[0], ok)
	}
}

func TestCompilerRejectsPairedRowDiffReductionWhenRowsMayAlias(t *testing.T) {
	proto, err := Compile(`
local before = {
	{hp = 10},
}
local after = before
local total = 0
for i, left in before do
	local right = after[i]
	local delta = left.hp - right.hp
	if delta < 0 then
		delta = -delta
	end
	total = total + delta
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if strings.Contains(facts, "kind paired_row_diff") {
		t.Fatalf("compiled aliasing paired-row diff unexpectedly emitted paired-row reduction fact:\n%s\nbytecode:\n%s", facts, strings.Join(disassembleProto(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 0 {
		t.Fatalf("result is %v (%t), want number 0", results[0], ok)
	}
}

func TestNumericSuperinstructionPreservesNumericStringLoopConversion(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = "1", "3" do
	total = total + ((i * 3 - i // 2) % 17)
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if strings.Contains(joined, "ADD_NUMERIC_MOD_K") {
		t.Fatalf("compiled arithmetic still uses removed ADD_NUMERIC_MOD_K:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	got, ok := results[0].Number()
	if !ok || got != 16 {
		t.Fatalf("Run result is %v (%t), want number 16", got, ok)
	}
}

func TestFinalizedProtoCachesNumberConstants(t *testing.T) {
	proto, err := Compile(`
return input + 2
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	found := false
	for i, constant := range proto.constants {
		if number, ok := constant.Number(); ok && number == 2 {
			found = true
			if !proto.constantNumberOK[i] {
				t.Fatalf("constant %d is number 2 but constantNumberOK is false", i)
			}
			if proto.constantNumbers[i] != 2 {
				t.Fatalf("constantNumbers[%d] is %v, want 2", i, proto.constantNumbers[i])
			}
		}
	}
	if !found {
		t.Fatalf("compiled constants are %#v, want number 2", proto.constants)
	}
}

func TestCompilerDeduplicatesConstantsWithinProto(t *testing.T) {
	proto, err := Compile(`
local first = "same"
local second = "same"
local left = 7
local right = 7
return first, second, left, right, left + right
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	stringCount := 0
	numberCount := 0
	for _, constant := range proto.constants {
		if value, ok := constant.String(); ok && value == "same" {
			stringCount++
		}
		if value, ok := constant.Number(); ok && value == 7 {
			numberCount++
		}
	}
	if stringCount != 1 {
		t.Fatalf("compiled constants contain %d copies of string %q, want 1: %#v", stringCount, "same", proto.constants)
	}
	if numberCount != 1 {
		t.Fatalf("compiled constants contain %d copies of number 7, want 1: %#v", numberCount, proto.constants)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].String(); !ok || got != "same" {
		t.Fatalf("first result is %v (%t), want same", results[0], ok)
	}
	if got, ok := results[1].String(); !ok || got != "same" {
		t.Fatalf("second result is %v (%t), want same", results[1], ok)
	}
	if got, ok := results[2].Number(); !ok || got != 7 {
		t.Fatalf("third result is %v (%t), want 7", results[2], ok)
	}
	if got, ok := results[3].Number(); !ok || got != 7 {
		t.Fatalf("fourth result is %v (%t), want 7", results[3], ok)
	}
	if got, ok := results[4].Number(); !ok || got != 14 {
		t.Fatalf("fifth result is %v (%t), want 14", results[4], ok)
	}
}

func TestCompilerUsesConstantComparisonBranches(t *testing.T) {
	proto, err := Compile(`
local i = 0
local total = 0
while i < 3 do
	i = i + 1
	if i == 2 then
		total = total + 10
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"JUMP_IF_NOT_LESS_K", "JUMP_IF_NOT_EQUAL_K"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled branches are missing %s:\n%s", want, joined)
		}
	}
}

func TestCompilerUsesModuloConstantBranch(t *testing.T) {
	proto, err := Compile(`
local i = 0
local total = 0
while i < 10 do
	i = i + 1
	if i % 5 == 0 then
		total = total + 10
	elseif i % 2 == 0 then
		total = total + 2
	else
		total = total + 1
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"MOD_K", "JUMP_IF_NOT_EQUAL_K"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled modulo branch is missing %s:\n%s", want, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 32 {
		t.Fatalf("Run result is %v (%t), want number 32", got, ok)
	}
}

func TestModuloConstantBranchFallsBackToMetamethod(t *testing.T) {
	proto, err := Compile(`
local value = setmetatable({}, {
	__mod = function(left, right)
		return 0
	end,
})
if value % 5 == 0 then
	return 1
end
return 2
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "MOD_K") || !strings.Contains(joined, "JUMP_IF_NOT_EQUAL_K") {
		t.Fatalf("compiled modulo branch is missing general modulo sequence:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 1 {
		t.Fatalf("Run result is %v (%t), want number 1", got, ok)
	}
}

func TestCompilerAddsTableLiteralCapacityHints(t *testing.T) {
	proto, err := Compile(`return {1, 2, name = "ember"}`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "NEW_TABLE r0 2 1") {
		t.Fatalf("compiled table literal is missing capacity hints:\n%s", joined)
	}
}

func TestCompilerUsesTableIntrinsicOpcodes(t *testing.T) {
	proto, err := Compile(`
local values = {}
table.insert(values, 1)
return table.remove(values, 1)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"TABLE_INSERT", "TABLE_REMOVE"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled table calls are missing %s:\n%s", want, joined)
		}
	}
}

func TestCompilerUsesStringFieldOpcodes(t *testing.T) {
	proto, err := Compile(`
local player = {stats = {hp = 10}}
player.stats.hp = player.stats.hp + 5
return player.stats.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD", "SET_STRING_FIELD"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled named field access is missing %s:\n%s", want, joined)
		}
	}
}

func TestCompilerLowersAddStringFieldToCanonicalOpcodes(t *testing.T) {
	proto, err := Compile(`
local counter = {value = 1}
local amount = 2
counter.value = counter.value + amount
return counter.value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "ADD ") || !strings.Contains(joined, "GET_STRING_FIELD") || !strings.Contains(joined, "SET_STRING_FIELD") {
		t.Fatalf("compiled field increment is missing canonical GET/ADD/SET sequence:\n%s", joined)
	}
}

func TestCompilerLowersSubStringFieldToCanonicalOpcodes(t *testing.T) {
	proto, err := Compile(`
local counter = {value = 10}
local amount = 3
counter.value = counter.value - amount
return counter.value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "SUB ") || !strings.Contains(joined, "GET_STRING_FIELD") || !strings.Contains(joined, "SET_STRING_FIELD") {
		t.Fatalf("compiled field decrement is missing canonical GET/SUB/SET sequence:\n%s", joined)
	}
}

func TestCompileAndRunCanonicalAddStringFieldUsesMetatableSemantics(t *testing.T) {
	proto, err := Compile(`
local backing = {value = 10}
local proxy = {}
setmetatable(proxy, {
	__index = backing,
	__newindex = backing
})
local amount = 2
proxy.value = proxy.value + amount
return backing.value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "ADD ") || !strings.Contains(joined, "GET_STRING_FIELD") || !strings.Contains(joined, "SET_STRING_FIELD") {
		t.Fatalf("compiled field increment is missing canonical GET/ADD/SET sequence:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 12 {
		t.Fatalf("Run result is %v (%t), want number 12", got, ok)
	}
}

func TestCompileAndRunCanonicalSubStringFieldUsesMetatableSemantics(t *testing.T) {
	proto, err := Compile(`
local backing = {value = 10}
local proxy = {}
setmetatable(proxy, {
	__index = backing,
	__newindex = backing
})
local amount = 3
proxy.value = proxy.value - amount
return backing.value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "SUB ") || !strings.Contains(joined, "GET_STRING_FIELD") || !strings.Contains(joined, "SET_STRING_FIELD") {
		t.Fatalf("compiled field decrement is missing canonical GET/SUB/SET sequence:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 7 {
		t.Fatalf("Run result is %v (%t), want number 7", got, ok)
	}
}

func TestCompilerUsesSelectVarargCountFastCall(t *testing.T) {
	proto, err := Compile(`
local function count(...)
	return select("#", ...)
end
return count(1, 2, 3)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, want := len(proto.prototypes), 1; got != want {
		t.Fatalf("compiled root has %d child prototypes, want %d", got, want)
	}

	joined := strings.Join(disassembleProto(proto.prototypes[0]), "\n")
	if !strings.Contains(joined, "FAST_CALL") || !strings.Contains(joined, "SELECT") {
		t.Fatalf("compiled select count is missing SELECT fast call:\n%s", joined)
	}
	if strings.Contains(joined, "VARARG r") {
		t.Fatalf("compiled select count kept open VARARG plumbing:\n%s", joined)
	}
}

func TestCompilerUsesCoroutineResumeIntrinsicOpcode(t *testing.T) {
	proto, err := Compile(`
local co = coroutine.create(function(value)
	return value + 1
end)
local ok, value = coroutine.resume(co, 41)
return ok, value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "COROUTINE_RESUME") {
		t.Fatalf("compiled coroutine resume is missing COROUTINE_RESUME:\n%s", joined)
	}
}

func TestCompilerUsesMathMinIntrinsicOpcode(t *testing.T) {
	proto, err := Compile(`
local value = math.min(4, 2)
return value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "MATH_MIN") {
		t.Fatalf("compiled math.min is missing MATH_MIN:\n%s", joined)
	}
	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "intrinsic") || !strings.Contains(facts, "MATH_MIN") {
		t.Fatalf("compiled math.min is missing intrinsic descriptor:\n%s", facts)
	}
}

func TestRunDirectFrameBaseIntrinsicGuardHitsAfterFirstResolution(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 6 do
	total = total + math.min(i, 3)
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled intrinsic guard program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameInstrumented = true
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 15 {
		t.Fatalf("result is %v (%t), want number 15", results[0], ok)
	}
	if counts.intrinsicGuardChecks == 0 {
		t.Fatal("intrinsic guard checks = 0, want repeated math.min guard checks")
	}
	if counts.intrinsicGuardHits == 0 {
		t.Fatalf("intrinsic guard hits = 0, want base math.min guard to hit after first resolution (misses %d)", counts.intrinsicGuardMisses)
	}
}

func TestIntrinsicDescriptorsCarryGuardIdentity(t *testing.T) {
	proto, err := Compile(`
local values = {}
table.insert(values, 1)
local value = math.min(4, 2)
return values[1] + value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	intrinsics := deriveProtoDiagnosticFacts(proto).intrinsicOps
	if len(intrinsics) != 2 {
		t.Fatalf("intrinsic descriptor count = %d, want 2:\n%s", len(intrinsics), strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	var tableInsert intrinsicOpDesc
	var mathMin intrinsicOpDesc
	for _, desc := range intrinsics {
		switch desc.nativeID {
		case nativeFuncTableInsert:
			tableInsert = desc
		case nativeFuncMathMin:
			mathMin = desc
		}
	}
	if tableInsert.globalName != "table" || tableInsert.field != "insert" || tableInsert.nativeID != nativeFuncTableInsert {
		t.Fatalf("table.insert descriptor = %#v, want table insert native identity", tableInsert)
	}
	if mathMin.globalName != "math" || mathMin.field != "min" || mathMin.nativeID != nativeFuncMathMin {
		t.Fatalf("math.min descriptor = %#v, want math min native identity", mathMin)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	for _, want := range []string{
		"intrinsic",
		"global table",
		"field insert",
		"native TABLE_INSERT",
		"global math",
		"field min",
		"native MATH_MIN",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("intrinsic facts missing %q:\n%s", want, facts)
		}
	}
}

func TestCompilerRecordsRegisterAndConstantKindFacts(t *testing.T) {
	artifact := parseSourceForOptimizationTest(t, `
local n = 4
local s = "kind"
local b = n < 5
local t = {}
return n, s, b, t
`)
	proto, err := compileProgramWithOptions(artifact, compilerOptions{optimizations: optimizationOptions{
		disabledCategories: map[optimizationCategory]bool{optimizationBytecodePeephole: true},
	}})
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"constant_kind",
		"number",
		"string",
		"register_kind",
		"source constant",
		"source comparison",
		"source table_literal",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled kind fact program is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("Run returned %d results, want 4", len(results))
	}
}

func TestCompilerRecordsNumericOperandFactsForProvenNumbers(t *testing.T) {
	artifact := parseSourceForOptimizationTest(t, `
local left = 4
local right = 2
local sum = left + right
local scaled = sum * 3
local small = scaled < 20
return sum, scaled, small
`)
	proto, err := compileProgramWithOptions(artifact, compilerOptions{optimizations: optimizationOptions{
		disabledCategories: map[optimizationCategory]bool{optimizationBytecodePeephole: true},
	}})
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"numeric_operand",
		"ADD",
		"MUL_K",
		"LESS",
		"left r",
		"right r",
		"right k",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled numeric fact program is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Run returned %d results, want 3", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 6 {
		t.Fatalf("first result = %v (number %v), want number 6", results[0], ok)
	}
	if got, ok := results[1].Number(); !ok || got != 18 {
		t.Fatalf("second result = %v (number %v), want number 18", results[1], ok)
	}
	if got, ok := results[2].Bool(); !ok || !got {
		t.Fatalf("third result = %v (bool %v), want true", results[2], ok)
	}
}

func TestKindProvenNumericComparisonStillFallsBackForNaN(t *testing.T) {
	proto, err := Compile(`
local zero = 0
local nan = zero / zero
return nan < 1
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "numeric_operand") || !strings.Contains(facts, "LESS") {
		t.Fatalf("compiled NaN comparison did not record numeric comparison facts:\n%s", facts)
	}

	_, err = Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want NaN comparison error")
	}
	if !strings.Contains(err.Error(), "NaN") {
		t.Fatalf("Run error is %q, want NaN comparison detail", err)
	}
}

func TestCompilerRecordsSlotKindFacts(t *testing.T) {
	proto, err := Compile(`
local row = {hp = 3, tag = "kind", alive = true, child = {value = 2}}
local i = 0
local total = 0
while i < 4 do
	total = total + row.child.value
	total = total + row.child.value
	if row.alive then
		total = total + row.hp
	end
	i = i + 1
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"slot_kind",
		"field hp",
		"number",
		"field tag",
		"string",
		"field alive",
		"boolean",
		"field child",
		"table",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled slot kind program is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 28 {
		t.Fatalf("Run result is %v (%t), want 28", got, ok)
	}
}

func TestRunWithDirectFrameMechanismCountersGroupsAttribution(t *testing.T) {
	pathProto, err := Compile(`
local row = {child = {value = 3}}
local i = 0
local total = 0
while i < 6 do
	total = total + row.child.value
	total = total + row.child.value
	i = i + 1
end
return total
`)
	if err != nil {
		t.Fatalf("Compile path program returned error: %v", err)
	}
	pathResults, pathSnapshot, err := runWithDirectFrameMechanismCounters(pathProto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters path run returned error: %v", err)
	}
	gotPath, ok := pathResults[0].Number()
	if !ok || gotPath != 36 {
		t.Fatalf("instrumented path run result is %v (%t), want 36", pathResults[0], ok)
	}
	if len(pathSnapshot.rankedOpcodes()) == 0 {
		t.Fatal("ranked opcodes are empty, want direct-frame dispatch attribution")
	}

	callProto, err := Compile(`
local function add(a, b)
	return a + b
end
local total = 0
for i = 1, 8 do
	total = total + add(i, 2)
end
return total
`)
	if err != nil {
		t.Fatalf("Compile call program returned error: %v", err)
	}
	callResults, callSnapshot, err := runWithDirectFrameMechanismCounters(callProto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters call run returned error: %v", err)
	}
	gotCall, ok := callResults[0].Number()
	if !ok || gotCall != 52 {
		t.Fatalf("instrumented call run result is %v (%t), want 52", callResults[0], ok)
	}
	if callSnapshot.opcodeCount(opCallLocalOne) == 0 {
		t.Fatalf("CALL_LOCAL_ONE dispatch count = 0; ranked opcodes: %#v", callSnapshot.rankedOpcodes())
	}
	if callSnapshot.picCounts.fixedCallFrameReuses == 0 {
		t.Fatal("fixed-call frame reuses = 0, want grouped fixed-call attribution")
	}
}

func TestScenarioMechanismAttributionCoversCurrentWorstRows(t *testing.T) {
	cases := loadScenarioBenchmarkCases(t, []string{
		"combat_tick",
		"event_dispatch",
		"buff_stack_tick",
		"ability_resolution",
		"economy_market_tick",
		"cooldown_scheduler",
		"quest_progress_update",
		"behavior_tree_tick",
		"path_relaxation",
		"threat_aggro_table",
		"save_state_diff",
		"dialogue_condition_eval",
		"ai_utility_scoring",
		"formation_layout_score",
		"projectile_sweep",
		"procgen_room_scoring",
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proto, err := Compile(tc.source)
			if err != nil {
				t.Fatalf("Compile returned error: %v", err)
			}
			results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
			if err != nil {
				t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
			}
			if got := singleResultString(t, results); got != tc.want {
				t.Fatalf("instrumented run result is %q, want %q", got, tc.want)
			}
			if len(snapshot.rankedOpcodes()) == 0 {
				t.Fatal("ranked opcodes are empty, want direct-frame dispatch attribution")
			}
			if snapshot.picCounts.totalMechanismActivity() == 0 {
				t.Fatal("mechanism counters are empty, want non-dispatch attribution for current worst rows")
			}
			t.Logf("%s", summarizeDirectFrameMechanisms(snapshot))
		})
	}
}

func indexedMapBranchFixtureSource(secondOrder string) string {
	return `
local market = {
	stock = {wood = 10, ore = 2},
	demand = {wood = 8, ore = 7},
	price = {wood = 3, ore = 4},
}
local orders = {
	{good = "wood", amount = 7, kind = "buy"},
	` + secondOrder + `,
	{good = "ore", amount = 5, kind = "buy"},
}
local day = 2
local cash = 0
for _, order in orders do
	local good = order.good
	local pressure = market.demand[good] - market.stock[good] // 5
	local price = market.price[good] + pressure
	if price < 1 then
		price = 1
	end
	if order.kind == "buy" then
		local amount = math.min(order.amount + day % 3, market.stock[good])
		market.stock[good] = market.stock[good] - amount
		cash = cash - amount * price
	else
		local amount = order.amount + day % 2
		market.stock[good] = market.stock[good] + amount
		cash = cash + amount * price
	end
	market.price[good] = price + day % 2
end
return cash + market.stock.wood + market.stock.ore + market.price.wood + market.price.ore
`
}

type scenarioBenchmarkCase struct {
	name   string
	source string
	want   string
}

func loadScenarioBenchmarkCases(t *testing.T, names []string) []scenarioBenchmarkCase {
	t.Helper()
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	fileSet := token.NewFileSet()
	file, err := goparser.ParseFile(fileSet, "top10_luau_benchmark_test.go", nil, 0)
	if err != nil {
		t.Fatalf("ParseFile returned error: %v", err)
	}
	var cases []scenarioBenchmarkCase
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok || len(valueSpec.Names) != 1 || valueSpec.Names[0].Name != "scenarioLuauCases" || len(valueSpec.Values) != 1 {
				continue
			}
			lit, ok := valueSpec.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("scenarioLuauCases is %T, want composite literal", valueSpec.Values[0])
			}
			for _, element := range lit.Elts {
				caseLit, ok := element.(*ast.CompositeLit)
				if !ok {
					t.Fatalf("scenario case is %T, want composite literal", element)
				}
				tc := parseScenarioBenchmarkCase(t, caseLit)
				if wanted[tc.name] {
					cases = append(cases, tc)
					delete(wanted, tc.name)
				}
			}
		}
	}
	if len(wanted) != 0 {
		var missing []string
		for name := range wanted {
			missing = append(missing, name)
		}
		t.Fatalf("missing scenario benchmark cases: %s", strings.Join(missing, ", "))
	}
	return cases
}

func parseScenarioBenchmarkCase(t *testing.T, lit *ast.CompositeLit) scenarioBenchmarkCase {
	t.Helper()
	var tc scenarioBenchmarkCase
	for _, element := range lit.Elts {
		keyValue, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := keyValue.Key.(*ast.Ident)
		if !ok {
			continue
		}
		value, ok := keyValue.Value.(*ast.BasicLit)
		if !ok || value.Kind != token.STRING {
			continue
		}
		text, err := strconv.Unquote(value.Value)
		if err != nil {
			t.Fatalf("Unquote(%s) returned error: %v", value.Value, err)
		}
		switch key.Name {
		case "name":
			tc.name = text
		case "source":
			tc.source = text
		case "want":
			tc.want = text
		}
	}
	if tc.name == "" || tc.source == "" || tc.want == "" {
		t.Fatalf("incomplete scenario benchmark case: %#v", tc)
	}
	return tc
}

func singleResultString(t *testing.T, results []Value) string {
	t.Helper()
	if len(results) != 1 {
		t.Fatalf("instrumented run returned %d results, want 1", len(results))
	}
	result := results[0]
	if number, ok := result.Number(); ok {
		return strconv.FormatFloat(number, 'g', -1, 64)
	}
	if str, ok := result.String(); ok {
		return str
	}
	if value, ok := result.Bool(); ok {
		return strconv.FormatBool(value)
	}
	if result.IsNil() {
		return "nil"
	}
	t.Fatalf("instrumented run result has unsupported kind %s", result.Kind())
	return ""
}

func summarizeDirectFrameMechanisms(snapshot directFrameMechanismSnapshot) string {
	ranked := snapshot.rankedOpcodes()
	if len(ranked) > 8 {
		ranked = ranked[:8]
	}
	topOpcodes := make([]string, 0, len(ranked))
	for _, count := range ranked {
		topOpcodes = append(topOpcodes, fmt.Sprintf("%s=%d", opcodeName(count.op), count.count))
	}
	pic := snapshot.picCounts
	return fmt.Sprintf(
		"opcodes[%s] pic{hits=%d/%d keyMiss=%d shapeMiss=%d metaMiss=%d missing=%d nilWrite=%d invalid=%d arrayIndex=%d scalarEq=%d sideTable=%d sideCall=%d sideMeta=%d intrinsic=%d/%d/%d fixed=%d/%d/%d/%d}",
		strings.Join(topOpcodes, ", "),
		pic.monomorphicHits,
		pic.polymorphicHits,
		pic.keyMisses,
		pic.shapeMisses,
		pic.metatableMisses,
		pic.missingKeyFallbacks,
		pic.nilWriteFallbacks,
		pic.invalidKeyFallbacks,
		pic.numericArrayIndexHits,
		pic.scalarEqualityFastChecks,
		pic.sideExitCount(directFrameSideExitReasonTable),
		pic.sideExitCount(directFrameSideExitReasonCall),
		pic.sideExitCount(directFrameSideExitReasonMetatable),
		pic.intrinsicGuardChecks,
		pic.intrinsicGuardHits,
		pic.intrinsicGuardMisses,
		pic.fixedCallFrameReuses,
		pic.fixedCallFrameMaterializations,
		pic.fixedCallArgCopies,
		pic.fixedCallRegisterCopies,
	)
}

func TestCompilerUsesFixedOneResultCallOpcode(t *testing.T) {
	proto, err := Compile(`
local function add(a, b)
	return a + b
end
local value = add(2, 3)
return value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "CALL_ONE") && !strings.Contains(joined, "CALL_LOCAL_ONE") {
		t.Fatalf("compiled fixed-result call is missing one-result call opcode:\n%s", joined)
	}
	if strings.Contains(joined, "CALL r") {
		t.Fatalf("compiled fixed-result call kept generic open call:\n%s", joined)
	}
}

func TestCompilerUsesMethodOneResultCallOpcode(t *testing.T) {
	proto, err := Compile(`
local counter = {value = 0}
function counter:add(amount)
	self.value = self.value + amount
	return self.value
end
local value = counter:add(5)
return value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "CALL_METHOD_ONE") {
		t.Fatalf("compiled method call is missing CALL_METHOD_ONE:\n%s", joined)
	}
	if strings.Contains(joined, "GET_STRING_FIELD") && strings.Contains(joined, "CALL_ONE") {
		t.Fatalf("compiled method call kept separate field load and call:\n%s", joined)
	}
}

func TestCompilerUsesStringFieldEqualityBranchOpcode(t *testing.T) {
	proto, err := Compile(`
local item = {kind = "gem", count = 3}
if item.kind == "gem" or item.kind == "key" then
	return item.count
end
return 0
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "GET_STRING_FIELD") || !strings.Contains(joined, "JUMP_IF_NOT_EQUAL_K") {
		t.Fatalf("compiled string field branch is missing general field sequence:\n%s", joined)
	}
}

func TestRunStringTagElseIfChainMetatableFallbackPreservesRepeatedReads(t *testing.T) {
	proto, err := Compile(`
local buff = {kind = "seed", power = 5}
buff.kind = nil
local calls = 0
setmetatable(buff, {
	__index = function(_, key)
		if key == "kind" then
			calls = calls + 1
			if calls == 1 then
				return "none"
			elseif calls == 2 then
				return "regen"
			end
			return "none"
		end
		return nil
	end,
})

local total = 0
if buff.kind == "poison" then
	total = 1
elseif buff.kind == "regen" then
	total = 2
elseif buff.kind == "shield" then
	total = 3
else
	total = 4
end
return total, calls
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "JUMP_IF_TABLE_HAS_METATABLE") {
		t.Fatalf("compiled tag chain should include a metatable guard:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	total, ok := results[0].Number()
	if !ok || total != 2 {
		t.Fatalf("Run total is %v (%t), want number 2", total, ok)
	}
	calls, ok := results[1].Number()
	if !ok || calls != 2 {
		t.Fatalf("Run calls is %v (%t), want number 2", calls, ok)
	}
}

func TestRunStringFieldEqualityBranchOpcode(t *testing.T) {
	proto, err := Compile(`
local direct = {kind = "gem", count = 3}
local fallback = setmetatable({count = 5}, {__index = {kind = "key"}})
local miss = {kind = "ore", count = 7}
local total = 0
if direct.kind == "gem" or direct.kind == "key" then
	total = total + direct.count
end
if fallback.kind == "gem" or fallback.kind == "key" then
	total = total + fallback.count
end
if miss.kind == "gem" or miss.kind == "key" then
	total = total + 100
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "GET_STRING_FIELD") || !strings.Contains(joined, "JUMP_IF_NOT_EQUAL_K") {
		t.Fatalf("compiled string field branch is missing general field sequence:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	got, ok := results[0].Number()
	if !ok || got != 8 {
		t.Fatalf("Run returned %v, want number 8", results[0])
	}
}

func TestCompilerUsesCanonicalStringFieldNilBranch(t *testing.T) {
	proto, err := Compile(`
local checks = {
	{key = false, score = 10},
	{score = 1},
}
local total = 0
for _, check in checks do
	if check.key ~= nil then
		total = total + check.score
	else
		total = total + 1
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD", "NOT_EQUAL", "JUMP_IF_FALSE"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled field nil branch is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled field nil branch program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 11 {
		t.Fatalf("Run result is %v (%t), want number 11", got, ok)
	}
}

func TestCompilerUsesCanonicalStringFieldNotBranch(t *testing.T) {
	proto, err := Compile(`
local nodes = {
	{blocked = false, cost = 5},
	{blocked = true, cost = 100},
	{cost = 7},
}
local total = 0
for _, node in nodes do
	if not node.blocked then
		total = total + node.cost
	else
		total = total + 1
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD", "JUMP_IF_FALSE"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled field not branch is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled field not branch program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 13 {
		t.Fatalf("Run result is %v (%t), want number 13", got, ok)
	}
}

func TestCompilerUsesCanonicalStringFieldEqualNilBranch(t *testing.T) {
	proto, err := Compile(`
local checks = {
	{flag = false, score = 100},
	{score = 7},
}
local total = 0
for _, check in checks do
	if check.flag == nil then
		total = total + check.score
	else
		total = total + 1
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD", "EQUAL", "JUMP_IF_FALSE"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled field == nil branch is missing %s:\n%s", want, joined)
		}
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled field == nil branch program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 8 {
		t.Fatalf("Run result is %v (%t), want number 8", got, ok)
	}
}

func TestCompilerUsesStringFieldNumericBranchOpcodes(t *testing.T) {
	proto, err := Compile(`
local entity = {shield = 3, hp = 0}
local score = 0
if entity.shield > 0 then
	score = score + 5
end
if entity.hp <= 0 then
	score = score + 7
end
return score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"GET_STRING_FIELD",
		"JUMP_IF_NOT_GREATER_K",
		"JUMP_IF_GREATER_K",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled numeric field branch is missing %s:\n%s", want, joined)
		}
	}
}

func TestRunStringFieldNumericBranchOpcodes(t *testing.T) {
	proto, err := Compile(`
local direct = {shield = 3, hp = 0}
local fallback = setmetatable({}, {__index = {shield = 2, hp = 5}})
local miss = {shield = 0, hp = 9}
local score = 0
if direct.shield > 0 then
	score = score + 1
end
if direct.hp <= 0 then
	score = score + 10
end
if fallback.shield > 0 then
	score = score + 100
end
if fallback.hp <= 0 then
	score = score + 1000
end
if miss.shield > 0 then
	score = score + 10000
end
return score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "GET_STRING_FIELD") ||
		!strings.Contains(joined, "JUMP_IF_NOT_GREATER_K") ||
		!strings.Contains(joined, "JUMP_IF_GREATER_K") {
		t.Fatalf("compiled numeric field branch is missing general sequence:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	got, ok := results[0].Number()
	if !ok || got != 111 {
		t.Fatalf("Run returned %v, want number 111", results[0])
	}
}

func TestCompilerUsesCanonicalStringFieldTruthyBranch(t *testing.T) {
	proto, err := Compile(`
local entity = {alive = true, hp = 3}
local score = 0
if entity.alive then
	score = score + entity.hp
end
return score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD", "JUMP_IF_FALSE"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled truthy field branch is missing %s:\n%s", want, joined)
		}
	}
}

func TestCompilerUsesCanonicalRowStringFieldTruthyInAndBranch(t *testing.T) {
	proto, err := Compile(`
local actors = {
	{alive = true, score = 5},
	{alive = false, score = 100},
	{alive = true, score = 9},
}
local top = 6
local total = 0
for _, actor in actors do
	local value = actor.score
	if actor.alive and value > top then
		total = total + value
	else
		total = total + 1
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD", "JUMP_IF_FALSE"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled row boolean and branch is missing %s:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "GREATER") {
		t.Fatalf("compiled row boolean and branch is missing numeric comparison:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 11 {
		t.Fatalf("Run result is %v (%t), want number 11", got, ok)
	}
}

func TestCompilerUsesCanonicalRowStringFieldNilInAndBranch(t *testing.T) {
	proto, err := Compile(`
local checks = {
	{key = "met_guard", score = 5},
	{score = 100},
	{key = "has_badge", score = 9},
}
local top = 6
local total = 0
for _, check in checks do
	local value = check.score
	if check.key ~= nil and value > top then
		total = total + value
	else
		total = total + 1
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD", "NOT_EQUAL", "JUMP_IF_FALSE"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled row nil and branch is missing %s:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "GREATER") {
		t.Fatalf("compiled row nil and branch is missing numeric comparison:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 11 {
		t.Fatalf("Run result is %v (%t), want number 11", got, ok)
	}
}

func TestRunCanonicalStringFieldTruthyBranch(t *testing.T) {
	proto, err := Compile(`
local direct = {alive = true}
local dead = {alive = false}
local miss = {}
local fallback = setmetatable({}, {__index = {alive = true}})
local score = 0
if direct.alive then
	score = score + 1
end
if dead.alive then
	score = score + 10
end
if miss.alive then
	score = score + 100
end
if fallback.alive then
	score = score + 1000
end
return score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD", "JUMP_IF_FALSE"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled truthy field branch is missing %s:\n%s", want, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	got, ok := results[0].Number()
	if !ok || got != 1001 {
		t.Fatalf("Run returned %v, want number 1001", results[0])
	}
}

func TestCompileAndRunMethodFieldAddFallsBackForMetatableRead(t *testing.T) {
	proto, err := Compile(`
local fallback = {value = 10}
local counter = setmetatable({}, {__index = fallback})
function counter:add(amount)
	self.value = self.value + amount
	return self.value
end
local first = counter:add(5)
local second = counter:add(1)
return first, second, fallback.value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 15 {
		t.Fatalf("first result is %v (%t), want number 15", got, ok)
	}
	if got, ok := results[1].Number(); !ok || got != 16 {
		t.Fatalf("second result is %v (%t), want number 16", got, ok)
	}
	if got, ok := results[2].Number(); !ok || got != 10 {
		t.Fatalf("fallback value is %v (%t), want number 10", got, ok)
	}
}

func TestCompileAndRunClosureUpvalueAdd(t *testing.T) {
	proto, err := Compile(`
local function makeCounter(seed)
	local value = seed
	return function(step)
		value = value + step
		return value
	end
end
local counter = makeCounter(10)
return counter(2), counter(3)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 12 {
		t.Fatalf("first result is %v (%t), want number 12", got, ok)
	}
	if got, ok := results[1].Number(); !ok || got != 15 {
		t.Fatalf("second result is %v (%t), want number 15", got, ok)
	}
}

func TestCompileAndRunVariadicWeightedScore(t *testing.T) {
	proto, err := Compile(`
local function score(...)
	local count = select("#", ...)
	local a, b, c, d, e = ...
	return count + a * 2 + b * 3 + c * 5 + d * 7 + e * 11
end
return score(1, 2, 3, 4, 5), score(3, 4, 5, 6, 7)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 111 {
		t.Fatalf("first result is %v (%t), want number 111", got, ok)
	}
	if got, ok := results[1].Number(); !ok || got != 167 {
		t.Fatalf("second result is %v (%t), want number 167", got, ok)
	}
}

func TestRunSelfUpvalueCallFallsBackAfterReassignment(t *testing.T) {
	proto, err := Compile(`
local function replacement(value)
	return value + 40
end
local function f(n)
	if n == 0 then
		return 0
	end
	f = replacement
	return f(n - 1)
end
return f(2)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 41 {
		t.Fatalf("Run result is %v (%t), want number 41", got, ok)
	}
}

func TestCompilerUsesLocalOneResultCallOpcode(t *testing.T) {
	proto, err := Compile(`
local function score(n)
	return n + 1
end
local value = score(3)
return value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "CALL_LOCAL_ONE") {
		t.Fatalf("compiled local call is missing CALL_LOCAL_ONE:\n%s", joined)
	}
	if strings.Contains(joined, "MOVE") && strings.Contains(joined, "CALL_ONE") {
		t.Fatalf("compiled local call kept separate callee move and call:\n%s", joined)
	}
	if !protoSupportsDirectFrame(proto) {
		t.Fatalf("compiled local call is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 4 {
		t.Fatalf("Run result is %v (%t), want number 4", got, ok)
	}
}

func TestCompileAndRunCoroutineYieldThroughFixedScriptCall(t *testing.T) {
	proto, err := Compile(`
local function inner()
	coroutine.yield("pause")
	return 7
end
local co = coroutine.create(function()
	local value = inner()
	return value + 1
end)
local ok, first = coroutine.resume(co)
local ok2, second = coroutine.resume(co)
return ok, first, ok2, second, coroutine.status(co)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Bool(); !ok || !got {
		t.Fatalf("first resume ok is %v (%t), want true", got, ok)
	}
	if got, ok := results[1].String(); !ok || got != "pause" {
		t.Fatalf("first resume value is %q (%t), want pause", got, ok)
	}
	if got, ok := results[2].Bool(); !ok || !got {
		t.Fatalf("second resume ok is %v (%t), want true", got, ok)
	}
	if got, ok := results[3].Number(); !ok || got != 8 {
		t.Fatalf("second resume value is %v (%t), want 8", got, ok)
	}
	if got, ok := results[4].String(); !ok || got != "dead" {
		t.Fatalf("coroutine status is %q (%t), want dead", got, ok)
	}
}

func TestBytecodeIRBlockOrderSplitsJumpTargetsAndFallthrough(t *testing.T) {
	var builder bytecodeBuilder
	jumpElse := builder.emitJumpIfFalse(0)
	builder.emitLoadConst(1, NumberValue(1))
	jumpEnd := builder.emitJump()
	elseStart := builder.pc()
	builder.patchJump(jumpElse, elseStart)
	builder.emitLoadConst(1, NumberValue(2))
	end := builder.pc()
	builder.patchJump(jumpEnd, end)
	builder.emit(instruction{op: opReturn, a: 1, b: 1})

	blocks := bytecodeIRBlockOrder(builder.ir)
	want := []bytecodeIRBlock{
		{id: 0, start: 0, end: 1},
		{id: 1, start: 1, end: 3},
		{id: 2, start: 3, end: 4},
		{id: 3, start: 4, end: 5},
	}
	if !reflect.DeepEqual(blocks, want) {
		t.Fatalf("bytecodeIRBlockOrder() = %#v, want %#v", blocks, want)
	}
}

func TestOptimizeBytecodeIRRemovesSelfMovesWithBranches(t *testing.T) {
	var builder bytecodeBuilder
	jumpElse := builder.emitJumpIfFalse(0)
	builder.emit(instruction{op: opMove, a: 1, b: 1})
	builder.emitLoadConst(1, NumberValue(1))
	jumpEnd := builder.emitJump()
	elseStart := builder.pc()
	builder.patchJump(jumpElse, elseStart)
	builder.emit(instruction{op: opMove, a: 2, b: 2})
	builder.emitLoadConst(1, NumberValue(2))
	end := builder.pc()
	builder.patchJump(jumpEnd, end)
	builder.emit(instruction{op: opReturn, a: 1, b: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opJumpIfFalse, a: 0, b: 3},
		{op: opLoadConst, a: 1, b: 0},
		{op: opJump, b: 4},
		{op: opLoadConst, a: 1, b: 1},
		{op: opReturn, a: 1, b: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRKeepsMoveRoundTripWhenTempLiveOut(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(1, NumberValue(7))
	jumpElse := builder.emitJumpIfFalse(0)
	builder.emit(instruction{op: opMove, a: 2, b: 1})
	builder.emit(instruction{op: opMove, a: 1, b: 2})
	jumpEnd := builder.emitJump()
	elseStart := builder.pc()
	builder.patchJump(jumpElse, elseStart)
	builder.emitLoadConst(2, NumberValue(9))
	end := builder.pc()
	builder.patchJump(jumpEnd, end)
	builder.emit(instruction{op: opReturnOne, a: 2})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 1, b: 0},
		{op: opJumpIfFalse, a: 0, b: 4},
		{op: opMove, a: 2, b: 1},
		{op: opJump, b: 5},
		{op: opLoadConst, a: 2, b: 1},
		{op: opReturnOne, a: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesBlockLocalMoveRoundTripWithBranches(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(1, NumberValue(7))
	jumpElse := builder.emitJumpIfFalse(0)
	builder.emit(instruction{op: opMove, a: 2, b: 1})
	builder.emit(instruction{op: opMove, a: 1, b: 2})
	builder.emitLoadConst(2, NumberValue(8))
	jumpEnd := builder.emitJump()
	elseStart := builder.pc()
	builder.patchJump(jumpElse, elseStart)
	builder.emitLoadConst(2, NumberValue(9))
	end := builder.pc()
	builder.patchJump(jumpEnd, end)
	builder.emit(instruction{op: opReturnOne, a: 2})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opJumpIfFalse, a: 0, b: 3},
		{op: opLoadConst, a: 2, b: 1},
		{op: opJump, b: 4},
		{op: opLoadConst, a: 2, b: 2},
		{op: opReturnOne, a: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizerPropagatesSingleUseMoves(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(1, NumberValue(2))
	builder.emitLoadConst(2, NumberValue(3))
	builder.emit(instruction{op: opMove, a: 3, b: 1})
	builder.emit(instruction{op: opAdd, a: 4, b: 3, c: 2})
	builder.emit(instruction{op: opReturnOne, a: 4})

	optimized := optimizeBytecodeIRWithConstants(builder.ir, builder.constants, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 1, b: 0},
		{op: opLoadConst, a: 2, b: 1},
		{op: opAdd, a: 4, b: 1, c: 2},
		{op: opReturnOne, a: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestRegisterCoalescingPreservesBranchValues(t *testing.T) {
	var builder bytecodeBuilder
	jumpElse := builder.emitJumpIfFalse(0)
	builder.emitLoadConst(1, NumberValue(10))
	builder.emit(instruction{op: opMove, a: 3, b: 1})
	jumpEnd := builder.emitJump()
	elseStart := builder.pc()
	builder.patchJump(jumpElse, elseStart)
	builder.emitLoadConst(2, NumberValue(20))
	builder.emit(instruction{op: opMove, a: 3, b: 2})
	end := builder.pc()
	builder.patchJump(jumpEnd, end)
	builder.emit(instruction{op: opReturnOne, a: 3})

	optimized := optimizeBytecodeIRWithConstants(builder.ir, builder.constants, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opJumpIfFalse, a: 0, b: 3},
		{op: opLoadConst, a: 3, b: 0},
		{op: opJump, b: 4},
		{op: opLoadConst, a: 3, b: 1},
		{op: opReturnOne, a: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}

	proto := newProto(builder.constants, got, nil, nil, 4, 1, false)
	for _, tc := range []struct {
		name string
		arg  Value
		want float64
	}{
		{name: "then", arg: BoolValue(true), want: 10},
		{name: "else", arg: BoolValue(false), want: 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			thread := newVMThread(runtimeGlobals(nil))
			results, err := thread.run(proto, []Value{tc.arg}, nil)
			if err != nil {
				t.Fatalf("thread.run returned error: %v", err)
			}
			got, ok := results[0].Number()
			if !ok || got != tc.want {
				t.Fatalf("result is %v (%t), want %v", results[0], ok, tc.want)
			}
		})
	}
}

func TestOptimizerDoesNotHoistLoopInvariantFieldLoadAcrossMetamethodOperation(t *testing.T) {
	var builder bytecodeBuilder
	field := builder.addConstant(StringValue("hp"))
	metaFallback := builder.emit(instruction{op: opJumpIfTableHasMetatable, a: 0})
	loopStart := builder.pc()
	builder.emit(instruction{op: opGetStringField, a: 2, b: 0, c: field})
	builder.emit(instruction{op: opAdd, a: 3, b: 3, c: 2})
	builder.emit(instruction{op: opJump, b: loopStart})
	fallback := builder.pc()
	builder.patchJump(metaFallback, fallback)
	builder.emit(instruction{op: opReturnOne, a: 3})

	optimized := optimizeBytecodeIRWithConstants(builder.ir, builder.constants, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opJumpIfTableHasMetatable, a: 0, d: 4},
		{op: opGetStringField, a: 2, b: 0, c: field},
		{op: opAdd, a: 3, b: 3, c: 2},
		{op: opJump, b: 1},
		{op: opReturnOne, a: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizerDoesNotHoistFieldLoadAcrossMutation(t *testing.T) {
	var builder bytecodeBuilder
	field := builder.addConstant(StringValue("hp"))
	metaFallback := builder.emit(instruction{op: opJumpIfTableHasMetatable, a: 0})
	loopStart := builder.pc()
	builder.emitLoadConst(4, NumberValue(1))
	builder.emit(instruction{op: opSetStringField, a: 0, b: field, c: 4})
	builder.emit(instruction{op: opGetStringField, a: 2, b: 0, c: field})
	builder.emit(instruction{op: opAdd, a: 3, b: 3, c: 2})
	builder.emit(instruction{op: opJump, b: loopStart})
	fallback := builder.pc()
	builder.patchJump(metaFallback, fallback)
	builder.emit(instruction{op: opReturnOne, a: 3})

	optimized := optimizeBytecodeIRWithConstants(builder.ir, builder.constants, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opJumpIfTableHasMetatable, a: 0, d: 6},
		{op: opLoadConst, a: 4, b: 1},
		{op: opSetStringField, a: 0, b: field, c: 4},
		{op: opGetStringField, a: 2, b: 0, c: field},
		{op: opAdd, a: 3, b: 3, c: 2},
		{op: opJump, b: 1},
		{op: opReturnOne, a: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemapsBackwardJumpTarget(t *testing.T) {
	var builder bytecodeBuilder
	loopStart := builder.pc()
	builder.emit(instruction{op: opMove, a: 1, b: 1})
	builder.emitLoadConst(1, NumberValue(1))
	jumpExit := builder.emitJumpIfFalse(0)
	builder.emit(instruction{op: opMove, a: 2, b: 2})
	builder.emit(instruction{op: opJump, b: loopStart})
	exit := builder.pc()
	builder.patchJump(jumpExit, exit)
	builder.emit(instruction{op: opReturnOne, a: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 1, b: 0},
		{op: opJumpIfFalse, a: 0, b: 3},
		{op: opJump, b: 0},
		{op: opReturnOne, a: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadPureTemporaries(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(1, NumberValue(2))
	builder.emit(instruction{op: opMove, a: 2, b: 1})
	builder.emitLoadConst(3, NumberValue(9))
	builder.emit(instruction{op: opReturnOne, a: 3})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 3, b: 1},
		{op: opReturnOne, a: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadFoldedNumericArithmetic(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(1, NumberValue(2))
	builder.emitLoadConst(2, NumberValue(3))
	builder.emit(instruction{op: opAdd, a: 3, b: 1, c: 2})
	builder.emitLoadConst(4, NumberValue(9))
	builder.emit(instruction{op: opReturnOne, a: 4})

	builder.optimize(optimizationOptions{})
	got := assembleBytecodeIR(builder.ir)
	want := []instruction{
		{op: opLoadConst, a: 4, b: 0},
		{op: opReturnOne, a: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadFoldedInPlaceNumericArithmetic(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(1, NumberValue(2))
	addend := builder.addConstant(NumberValue(3))
	builder.emit(instruction{op: opAddK, a: 1, b: 1, c: addend})
	builder.emit(instruction{op: opNeg, a: 2, b: 1})
	builder.emitLoadConst(3, NumberValue(9))
	builder.emit(instruction{op: opReturnOne, a: 3})

	builder.optimize(optimizationOptions{})
	got := assembleBytecodeIR(builder.ir)
	want := []instruction{
		{op: opLoadConst, a: 3, b: 0},
		{op: opReturnOne, a: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRKeepsDeadUnprovenArithmetic(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(1, StringValue("fallback"))
	builder.emit(instruction{op: opAdd, a: 2, b: 0, c: 1})
	builder.emitLoadConst(3, NumberValue(9))
	builder.emit(instruction{op: opReturnOne, a: 3})

	builder.optimize(optimizationOptions{})
	got := assembleBytecodeIR(builder.ir)
	want := []instruction{
		{op: opLoadConst, a: 1, b: 0},
		{op: opAdd, a: 2, b: 0, c: 1},
		{op: opLoadConst, a: 3, b: 1},
		{op: opReturnOne, a: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRKeepsDeadEffectfulInstructions(t *testing.T) {
	var builder bytecodeBuilder
	name := builder.addConstant(StringValue("x"))
	builder.emit(instruction{op: opLoadGlobal, a: 1, b: name})
	builder.emit(instruction{op: opNewTable, a: 2})
	builder.emitLoadConst(3, NumberValue(9))
	builder.emit(instruction{op: opReturnOne, a: 3})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadGlobal, a: 1, b: name},
		{op: opNewTable, a: 2},
		{op: opLoadConst, a: 3, b: 1},
		{op: opReturnOne, a: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestInstructionReadModelCoversIntrinsicArgumentWindows(t *testing.T) {
	tests := []struct {
		name string
		ins  instruction
		want []int
	}{
		{name: "table insert", ins: instruction{op: opFastCall, a: 4, b: int(nativeFuncTableInsert), c: 2, d: 1}, want: []int{4, 5}},
		{name: "table remove", ins: instruction{op: opFastCall, a: 4, b: int(nativeFuncTableRemove), c: 1, d: 1}, want: []int{4}},
		{name: "math min", ins: instruction{op: opFastCall, a: 4, b: int(nativeFuncMathMin), c: 2, d: 1}, want: []int{4, 5}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registersMatching(tt.ins, func(register int) bool {
				return instructionReadsRegister(tt.ins, register)
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("read registers = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestInstructionReadModelCoversFixedCallArgumentWindows(t *testing.T) {
	tests := []struct {
		name string
		ins  instruction
		want []int
	}{
		{name: "call no args", ins: instruction{op: opCall, a: 8, b: 4, c: 0, d: 1}, want: []int{4}},
		{name: "call fixed args", ins: instruction{op: opCall, a: 8, b: 4, c: 2, d: 1}, want: []int{4, 5, 6}},
		{name: "call one fixed args", ins: instruction{op: opCallOne, a: 8, b: 4, c: 2, d: 1}, want: []int{4, 5, 6}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registersMatching(tt.ins, func(register int) bool {
				return instructionReadsRegister(tt.ins, register)
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("read registers = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestInstructionReadModelCoversOpenCallArgumentPrefixWindow(t *testing.T) {
	ins := instruction{op: opCall, a: 8, b: 4, c: -3, d: 1}
	got := registersMatching(ins, func(register int) bool {
		return instructionReadsRegister(ins, register)
	})
	want := []int{4, 5, 6}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read registers = %#v, want %#v", got, want)
	}
}

func TestInstructionWriteModelCoversFixedVarargResultWindow(t *testing.T) {
	tests := []struct {
		name string
		ins  instruction
		want []int
	}{
		{name: "default one", ins: instruction{op: opVararg, a: 4, b: 0}, want: []int{4}},
		{name: "fixed results", ins: instruction{op: opVararg, a: 4, b: 3}, want: []int{4, 5, 6}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registersMatching(tt.ins, func(register int) bool {
				return instructionWritesRegister(tt.ins, register)
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("write registers = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestInstructionReadModelCoversOpenReturnPrefixWindow(t *testing.T) {
	ins := instruction{op: opReturn, a: 4, b: -3}
	got := registersMatching(ins, func(register int) bool {
		return instructionReadsRegister(ins, register)
	})
	want := []int{4, 5}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read registers = %#v, want %#v", got, want)
	}
}

func TestInstructionReadModelCoversTableFieldAndIndexOperands(t *testing.T) {
	tests := []struct {
		name string
		ins  instruction
		want []int
	}{
		{name: "set field", ins: instruction{op: opSetField, a: 4, b: 0, c: 6}, want: []int{4, 6}},
		{name: "get index", ins: instruction{op: opGetIndex, a: 8, b: 4, c: 6}, want: []int{4, 6}},
		{name: "set index", ins: instruction{op: opSetIndex, a: 4, b: 5, c: 6}, want: []int{4, 5, 6}},
		{name: "get string field", ins: instruction{op: opGetStringField, a: 8, b: 4, c: 0}, want: []int{4}},
		{name: "set string field", ins: instruction{op: opSetStringField, a: 4, b: 0, c: 6}, want: []int{4, 6}},
		{name: "get string field index", ins: instruction{op: opGetStringFieldIndex, a: 8, b: 4, c: 0, d: 6}, want: []int{4, 6}},
		{name: "set string field index", ins: instruction{op: opSetStringFieldIndex, a: 4, b: 0, c: 5, d: 6}, want: []int{4, 5, 6}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registersMatching(tt.ins, func(register int) bool {
				return instructionReadsRegister(tt.ins, register)
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("read registers = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestInstructionReadWriteModelCoversIteratorOpcodes(t *testing.T) {
	tests := []struct {
		name       string
		ins        instruction
		wantReads  []int
		wantWrites []int
	}{
		{
			name:       "prepare iter",
			ins:        instruction{op: opPrepareIter, a: 8, b: 1, c: 2},
			wantReads:  []int{8},
			wantWrites: []int{1, 2, 8},
		},
		{
			name:       "array next",
			ins:        instruction{op: opArrayNext, a: 8, b: 1, c: 2, d: 3},
			wantReads:  []int{1, 2, 8},
			wantWrites: []int{8, 9, 10},
		},
		{
			name:       "array next jump2",
			ins:        instruction{op: opArrayNextJump2, a: 8, b: 1, c: 2, d: 20},
			wantReads:  []int{1, 2, 8},
			wantWrites: []int{8, 9},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotReads := registersMatching(tt.ins, func(register int) bool {
				return instructionReadsRegister(tt.ins, register)
			})
			if !reflect.DeepEqual(gotReads, tt.wantReads) {
				t.Fatalf("read registers = %#v, want %#v", gotReads, tt.wantReads)
			}
			gotWrites := registersMatching(tt.ins, func(register int) bool {
				return instructionWritesRegister(tt.ins, register)
			})
			if !reflect.DeepEqual(gotWrites, tt.wantWrites) {
				t.Fatalf("write registers = %#v, want %#v", gotWrites, tt.wantWrites)
			}
		})
	}
}

func TestInstructionReadModelCoversComparisonBranchOperands(t *testing.T) {
	tests := []struct {
		name string
		ins  instruction
		want []int
	}{
		{name: "numeric for check", ins: instruction{op: opNumericForCheck, a: 8, b: 1, c: 2, d: 20}, want: []int{1, 2, 8}},
		{name: "not equal constant", ins: instruction{op: opJumpIfNotEqualK, a: 8, b: 1, d: 20}, want: []int{8}},
		{name: "not less constant", ins: instruction{op: opJumpIfNotLessK, a: 8, b: 1, d: 20}, want: []int{8}},
		{name: "not greater constant", ins: instruction{op: opJumpIfNotGreaterK, a: 8, b: 1, d: 20}, want: []int{8}},
		{name: "less constant", ins: instruction{op: opJumpIfLessK, a: 8, b: 1, d: 20}, want: []int{8}},
		{name: "greater constant", ins: instruction{op: opJumpIfGreaterK, a: 8, b: 1, d: 20}, want: []int{8}},
		{name: "not less register", ins: instruction{op: opJumpIfNotLess, a: 8, b: 1, d: 20}, want: []int{1, 8}},
		{name: "not greater register", ins: instruction{op: opJumpIfNotGreater, a: 8, b: 1, d: 20}, want: []int{1, 8}},
		{name: "less register", ins: instruction{op: opJumpIfLess, a: 8, b: 1, d: 20}, want: []int{1, 8}},
		{name: "greater register", ins: instruction{op: opJumpIfGreater, a: 8, b: 1, d: 20}, want: []int{1, 8}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registersMatching(tt.ins, func(register int) bool {
				return instructionReadsRegister(tt.ins, register)
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("read registers = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestInstructionReadModelCoversTablePredicateBranchOperands(t *testing.T) {
	tests := []struct {
		name string
		ins  instruction
		want []int
	}{
		{name: "table has metatable", ins: instruction{op: opJumpIfTableHasMetatable, a: 8, d: 20}, want: []int{8}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registersMatching(tt.ins, func(register int) bool {
				return instructionReadsRegister(tt.ins, register)
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("read registers = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestOptimizeBytecodeIRRemovesDeadLoadAroundStringFieldIndexOps(t *testing.T) {
	var builder bytecodeBuilder
	first := builder.addConstant(StringValue("stats"))
	builder.emitLoadConst(9, NumberValue(99))
	builder.emitLoadConst(3, StringValue("hp"))
	builder.emit(instruction{op: opGetStringFieldIndex, a: 1, b: 0, c: first, d: 3})
	builder.emitLoadConst(2, NumberValue(7))
	builder.emit(instruction{op: opSetStringFieldIndex, a: 0, b: first, c: 3, d: 2})
	builder.emit(instruction{op: opReturnOne, a: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 3, b: 2},
		{op: opGetStringFieldIndex, a: 1, b: 0, c: first, d: 3},
		{op: opLoadConst, a: 2, b: 3},
		{op: opSetStringFieldIndex, a: 0, b: first, c: 3, d: 2},
		{op: opReturnOne, a: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadLoadAroundIteratorOps(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(9, NumberValue(99))
	builder.emit(instruction{op: opPrepareIter, a: 0, b: 1, c: 2})
	jumpEnd := builder.emit(instruction{op: opArrayNextJump2, a: 3, b: 1, c: 2})
	builder.emit(instruction{op: opReturnOne, a: 3})
	end := builder.pc()
	builder.patchJumpD(jumpEnd, end)

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opPrepareIter, a: 0, b: 1, c: 2},
		{op: opArrayNextJump2, a: 3, b: 1, c: 2, d: 3},
		{op: opReturnOne, a: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadLoadAroundComparisonBranch(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(9, NumberValue(99))
	builder.emitLoadConst(1, NumberValue(3))
	builder.emitLoadConst(2, NumberValue(5))
	jumpEnd := builder.emit(instruction{op: opJumpIfNotLess, a: 1, b: 2})
	builder.emit(instruction{op: opReturnOne, a: 1})
	end := builder.pc()
	builder.patchJumpD(jumpEnd, end)

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 1, b: 1},
		{op: opLoadConst, a: 2, b: 2},
		{op: opJumpIfNotLess, a: 1, b: 2, d: 4},
		{op: opReturnOne, a: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRKeepsIntrinsicArgumentLoads(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(1, NumberValue(4))
	builder.emitLoadConst(2, NumberValue(7))
	builder.emit(instruction{op: opFastCall, a: 1, b: int(nativeFuncMathMin), c: 2, d: 1})
	builder.emit(instruction{op: opReturnOne, a: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 1, b: 0},
		{op: opLoadConst, a: 2, b: 1},
		{op: opFastCall, a: 1, b: int(nativeFuncMathMin), c: 2, d: 1},
		{op: opReturnOne, a: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadLoadAroundProvenIntrinsicReads(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(9, NumberValue(99))
	builder.emitLoadConst(1, NumberValue(4))
	builder.emitLoadConst(2, NumberValue(7))
	builder.emit(instruction{op: opFastCall, a: 1, b: int(nativeFuncMathMin), c: 2, d: 1})
	builder.emit(instruction{op: opReturnOne, a: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 1, b: 1},
		{op: opLoadConst, a: 2, b: 2},
		{op: opFastCall, a: 1, b: int(nativeFuncMathMin), c: 2, d: 1},
		{op: opReturnOne, a: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadLoadAroundFixedCallReads(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(9, NumberValue(99))
	builder.emitLoadConst(0, NumberValue(100))
	builder.emitLoadConst(1, NumberValue(4))
	builder.emit(instruction{op: opCall, a: 2, b: 0, c: 1, d: 1})
	builder.emit(instruction{op: opReturnOne, a: 2})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 0, b: 1},
		{op: opLoadConst, a: 1, b: 2},
		{op: opCall, a: 2, b: 0, c: 1, d: 1},
		{op: opReturnOne, a: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadLoadAroundOpenCallWithoutPrefix(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(9, NumberValue(99))
	builder.emitLoadConst(0, NumberValue(100))
	builder.emit(instruction{op: opCall, a: 2, b: 0, c: -1, d: 1})
	builder.emit(instruction{op: opReturnOne, a: 2})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 0, b: 1},
		{op: opCall, a: 2, b: 0, c: -1, d: 1},
		{op: opReturnOne, a: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadLoadAroundOpenArgumentCall(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(9, NumberValue(99))
	builder.emitLoadConst(0, NumberValue(100))
	builder.emitLoadConst(1, NumberValue(4))
	builder.emit(instruction{op: opVararg, a: 2, b: -1})
	builder.emit(instruction{op: opCall, a: 4, b: 0, c: -2, d: 1})
	builder.emit(instruction{op: opReturnOne, a: 4})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 0, b: 1},
		{op: opLoadConst, a: 1, b: 2},
		{op: opVararg, a: 2, b: -1},
		{op: opCall, a: 4, b: 0, c: -2, d: 1},
		{op: opReturnOne, a: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadLoadAroundOpenResultCall(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(9, NumberValue(99))
	builder.emitLoadConst(0, NumberValue(100))
	builder.emitLoadConst(1, NumberValue(4))
	builder.emit(instruction{op: opCall, a: 2, b: 0, c: 1, d: -1})
	builder.emit(instruction{op: opReturn, a: 2, b: -1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 0, b: 1},
		{op: opLoadConst, a: 1, b: 2},
		{op: opCall, a: 2, b: 0, c: 1, d: -1},
		{op: opReturn, a: 2, b: -1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadLoadAroundFixedVararg(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(9, NumberValue(99))
	builder.emitLoadConst(5, NumberValue(5))
	builder.emit(instruction{op: opVararg, a: 4, b: 2})
	builder.emit(instruction{op: opReturnOne, a: 5})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opVararg, a: 4, b: 2},
		{op: opReturnOne, a: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadLoadAroundOpenVarargReturn(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(9, NumberValue(99))
	builder.emit(instruction{op: opVararg, a: 4, b: -1})
	builder.emit(instruction{op: opReturn, a: 4, b: -1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opVararg, a: 4, b: -1},
		{op: opReturn, a: 4, b: -1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRKeepsOpenReturnPrefixRegisters(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(9, NumberValue(99))
	builder.emitLoadConst(0, NumberValue(10))
	builder.emitLoadConst(1, NumberValue(20))
	builder.emit(instruction{op: opReturn, a: 0, b: -3})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 0, b: 1},
		{op: opLoadConst, a: 1, b: 2},
		{op: opReturn, a: 0, b: -3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadLoadAroundTableFieldWrite(t *testing.T) {
	var builder bytecodeBuilder
	field := builder.addConstant(StringValue("hp"))
	builder.emitLoadConst(9, NumberValue(99))
	builder.emitLoadConst(1, NumberValue(7))
	builder.emit(instruction{op: opSetField, a: 0, b: field, c: 1})
	builder.emit(instruction{op: opReturnOne, a: 0})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 1, b: 2},
		{op: opSetField, a: 0, b: field, c: 1},
		{op: opReturnOne, a: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestCompileRunTableFieldDCEPreservesEffects(t *testing.T) {
	proto, err := Compile(`
local row = {hp = 10}
local dead = 99
row.hp = 12
local got = row.hp
return got
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 12 {
		t.Fatalf("Run result is %v (%t), want number 12", got, ok)
	}
}

func TestCompileRunTablePredicateDCEPreservesEffects(t *testing.T) {
	proto, err := Compile(`
local row = {alive = false, value = 5}
local dead = 99
if row.alive then
	return 1
end
return row.value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD", "JUMP_IF_FALSE"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled table predicate program is missing %s:\n%s", want, joined)
		}
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 5 {
		t.Fatalf("Run result is %v (%t), want number 5", got, ok)
	}
}

func TestCompileRunFixedVarargDCEPreservesEffects(t *testing.T) {
	proto, err := Compile(`
local function second(...)
	local a, b = ...
	local dead = 99
	return b
end
return second(10, 21)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, want := len(proto.prototypes), 1; got != want {
		t.Fatalf("compiled root has %d child prototypes, want %d", got, want)
	}
	joined := strings.Join(disassembleProto(proto.prototypes[0]), "\n")
	if !strings.Contains(joined, "VARARG r") {
		t.Fatalf("compiled variadic function is missing VARARG:\n%s", joined)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 21 {
		t.Fatalf("Run result is %v (%t), want number 21", got, ok)
	}
}

func TestCompileRunOpenReturnPrefixDCEPreservesEffects(t *testing.T) {
	proto, err := Compile(`
local function rest()
	return 30, 40
end
local function all()
	local dead = 99
	return 10, 20, rest()
end
return all()
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, want := len(proto.prototypes), 2; got != want {
		t.Fatalf("compiled root has %d child prototypes, want %d", got, want)
	}
	joined := strings.Join(disassembleProto(proto.prototypes[1]), "\n")
	if !strings.Contains(joined, "RETURN r") || !strings.Contains(joined, "-3") {
		t.Fatalf("compiled open return function is missing open RETURN:\n%s", joined)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	wants := []float64{10, 20, 30, 40}
	if len(results) != len(wants) {
		t.Fatalf("Run returned %d results, want %d", len(results), len(wants))
	}
	for i, want := range wants {
		got, ok := results[i].Number()
		if !ok || got != want {
			t.Fatalf("result %d is %v (%t), want number %v", i, results[i], ok, want)
		}
	}
}

func TestCompileRunOpenVarargReturnDCEPreservesEffects(t *testing.T) {
	proto, err := Compile(`
local function pass(...)
	local dead = 99
	return ...
end
return pass(10, 20, 30)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, want := len(proto.prototypes), 1; got != want {
		t.Fatalf("compiled root has %d child prototypes, want %d", got, want)
	}
	joined := strings.Join(disassembleProto(proto.prototypes[0]), "\n")
	if !strings.Contains(joined, "VARARG r") || !strings.Contains(joined, "RETURN r") || !strings.Contains(joined, "-1") {
		t.Fatalf("compiled open vararg return is missing open value-list ops:\n%s", joined)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	wants := []float64{10, 20, 30}
	if len(results) != len(wants) {
		t.Fatalf("Run returned %d results, want %d", len(results), len(wants))
	}
	for i, want := range wants {
		got, ok := results[i].Number()
		if !ok || got != want {
			t.Fatalf("result %d is %v (%t), want number %v", i, results[i], ok, want)
		}
	}
}

func TestCompileRunOpenResultCallDCEPreservesEffects(t *testing.T) {
	proto, err := Compile(`
local function pair()
	return 10, 20
end
local function pass()
	local dead = 99
	return pair()
end
return pass()
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, want := len(proto.prototypes), 2; got != want {
		t.Fatalf("compiled root has %d child prototypes, want %d", got, want)
	}
	joined := strings.Join(disassembleProto(proto.prototypes[1]), "\n")
	if !strings.Contains(joined, "CALL") || !strings.Contains(joined, "-1") || !strings.Contains(joined, "RETURN") {
		t.Fatalf("compiled open-result call function is missing open call/return:\n%s", joined)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	wants := []float64{10, 20}
	if len(results) != len(wants) {
		t.Fatalf("Run returned %d results, want %d", len(results), len(wants))
	}
	for i, want := range wants {
		got, ok := results[i].Number()
		if !ok || got != want {
			t.Fatalf("result %d is %v (%t), want number %v", i, results[i], ok, want)
		}
	}
}

func TestCompileRunOpenArgumentCallDCEPreservesEffects(t *testing.T) {
	proto, err := Compile(`
local function take(a, b, c)
	return a + b + c
end
local function rest()
	return 20, 30
end
local function pass()
	local dead = 99
	return take(10, rest())
end
return pass()
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	hasOpenArgCall := false
	for _, child := range proto.prototypes {
		joined := strings.Join(disassembleProto(child), "\n")
		if strings.Contains(joined, "CALL") && strings.Contains(joined, "-2") {
			hasOpenArgCall = true
			break
		}
	}
	if !hasOpenArgCall {
		t.Fatalf("compiled program is missing open-argument CALL")
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 60 {
		t.Fatalf("Run result is %v (%t), want number 60", got, ok)
	}
}

func TestCompileRunDeadArithmeticDCEPreservesMetamethodEffects(t *testing.T) {
	proto, err := Compile(`
local left = setmetatable({}, {
	__add = function()
		return missing_global()
	end,
})
local dead = left + 1
return 9
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	_, err = Run(proto)
	if err == nil {
		t.Fatal("Run succeeded, want dead arithmetic metamethod error")
	}
	if !strings.Contains(err.Error(), "undefined global \"missing_global\"") {
		t.Fatalf("Run error is %q, want dead arithmetic metamethod error", err)
	}
}

func TestOptimizeBytecodeIRKeepsTableInsertArgumentLoads(t *testing.T) {
	var builder bytecodeBuilder
	builder.emit(instruction{op: opNewTable, a: 1})
	builder.emitLoadConst(2, NumberValue(7))
	builder.emit(instruction{op: opFastCall, a: 1, b: int(nativeFuncTableInsert), c: 2, d: 1})
	builder.emit(instruction{op: opReturnOne, a: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opNewTable, a: 1},
		{op: opLoadConst, a: 2, b: 0},
		{op: opFastCall, a: 1, b: int(nativeFuncTableInsert), c: 2, d: 1},
		{op: opReturnOne, a: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestBytecodeIRLivenessTracksBranchJoinRegisters(t *testing.T) {
	var builder bytecodeBuilder
	jumpElse := builder.emitJumpIfFalse(0)
	builder.emitLoadConst(1, NumberValue(1))
	jumpEnd := builder.emitJump()
	elseStart := builder.pc()
	builder.patchJump(jumpElse, elseStart)
	builder.emitLoadConst(1, NumberValue(2))
	end := builder.pc()
	builder.patchJump(jumpEnd, end)
	builder.emit(instruction{op: opReturn, a: 1, b: 1})

	liveness := bytecodeIRLiveness(builder.ir)

	assertBytecodeIRLiveness(t, liveness, 0, []int{0}, nil, nil)
	assertBytecodeIRLiveness(t, liveness, 1, nil, []int{1}, []int{1})
	assertBytecodeIRLiveness(t, liveness, 2, nil, []int{1}, []int{1})
	assertBytecodeIRLiveness(t, liveness, 3, []int{1}, nil, nil)
}

func TestProtoTracksEntryNilRegisters(t *testing.T) {
	var builder bytecodeBuilder
	builder.emit(instruction{op: opReturnOne, a: 2})

	proto := builder.proto(nil, 4, 1, false)

	if !reflect.DeepEqual(proto.entryNilRegisters, []int{2}) {
		t.Fatalf("entry nil registers are %#v, want [2]", proto.entryNilRegisters)
	}
}

func TestRunFrameResetClearsMissingParameterOnReuse(t *testing.T) {
	proto, err := Compile(`
local function maybe(flag, value)
	if flag then
		value = 42
	end
	return value
end
maybe(true)
return maybe(false)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	if !results[0].IsNil() {
		t.Fatalf("Run result is %s, want nil", results[0].Kind())
	}
}

func TestRegisterAllocationReusesNestedExpressionTemporaries(t *testing.T) {
	proto, err := Compile("return (a + b) + (c + d) + (e + f)")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, want := proto.registers, 3; got != want {
		t.Fatalf("nested expression register count is %d, want %d after temporary reuse", got, want)
	}

	results, err := RunWithGlobals(proto, map[string]Value{
		"a": NumberValue(1),
		"b": NumberValue(2),
		"c": NumberValue(3),
		"d": NumberValue(4),
		"e": NumberValue(5),
		"f": NumberValue(6),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	number, ok := results[0].Number()
	if !ok || number != 21 {
		t.Fatalf("RunWithGlobals result is %#v, want number 21", results[0])
	}
}

func TestRegisterAllocationReusesTableFieldTemporaries(t *testing.T) {
	proto, err := Compile(`return {
	[a + b] = c + d,
	[e + f] = g + h,
	name = i + j,
}`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, want := proto.registers, 4; got != want {
		t.Fatalf("table literal register count is %d, want %d after field temporary reuse", got, want)
	}

	results, err := RunWithGlobals(proto, map[string]Value{
		"a": NumberValue(1),
		"b": NumberValue(2),
		"c": NumberValue(3),
		"d": NumberValue(4),
		"e": NumberValue(5),
		"f": NumberValue(6),
		"g": NumberValue(7),
		"h": NumberValue(8),
		"i": NumberValue(9),
		"j": NumberValue(10),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	table, ok := results[0].Table()
	if !ok {
		t.Fatalf("RunWithGlobals result is %s, want table", results[0].Kind())
	}
	assertTableNumber(t, table, NumberValue(3), 7)
	assertTableNumber(t, table, NumberValue(11), 15)
	assertTableNumber(t, table, StringValue("name"), 19)
}

func TestRegisterCompactionShrinksAfterDeadCodeCleanup(t *testing.T) {
	proto, err := Compile(`
local live = 7
local dead = 99
return live
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, want := proto.registers, 1; got != want {
		t.Fatalf("compiled register count is %d, want %d after dead local cleanup", got, want)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 7 {
		t.Fatalf("Run result is %v (%t), want number 7", got, ok)
	}
}

func TestCompilerShrinksFrameUsingLiveness(t *testing.T) {
	proto, err := Compile(`
local a = 1
local b = a + 2
local c = b + 3
local d = c + 4
return d
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, want := proto.registers, 1; got != want {
		t.Fatalf("compiled register count is %d, want %d after liveness frame shrink", got, want)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 10 {
		t.Fatalf("Run result is %v (%t), want 10", got, ok)
	}
}

func TestFrameShrinkPreservesCapturedAndVarargRegisters(t *testing.T) {
	proto, err := Compile(`
local function collect(...)
	local base = 4
	local function add(x)
		return base + x
	end
	local first, second = ...
	return add(first), second, select("#", ...)
end
return collect(3, 8, 13)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	want := []float64{7, 8, 3}
	if len(results) != len(want) {
		t.Fatalf("Run returned %d results, want %d", len(results), len(want))
	}
	for i, wantNumber := range want {
		got, ok := results[i].Number()
		if !ok || got != wantNumber {
			t.Fatalf("result %d is %v (%t), want %v", i, results[i], ok, wantNumber)
		}
	}
}

func TestRegisterAllocationClaimsFixedVarargResultSpan(t *testing.T) {
	compiler := compiler{
		variadic:  true,
		freeTemps: []int{2, 3, 4},
	}

	if err := compiler.compileVarargToResults(term{vararg: true}, 2, 3); err != nil {
		t.Fatalf("compileVarargToResults returned error: %v", err)
	}
	if got, want := compiler.nextReg, 5; got != want {
		t.Fatalf("compiler nextReg is %d, want %d after fixed vararg result reservation", got, want)
	}
	if len(compiler.freeTemps) != 0 {
		t.Fatalf("compiler free temps are %#v, want fixed vararg result span claimed", compiler.freeTemps)
	}
}

func TestRegisterAllocationReusesBranchConditionTemporary(t *testing.T) {
	proto, err := Compile(`if a + b then
end
return {
	[c + d] = e + f,
}`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, want := proto.registers, 4; got != want {
		t.Fatalf("branch condition register count is %d, want %d after condition temporary reuse", got, want)
	}

	results, err := RunWithGlobals(proto, map[string]Value{
		"a": NumberValue(1),
		"b": NumberValue(2),
		"c": NumberValue(3),
		"d": NumberValue(4),
		"e": NumberValue(5),
		"f": NumberValue(6),
	})
	if err != nil {
		t.Fatalf("RunWithGlobals returned error: %v", err)
	}
	table, ok := results[0].Table()
	if !ok {
		t.Fatalf("RunWithGlobals result is %s, want table", results[0].Kind())
	}
	assertTableNumber(t, table, NumberValue(7), 11)
}

func TestOpcodeMetadataCoversEveryOpcode(t *testing.T) {
	for _, op := range allOpcodes {
		meta, ok := opcodeMetadata(op)
		if !ok {
			t.Fatalf("missing opcode metadata for %s (%d)", opcodeName(op), op)
		}
		if meta.name == "" {
			t.Fatalf("opcode metadata for %d has empty name", op)
		}
		if meta.directFrame != wantDirectFrameOpcodeSupported(op) {
			t.Fatalf("opcode metadata direct-frame support for %s is %t, want %t", opcodeName(op), meta.directFrame, wantDirectFrameOpcodeSupported(op))
		}
		if meta.directFrame && meta.directFrameUnsupportedReason != "" {
			t.Fatalf("opcode metadata direct-frame supported %s has unsupported reason %q", opcodeName(op), meta.directFrameUnsupportedReason)
		}
		if !meta.directFrame && meta.directFrameUnsupportedReason == "" {
			t.Fatalf("opcode metadata direct-frame unsupported %s has empty unsupported reason", opcodeName(op))
		}
		if meta.controlFlow != wantOpcodeControlFlow(op) {
			t.Fatalf("opcode metadata control flow for %s is %d, want %d", opcodeName(op), meta.controlFlow, wantOpcodeControlFlow(op))
		}
		if meta.jumpTarget != classifiedOpcodeJumpTarget(t, op) {
			t.Fatalf("opcode metadata jump target for %s is %d, want %d", opcodeName(op), meta.jumpTarget, classifiedOpcodeJumpTarget(t, op))
		}
		if meta.operands != classifiedOpcodeOperandShape(op) {
			t.Fatalf("opcode metadata operands for %s are %#v, want %#v", opcodeName(op), meta.operands, classifiedOpcodeOperandShape(op))
		}
		if meta.operands == (opcodeOperandShape{}) {
			t.Fatalf("opcode metadata operands for %s are empty", opcodeName(op))
		}
		wantEffects := wantOpcodeEffects(op)
		if meta.effects != wantEffects {
			t.Fatalf("opcode metadata effects for %s are %#v, want %#v", opcodeName(op), meta.effects, wantEffects)
		}
		if meta.effects.mayYield && !meta.effects.invokesScriptOrHostCode {
			t.Fatalf("opcode metadata %s may yield without invoking script or host code", opcodeName(op))
		}
		if meta.controlFlow == opcodeControlBranch && meta.jumpTarget == opcodeJumpTargetNone {
			t.Fatalf("opcode metadata branch %s has no jump target", opcodeName(op))
		}
		if meta.controlFlow == opcodeControlJump && meta.jumpTarget == opcodeJumpTargetNone {
			t.Fatalf("opcode metadata jump %s has no jump target", opcodeName(op))
		}
		if meta.controlFlow == opcodeControlReturn && meta.jumpTarget != opcodeJumpTargetNone {
			t.Fatalf("opcode metadata return %s has jump target %d", opcodeName(op), meta.jumpTarget)
		}
	}
}

func TestOpcodeMetadataValidationRejectsMalformedEntries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*[opcodeLimit]opcodeMetadataEntry)
		want   string
	}{
		{
			name: "empty name",
			mutate: func(table *[opcodeLimit]opcodeMetadataEntry) {
				table[opAdd].name = ""
			},
			want: "missing name",
		},
		{
			name: "unclassified effects",
			mutate: func(table *[opcodeLimit]opcodeMetadataEntry) {
				table[opAdd].effects.classified = false
			},
			want: "effects are unclassified",
		},
		{
			name: "empty operands",
			mutate: func(table *[opcodeLimit]opcodeMetadataEntry) {
				table[opAdd].operands = opcodeOperandShape{}
			},
			want: "missing operand shape",
		},
		{
			name: "branch without jump target",
			mutate: func(table *[opcodeLimit]opcodeMetadataEntry) {
				table[opJumpIfFalse].jumpTarget = opcodeJumpTargetNone
			},
			want: "control flow without jump target",
		},
		{
			name: "yield without invocation",
			mutate: func(table *[opcodeLimit]opcodeMetadataEntry) {
				table[opCall].effects.invokesScriptOrHostCode = false
			},
			want: "may yield without invoking script or host code",
		},
		{
			name: "jump slot without operand",
			mutate: func(table *[opcodeLimit]opcodeMetadataEntry) {
				table[opJump].operands.b = bytecodeOperandRegister
			},
			want: "jump target metadata does not match operand shape",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := opcodeMetadataTable
			tt.mutate(&table)
			err := validateOpcodeMetadataTable(table)
			if err == nil {
				t.Fatal("validateOpcodeMetadataTable returned nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateOpcodeMetadataTable error is %q, want %q", err, tt.want)
			}
		})
	}
}

func wantOpcodeEffects(op opcode) opcodeEffects {
	effects := opcodeEffects{classified: true}
	if wantOpcodeCallbackMask(op) {
		return opcodeEffects{
			classified:                  true,
			invokesScriptOrHostCode:     true,
			mayYield:                    true,
			mayError:                    true,
			allocatesOrObservesIdentity: true,
			readsGlobals:                true,
			writesGlobals:               true,
			readsUpvalues:               true,
			writesUpvalues:              true,
			readsTables:                 true,
			writesTables:                true,
			readsUnknownHeap:            true,
			writesUnknownHeap:           true,
		}
	}
	switch op {
	case opLoadGlobal:
		effects.readsGlobals = true
	case opSetGlobal:
		effects.writesGlobals = true
	case opGetUpvalue:
		effects.readsUpvalues = true
	case opSetUpvalue:
		effects.writesUpvalues = true
	case opJumpIfTableHasMetatable:
		effects.readsTables = true
	case opNewTable, opVararg:
		effects.allocatesOrObservesIdentity = true
	case opClosure:
		effects.readsUpvalues = true
		effects.allocatesOrObservesIdentity = true
	case opNumericForCheck:
		effects.mayError = true
	}
	return effects
}

func wantOpcodeCallbackMask(op opcode) bool {
	switch op {
	case opSetField,
		opGetStringField,
		opSetStringField,
		opGetStringFieldIndex,
		opSetStringFieldIndex,
		opGetIndex,
		opSetIndex,
		opPrepareIter,
		opArrayNext,
		opArrayNextJump2,
		opAdd,
		opSub,
		opMul,
		opDiv,
		opMod,
		opIDiv,
		opPow,
		opNeg,
		opAddK,
		opSubK,
		opMulK,
		opDivK,
		opModK,
		opIDivK,
		opLen,
		opConcat,
		opConcatChain,
		opEqual,
		opNotEqual,
		opLess,
		opLessEqual,
		opGreater,
		opGreaterEqual,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfNotGreaterK,
		opJumpIfLessK,
		opJumpIfGreaterK,
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfLess,
		opJumpIfGreater,
		opFastCall,
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallUpvalueOne,
		opCallMethodOne:
		return true
	default:
		return false
	}
}

func wantDirectFrameOpcodeSupported(op opcode) bool {
	switch op {
	case opLoadConst,
		opLoadGlobal,
		opSetGlobal,
		opNewTable,
		opSetField,
		opSetStringField,
		opSetStringFieldIndex,
		opGetStringField,
		opGetStringFieldIndex,
		opSetIndex,
		opGetIndex,
		opClosure,
		opGetUpvalue,
		opSetUpvalue,
		opVararg,
		opPrepareIter,
		opArrayNext,
		opArrayNextJump2,
		opMove,
		opAdd,
		opSub,
		opMul,
		opDiv,
		opMod,
		opIDiv,
		opAddK,
		opSubK,
		opMulK,
		opDivK,
		opModK,
		opIDivK,
		opPow,
		opNeg,
		opLen,
		opConcat,
		opConcatChain,
		opEqual,
		opNotEqual,
		opLess,
		opLessEqual,
		opGreater,
		opGreaterEqual,
		opNumericForCheck,
		opNumericForLoop,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfNotGreaterK,
		opJumpIfLessK,
		opJumpIfGreaterK,
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfLess,
		opJumpIfGreater,
		opJumpIfTableHasMetatable,
		opFastCall,
		opJumpIfFalse,
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallUpvalueOne,
		opCallMethodOne,
		opJump,
		opReturnOne,
		opReturn:
		return true
	default:
		return false
	}
}

func wantOpcodeControlFlow(op opcode) opcodeControlFlowKind {
	switch op {
	case opJump,
		opNumericForLoop:
		return opcodeControlJump
	case opArrayNextJump2,
		opNumericForCheck,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfNotGreaterK,
		opJumpIfLessK,
		opJumpIfGreaterK,
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfLess,
		opJumpIfGreater,
		opJumpIfTableHasMetatable,
		opJumpIfFalse:
		return opcodeControlBranch
	case opReturnOne, opReturn:
		return opcodeControlReturn
	default:
		return opcodeControlNone
	}
}

func classifiedOpcodeJumpTarget(t *testing.T, op opcode) opcodeJumpTargetSlot {
	t.Helper()
	operands := classifyInstructionOperands(instruction{op: op})
	bJump := operands.b.kind == bytecodeOperandJumpTarget
	dJump := operands.d.kind == bytecodeOperandJumpTarget
	if bJump && dJump {
		t.Fatalf("%s classifies both b and d as jump targets", opcodeName(op))
	}
	if bJump {
		return opcodeJumpTargetB
	}
	if dJump {
		return opcodeJumpTargetD
	}
	return opcodeJumpTargetNone
}

func classifiedOpcodeOperandShape(op opcode) opcodeOperandShape {
	operands := classifyInstructionOperands(instruction{op: op})
	return opcodeOperandShape{
		a: operands.a.kind,
		b: operands.b.kind,
		c: operands.c.kind,
		d: operands.d.kind,
	}
}

func TestRunRejectsInvalidProtoBeforeHostEffects(t *testing.T) {
	calls := 0
	proto := newProto(
		[]Value{StringValue("tick")},
		[]instruction{
			{op: opLoadGlobal, a: 0, b: 0},
			{op: opCall, a: 0, b: 0, c: 0, d: 0},
			{op: opLoadConst, a: 0, b: 99},
			{op: opReturn, a: 0, b: 1},
		},
		nil,
		nil,
		1,
		0,
		false,
	)

	_, err := RunWithGlobals(proto, map[string]Value{
		"tick": HostFuncValue(func(args []Value) ([]Value, error) {
			calls++
			return nil, nil
		}),
	})
	if err == nil {
		t.Fatal("RunWithGlobals succeeded, want invalid prototype error")
	}
	if !strings.Contains(err.Error(), "invalid prototype") {
		t.Fatalf("RunWithGlobals error is %q, want invalid prototype", err)
	}
	if calls != 0 {
		t.Fatalf("host function was called %d times, want 0", calls)
	}
}

func TestNewProtoStoresVerificationError(t *testing.T) {
	proto := newProto(
		nil,
		[]instruction{
			{op: opLoadConst, a: 0, b: 99},
			{op: opReturn, a: 0, b: 1},
		},
		nil,
		nil,
		1,
		0,
		false,
	)

	if proto.verifyErr == nil {
		t.Fatal("newProto stored nil verifyErr, want constant range error")
	}
	if !strings.Contains(proto.verifyErr.Error(), "constant index 99 out of range") {
		t.Fatalf("verifyErr is %q, want constant range error", proto.verifyErr)
	}
}

func TestRunRejectsInvalidClosureUpvalueBeforeHostEffects(t *testing.T) {
	calls := 0
	child := newProto(
		nil,
		[]instruction{{op: opReturn, a: 0, b: 1}},
		nil,
		[]upvalueDesc{{local: true, index: 99}},
		1,
		0,
		false,
	)
	proto := newProto(
		[]Value{StringValue("tick")},
		[]instruction{
			{op: opLoadGlobal, a: 0, b: 0},
			{op: opCall, a: 0, b: 0, c: 0, d: 0},
			{op: opClosure, a: 0, b: 0},
			{op: opReturn, a: 0, b: 1},
		},
		[]*Proto{child},
		nil,
		1,
		0,
		false,
	)

	_, err := RunWithGlobals(proto, map[string]Value{
		"tick": HostFuncValue(func(args []Value) ([]Value, error) {
			calls++
			return nil, nil
		}),
	})
	if err == nil {
		t.Fatal("RunWithGlobals succeeded, want invalid prototype error")
	}
	if !strings.Contains(err.Error(), "invalid prototype") {
		t.Fatalf("RunWithGlobals error is %q, want invalid prototype", err)
	}
	if calls != 0 {
		t.Fatalf("host function was called %d times, want 0", calls)
	}
}

func parseSourceForBytecodeIRTest(t *testing.T, source string) sourceArtifact {
	t.Helper()
	artifact, err := parseSource(Source{Text: source})
	if err != nil {
		t.Fatalf("parseSource returned error: %v", err)
	}
	return artifact
}

func compilerForBytecodeIRTest(artifact sourceArtifact, options compilerOptions) compiler {
	return compiler{
		bind:            artifact.bind,
		symbolRegisters: newDenseSymbolSlots(len(artifact.bind.symbols)),
		options:         options,
	}
}

func findBytecodeIRInstruction(ir []bytecodeIRInstruction, op opcode) (bytecodeIRInstruction, bool) {
	for _, ins := range ir {
		if ins.op == op {
			return ins, true
		}
	}
	return bytecodeIRInstruction{}, false
}

func assertBytecodeIRLiveness(t *testing.T, liveness []bytecodeIRLivenessBlock, blockID int, liveIn []int, liveOut []int, def []int) {
	t.Helper()
	if blockID >= len(liveness) {
		t.Fatalf("missing liveness block %d in %#v", blockID, liveness)
	}
	block := liveness[blockID]
	if !reflect.DeepEqual(block.liveIn.values(), normalizeIntSetExpectation(liveIn)) {
		t.Fatalf("block %d liveIn is %#v, want %#v", blockID, block.liveIn.values(), liveIn)
	}
	if !reflect.DeepEqual(block.liveOut.values(), normalizeIntSetExpectation(liveOut)) {
		t.Fatalf("block %d liveOut is %#v, want %#v", blockID, block.liveOut.values(), liveOut)
	}
	if !reflect.DeepEqual(block.def.values(), normalizeIntSetExpectation(def)) {
		t.Fatalf("block %d def is %#v, want %#v", blockID, block.def.values(), def)
	}
}

func normalizeIntSetExpectation(values []int) []int {
	if values == nil {
		return []int{}
	}
	return values
}

func assertTableNumber(t *testing.T, table *Table, key Value, want float64) {
	t.Helper()
	value, err := table.Get(key)
	if err != nil {
		t.Fatalf("table.Get(%#v) returned error: %v", key, err)
	}
	number, ok := value.Number()
	if !ok || number != want {
		t.Fatalf("table.Get(%#v) is %#v, want number %v", key, value, want)
	}
}

func TestRunDirectFrameNumericIndexReadsArraySlotWithoutGenericFallback(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{value = 7},
	{value = 9},
}
local i = 2
return rows[i].value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if joined := strings.Join(disassembleProto(proto), "\n"); !strings.Contains(joined, "GET_INDEX") {
		t.Fatalf("compiled numeric index read is missing GET_INDEX:\n%s", joined)
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameInstrumented = true
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 9 {
		t.Fatalf("result is %v (%t), want number 9", results[0], ok)
	}
	if counts.invalidKeyFallbacks != 0 {
		t.Fatalf("invalid key fallbacks = %d, want numeric array index handled directly", counts.invalidKeyFallbacks)
	}
	if counts.numericArrayIndexHits != 1 {
		t.Fatalf("numeric array index hits = %d, want one direct array read", counts.numericArrayIndexHits)
	}
}
