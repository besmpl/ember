package ember

import (
	"reflect"
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
		builder.emit(instruction{op: opJumpIfStringFieldNotGreaterK, a: 0, b: field, c: value, d: 1})

		_, err := builder.finalizeProto(nil, 1, 0, false)
		if err == nil {
			t.Fatal("finalizeProto succeeded, want non-string field error")
		}
		if !strings.Contains(err.Error(), "constant index 0 is number, want string") {
			t.Fatalf("finalizeProto error is %q, want non-string field detail", err)
		}
	})

	t.Run("value", func(t *testing.T) {
		var builder bytecodeBuilder
		field := builder.addConstant(StringValue("shield"))
		value := builder.addConstant(StringValue("zero"))
		builder.emit(instruction{op: opJumpIfStringFieldGreaterK, a: 0, b: field, c: value, d: 1})

		_, err := builder.finalizeProto(nil, 1, 0, false)
		if err == nil {
			t.Fatal("finalizeProto succeeded, want non-number value error")
		}
		if !strings.Contains(err.Error(), "constant index 1 is string, want number") {
			t.Fatalf("finalizeProto error is %q, want non-number value detail", err)
		}
	})
}

func TestBytecodeFinalizerRejectsInvalidStringFieldTruthyBranchConstant(t *testing.T) {
	var builder bytecodeBuilder
	field := builder.addConstant(NumberValue(1))
	builder.emit(instruction{op: opJumpIfStringFieldFalse, a: 0, b: field, d: 1})

	_, err := builder.finalizeProto(nil, 1, 0, false)
	if err == nil {
		t.Fatal("finalizeProto succeeded, want non-string field error")
	}
	if !strings.Contains(err.Error(), "constant index 0 is number, want string") {
		t.Fatalf("finalizeProto error is %q, want non-string field detail", err)
	}
}

func TestBytecodeFinalizerRejectsInvalidRowStringFieldReadSlot(t *testing.T) {
	var builder bytecodeBuilder
	field := builder.addConstant(StringValue("kind"))
	builder.emit(instruction{op: opGetRowStringField, a: 0, b: 1, c: field, d: -1})

	_, err := builder.finalizeProto(nil, 2, 0, false)
	if err == nil {
		t.Fatal("finalizeProto succeeded, want invalid row string field slot error")
	}
	if !strings.Contains(err.Error(), "negative row string field slot") {
		t.Fatalf("finalizeProto error is %q, want row slot detail", err)
	}
}

func TestBytecodeFinalizerRejectsInvalidSubStringFieldConstant(t *testing.T) {
	var builder bytecodeBuilder
	field := builder.addConstant(NumberValue(1))
	builder.emit(instruction{op: opSubStringField, a: 0, b: field, c: 1})

	_, err := builder.finalizeProto(nil, 2, 0, false)
	if err == nil {
		t.Fatal("finalizeProto succeeded, want non-string field error")
	}
	if !strings.Contains(err.Error(), "constant index 0 is number, want string") {
		t.Fatalf("finalizeProto error is %q, want non-string field detail", err)
	}
}

func TestBytecodeFinalizerRejectsInvalidSubAddStringFieldConstant(t *testing.T) {
	var builder bytecodeBuilder
	target := builder.addConstant(StringValue("hp"))
	add := builder.addConstant(NumberValue(1))
	desc := builder.addRowFieldSubAddOp(rowFieldSubAddOp{
		target:     target,
		add:        add,
		targetSlot: 0,
		addSlot:    1,
	})
	builder.emit(instruction{op: opSubAddStringField, a: 0, b: desc, c: 1})

	_, err := builder.finalizeProto(nil, 2, 0, false)
	if err == nil {
		t.Fatal("finalizeProto succeeded, want non-string add field error")
	}
	if !strings.Contains(err.Error(), "constant index 1 is number, want string") {
		t.Fatalf("finalizeProto error is %q, want non-string add field detail", err)
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

func TestBytecodeVerifierRejectsDirectRegisterProtoWithCapturedLocals(t *testing.T) {
	proto := newProto(
		nil,
		[]instruction{{op: opReturn, a: 0, b: 1}},
		nil,
		nil,
		1,
		0,
		false,
	)
	proto.directRegisters = true
	proto.capturedLocals = []bool{true}

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want direct-register captured-local error")
	}
	if !strings.Contains(err.Error(), "direct-register prototype has captured locals") {
		t.Fatalf("verifyProto error is %q, want direct-register captured-local detail", err)
	}
}

func TestBytecodeVerifierRejectsDirectFrameDispatchForUnsupportedOpcode(t *testing.T) {
	proto := newProto(
		[]Value{StringValue("missing")},
		[]instruction{
			{op: opLoadGlobal, a: 0, b: 0},
			{op: opReturnOne, a: 0},
		},
		nil,
		nil,
		1,
		0,
		false,
	)
	proto.directFrameDispatch = true

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want unsupported direct-frame opcode error")
	}
	if !strings.Contains(err.Error(), "direct-frame prototype contains unsupported opcode") {
		t.Fatalf("verifyProto error is %q, want unsupported direct-frame opcode detail", err)
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

func TestBytecodeVerifierRejectsStaleNumericForDescriptors(t *testing.T) {
	proto := newProto(
		[]Value{NumberValue(0)},
		[]instruction{
			{op: opNumericForCheck, a: 0, b: 1, c: 2, d: 4},
			{op: opAdd, a: 3, b: 3, c: 0},
			{op: opAdd, a: 0, b: 0, c: 2},
			{op: opJump, b: 0},
			{op: opReturnOne, a: 3},
		},
		nil,
		nil,
		4,
		0,
		false,
	)
	proto.numericForLoops = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale numeric for descriptor error")
	}
	if !strings.Contains(err.Error(), "numeric for descriptors [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want numeric for descriptor detail", err)
	}
}

func TestBytecodeVerifierRejectsStaleIntrinsicDescriptors(t *testing.T) {
	proto := newProto(
		nil,
		[]instruction{
			{op: opTableInsert, a: 0, b: 2, d: 1},
			{op: opReturnOne, a: 0},
		},
		nil,
		nil,
		2,
		0,
		false,
	)
	proto.intrinsicOps = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale intrinsic descriptor error")
	}
	if !strings.Contains(err.Error(), "intrinsic descriptors [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want intrinsic descriptor detail", err)
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
	got, ok := frame.cells[1].value.Number()
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
			if tt.table.stringFieldMap != nil {
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

func TestTableInlineStringFieldSlotsAreVersionGuarded(t *testing.T) {
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
	if _, ok := table.rawStringFieldAtSlot(regenSlot, "regen"); ok {
		t.Fatal("rawStringFieldAtSlot(regen) used stale version after hp update")
	}
	updated, ok := table.rawStringField("hp")
	if !ok {
		t.Fatal("rawStringField(hp) failed after slot update")
	}
	if got, ok := updated.Number(); !ok || got != 9 {
		t.Fatalf("updated hp is %v (%t), want number 9", updated, ok)
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
	object.metatable = metatable

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
	if len(table.fields) != 0 {
		t.Fatalf("fast array spilled %d hash fields, want none", len(table.fields))
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

	thread := newVMThread(runtimeGlobals(nil))
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

func TestVMThreadKeepsRecursiveFibonacciAllocationsBounded(t *testing.T) {
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

	thread := newVMThread(runtimeGlobals(nil))
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
	wantLines := []int{1, 2}
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
	if len(table.fields) != 0 {
		t.Fatalf("hash fields has %d entries, want 0 for contiguous array writes", len(table.fields))
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
	if got, want := len(table.fields), 1; got != want {
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
	if got, want := len(table.fields), 0; got != want {
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

func TestTableRawNextIncludesArrayAndHashKeysInDeterministicOrder(t *testing.T) {
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
	if number, ok := firstKey.Number(); !ok || number != 1 {
		t.Fatalf("first next key is %v (%t), want number 1", number, ok)
	}
	if text, ok := firstValue.String(); !ok || text != "first" {
		t.Fatalf("first next value is %q (%t), want first", text, ok)
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
	if text, ok := thirdKey.String(); !ok || text != "name" {
		t.Fatalf("third next key is %q (%t), want name", text, ok)
	}
	if text, ok := thirdValue.String(); !ok || text != "ember" {
		t.Fatalf("third next value is %q (%t), want ember", text, ok)
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
		"direct_registers false",
		"direct_frame_dispatch false",
		"captured_locals r1",
		"entry_nil none",
		"direct_frame_rejection prototype has captured locals",
		"constant_key k0 string \"hp\"",
		"constant_number k1 3",
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

func TestCompilerLowersNumericForToCombinedLoopCheck(t *testing.T) {
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
	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "numeric_for") {
		t.Fatalf("compiled numeric for is missing numeric loop descriptor:\n%s", facts)
	}
	if !strings.Contains(facts, "increment") {
		t.Fatalf("compiled numeric for descriptor is missing increment pc:\n%s", facts)
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

func TestCompilerUsesAddNumericModKOpcode(t *testing.T) {
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
	if !strings.Contains(joined, "ADD_NUMERIC_MOD_K") {
		t.Fatalf("compiled numeric update is missing ADD_NUMERIC_MOD_K:\n%s", joined)
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

func TestFinalizedProtoMarksDirectRegisterFrames(t *testing.T) {
	direct, err := Compile("return 1")
	if err != nil {
		t.Fatalf("Compile direct returned error: %v", err)
	}
	if !direct.directRegisters {
		t.Fatal("direct prototype is not marked for direct registers")
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
	if captured.directRegisters {
		t.Fatal("capturing parent prototype is marked for direct registers")
	}
	if !captured.prototypes[0].directRegisters {
		t.Fatal("non-capturing child frame should still use direct registers")
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
	if !proto.directRegisters {
		t.Fatal("compiled scalar loop is not marked for direct registers")
	}
	if !proto.directFrameDispatch {
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

func TestProtoDirectFrameRejectionReportsFirstUnsupportedOpcode(t *testing.T) {
	var builder bytecodeBuilder
	name := builder.addConstant(StringValue("missing"))
	builder.emit(instruction{op: opLoadGlobal, a: 0, b: name})
	builder.emit(instruction{op: opReturnOne, a: 0})
	proto := builder.proto(nil, 2, 0, false)

	rejection, ok := protoDirectFrameRejection(proto)
	if !ok {
		t.Fatal("protoDirectFrameRejection reported no blocker, want LOAD_GLOBAL blocker")
	}
	if rejection.pc != 0 || rejection.op != opLoadGlobal {
		t.Fatalf("rejection = pc %d op %v, want pc 0 LOAD_GLOBAL", rejection.pc, rejection.op)
	}
	if !strings.Contains(rejection.reason, "unsupported opcode") {
		t.Fatalf("rejection reason is %q, want unsupported opcode detail", rejection.reason)
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
	if !proto.directFrameDispatch {
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
	if !proto.directFrameDispatch {
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
	if !strings.Contains(joined, "PREPARE_ITER") || !strings.Contains(joined, "CALL r") {
		t.Fatalf("compiled array iteration is missing iterator setup/call:\n%s", joined)
	}
	if !proto.directFrameDispatch {
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
		"JUMP_IF_STRING_FIELD_FALSE",
		"JUMP_IF_STRING_FIELD_NOT_EQUAL_K",
		"JUMP_IF_STRING_FIELD_NOT_GREATER_K",
		"JUMP_IF_STRING_FIELD_GREATER_K",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled branch program is missing %s:\n%s", want, joined)
		}
	}
	if !proto.directFrameDispatch {
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

func TestRunDirectFrameRowStringFieldBranchPreservesSlotSemantics(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{kind = "ore", count = 1},
	{kind = "gem", count = 2},
	{kind = "key", count = 3},
}
local score = 0
for _, item in rows do
	if item.kind == "gem" or item.kind == "key" then
		score = score + item.count
	end
end
return score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_K") {
		t.Fatalf("compiled row branch program is missing row field branch:\n%s", joined)
	}
	if !strings.Contains(joined, "GET_ROW_STRING_FIELD") {
		t.Fatalf("compiled row branch program is missing row slot read:\n%s", joined)
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled row branch program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
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
	if !strings.Contains(joined, "ADD_NUMERIC_MOD_K") {
		t.Fatalf("compiled arithmetic is missing ADD_NUMERIC_MOD_K:\n%s", joined)
	}
	for _, want := range []string{"number 3", "number 2", "number 17"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled arithmetic descriptor is missing %s:\n%s", want, joined)
		}
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
	if !strings.Contains(joined, "ADD_NUMERIC_MOD_K") {
		t.Fatalf("compiled arithmetic is missing ADD_NUMERIC_MOD_K:\n%s", joined)
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
local value = 1
return value + 2
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
	if !strings.Contains(joined, "JUMP_IF_MOD_K_NOT_EQUAL_K") {
		t.Fatalf("compiled modulo branch is missing direct modulo jump:\n%s", joined)
	}
	if strings.Contains(joined, " MOD_K r") {
		t.Fatalf("compiled modulo branch materialized modulo register:\n%s", joined)
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
	if !strings.Contains(joined, "JUMP_IF_MOD_K_NOT_EQUAL_K") {
		t.Fatalf("compiled modulo branch is missing direct modulo jump:\n%s", joined)
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

func TestCompilerUsesTwoStepStringFieldOpcode(t *testing.T) {
	proto, err := Compile(`
local player = {stats = {hp = 10}}
return player.stats.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "GET_STRING_FIELD2") {
		t.Fatalf("compiled two-step named field read is missing GET_STRING_FIELD2:\n%s", joined)
	}
}

func TestTwoStepStringFieldReadSeesIntermediateMutation(t *testing.T) {
	proto, err := Compile(`
local player = {stats = {hp = 10}}
local first = player.stats.hp
player.stats = {hp = 20}
return first, player.stats.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "GET_STRING_FIELD2") {
		t.Fatalf("compiled two-step named field read is missing GET_STRING_FIELD2:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 10 {
		t.Fatalf("first result is %v (%t), want number 10", got, ok)
	}
	if got, ok := results[1].Number(); !ok || got != 20 {
		t.Fatalf("second result is %v (%t), want number 20", got, ok)
	}
}

func TestCompilerUsesTwoStepStringFieldSetOpcode(t *testing.T) {
	proto, err := Compile(`
local player = {stats = {hp = 10}}
player.stats.hp = 12
return player.stats.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "SET_STRING_FIELD2") {
		t.Fatalf("compiled two-step named field write is missing SET_STRING_FIELD2:\n%s", joined)
	}
}

func TestCompilerUsesAddStringFieldOpcode(t *testing.T) {
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
	if !strings.Contains(joined, "ADD_STRING_FIELD") {
		t.Fatalf("compiled field increment is missing ADD_STRING_FIELD:\n%s", joined)
	}
}

func TestCompilerUsesSubStringFieldOpcode(t *testing.T) {
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
	if !strings.Contains(joined, "SUB_STRING_FIELD") {
		t.Fatalf("compiled field decrement is missing SUB_STRING_FIELD:\n%s", joined)
	}
}

func TestCompilerUsesSubAddStringFieldOpcode(t *testing.T) {
	proto, err := Compile(`
local entity = {hp = 10, shield = 4, regen = 2}
local incoming = 3
entity.hp = entity.hp - incoming + entity.regen
return entity.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "SUB_ADD_STRING_FIELD") {
		t.Fatalf("compiled same-row field update is missing SUB_ADD_STRING_FIELD:\n%s", joined)
	}
	if !strings.Contains(joined, "slots 0 2") {
		t.Fatalf("compiled same-row field update is missing row slot descriptor:\n%s", joined)
	}
}

func TestCompilerPropagatesRowSlotsThroughGenericFor(t *testing.T) {
	proto, err := Compile(`
local entities = {
	{hp = 10, shield = 4, regen = 2},
	{hp = 20, shield = 8, regen = 3},
}
local incoming = 3
for _, entity in entities do
	entity.hp = entity.hp - incoming + entity.regen
end
return entities[1].hp + entities[2].hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "SUB_ADD_STRING_FIELD") {
		t.Fatalf("compiled generic-for row update is missing SUB_ADD_STRING_FIELD:\n%s", joined)
	}
	if !strings.Contains(joined, "slots 0 2") {
		t.Fatalf("compiled generic-for row update is missing propagated row slots:\n%s", joined)
	}
}

func TestCompilerUsesAddSubStringField2Opcode(t *testing.T) {
	proto, err := Compile(`
local player = {stats = {hp = 100, shield = 25}, inventory = {coins = 3}}
player.stats.hp = player.stats.hp + player.stats.shield - player.inventory.coins
return player.stats.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "ADD_SUB_STRING_FIELD2") {
		t.Fatalf("compiled nested field update is missing ADD_SUB_STRING_FIELD2:\n%s", joined)
	}
}

func TestCompileAndRunAddSubStringField2OpcodeUsesMetatableSemantics(t *testing.T) {
	proto, err := Compile(`
local log = {value = ""}
local stats = {hp = 10, shield = 5}
local inventory = {coins = 2}
local player = {}
setmetatable(player, {
	__index = function(_, key)
		log.value = log.value .. key .. ","
		if key == "stats" then
			return stats
		end
		return inventory
	end
})
player.stats.hp = player.stats.hp + player.stats.shield - player.inventory.coins
return log.value, stats.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "ADD_SUB_STRING_FIELD2") {
		t.Fatalf("compiled nested field update is missing ADD_SUB_STRING_FIELD2:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	gotLog, ok := results[0].String()
	if !ok || gotLog != "stats,stats,inventory,stats," {
		t.Fatalf("first result is %q (%t), want metatable lookup order", gotLog, ok)
	}
	gotHP, ok := results[1].Number()
	if !ok || gotHP != 13 {
		t.Fatalf("second result is %v (%t), want number 13", gotHP, ok)
	}
}

func TestCompileAndRunAddStringFieldOpcodeUsesMetatableSemantics(t *testing.T) {
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
	if !strings.Contains(joined, "ADD_STRING_FIELD") {
		t.Fatalf("compiled field increment is missing ADD_STRING_FIELD:\n%s", joined)
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

func TestCompileAndRunSubStringFieldOpcodeUsesMetatableSemantics(t *testing.T) {
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
	if !strings.Contains(joined, "SUB_STRING_FIELD") {
		t.Fatalf("compiled field decrement is missing SUB_STRING_FIELD:\n%s", joined)
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

func TestCompileAndRunSubAddStringFieldOpcodeUsesMetatableSemantics(t *testing.T) {
	proto, err := Compile(`
local backing = {hp = 10, regen = 2}
local proxy = {}
setmetatable(proxy, {
	__index = backing,
	__newindex = backing
})
local incoming = 3
proxy.hp = proxy.hp - incoming + proxy.regen
return backing.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "SUB_ADD_STRING_FIELD") {
		t.Fatalf("compiled same-row field update is missing SUB_ADD_STRING_FIELD:\n%s", joined)
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

func TestCompilerUsesSelectVarargCountOpcode(t *testing.T) {
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
	if !strings.Contains(joined, "SELECT_VARARG_COUNT") {
		t.Fatalf("compiled select count is missing SELECT_VARARG_COUNT:\n%s", joined)
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

func TestCompilerUsesDynamicFieldCallOpcode(t *testing.T) {
	proto, err := Compile(`
local state = {score = 0}
local handlers = {}
function handlers.score(s, amount)
	s.score = s.score + amount
	return s.score
end
local event = {kind = "score", amount = 5}
local result = handlers[event.kind](state, event.amount)
return result
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "CALL_TABLE_FIELD_KEY_ONE") {
		t.Fatalf("compiled dynamic field call is missing CALL_TABLE_FIELD_KEY_ONE:\n%s", joined)
	}
}

func TestDynamicFieldCallSeesHandlerMutation(t *testing.T) {
	proto, err := Compile(`
local state = {score = 0}
local handlers = {}
function handlers.score(s, amount)
	s.score = s.score + amount
	return s.score
end
local event = {kind = "score", amount = 5}
local first = handlers[event.kind](state, event.amount)
function handlers.score(s, amount)
	s.score = s.score + amount * 2
	return s.score
end
local second = handlers[event.kind](state, event.amount)
return first, second, state.score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "CALL_TABLE_FIELD_KEY_ONE") {
		t.Fatalf("compiled dynamic field call is missing CALL_TABLE_FIELD_KEY_ONE:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	wants := []float64{5, 15, 15}
	for i, want := range wants {
		got, ok := results[i].Number()
		if !ok || got != want {
			t.Fatalf("result %d is %v (%t), want %v", i, got, ok, want)
		}
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
	if !strings.Contains(joined, "JUMP_IF_STRING_FIELD_NOT_EQUAL_K") {
		t.Fatalf("compiled string field branch is missing JUMP_IF_STRING_FIELD_NOT_EQUAL_K:\n%s", joined)
	}
}

func TestCompilerUsesRowStringFieldEqualityBranchOpcode(t *testing.T) {
	proto, err := Compile(`
local inventory = {
	{kind = "ore", count = 12},
	{kind = "gem", count = 3},
}
local score = 0
for _, item in inventory do
	if item.kind == "gem" or item.kind == "key" then
		score = score + item.count
	end
end
return score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_K") {
		t.Fatalf("compiled row string field branch is missing JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_K:\n%s", joined)
	}
	if !strings.Contains(joined, "slot 0") {
		t.Fatalf("compiled row string field branch is missing propagated slot:\n%s", joined)
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
	if !strings.Contains(joined, "JUMP_IF_STRING_FIELD_NOT_EQUAL_K") {
		t.Fatalf("compiled string field branch is missing JUMP_IF_STRING_FIELD_NOT_EQUAL_K:\n%s", joined)
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
		"JUMP_IF_STRING_FIELD_NOT_GREATER_K",
		"JUMP_IF_STRING_FIELD_GREATER_K",
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
	if !strings.Contains(joined, "JUMP_IF_STRING_FIELD_NOT_GREATER_K") ||
		!strings.Contains(joined, "JUMP_IF_STRING_FIELD_GREATER_K") {
		t.Fatalf("compiled numeric field branch is missing optimized branch:\n%s", joined)
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

func TestCompilerUsesStringFieldTruthyBranchOpcode(t *testing.T) {
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
	if !strings.Contains(joined, "JUMP_IF_STRING_FIELD_FALSE") {
		t.Fatalf("compiled truthy field branch is missing JUMP_IF_STRING_FIELD_FALSE:\n%s", joined)
	}
}

func TestRunStringFieldTruthyBranchOpcode(t *testing.T) {
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
	if !strings.Contains(joined, "JUMP_IF_STRING_FIELD_FALSE") {
		t.Fatalf("compiled truthy field branch is missing JUMP_IF_STRING_FIELD_FALSE:\n%s", joined)
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

func TestCompilerUsesSelfUpvalueOneResultCallOpcode(t *testing.T) {
	proto, err := Compile(`
local function fib(n)
	if n < 2 then
		return n
	end
	return fib(n - 1) + fib(n - 2)
end
return fib(4)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, want := len(proto.prototypes), 1; got != want {
		t.Fatalf("compiled root has %d child prototypes, want %d", got, want)
	}

	joined := strings.Join(disassembleProto(proto.prototypes[0]), "\n")
	if !strings.Contains(joined, "CALL_UPVALUE_SELF_K_ONE") &&
		!strings.Contains(joined, "CALL_UPVALUE_SELF_ADD_K_ONE") {
		t.Fatalf("compiled recursive upvalue call is missing self-call opcode:\n%s", joined)
	}
	if strings.Contains(joined, "GET_UPVALUE") {
		t.Fatalf("compiled recursive upvalue call kept separate GET_UPVALUE:\n%s", joined)
	}
}

func TestCompilerUsesSelfUpvaluePairAddOpcode(t *testing.T) {
	proto, err := Compile(`
local function fib(n)
	if n < 2 then
		return n
	end
	return fib(n - 1) + fib(n - 2)
end
return fib(6)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if got, want := len(proto.prototypes), 1; got != want {
		t.Fatalf("compiled root has %d child prototypes, want %d", got, want)
	}

	joined := strings.Join(disassembleProto(proto.prototypes[0]), "\n")
	if !strings.Contains(joined, "CALL_UPVALUE_SELF_ADD_K_ONE") {
		t.Fatalf("compiled recursive pair-add is missing CALL_UPVALUE_SELF_ADD_K_ONE:\n%s", joined)
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
	bindCursor := 0
	return compiler{
		bind:            artifact.bind,
		bindCursor:      &bindCursor,
		symbolRegisters: make(map[int]int),
		locals:          make(map[string]int),
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
