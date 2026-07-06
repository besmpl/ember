package ember

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"reflect"
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
	proto.numericForLoops = []numericForLoopDesc{{checkPC: 99}}
	proto.intrinsicOps = []intrinsicOpDesc{{pc: 99}}
	proto.capturedLocals = []bool{true}
	proto.directRegisters = false
	proto.directFrameDispatch = false
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
	if len(proto.numericForLoops) != 0 {
		t.Fatalf("numericForLoops = %#v, want rebuilt empty facts", proto.numericForLoops)
	}
	if len(proto.intrinsicOps) != 0 {
		t.Fatalf("intrinsicOps = %#v, want rebuilt empty facts", proto.intrinsicOps)
	}
	if len(proto.capturedLocals) != 0 {
		t.Fatalf("capturedLocals = %#v, want rebuilt empty facts", proto.capturedLocals)
	}
	if !proto.directRegisters || !proto.directFrameDispatch {
		t.Fatalf("direct facts = registers %t dispatch %t, want true true", proto.directRegisters, proto.directFrameDispatch)
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

func TestBytecodeFinalizerRejectsInvalidRowStringFieldWriteSlot(t *testing.T) {
	var builder bytecodeBuilder
	field := builder.addConstant(StringValue("kind"))
	builder.emit(instruction{op: opSetRowStringField, a: 0, b: field, c: 1, d: -1})

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
			{op: opSetGlobal, a: 0, b: 0},
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
	if !strings.Contains(err.Error(), "direct-frame prototype contains unsupported opcode SET_GLOBAL") {
		t.Fatalf("verifyProto error is %q, want unsupported SET_GLOBAL detail", err)
	}
	if !strings.Contains(err.Error(), "global writes require generic frame environment semantics") {
		t.Fatalf("verifyProto error is %q, want unsupported reason detail", err)
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

func TestBytecodeVerifierRejectsStaleConstantKindFacts(t *testing.T) {
	proto := newProto(
		[]Value{NumberValue(4)},
		[]instruction{
			{op: opLoadConst, a: 0, b: 0},
			{op: opReturnOne, a: 0},
		},
		nil,
		nil,
		1,
		0,
		false,
	)
	proto.constantKindFacts = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale constant kind fact error")
	}
	if !strings.Contains(err.Error(), "constant kind facts [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want constant kind fact detail", err)
	}
}

func TestBytecodeVerifierRejectsStaleRegisterKindFacts(t *testing.T) {
	proto := newProto(
		[]Value{NumberValue(4)},
		[]instruction{
			{op: opLoadConst, a: 0, b: 0},
			{op: opReturnOne, a: 0},
		},
		nil,
		nil,
		1,
		0,
		false,
	)
	proto.registerKindFacts = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale register kind fact error")
	}
	if !strings.Contains(err.Error(), "register kind facts [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want register kind fact detail", err)
	}
}

func TestBytecodeVerifierRejectsStaleNumericOperandFacts(t *testing.T) {
	proto := newProto(
		[]Value{NumberValue(4), NumberValue(2)},
		[]instruction{
			{op: opLoadConst, a: 0, b: 0},
			{op: opLoadConst, a: 1, b: 1},
			{op: opAdd, a: 2, b: 0, c: 1},
			{op: opReturnOne, a: 2},
		},
		nil,
		nil,
		3,
		0,
		false,
	)
	proto.numericOperandFacts = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale numeric operand fact error")
	}
	if !strings.Contains(err.Error(), "numeric operand facts [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want numeric operand fact detail", err)
	}
}

func TestBytecodeVerifierRejectsStaleReductionFacts(t *testing.T) {
	proto := newProto(
		nil,
		[]instruction{
			{op: opJumpIfNotGreater, a: 0, b: 1, d: 2},
			{op: opMove, a: 1, b: 0},
			{op: opReturnOne, a: 1},
		},
		nil,
		nil,
		2,
		2,
		false,
	)
	proto.reductionFacts = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale reduction fact error")
	}
	if !strings.Contains(err.Error(), "reduction facts [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want reduction fact detail", err)
	}
}

func TestBytecodeVerifierRejectsStaleDirectBlockPlans(t *testing.T) {
	proto := newProto(
		[]Value{NumberValue(0)},
		[]instruction{
			{op: opJumpIfNotLessK, a: 0, b: 0, d: 3},
			{op: opNeg, a: 0, b: 0},
			{op: opJump, b: 3},
			{op: opReturnOne, a: 0},
		},
		nil,
		nil,
		1,
		1,
		false,
	)
	proto.directBlockPlans = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale direct block plan error")
	}
	if !strings.Contains(err.Error(), "direct block plans [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want direct block plan detail", err)
	}
}

func TestBytecodeVerifierRejectsStaleVerifiedPlans(t *testing.T) {
	proto := newProto(
		[]Value{NumberValue(0)},
		[]instruction{
			{op: opJumpIfNotLessK, a: 0, b: 0, d: 3},
			{op: opNeg, a: 0, b: 0},
			{op: opJump, b: 3},
			{op: opReturnOne, a: 0},
		},
		nil,
		nil,
		1,
		1,
		false,
	)
	proto.verifiedPlans = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale verified plan error")
	}
	if !strings.Contains(err.Error(), "verified plans [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want verified plan detail", err)
	}
}

func TestVerifyRegionRejectsCallRisk(t *testing.T) {
	proto := &Proto{
		code: []instruction{
			{op: opCallLocalOne, a: 0, b: 0, c: 1, d: 1},
			{op: opReturnOne, a: 0},
		},
		registers: 2,
	}
	_, rejection, ok := verifyRegion(proto, 0, verifiedPlanCandidate{
		kind: verifiedPlanKindDirectBlock,
		directBlock: directBlockPlanDesc{
			pc:       0,
			kind:     "row_field_add_store",
			startPC:  0,
			resumePC: 1,
		},
	})
	if ok {
		t.Fatal("verifyRegion accepted call-risk region, want rejection")
	}
	if !strings.Contains(rejection.reason, "call") {
		t.Fatalf("verifyRegion rejection reason is %q, want call risk detail", rejection.reason)
	}
}

func TestBytecodeVerifierRejectsStaleSlotKindFacts(t *testing.T) {
	proto := newProto(
		[]Value{StringValue("hp"), NumberValue(4)},
		[]instruction{
			{op: opNewTable, a: 0, c: 1},
			{op: opLoadConst, a: 1, b: 1},
			{op: opSetStringField, a: 0, b: 0, c: 1},
			{op: opReturnOne, a: 0},
		},
		nil,
		nil,
		2,
		0,
		false,
	)
	proto.slotKindFacts = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale slot kind fact error")
	}
	if !strings.Contains(err.Error(), "slot kind facts [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want slot kind fact detail", err)
	}
}

func TestBytecodeVerifierRejectsStalePathKindFacts(t *testing.T) {
	proto := newProto(
		[]Value{StringValue("child"), StringValue("value"), NumberValue(0), NumberValue(1)},
		[]instruction{
			{op: opLoadConst, a: 1, b: 2},
			{op: opGetStringField2, a: 2, b: 0, c: 0, d: 1},
			{op: opGetStringField2, a: 3, b: 0, c: 0, d: 1},
			{op: opAddK, a: 1, b: 1, c: 3},
			{op: opJump, b: 1},
			{op: opReturnOne, a: 1},
		},
		nil,
		nil,
		4,
		1,
		false,
	)
	proto.pathKindFacts = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale path kind fact error")
	}
	if !strings.Contains(err.Error(), "path kind facts [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want path kind fact detail", err)
	}
}

func TestBytecodeVerifierRejectsStalePredicateBranchDescriptors(t *testing.T) {
	proto := newProto(
		nil,
		[]instruction{
			{op: opJumpIfFalse, a: 0, b: 2},
			{op: opReturnOne, a: 0},
			{op: opReturnOne, a: 0},
		},
		nil,
		nil,
		1,
		1,
		false,
	)
	proto.predicateBranches = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale predicate branch descriptor error")
	}
	if !strings.Contains(err.Error(), "predicate branch descriptors [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want predicate branch descriptor detail", err)
	}
}

func TestBytecodeVerifierRejectsStaleBranchRefinements(t *testing.T) {
	proto := newProto(
		nil,
		[]instruction{
			{op: opJumpIfFalse, a: 0, b: 2},
			{op: opReturnOne, a: 0},
			{op: opReturnOne, a: 0},
		},
		nil,
		nil,
		1,
		1,
		false,
	)
	proto.branchRefinements = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale branch refinement error")
	}
	if !strings.Contains(err.Error(), "branch refinements [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want branch refinement detail", err)
	}
}

func TestBytecodeVerifierRejectsStaleFiniteTagRefinements(t *testing.T) {
	proto := newProto(
		[]Value{StringValue("poison"), StringValue("regen")},
		[]instruction{
			{op: opJumpIfNotEqualK, a: 0, b: 0, d: 2},
			{op: opReturnOne, a: 0},
			{op: opJumpIfNotEqualK, a: 0, b: 1, d: 4},
			{op: opReturnOne, a: 0},
			{op: opReturnOne, a: 0},
		},
		nil,
		nil,
		1,
		1,
		false,
	)
	proto.finiteTagRefinements = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale finite tag refinement error")
	}
	if !strings.Contains(err.Error(), "finite tag refinements [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want finite tag refinement detail", err)
	}
}

func TestBytecodeVerifierRejectsStalePathFacts(t *testing.T) {
	proto := newProto(
		[]Value{StringValue("child"), NumberValue(0), NumberValue(1)},
		[]instruction{
			{op: opLoadConst, a: 1, b: 1},
			{op: opGetStringField, a: 2, b: 0, c: 0},
			{op: opGetStringField, a: 3, b: 0, c: 0},
			{op: opAddK, a: 1, b: 1, c: 2},
			{op: opJump, b: 1},
			{op: opReturnOne, a: 1},
		},
		nil,
		nil,
		4,
		1,
		false,
	)
	proto.pathFacts = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale path fact error")
	}
	if !strings.Contains(err.Error(), "path facts [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want path fact detail", err)
	}
}

func TestBytecodeVerifierRejectsStalePathFactRejections(t *testing.T) {
	proto := newProto(
		[]Value{StringValue("child"), NumberValue(0), NumberValue(1)},
		[]instruction{
			{op: opLoadConst, a: 1, b: 1},
			{op: opGetStringField, a: 2, b: 0, c: 0},
			{op: opSetStringField, a: 0, b: 0, c: 1},
			{op: opGetStringField, a: 3, b: 0, c: 0},
			{op: opAddK, a: 1, b: 1, c: 2},
			{op: opJump, b: 1},
			{op: opReturnOne, a: 1},
		},
		nil,
		nil,
		4,
		1,
		false,
	)
	proto.pathFactRejections = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale path fact rejection error")
	}
	if !strings.Contains(err.Error(), "path fact rejections [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want path fact rejection detail", err)
	}
}

func TestBytecodeVerifierRejectsStalePathPlans(t *testing.T) {
	proto := newProto(
		[]Value{StringValue("child"), StringValue("value"), NumberValue(0), NumberValue(1)},
		[]instruction{
			{op: opLoadConst, a: 1, b: 2},
			{op: opGetStringField2, a: 2, b: 0, c: 0, d: 1},
			{op: opGetStringField2, a: 3, b: 0, c: 0, d: 1},
			{op: opAddK, a: 1, b: 1, c: 3},
			{op: opJump, b: 1},
			{op: opReturnOne, a: 1},
		},
		nil,
		nil,
		4,
		1,
		false,
	)
	proto.pathPlans = nil

	err := verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale path plan error")
	}
	if !strings.Contains(err.Error(), "path plans [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want path plan detail", err)
	}
}

func TestBytecodeVerifierRejectsStaleBlockPlans(t *testing.T) {
	proto, err := Compile(`
local delta = -7
if delta < 0 then
	delta = -delta
end
return delta
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.blockPlans) == 0 {
		t.Fatalf("compiled absolute-delta program has no block plans:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	proto.blockPlans = nil

	err = verifyProto(proto)
	if err == nil {
		t.Fatal("verifyProto succeeded, want stale block plan error")
	}
	if !strings.Contains(err.Error(), "block plans [] do not match finalized plan") {
		t.Fatalf("verifyProto error is %q, want block plan detail", err)
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
	for _, key := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "target"} {
		table.setRawStringField(key, NumberValue(float64(len(key))))
	}
	if table.stringFieldMap == nil {
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

	table.setRawStringField("a", NumberValue(100))
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
	for _, key := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "target"} {
		table.setRawStringField(key, NumberValue(float64(len(key))))
	}
	if table.stringFieldMap == nil {
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

func TestTableFieldCallCacheRetainsFourHandlerKeys(t *testing.T) {
	handlers := NewTable()
	closures := make([]*closure, 4)
	for index, key := range []string{"score", "heal", "buff", "log"} {
		closures[index] = &closure{proto: &Proto{}}
		handlers.setRawStringField(key, functionValue(closures[index].proto, nil))
	}

	var cache tableFieldCallCache
	for index, key := range []string{"score", "heal", "buff", "log"} {
		cache.store(handlers, key, closures[index])
	}
	for index, key := range []string{"score", "heal", "buff", "log"} {
		closure, ok := cache.get(handlers, key)
		if !ok {
			t.Fatalf("cache.get(%s) missed, want handler PIC hit", key)
		}
		if closure != closures[index] {
			t.Fatalf("cache.get(%s) returned %#v, want %#v", key, closure, closures[index])
		}
	}
}

func TestTableFieldCallCacheEvictsAndRejectsStaleHandlerValues(t *testing.T) {
	handlers := NewTable()
	keys := []string{"score", "heal", "buff", "log", "spawn"}
	closures := make([]*closure, len(keys))
	for index, key := range keys {
		closures[index] = &closure{proto: &Proto{}}
		handlers.setRawStringField(key, functionValue(closures[index].proto, nil))
	}

	var cache tableFieldCallCache
	for index, key := range keys[:4] {
		cache.store(handlers, key, closures[index])
	}
	cache.store(handlers, "spawn", closures[4])
	if _, ok := cache.get(handlers, "score"); ok {
		t.Fatal("cache.get(score) hit after fifth handler, want oldest entry evicted")
	}
	gotClosure, ok := cache.get(handlers, "spawn")
	if !ok {
		t.Fatal("cache.get(spawn) missed, want newest handler entry")
	}
	if gotClosure != closures[4] {
		t.Fatalf("cache.get(spawn) returned %#v, want %#v", gotClosure, closures[4])
	}

	updated := &closure{proto: &Proto{}}
	handlers.setRawStringField("spawn", functionValue(updated.proto, nil))
	if _, ok := cache.get(handlers, "spawn"); ok {
		t.Fatal("cache.get(spawn) hit after handler mutation, want stale value token rejected")
	}
}

func TestTableFieldCallCacheCountsHitsAndMisses(t *testing.T) {
	handlers := NewTable()
	closures := make([]*closure, 2)
	for index, key := range []string{"score", "heal"} {
		closures[index] = &closure{proto: &Proto{}}
		handlers.setRawStringField(key, functionValue(closures[index].proto, nil))
	}

	var cache tableFieldCallCache
	for index, key := range []string{"score", "heal"} {
		cache.store(handlers, key, closures[index])
	}

	var counts directFramePICCounts
	if _, ok := cache.getCounted(handlers, "score", &counts); !ok {
		t.Fatal("cache.getCounted(score) missed, want monomorphic hit")
	}
	if _, ok := cache.getCounted(handlers, "heal", &counts); !ok {
		t.Fatal("cache.getCounted(heal) missed, want polymorphic hit")
	}
	if _, ok := cache.getCounted(handlers, "missing", &counts); ok {
		t.Fatal("cache.getCounted(missing) hit, want key miss")
	}
	updated := &closure{proto: &Proto{}}
	handlers.setRawStringField("heal", functionValue(updated.proto, nil))
	if _, ok := cache.getCounted(handlers, "heal", &counts); ok {
		t.Fatal("cache.getCounted(heal) hit after handler mutation, want shape miss")
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

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
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

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
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
		"direct_leaf_call_one false",
		"captured_locals r1",
		"entry_nil none",
		"direct_frame_rejection prototype has captured locals",
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
	if got, want := proto.registers, 5; got != want {
		t.Fatalf("compiled numeric for uses %d registers, want %d", got, want)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, oldCoercion := range []string{"ADD r1 r1 r4", "ADD r2 r2 r4", "ADD r3 r3 r4"} {
		if strings.Contains(joined, oldCoercion) {
			t.Fatalf("compiled numeric for kept register-form zero coercion %q:\n%s", oldCoercion, joined)
		}
	}
	if !strings.Contains(joined, "ADD_K") {
		t.Fatalf("compiled numeric for did not use constant-form coercions:\n%s", joined)
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
	builder.emit(instruction{op: opSetGlobal, a: name, b: 0})
	builder.emit(instruction{op: opReturnOne, a: 0})
	proto := builder.proto(nil, 2, 0, false)

	rejection, ok := protoDirectFrameRejection(proto)
	if !ok {
		t.Fatal("protoDirectFrameRejection reported no blocker, want SET_GLOBAL blocker")
	}
	if rejection.pc != 0 || rejection.op != opSetGlobal {
		t.Fatalf("rejection = pc %d op %v, want pc 0 SET_GLOBAL", rejection.pc, rejection.op)
	}
	if !strings.Contains(rejection.reason, "global writes require generic frame environment semantics") {
		t.Fatalf("rejection reason is %q, want SET_GLOBAL unsupported reason detail", rejection.reason)
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
	if !proto.directFrameDispatch {
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
	if !proto.directFrameDispatch {
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
	if !proto.directFrameDispatch {
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
	if counts.invalidKeyFallbacks != 1 {
		t.Fatalf("invalidKeyFallbacks = %d, want 1", counts.invalidKeyFallbacks)
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
	if !proto.directFrameDispatch {
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
	if !proto.directFrameDispatch {
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
local value = 1
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
	if !proto.directFrameDispatch {
		t.Fatalf("compiled newindex island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	backing := NewTable()
	metatable := NewTable()
	metatable.setRawStringField("__newindex", TableValue(backing))
	proxy := NewTable()
	proxy.setMetatable(metatable)

	var counts directFrameOpcodeCounts
	thread := newVMThread(runtimeGlobals(map[string]Value{"proxy": TableValue(proxy)}))
	thread.directFrameOpcodeCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 3 {
		t.Fatalf("thread.run result is %v (%t), want number 3", got, ok)
	}
	if value, ok := backing.rawStringField("value"); !ok || value.number != 4 {
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
	if !proto.directFrameDispatch {
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
local value = 1
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
	if !proto.directFrameDispatch {
		t.Fatalf("compiled dynamic newindex island program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	backing := NewTable()
	metatable := NewTable()
	metatable.setRawStringField("__newindex", TableValue(backing))
	proxy := NewTable()
	proxy.setMetatable(metatable)

	var counts directFrameOpcodeCounts
	thread := newVMThread(runtimeGlobals(map[string]Value{"proxy": TableValue(proxy)}))
	thread.directFrameOpcodeCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 3 {
		t.Fatalf("thread.run result is %v (%t), want number 3", got, ok)
	}
	if value, ok := backing.rawStringField("value"); !ok || value.number != 4 {
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
	if !proto.directFrameDispatch {
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
	intrinsicThread.directFramePICCounts = &intrinsicCounts
	if _, err := intrinsicThread.run(intrinsicProto, nil, nil); err != nil {
		t.Fatalf("intrinsic thread.run returned error: %v", err)
	}
	if got := intrinsicCounts.sideExitCount(directFrameSideExitReasonIntrinsic); got == 0 {
		t.Fatalf("intrinsic side exits = %d, want at least one", got)
	}
}

func TestRunDirectFrameSideExitCountersRecordDebugAndBudgetBlocks(t *testing.T) {
	proto, err := Compile(`return 1`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled block counter program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	var debugCounts directFramePICCounts
	debugThread := newVMThread(runtimeGlobals(nil))
	debugThread.directFramePICCounts = &debugCounts
	debugThread.debugHook = func(_ *globalEnv, _ vmDebugEvent) error { return nil }
	if _, err := debugThread.run(proto, nil, nil); err != nil {
		t.Fatalf("debug thread.run returned error: %v", err)
	}
	if got := debugCounts.sideExitCount(directFrameSideExitReasonDebug); got == 0 {
		t.Fatalf("debug side exits = %d, want at least one", got)
	}

	var budgetCounts directFramePICCounts
	budgetThread := newVMThread(runtimeGlobals(nil))
	budgetThread.directFramePICCounts = &budgetCounts
	budgetThread.instructionBudget = 10
	if _, err := budgetThread.run(proto, nil, nil); err != nil {
		t.Fatalf("budget thread.run returned error: %v", err)
	}
	if got := budgetCounts.sideExitCount(directFrameSideExitReasonBudget); got == 0 {
		t.Fatalf("budget side exits = %d, want at least one", got)
	}
}

func TestRunDirectFrameNestedStringFieldPathsPreserveValues(t *testing.T) {
	proto, err := Compile(`
local player = {
	stats = {hp = 10, shield = 3},
	bonus = {hp = 2},
	incoming = {hp = 4},
}
local before = player.stats.hp
player.stats.hp = player.stats.hp + player.bonus.hp - player.incoming.hp
return before, player.stats.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"GET_STRING_FIELD2", "ADD_SUB_STRING_FIELD2"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled nested field program is missing %s:\n%s", want, joined)
		}
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled nested field program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 10 {
		t.Fatalf("first result is %v (%t), want number 10", got, ok)
	}
	if got, ok := results[1].Number(); !ok || got != 8 {
		t.Fatalf("second result is %v (%t), want number 8", got, ok)
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
	if !proto.directFrameDispatch {
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
	if !proto.directFrameDispatch {
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
	if !strings.Contains(joined, "LOAD_GLOBAL") || !strings.Contains(joined, "CALL") {
		t.Fatalf("compiled rawlen program is missing global call shape:\n%s", joined)
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled rawlen program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
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
	if !proto.directFrameDispatch {
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

func TestCompilerPropagatesRowSlotsThroughLocalArrayIndex(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{hp = 10, alive = true},
	{hp = 4, alive = false},
}
local indexes = {1, 2}
local score = 0
for _, index in indexes do
	local row = rows[index]
	if row.alive then
		score = score + row.hp
	else
		score = score - row.hp
	end
end
return score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "JUMP_IF_STRING_FIELD_FALSE") {
		t.Fatalf("compiled indexed row program is missing row truthy branch:\n%s", joined)
	}
	if !strings.Contains(joined, "GET_ROW_STRING_FIELD") {
		t.Fatalf("compiled indexed row program is missing row slot read:\n%s", joined)
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled indexed row program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
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

func TestRunRowStringFieldReadFallsBackAfterShapeChange(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{drop = 1, keep = 7},
}
local row = rows[1]
row.drop = nil
return row.keep
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "GET_ROW_STRING_FIELD") {
		t.Fatalf("compiled stale row slot program is missing row slot read:\n%s", joined)
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled stale row slot program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
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
	if !proto.directFrameDispatch {
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
local left = "apple"
local right = "pear"
if left < right then
	return 7
end
return 0
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
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
	if !proto.directFrameDispatch {
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

func TestCompilerRecordsMaxReductionFacts(t *testing.T) {
	proto, err := Compile(`
local scores = {3, 8, 5, 12}
local best = -999
local bestIndex = 0
for i, score in scores do
	if score > best then
		best = score
		bestIndex = i
	end
end
return best, bestIndex
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"reduction",
		"kind max",
		"accumulator r",
		"candidate r",
		"predicate pc",
		"mutation pc",
		"mutations 2",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled reduction program is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Run returned %d results, want 2", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 12 {
		t.Fatalf("first result is %v (%t), want number 12", got, ok)
	}
	if got, ok := results[1].Number(); !ok || got != 4 {
		t.Fatalf("second result is %v (%t), want number 4", got, ok)
	}
}

func TestCompilerRecordsAllCompleteReductionFacts(t *testing.T) {
	proto, err := Compile(`
local objectives = {
	{have = 1, need = 1},
	{have = 1, need = 2},
}
local complete = true
for _, objective in objectives do
	if objective.have < objective.need then
		complete = false
	end
end
return complete
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"reduction",
		"kind all_complete",
		"accumulator r",
		"predicate pc",
		"mutation pc",
		"mutations 1",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled all-complete reduction program is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Bool(); !ok || got {
		t.Fatalf("result is %v (%t), want false", results[0], ok)
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

func TestCompilerRecordsAbsoluteDeltaReductionFacts(t *testing.T) {
	proto, err := Compile(`
local before = {hp = 10}
local after = {hp = 17}
local delta = before.hp - after.hp
if delta < 0 then
	delta = -delta
end
return delta
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"reduction",
		"kind absolute_delta",
		"accumulator r",
		"predicate pc",
		"mutation pc",
		"mutations 1",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled absolute-delta program is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 7 {
		t.Fatalf("result is %v (%t), want number 7", results[0], ok)
	}
}

func TestRunDirectFrameUsesAbsoluteDeltaBlockPlan(t *testing.T) {
	proto, err := Compile(`
local delta = -7
if delta < 0 then
	delta = -delta
end
return delta
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"direct_block_plan",
		"kind absolute_delta",
		"start pc",
		"resume pc",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled absolute-delta program is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled absolute-delta program is not direct-frame eligible:\n%s", facts)
	}

	var counts directFrameOpcodeCounts
	var picCounts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &counts
	thread.directFramePICCounts = &picCounts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("thread.run returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 7 {
		t.Fatalf("result is %v (%t), want number 7", results[0], ok)
	}
	if counts.count(opJumpIfNotLessK) == 0 {
		t.Fatal("direct-frame JUMP_IF_NOT_LESS_K count is 0, want block plan entry counted")
	}
	if got := counts.count(opNeg); got != 0 {
		t.Fatalf("direct-frame NEG count is %d, want absolute-delta block plan to skip NEG dispatch", got)
	}
	if got := counts.count(opJump); got != 0 {
		t.Fatalf("direct-frame JUMP count is %d, want absolute-delta block plan to skip trailing JUMP dispatch", got)
	}
}

func TestCompilerRecordsTypedBlockPlanForAbsoluteDelta(t *testing.T) {
	proto, err := Compile(`
local delta = -7
if delta < 0 then
	delta = -delta
end
return delta
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"block_plan",
		"family absolute_delta",
		"start pc",
		"resume pc",
		"fallback pc",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled absolute-delta program is missing typed block plan %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}
}

func TestCompilerRecordsDynamicPathAddStoreBlockPlan(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 10}}
local key = "value"
local delta = 3
for i = 1, 6 do
	row.child[key] = row.child[key] + delta
end
return row.child[key]
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"block_plan",
		"family dynamic_path_add_store",
		"field child dynamic_key",
		"op ADD",
		"resume pc",
		"fallback pc",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled dynamic path update is missing block plan %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}
}

func TestCompilerRecordsRowFieldAddFieldStoreBlockPlan(t *testing.T) {
	proto, err := Compile(`
local actor = {energy = 30, haste = 1}
for i = 1, 4 do
	actor.energy = actor.energy + 2 + actor.haste
end
return actor.energy
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"block_plan",
		"family row_field_add_field_store",
		"field energy",
		"add_field haste",
		"op ADD",
		"resume pc",
		"fallback pc",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled row field add-field update is missing block plan %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}
}

func TestRunDirectFrameUsesRowFieldAddFieldStoreBlockPlan(t *testing.T) {
	proto, err := Compile(`
local actor = {energy = 30, haste = 1}
for i = 1, 4 do
	actor.energy = actor.energy + 2 + actor.haste
end
return actor.energy
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "family row_field_add_field_store") {
		t.Fatalf("compiled row field add-field update is missing block plan:\n%s\nbytecode:\n%s", facts, strings.Join(disassembleProto(proto), "\n"))
	}

	var counts directFrameOpcodeCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 42 {
		t.Fatalf("thread.run result is %v (%t), want 42", got, ok)
	}
	if got := counts.count(opSetRowStringField); got != 0 {
		t.Fatalf("SET_ROW_STRING_FIELD dispatch count = %d, want row field add-field block to skip stores", got)
	}
}

func TestRowFieldAddFieldStoreBlockPlanFallsBackForStringNumberField(t *testing.T) {
	proto, err := Compile(`
local actor = {energy = 30, haste = "1"}
for i = 1, 4 do
	actor.energy = actor.energy + 2 + actor.haste
end
return actor.energy
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "family row_field_add_field_store") {
		t.Fatalf("compiled row field add-field update is missing block plan:\n%s\nbytecode:\n%s", facts, strings.Join(disassembleProto(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 42 {
		t.Fatalf("Run result is %v (%t), want 42 from string-number fallback", got, ok)
	}
}

func TestRunDirectFrameUsesDynamicPathAddStoreBlockPlan(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 10}}
local key = "value"
local delta = 3
for i = 1, 6 do
	row.child[key] = row.child[key] + delta
end
return row.child[key]
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "family dynamic_path_add_store") {
		t.Fatalf("compiled dynamic path update is missing block plan:\n%s\nbytecode:\n%s", facts, strings.Join(disassembleProto(proto), "\n"))
	}

	var counts directFrameOpcodeCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 28 {
		t.Fatalf("thread.run result is %v (%t), want 28", got, ok)
	}
	if got := counts.count(opSetStringFieldIndex); got != 0 {
		t.Fatalf("SET_STRING_FIELD_INDEX dispatch count = %d, want dynamic path block to skip stores", got)
	}
}

func TestDynamicPathAddStoreBlockPlanFallsBackForMetatable(t *testing.T) {
	proto, err := Compile(`
local log = {value = 0}
local child = {}
setmetatable(child, {
	__index = function(_, key)
		if key == "value" then
			return 10
		end
		return 0
	end,
	__newindex = function(_, key, value)
		if key == "value" then
			log.value = value
		end
	end,
})
local row = {child = child}
local key = "value"
local delta = 3
for i = 1, 2 do
	row.child[key] = row.child[key] + delta
end
return log.value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "family dynamic_path_add_store") {
		t.Fatalf("compiled dynamic path update is missing block plan:\n%s\nbytecode:\n%s", facts, strings.Join(disassembleProto(proto), "\n"))
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 13 {
		t.Fatalf("Run result is %v (%t), want 13 from metatable fallback", got, ok)
	}
}

func TestRunDirectFrameVerifiedPlansArePICOptIn(t *testing.T) {
	proto, err := Compile(`
local delta = -7
if delta < 0 then
	delta = -delta
end
return delta
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "direct_block_plan") || !strings.Contains(facts, "kind absolute_delta") {
		t.Fatalf("compiled absolute-delta program is missing direct block plan:\n%s\nbytecode:\n%s", facts, strings.Join(disassembleProto(proto), "\n"))
	}

	var counts directFrameOpcodeCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 7 {
		t.Fatalf("result is %v (%t), want number 7", results[0], ok)
	}
	if got := counts.count(opNeg); got == 0 {
		t.Fatalf("direct-frame NEG count is %d, want ordinary dispatch when PIC counters are disabled", got)
	}
}

func TestRunDirectFrameAbsoluteDeltaBlockPlanResumesAfterSkippedMutation(t *testing.T) {
	proto, err := Compile(`
local delta = 7
if delta < 0 then
	delta = -delta
end
return delta + 1
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "direct_block_plan") || !strings.Contains(facts, "kind absolute_delta") {
		t.Fatalf("compiled positive absolute-delta program is missing direct block plan:\n%s\nbytecode:\n%s", facts, strings.Join(disassembleProto(proto), "\n"))
	}

	var counts directFrameOpcodeCounts
	var picCounts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &counts
	thread.directFramePICCounts = &picCounts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 8 {
		t.Fatalf("result is %v (%t), want number 8", results[0], ok)
	}
	if got := counts.count(opNeg); got != 0 {
		t.Fatalf("direct-frame NEG count is %d, want skipped mutation path to bypass NEG", got)
	}
	if counts.count(opAddK) == 0 {
		t.Fatal("direct-frame ADD_K count is 0, want block plan to resume at following bytecode")
	}
}

func TestRunDirectFrameUsesMaxReductionBlockPlan(t *testing.T) {
	proto, err := Compile(`
local best = 1
local score = 3
if score > best then
	best = score
end
return best + 1
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"direct_block_plan",
		"kind max",
		"start pc",
		"resume pc",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled max reduction program is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled max reduction program is not direct-frame eligible:\n%s", facts)
	}

	var counts directFrameOpcodeCounts
	var picCounts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &counts
	thread.directFramePICCounts = &picCounts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 4 {
		t.Fatalf("result is %v (%t), want number 4", results[0], ok)
	}
	if counts.count(opJumpIfNotGreater) == 0 {
		t.Fatal("direct-frame JUMP_IF_NOT_GREATER count is 0, want block plan entry counted")
	}
	if got := counts.count(opMove); got != 1 {
		t.Fatalf("direct-frame MOVE count is %d, want only post-block result move to dispatch", got)
	}
	if got := counts.count(opJump); got != 0 {
		t.Fatalf("direct-frame JUMP count is %d, want max block plan to skip trailing JUMP dispatch", got)
	}
	if counts.count(opAddK) == 0 {
		t.Fatal("direct-frame ADD_K count is 0, want max block plan to resume at following bytecode")
	}
}

func TestRunDirectFrameMaxReductionBlockPlanResumesAfterSkippedMutation(t *testing.T) {
	proto, err := Compile(`
local best = 5
local score = 3
if score > best then
	best = score
end
return best + 1
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "direct_block_plan") || !strings.Contains(facts, "kind max") {
		t.Fatalf("compiled skipped max program is missing direct block plan:\n%s\nbytecode:\n%s", facts, strings.Join(disassembleProto(proto), "\n"))
	}

	var counts directFrameOpcodeCounts
	var picCounts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &counts
	thread.directFramePICCounts = &picCounts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 6 {
		t.Fatalf("result is %v (%t), want number 6", results[0], ok)
	}
	if got := counts.count(opMove); got != 1 {
		t.Fatalf("direct-frame MOVE count is %d, want only post-block result move to dispatch", got)
	}
	if got := counts.count(opJump); got != 0 {
		t.Fatalf("direct-frame JUMP count is %d, want skipped max path to bypass trailing JUMP", got)
	}
	if counts.count(opAddK) == 0 {
		t.Fatal("direct-frame ADD_K count is 0, want max block plan to resume at following bytecode")
	}
}

func TestCompilerRecordsPairedRowDiffReductionFacts(t *testing.T) {
	proto, err := Compile(`
local before = {
	{hp = 10},
	{hp = 20},
}
local after = {
	{hp = 13},
	{hp = 12},
}
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
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"reduction",
		"kind paired_row_diff",
		"accumulator r",
		"candidate r",
		"predicate pc",
		"mutation pc",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled paired-row diff program is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 11 {
		t.Fatalf("result is %v (%t), want number 11", results[0], ok)
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

func TestRunDirectFrameUsesPairedRowDiffBlockPlan(t *testing.T) {
	proto, err := Compile(`
local before = {
	{hp = 10},
	{hp = 20},
}
local after = {
	{hp = 13},
	{hp = 12},
}
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
	joined := strings.Join(disassembleProto(proto), "\n")
	hasPairedRowBlockPlan := false
	for _, line := range strings.Split(facts, "\n") {
		if strings.Contains(line, "direct_block_plan") && strings.Contains(line, "kind paired_row_diff") {
			hasPairedRowBlockPlan = true
			break
		}
	}
	if !hasPairedRowBlockPlan {
		t.Fatalf("compiled paired-row diff program is missing paired-row direct block plan:\n%s\nbytecode:\n%s", facts, joined)
	}

	var counts directFrameOpcodeCounts
	var picCounts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &counts
	thread.directFramePICCounts = &picCounts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 11 {
		t.Fatalf("result is %v (%t), want number 11", results[0], ok)
	}
	if counts.count(opGetIndex) == 0 {
		t.Fatal("direct-frame GET_INDEX count is 0, want paired-row block plan entry counted")
	}
	if got := counts.count(opGetRowStringField); got != 0 {
		t.Fatalf("direct-frame GET_ROW_STRING_FIELD count is %d, want paired-row block plan to skip row field dispatch", got)
	}
	if got := counts.count(opSub); got != 0 {
		t.Fatalf("direct-frame SUB count is %d, want paired-row block plan to skip subtraction dispatch", got)
	}
}

func TestRunDirectFrameUsesRowFieldAddStoreBlockPlan(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{hp = 10},
	{hp = 20},
}
for _, row in rows do
	row.hp = row.hp + 3
end
return rows[1].hp + rows[2].hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	hasRowFieldAddStoreBlockPlan := false
	for _, line := range strings.Split(facts, "\n") {
		if strings.Contains(line, "direct_block_plan") && strings.Contains(line, "kind row_field_add_store") {
			hasRowFieldAddStoreBlockPlan = true
			break
		}
	}
	if !hasRowFieldAddStoreBlockPlan {
		t.Fatalf("compiled row field add-store program is missing row-field direct block plan:\n%s\nbytecode:\n%s", facts, joined)
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled row field add-store program is not direct-frame eligible:\n%s", facts)
	}

	var counts directFrameOpcodeCounts
	var picCounts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &counts
	thread.directFramePICCounts = &picCounts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 36 {
		t.Fatalf("result is %v (%t), want number 36", results[0], ok)
	}
	if counts.count(opAddStringField) == 0 {
		t.Fatal("direct-frame ADD_STRING_FIELD count is 0, want row-field block plan entry counted")
	}
	if got := counts.count(opAddK); got != 0 {
		t.Fatalf("direct-frame ADD_K count is %d, want row-field block plan to skip numeric dispatch", got)
	}
	if got := counts.count(opSetRowStringField); got != 0 {
		t.Fatalf("direct-frame SET_ROW_STRING_FIELD count is %d, want row-field block plan to skip store dispatch", got)
	}
}

func TestRunDirectFrameDirectBlockPlanCounters(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{hp = 10},
	{hp = 20},
}
for _, row in rows do
	row.hp = row.hp + 3
end
return rows[1].hp + rows[2].hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "kind row_field_add_store") {
		t.Fatalf("compiled row field add-store program is missing direct block plan:\n%s\nbytecode:\n%s", facts, strings.Join(disassembleProto(proto), "\n"))
	}
	verified, ok := proto.verifiedPlanAt(proto.directBlockPlans[0].pc)
	if !ok {
		t.Fatalf("verified plan shell missing at direct block pc %d", proto.directBlockPlans[0].pc)
	}
	if verified.kind != verifiedPlanKindDirectBlock {
		t.Fatalf("verified plan kind = %v, want direct block", verified.kind)
	}
	if verified.directBlock.kind != "row_field_add_store" {
		t.Fatalf("verified direct block kind = %q, want row_field_add_store", verified.directBlock.kind)
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 36 {
		t.Fatalf("result is %v (%t), want number 36", results[0], ok)
	}
	if got := counts.directBlockEntries; got != 2 {
		t.Fatalf("direct block entries = %d, want 2", got)
	}
	if got := counts.directBlockResumes; got != 2 {
		t.Fatalf("direct block resumes = %d, want 2", got)
	}
	if got := counts.directBlockFallbacks; got != 0 {
		t.Fatalf("direct block fallbacks = %d, want 0", got)
	}
}

func TestRunDirectFrameDirectBlockPlanCountersRecordFallbackReason(t *testing.T) {
	proto, err := Compile(`
local row = {hp = 10}
row.hp = row.hp + "3"
return row.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if !strings.Contains(facts, "kind row_field_add_store") {
		t.Fatalf("compiled numeric-string row add-store program is missing direct block plan:\n%s\nbytecode:\n%s", facts, strings.Join(disassembleProto(proto), "\n"))
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 13 {
		t.Fatalf("result is %v (%t), want number 13", results[0], ok)
	}
	if got := counts.directBlockEntries; got != 1 {
		t.Fatalf("direct block entries = %d, want 1", got)
	}
	if got := counts.directBlockResumes; got != 0 {
		t.Fatalf("direct block resumes = %d, want 0", got)
	}
	if got := counts.directBlockFallbacks; got != 1 {
		t.Fatalf("direct block fallbacks = %d, want 1", got)
	}
	if got := counts.directBlockSideExitCount(directFrameSideExitReasonGenericFrame); got != 1 {
		t.Fatalf("direct block generic fallbacks = %d, want 1", got)
	}
}

func TestExecuteVerifiedPlanFallbackPreservesPCAndRegisters(t *testing.T) {
	proto, err := Compile(`
local row = {hp = 10}
row.hp = row.hp + "3"
return row.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	verified, ok := proto.verifiedPlanAt(proto.directBlockPlans[0].pc)
	if !ok {
		t.Fatalf("verified plan shell missing at direct block pc %d", proto.directBlockPlans[0].pc)
	}
	plan := verified.directBlock
	frame := newVMFrame(proto, nil, nil)
	frame.pc = plan.startPC
	row := NewTable()
	row.setRawStringField("hp", NumberValue(10))
	frame.registers[plan.register] = TableValue(row)
	frame.registers[plan.candidate] = StringValue("3")

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFramePICCounts = &counts
	exit := thread.executeVerifiedPlan(frame, verified)

	if exit.kind != directFrameSideExitGenericFrame || exit.reason != directFrameSideExitReasonGenericFrame {
		t.Fatalf("verified plan exit = kind %d reason %d, want generic fallback", exit.kind, exit.reason)
	}
	if frame.pc != plan.startPC {
		t.Fatalf("frame pc after fallback = %d, want plan start %d", frame.pc, plan.startPC)
	}
	value, ok := row.rawStringField("hp")
	if !ok {
		t.Fatal("row hp missing after fallback")
	}
	if got, ok := value.Number(); !ok || got != 10 {
		t.Fatalf("row hp after fallback is %v (%t), want number 10", value, ok)
	}
	if got := counts.directBlockEntries; got != 1 {
		t.Fatalf("direct block entries = %d, want 1", got)
	}
	if got := counts.directBlockFallbacks; got != 1 {
		t.Fatalf("direct block fallbacks = %d, want 1", got)
	}
}

func TestRunDirectFrameUsesRowFieldBranchStoreBlockPlan(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{hp = 12},
	{hp = 8},
}
for _, row in rows do
	if row.hp > 10 then
		row.hp = 10
	end
end
return rows[1].hp + rows[2].hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	hasRowFieldBranchStoreBlockPlan := false
	for _, line := range strings.Split(facts, "\n") {
		if strings.Contains(line, "direct_block_plan") && strings.Contains(line, "kind row_field_branch_store") {
			hasRowFieldBranchStoreBlockPlan = true
			break
		}
	}
	if !hasRowFieldBranchStoreBlockPlan {
		t.Fatalf("compiled row field branch-store program is missing row-field branch direct block plan:\n%s\nbytecode:\n%s", facts, joined)
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled row field branch-store program is not direct-frame eligible:\n%s", facts)
	}

	var counts directFrameOpcodeCounts
	var picCounts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &counts
	thread.directFramePICCounts = &picCounts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 18 {
		t.Fatalf("result is %v (%t), want number 18", results[0], ok)
	}
	if counts.count(opJumpIfRowStringFieldNotGreaterK) == 0 {
		t.Fatal("direct-frame JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_K count is 0, want row-field block plan entry counted")
	}
	if got := counts.count(opSetRowStringField); got != 0 {
		t.Fatalf("direct-frame SET_ROW_STRING_FIELD count is %d, want row-field branch block plan to skip store dispatch", got)
	}
}

func TestRunDirectFrameUsesRowFieldBranchArithmeticStoreBlockPlan(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{hp = 15},
	{hp = 8},
}
for _, row in rows do
	if row.hp > 10 then
		row.hp = row.hp - 2
	end
end
return rows[1].hp + rows[2].hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	hasRowFieldBranchStoreBlockPlan := false
	for _, line := range strings.Split(facts, "\n") {
		if strings.Contains(line, "direct_block_plan") && strings.Contains(line, "kind row_field_branch_store") {
			hasRowFieldBranchStoreBlockPlan = true
			break
		}
	}
	if !hasRowFieldBranchStoreBlockPlan {
		t.Fatalf("compiled row field branch arithmetic-store program is missing row-field branch direct block plan:\n%s\nbytecode:\n%s", facts, joined)
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled row field branch arithmetic-store program is not direct-frame eligible:\n%s", facts)
	}

	var counts directFrameOpcodeCounts
	var picCounts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &counts
	thread.directFramePICCounts = &picCounts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 21 {
		t.Fatalf("result is %v (%t), want number 21", results[0], ok)
	}
	if counts.count(opJumpIfRowStringFieldNotGreaterK) == 0 {
		t.Fatal("direct-frame JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_K count is 0, want row-field block plan entry counted")
	}
	if got := counts.count(opSubStringField); got != 0 {
		t.Fatalf("direct-frame SUB_STRING_FIELD count is %d, want row-field branch block plan to skip arithmetic store dispatch", got)
	}
}

func TestRunDirectFrameUsesRowFieldBranchSubAddStoreBlockPlan(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{hp = 12, regen = 3},
	{hp = 8, regen = 5},
}
local incoming = 2
for _, row in rows do
	if row.hp > 10 then
		row.hp = row.hp - incoming + row.regen
	end
end
return rows[1].hp + rows[2].hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	hasRowFieldBranchStoreBlockPlan := false
	for _, line := range strings.Split(facts, "\n") {
		if strings.Contains(line, "direct_block_plan") && strings.Contains(line, "kind row_field_branch_store") {
			hasRowFieldBranchStoreBlockPlan = true
			break
		}
	}
	if !hasRowFieldBranchStoreBlockPlan {
		t.Fatalf("compiled row field branch sub-add program is missing row-field branch direct block plan:\n%s\nbytecode:\n%s", facts, joined)
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled row field branch sub-add program is not direct-frame eligible:\n%s", facts)
	}

	var counts directFrameOpcodeCounts
	var picCounts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFrameOpcodeCounts = &counts
	thread.directFramePICCounts = &picCounts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 21 {
		t.Fatalf("result is %v (%t), want number 21", results[0], ok)
	}
	if counts.count(opJumpIfRowStringFieldNotGreaterK) == 0 {
		t.Fatal("direct-frame JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_K count is 0, want row-field block plan entry counted")
	}
	if got := counts.count(opSubAddStringField); got != 0 {
		t.Fatalf("direct-frame SUB_ADD_STRING_FIELD count is %d, want row-field branch block plan to skip sub-add store dispatch", got)
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

func TestCompilerUsesRowStringFieldStoreOpcode(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{hp = 10, shield = 4},
	{hp = 20, shield = 8},
}
for _, row in rows do
	row.hp = row.shield
end
return rows[1].hp + rows[2].hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "SET_ROW_STRING_FIELD") {
		t.Fatalf("compiled row field write is missing SET_ROW_STRING_FIELD:\n%s", joined)
	}
	if !strings.Contains(joined, "slot 0") {
		t.Fatalf("compiled row field write is missing propagated slot:\n%s", joined)
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

func TestRunRowStringFieldStoreFallsBackToNewIndexAfterDelete(t *testing.T) {
	proto, err := Compile(`
local backing = {hp = 0}
local row = {hp = 10}
setmetatable(row, {__newindex = backing, __index = backing})
row.hp = nil
row.hp = 7
return row.hp, backing.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "SET_ROW_STRING_FIELD") {
		t.Fatalf("compiled row field write is missing SET_ROW_STRING_FIELD:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	for i, result := range results {
		got, ok := result.Number()
		if !ok || got != 7 {
			t.Fatalf("result %d is %v (%t), want number 7", i, result, ok)
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
	if len(proto.intrinsicOps) != 2 {
		t.Fatalf("intrinsic descriptor count = %d, want 2:\n%s", len(proto.intrinsicOps), strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	var tableInsert intrinsicOpDesc
	var mathMin intrinsicOpDesc
	for _, desc := range proto.intrinsicOps {
		switch desc.op {
		case opTableInsert:
			tableInsert = desc
		case opMathMin:
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
	proto, err := Compile(`
local n = 4
local s = "kind"
local b = n < 5
local t = {}
return n, s, b, t
`)
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
	proto, err := Compile(`
local left = 4
local right = 2
local sum = left + right
local scaled = sum * 3
local small = scaled < 20
return sum, scaled, small
`)
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

func TestCompilerRecordsBranchAndFiniteTagRefinements(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{kind = "poison", alive = true, key = "a", score = 3},
	{kind = "regen", alive = false, score = 5},
	{kind = "shield", alive = true, key = "c", score = 7},
}
local total = 0
for _, row in rows do
	if row.kind == "poison" then
		total = total + 1
	elseif row.kind == "regen" then
		total = total + 2
	elseif row.kind == "shield" then
		total = total + 3
	end
	if row.key ~= nil and row.alive then
		total = total + row.score
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"branch_refinement",
		"edge fallthrough",
		"edge target",
		"fact equal_const",
		"fact not_equal_const",
		"fact not_nil",
		"fact truthy",
		"finite_tag_refinement",
		"source register",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled refinement program is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 16 {
		t.Fatalf("Run result is %v (%t), want 16", got, ok)
	}
}

func TestCompilerRecordsPredicateBranchDescriptors(t *testing.T) {
	proto, err := Compile(`
local row = {kind = "npc", alive = true, child = {value = 3}}
local limit = 4
local total = 0
if limit < 5 then
	total = total + 1
end
if row.kind == "npc" then
	total = total + 2
end
if row.alive then
	total = total + 4
end
local i = 0
while i < 4 do
	if row.child.value > 0 then
		total = total + 8
	end
	total = total + row.child.value
	total = total + row.child.value
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
		"predicate_branch",
		"source register",
		"source row_field",
		"source path_field",
		"op truthy",
		"op equal_const",
		"op numeric_compare",
		"field child.value",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled predicate descriptor program is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 63 {
		t.Fatalf("Run result is %v (%t), want 63", got, ok)
	}
}

func TestCompilerRecordsSlotAndPathKindFacts(t *testing.T) {
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
		"path_kind",
		"source path_parent",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled slot/path kind program is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
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

func TestCompilerRecordsLoopLocalOneSegmentPathFact(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 3}}
local i = 0
local total = 0
while i < 4 do
	local first = row.child
	local second = row.child
	total = total + first.value + second.value
	i = i + 1
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"path_fact", "field child", "hits 2"} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled repeated path is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 24 {
		t.Fatalf("Run result is %v (%t), want 24", got, ok)
	}
}

func TestCompilerRecordsLoopLocalTwoSegmentFieldPathFact(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 3}}
local i = 0
local total = 0
while i < 4 do
	total = total + row.child.value
	total = total + row.child.value
	i = i + 1
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"path_fact", "field child.value", "hits 2", "birth pc", "backedge pc", "kill none"} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled repeated two-segment path is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 24 {
		t.Fatalf("Run result is %v (%t), want 24", got, ok)
	}
}

func TestCompilerRecordsReadPathPlanForLoopLocalTwoSegmentFieldPath(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 3}}
local i = 0
local total = 0
while i < 4 do
	total = total + row.child.value
	total = total + row.child.value
	i = i + 1
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"path_plan", "access read", "base r0", "field child.value", "fallback pc"} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled repeated path is missing path plan %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}
}

func TestCompilerRecordsWritePathPlanForTwoSegmentFieldPath(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 3}}
row.child.value = 4
return row.child.value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"path_plan", "access write", "base r0", "field child.value", "fallback pc"} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled path write is missing path plan %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}
}

func TestCompilerRecordsDynamicWritePathPlanForTwoSegmentFieldPath(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 3}}
local key = "value"
row.child[key] = 4
return row.child[key]
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"path_plan", "access write", "field child dynamic_key", "key r", "value r", "fallback pc"} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled dynamic path write is missing path plan %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}
}

func TestCompilerRecordsReadModifyWritePathPlanForTwoSegmentFieldPath(t *testing.T) {
	proto, err := Compile(`
local player = {stats = {hp = 100, shield = 25}, inventory = {coins = 3}}
player.stats.hp = player.stats.hp + player.stats.shield - player.inventory.coins
return player.stats.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"path_plan",
		"access read_modify_write",
		"field stats.hp",
		"access read",
		"field stats.shield",
		"field inventory.coins",
		"fallback pc",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled nested path update is missing path plan %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}
}

func TestRunDirectFrameUsesRuntimePathCacheForTwoSegmentFieldPath(t *testing.T) {
	proto, err := Compile(`
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
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.pathFacts) == 0 {
		t.Fatalf("compiled path cache program has no path facts:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled path cache program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 36 {
		t.Fatalf("thread.run result is %v (%t), want 36", got, ok)
	}
	if thread.intrinsicGuards == nil || thread.intrinsicGuards.pathHits == 0 {
		t.Fatalf("path cache hits = 0, want repeated two-segment path hits")
	}
	if counts.pathCacheStores == 0 {
		t.Fatal("path cache stores = 0, want runtime path cache store attribution")
	}
	if counts.pathCacheMisses == 0 {
		t.Fatal("path cache misses = 0, want first runtime path cache lookup miss attribution")
	}
	if counts.pathCacheHits == 0 {
		t.Fatal("path cache hits = 0, want runtime path cache hit attribution")
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
	if pathSnapshot.picCounts.pathCacheHits == 0 {
		t.Fatal("path cache hits = 0, want grouped path-cache attribution")
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

func TestCandidateRegionsReportCoverageAndProfitability(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 10}}
local key = "value"
local delta = 3
for i = 1, 6 do
	row.child[key] = row.child[key] + delta
end
return row.child[key]
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 28 {
		t.Fatalf("instrumented run result is %v (%t), want 28", results[0], ok)
	}

	report := candidateRegions(proto, snapshot)
	if report.retiredBytecodes == 0 {
		t.Fatal("region coverage report has zero retired bytecodes, want per-pc attribution")
	}
	if report.coveredBytecodes == 0 {
		t.Fatal("region coverage report has zero covered bytecodes, want current block plans reported")
	}
	candidate, ok := report.candidateByKind("dynamic_path_add_store")
	if !ok {
		t.Fatalf("candidate report missing dynamic path region: %#v", report.candidates)
	}
	if candidate.retiredBytecodes == 0 || candidate.entries == 0 {
		t.Fatalf("dynamic path candidate has retired=%d entries=%d, want observed execution counts", candidate.retiredBytecodes, candidate.entries)
	}
	if len(candidate.requiredGuards) == 0 || len(candidate.tableSlots) == 0 {
		t.Fatalf("dynamic path candidate guards=%v slots=%v, want guard and slot attribution", candidate.requiredGuards, candidate.tableSlots)
	}
	if !candidate.cost.profitable {
		t.Fatalf("dynamic path candidate cost = %#v, want profitable region", candidate.cost)
	}
}

func TestCandidateRegionsRejectTinyDirectBlockProfitability(t *testing.T) {
	proto, err := Compile(`
local delta = -7
if delta < 0 then
	delta = -delta
end
return delta
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("runWithDirectFrameMechanismCounters returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 7 {
		t.Fatalf("instrumented run result is %v (%t), want 7", results[0], ok)
	}

	report := candidateRegions(proto, snapshot)
	candidate, ok := report.candidateByKind("absolute_delta")
	if !ok {
		t.Fatalf("candidate report missing absolute-delta region: %#v", report.candidates)
	}
	if candidate.cost.profitable {
		t.Fatalf("absolute-delta cost = %#v, want tiny one-shot direct block rejected", candidate.cost)
	}
	if candidate.cost.reason == "" {
		t.Fatalf("absolute-delta cost has empty rejection reason: %#v", candidate.cost)
	}
}

func TestScenarioRegionCoverageReportsCurrentWorstRows(t *testing.T) {
	cases := loadScenarioBenchmarkCases(t, []string{
		"event_dispatch",
		"economy_market_tick",
		"cooldown_scheduler",
		"path_relaxation",
		"threat_aggro_table",
		"save_state_diff",
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
			report := candidateRegions(proto, snapshot)
			if report.retiredBytecodes == 0 {
				t.Fatal("scenario region report has zero retired bytecodes")
			}
			t.Logf("%s", summarizeRegionCoverage(report))
		})
	}
}

func TestScenarioMechanismAttributionCoversCurrentWorstRows(t *testing.T) {
	cases := loadScenarioBenchmarkCases(t, []string{
		"event_dispatch",
		"economy_market_tick",
		"cooldown_scheduler",
		"path_relaxation",
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
		"opcodes[%s] pic{hits=%d/%d keyMiss=%d shapeMiss=%d metaMiss=%d missing=%d nilWrite=%d invalid=%d arrayIndex=%d sideTable=%d sideCall=%d sideMeta=%d directBlock=%d/%d/%d path=%d/%d/%d/%d intrinsic=%d/%d/%d fixed=%d/%d/%d/%d}",
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
		pic.sideExitCount(directFrameSideExitReasonTable),
		pic.sideExitCount(directFrameSideExitReasonCall),
		pic.sideExitCount(directFrameSideExitReasonMetatable),
		pic.directBlockEntries,
		pic.directBlockResumes,
		pic.directBlockFallbacks,
		pic.pathCacheHits,
		pic.pathCacheMisses,
		pic.pathCacheStale,
		pic.pathCacheStores,
		pic.intrinsicGuardChecks,
		pic.intrinsicGuardHits,
		pic.intrinsicGuardMisses,
		pic.fixedCallFrameReuses,
		pic.fixedCallFrameMaterializations,
		pic.fixedCallArgCopies,
		pic.fixedCallRegisterCopies,
	)
}

func summarizeRegionCoverage(report regionCoverageReport) string {
	coverage := 0.0
	if report.retiredBytecodes != 0 {
		coverage = float64(report.coveredBytecodes) / float64(report.retiredBytecodes) * 100
	}
	candidates := report.candidates
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	parts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		status := "cold"
		if candidate.cost.profitable {
			status = "profitable"
		} else if candidate.cost.reason != "" {
			status = candidate.cost.reason
		}
		parts = append(parts, fmt.Sprintf(
			"%s@%d entries=%d retired=%d saved=%d %s",
			candidate.kind,
			candidate.entryPC,
			candidate.entries,
			candidate.retiredBytecodes,
			candidate.cost.expectedSavedWork,
			status,
		))
	}
	return fmt.Sprintf(
		"regions{retired=%d covered=%d coverage=%.1f%% candidates=[%s]}",
		report.retiredBytecodes,
		report.coveredBytecodes,
		coverage,
		strings.Join(parts, "; "),
	)
}

func TestCompilerRecordsLoopLocalTwoSegmentDynamicPathFact(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 3}}
local key = "value"
local i = 0
local total = 0
while i < 4 do
	total = total + row.child[key]
	total = total + row.child[key]
	i = i + 1
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{"path_fact", "field child", "dynamic_key", "hits 2"} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled repeated two-segment dynamic path is missing %q:\n%s\nbytecode:\n%s", want, facts, joined)
		}
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 24 {
		t.Fatalf("Run result is %v (%t), want 24", got, ok)
	}
}

func TestRunDirectFrameUsesRuntimePathCacheForTwoSegmentDynamicPath(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 3}}
local key = "value"
local i = 0
local total = 0
while i < 6 do
	total = total + row.child[key]
	total = total + row.child[key]
	i = i + 1
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.pathFacts) == 0 {
		t.Fatalf("compiled dynamic path cache program has no path facts:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled dynamic path cache program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 36 {
		t.Fatalf("thread.run result is %v (%t), want 36", got, ok)
	}
	if thread.intrinsicGuards == nil || thread.intrinsicGuards.pathHits == 0 {
		t.Fatalf("dynamic path cache hits = 0, want repeated two-segment dynamic path hits")
	}
	if counts.pathCacheStores == 0 {
		t.Fatal("dynamic path cache stores = 0, want runtime path cache store attribution")
	}
	if counts.pathCacheMisses == 0 {
		t.Fatal("dynamic path cache misses = 0, want first runtime path cache lookup miss attribution")
	}
	if counts.pathCacheHits == 0 {
		t.Fatal("dynamic path cache hits = 0, want runtime path cache hit attribution")
	}
}

func TestRunDirectFrameUsesRuntimePathCacheForTwoSegmentFieldPathWrite(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 0}}
local i = 0
while i < 6 do
	row.child.value = i
	i = i + 1
end
return row.child.value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled path write program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	if !strings.Contains(strings.Join(disassembleProto(proto), "\n"), "SET_STRING_FIELD2") {
		t.Fatalf("compiled path write program is missing SET_STRING_FIELD2:\n%s", strings.Join(disassembleProto(proto), "\n"))
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 5 {
		t.Fatalf("thread.run result is %v (%t), want 5", got, ok)
	}
	if counts.pathCacheStores == 0 {
		t.Fatal("path write cache stores = 0, want runtime path cache store attribution")
	}
	if counts.pathCacheHits == 0 {
		t.Fatal("path write cache hits = 0, want runtime path cache hit attribution")
	}
}

func TestRunDirectFrameUsesRuntimePathCacheForTwoSegmentDynamicPathWrite(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 0}}
local key = "value"
local i = 0
while i < 6 do
	row.child[key] = i
	i = i + 1
end
return row.child[key]
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled dynamic path write program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	if !strings.Contains(strings.Join(disassembleProto(proto), "\n"), "SET_STRING_FIELD_INDEX") {
		t.Fatalf("compiled dynamic path write program is missing SET_STRING_FIELD_INDEX:\n%s", strings.Join(disassembleProto(proto), "\n"))
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 5 {
		t.Fatalf("thread.run result is %v (%t), want 5", got, ok)
	}
	if counts.pathCacheStores == 0 {
		t.Fatal("dynamic path write cache stores = 0, want runtime path cache store attribution")
	}
	if counts.pathCacheHits == 0 {
		t.Fatal("dynamic path write cache hits = 0, want runtime path cache hit attribution")
	}
}

func TestRunDirectFrameUsesRuntimePathCacheForTwoSegmentReadModifyWrite(t *testing.T) {
	proto, err := Compile(`
local player = {stats = {hp = 100, shield = 25}, inventory = {coins = 3}}
local i = 0
while i < 6 do
	player.stats.hp = player.stats.hp + player.stats.shield - player.inventory.coins
	i = i + 1
end
return player.stats.hp
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if !proto.directFrameDispatch {
		t.Fatalf("compiled path RMW program is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	if !strings.Contains(strings.Join(disassembleProto(proto), "\n"), "ADD_SUB_STRING_FIELD2") {
		t.Fatalf("compiled path RMW program is missing ADD_SUB_STRING_FIELD2:\n%s", strings.Join(disassembleProto(proto), "\n"))
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 232 {
		t.Fatalf("thread.run result is %v (%t), want 232", got, ok)
	}
	if counts.pathCacheStores < 3 {
		t.Fatalf("path RMW cache stores = %d, want target/add/sub path stores", counts.pathCacheStores)
	}
	if counts.pathCacheHits < 3 {
		t.Fatalf("path RMW cache hits = %d, want target/add/sub path hits", counts.pathCacheHits)
	}
}

func TestRuntimePathCacheCountersRecordStaleGuard(t *testing.T) {
	base := NewTable()
	child := NewTable()
	child.setRawStringField("value", NumberValue(1))
	base.setRawStringField("child", TableValue(child))
	firstSlot, ok := base.rawStringFieldSlot("child")
	if !ok {
		t.Fatal("base child slot missing")
	}
	secondSlot, ok := child.rawStringFieldSlot("value")
	if !ok {
		t.Fatal("child value slot missing")
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFramePICCounts = &counts
	thread.storeRuntimePathCache(17, base, "child", firstSlot, child, "value", secondSlot)

	replacement := NewTable()
	replacement.setRawStringField("value", NumberValue(2))
	base.setRawStringField("child", TableValue(replacement))

	if _, ok := thread.getRuntimePathCache(17, base, "child", "value"); ok {
		t.Fatal("getRuntimePathCache returned hit after parent slot changed, want stale miss")
	}
	if counts.pathCacheStores != 1 {
		t.Fatalf("path cache stores = %d, want 1", counts.pathCacheStores)
	}
	if counts.pathCacheStale != 1 {
		t.Fatalf("path cache stale = %d, want 1", counts.pathCacheStale)
	}
	if counts.pathCacheHits != 0 {
		t.Fatalf("path cache hits = %d, want 0", counts.pathCacheHits)
	}
}

func TestCompilerRecordsLoopLocalPathFactRejectionForTableWrite(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 3}}
local i = 0
local total = 0
while i < 4 do
	total = total + row.child.value
	row.child = {value = 4}
	total = total + row.child.value
	i = i + 1
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if strings.Contains(facts, "path_fact loop") {
		t.Fatalf("compiled mutating loop accepted path fact, want rejection:\n%s", facts)
	}
	for _, want := range []string{"path_fact_rejection", "table write", "birth pc", "kill table_local", "kill pc", "fallback pc"} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled mutating loop is missing rejection %q:\n%s", want, facts)
		}
	}
}

func TestCompilerRecordsLoopLocalPathFactRejectionForCall(t *testing.T) {
	proto, err := Compile(`
local row = {child = {value = 3}}
local function touch()
	return 1
end
local i = 0
local total = 0
while i < 4 do
	total = total + row.child.value
	touch()
	total = total + row.child.value
	i = i + 1
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	facts := strings.Join(disassembleProtoFacts(proto), "\n")
	if strings.Contains(facts, "path_fact loop") {
		t.Fatalf("compiled call loop accepted path fact, want rejection:\n%s", facts)
	}
	for _, want := range []string{"path_fact_rejection", "call", "birth pc", "kill call", "kill pc", "fallback pc"} {
		if !strings.Contains(facts, want) {
			t.Fatalf("compiled call loop is missing rejection %q:\n%s", want, facts)
		}
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
	if !proto.directFrameDispatch {
		t.Fatalf("compiled dynamic field call is not direct-frame eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
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

func TestCompilerUsesRowStringFieldPairEqualityBranchOpcode(t *testing.T) {
	proto, err := Compile(`
local events = {
	{kind = "kill", target = "wolf"},
	{kind = "visit", target = "tower"},
}
local objectives = {
	{kind = "kill", target = "wolf", score = 3},
	{kind = "kill", target = "spider", score = 5},
}
local total = 0
for _, event in events do
	for _, objective in objectives do
		if objective.kind == event.kind and objective.target == event.target then
			total = total + objective.score
		else
			total = total + 1
		end
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_FIELD") {
		t.Fatalf("compiled row field pair equality branch is missing JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_FIELD:\n%s", joined)
	}
	if !strings.Contains(joined, "slots 0 0") {
		t.Fatalf("compiled row field pair equality branch is missing propagated kind slots:\n%s", joined)
	}
	if !strings.Contains(joined, "slots 1 1") {
		t.Fatalf("compiled row field pair equality branch is missing propagated target slots:\n%s", joined)
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

func TestCompilerUsesRowStringFieldPairInequalityBranchOpcode(t *testing.T) {
	proto, err := Compile(`
local before = {
	{zone = "town"},
	{zone = "mine"},
}
local after = {
	{zone = "road"},
	{zone = "mine"},
}
local total = 0
for i, left in before do
	local right = after[i]
	if left.zone ~= right.zone then
		total = total + 17
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
	if !strings.Contains(joined, "JUMP_IF_ROW_STRING_FIELD_EQUAL_FIELD") {
		t.Fatalf("compiled row field pair inequality branch is missing JUMP_IF_ROW_STRING_FIELD_EQUAL_FIELD:\n%s", joined)
	}
	if !strings.Contains(joined, "slots 0 0") {
		t.Fatalf("compiled row field pair inequality branch is missing propagated slots:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 18 {
		t.Fatalf("Run result is %v (%t), want number 18", got, ok)
	}
}

func TestCompilerLoadsRowStringTagOnceForElseIfChain(t *testing.T) {
	proto, err := Compile(`
local buffs = {
	{kind = "poison", power = 3},
	{kind = "regen", power = 5},
	{kind = "shield", power = 7},
	{kind = "haste", power = 11},
	{kind = "unknown", power = 13},
}
local total = 0
for _, buff in buffs do
	if buff.kind == "poison" then
		total = total - buff.power
	elseif buff.kind == "regen" then
		total = total + buff.power
	elseif buff.kind == "shield" then
		total = total + buff.power * 2
	elseif buff.kind == "haste" then
		total = total + buff.power * 3
	else
		total = total + 1
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	lines := disassembleProto(proto)
	joined := strings.Join(lines, "\n")
	fastLimit := len(lines)
	for _, line := range lines {
		if strings.Contains(line, "JUMP_IF_TABLE_HAS_METATABLE") {
			fields := strings.Fields(line)
			target, err := strconv.Atoi(fields[len(fields)-1])
			if err != nil {
				t.Fatalf("metatable guard target is not numeric in line %q", line)
			}
			fastLimit = target
			break
		}
	}
	kindLoads := 0
	for _, line := range lines[:fastLimit] {
		if strings.Contains(line, "GET_ROW_STRING_FIELD") && strings.Contains(line, `"kind"`) {
			kindLoads++
		}
	}
	if kindLoads != 1 {
		t.Fatalf("compiled tag chain should load the row tag once:\n%s", joined)
	}
	if got := strings.Count(joined, "JUMP_IF_NOT_EQUAL_K"); got < 4 {
		t.Fatalf("compiled tag chain should branch from the loaded tag, got %d branches:\n%s", got, joined)
	}
	if !strings.Contains(joined, "JUMP_IF_TABLE_HAS_METATABLE") {
		t.Fatalf("compiled tag chain should preserve a metatable fallback path:\n%s", joined)
	}

	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, ok := results[0].Number()
	if !ok || got != 50 {
		t.Fatalf("Run result is %v (%t), want number 50", got, ok)
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

func TestCompilerLoadsRowStringTagOnceForElseIfChainWithAndGuards(t *testing.T) {
	proto, err := Compile(`
local rooms = {
	{kind = "combat", loot = 4},
	{kind = "treasure", loot = 5},
	{kind = "boss", loot = 6},
	{kind = "empty", loot = 7},
}
local total = 0
local depth = 9
for step = 1, 4 do
	for _, room in rooms do
		if room.kind == "combat" and step % 3 == 0 then
			total = total + room.loot
		elseif room.kind == "treasure" and depth > 8 then
			total = total + room.loot * 2
		elseif room.kind == "boss" and depth < 10 then
			total = total - room.loot
		else
			total = total + 1
		end
	end
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	lines := disassembleProto(proto)
	joined := strings.Join(lines, "\n")
	fastLimit := len(lines)
	for _, line := range lines {
		if strings.Contains(line, "JUMP_IF_TABLE_HAS_METATABLE") {
			fields := strings.Fields(line)
			target, err := strconv.Atoi(fields[len(fields)-1])
			if err != nil {
				t.Fatalf("metatable guard target is not numeric in line %q", line)
			}
			fastLimit = target
			break
		}
	}
	kindLoads := 0
	for _, line := range lines[:fastLimit] {
		if strings.Contains(line, "GET_ROW_STRING_FIELD") && strings.Contains(line, `"kind"`) {
			kindLoads++
		}
	}
	if kindLoads != 1 {
		t.Fatalf("compiled guarded tag chain should load the row tag once:\n%s", joined)
	}
	if got := strings.Count(joined, "JUMP_IF_NOT_EQUAL_K"); got < 3 {
		t.Fatalf("compiled guarded tag chain should branch from the loaded tag, got %d branches:\n%s", got, joined)
	}
	if !strings.Contains(joined, "JUMP_IF_TABLE_HAS_METATABLE") {
		t.Fatalf("compiled guarded tag chain should preserve a metatable fallback path:\n%s", joined)
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

func TestCompilerUsesStringFieldNilBranchOpcode(t *testing.T) {
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
	if !strings.Contains(joined, "JUMP_IF_STRING_FIELD_NIL") {
		t.Fatalf("compiled field nil branch is missing JUMP_IF_STRING_FIELD_NIL:\n%s", joined)
	}
	if !proto.directFrameDispatch {
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

func TestCompilerUsesStringFieldNotBranchOpcode(t *testing.T) {
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
	if !strings.Contains(joined, "JUMP_IF_STRING_FIELD_TRUE") {
		t.Fatalf("compiled field not branch is missing JUMP_IF_STRING_FIELD_TRUE:\n%s", joined)
	}
	if !proto.directFrameDispatch {
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

func TestCompilerUsesStringFieldEqualNilBranchOpcode(t *testing.T) {
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
	if !strings.Contains(joined, "JUMP_IF_STRING_FIELD_NOT_NIL") {
		t.Fatalf("compiled field == nil branch is missing JUMP_IF_STRING_FIELD_NOT_NIL:\n%s", joined)
	}
	if !proto.directFrameDispatch {
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
		"JUMP_IF_STRING_FIELD_NOT_GREATER_K",
		"JUMP_IF_STRING_FIELD_GREATER_K",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled numeric field branch is missing %s:\n%s", want, joined)
		}
	}
}

func TestCompilerUsesRowStringFieldNumericBranchOpcodes(t *testing.T) {
	proto, err := Compile(`
local entities = {
	{shield = 3, hp = 0},
	{shield = 0, hp = 4},
}
local score = 0
for _, entity in entities do
	if entity.shield > 0 then
		score = score + 5
	end
	if entity.hp <= 0 then
		score = score + 7
	end
end
return score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	for _, want := range []string{
		"JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_K",
		"JUMP_IF_ROW_STRING_FIELD_GREATER_K",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("compiled row numeric field branch is missing %s:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "slot 0") || !strings.Contains(joined, "slot 1") {
		t.Fatalf("compiled row numeric field branch is missing propagated slots:\n%s", joined)
	}
}

func TestCompilerUsesRowStringFieldRegisterNumericBranchOpcode(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{dist = 10},
	{dist = 4},
}
local candidates = {8, 4}
local score = 0
for i, row in rows do
	local candidate = candidates[i]
	if candidate < row.dist then
		score = score + row.dist
	else
		score = score + 1
	end
end
return score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_R") {
		t.Fatalf("compiled row field/register numeric branch is missing JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_R:\n%s", joined)
	}
	if !strings.Contains(joined, "slot 0") {
		t.Fatalf("compiled row field/register numeric branch is missing propagated slot:\n%s", joined)
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

func TestCompilerUsesRowStringFieldPairNumericBranchOpcode(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{have = 1, need = 3},
	{have = 2, need = 2},
}
local score = 0
for _, row in rows do
	if row.have < row.need then
		score = score + row.need
	else
		score = score + 1
	end
end
return score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}

	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "JUMP_IF_ROW_STRING_FIELD_NOT_LESS_FIELD") {
		t.Fatalf("compiled row field pair numeric branch is missing JUMP_IF_ROW_STRING_FIELD_NOT_LESS_FIELD:\n%s", joined)
	}
	if !strings.Contains(joined, "slots 0 1") {
		t.Fatalf("compiled row field pair numeric branch is missing propagated slots:\n%s", joined)
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

func TestCompilerUsesRowStringFieldTruthyInAndBranch(t *testing.T) {
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
	if !strings.Contains(joined, "JUMP_IF_STRING_FIELD_FALSE") {
		t.Fatalf("compiled row boolean and branch is missing JUMP_IF_STRING_FIELD_FALSE:\n%s", joined)
	}
	if !strings.Contains(joined, "slot 0") {
		t.Fatalf("compiled row boolean and branch is missing propagated alive slot:\n%s", joined)
	}
	if !strings.Contains(joined, "JUMP_IF_NOT_GREATER") {
		t.Fatalf("compiled row boolean and branch is missing register numeric branch:\n%s", joined)
	}
	for _, line := range disassembleProto(proto) {
		if strings.Contains(line, "GET_ROW_STRING_FIELD") && strings.Contains(line, `"alive"`) {
			t.Fatalf("compiled row boolean and branch should not materialize actor.alive:\n%s", joined)
		}
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

func TestCompilerUsesRowStringFieldNilInAndBranch(t *testing.T) {
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
	if !strings.Contains(joined, "JUMP_IF_STRING_FIELD_NIL") {
		t.Fatalf("compiled row nil and branch is missing JUMP_IF_STRING_FIELD_NIL:\n%s", joined)
	}
	if !strings.Contains(joined, "JUMP_IF_NOT_GREATER") {
		t.Fatalf("compiled row nil and branch is missing register numeric branch:\n%s", joined)
	}
	for _, line := range disassembleProto(proto) {
		if strings.Contains(line, "GET_ROW_STRING_FIELD") && strings.Contains(line, `"key"`) {
			t.Fatalf("compiled row nil and branch should not materialize check.key:\n%s", joined)
		}
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
	if !proto.directFrameDispatch {
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

func TestOptimizeBytecodeIRRemapsSpecializedBranchDTarget(t *testing.T) {
	var builder bytecodeBuilder
	field := builder.addConstant(StringValue("alive"))
	jumpElse := builder.emit(instruction{op: opJumpIfStringFieldFalse, a: 0, b: field, d: 0})
	builder.emitLoadConst(1, NumberValue(1))
	jumpEnd := builder.emitJump()
	elseStart := builder.pc()
	builder.patchJumpD(jumpElse, elseStart)
	builder.emit(instruction{op: opMove, a: 2, b: 2})
	builder.emitLoadConst(1, NumberValue(2))
	end := builder.pc()
	builder.patchJump(jumpEnd, end)
	builder.emit(instruction{op: opReturnOne, a: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opJumpIfStringFieldFalse, a: 0, b: field, d: 3},
		{op: opLoadConst, a: 1, b: 1},
		{op: opJump, b: 4},
		{op: opLoadConst, a: 1, b: 2},
		{op: opReturnOne, a: 1},
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

func TestOptimizeBytecodeIRRemovesDeadProvenNumericArithmetic(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(1, NumberValue(2))
	builder.emitLoadConst(2, NumberValue(3))
	builder.emit(instruction{op: opAdd, a: 3, b: 1, c: 2})
	builder.emitLoadConst(4, NumberValue(9))
	builder.emit(instruction{op: opReturnOne, a: 4})

	builder.optimize(optimizationOptions{})
	got := assembleBytecodeIR(builder.ir)
	want := []instruction{
		{op: opLoadConst, a: 4, b: 2},
		{op: opReturnOne, a: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadProvenInPlaceNumericArithmetic(t *testing.T) {
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
		{op: opLoadConst, a: 3, b: 2},
		{op: opReturnOne, a: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadProvenNumericAddModArithmetic(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(1, NumberValue(10))
	builder.emitLoadConst(2, NumberValue(4))
	desc := builder.addNumericAddModOp(numericAddModOp{
		mul:  builder.addConstant(NumberValue(3)),
		idiv: builder.addConstant(NumberValue(2)),
		mod:  builder.addConstant(NumberValue(17)),
	})
	builder.emit(instruction{op: opAddNumericModK, a: 1, b: 2, c: desc})
	builder.emitLoadConst(3, NumberValue(9))
	builder.emit(instruction{op: opReturnOne, a: 3})

	builder.optimize(optimizationOptions{})
	got := assembleBytecodeIR(builder.ir)
	want := []instruction{
		{op: opLoadConst, a: 3, b: 5},
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

func TestOptimizeBytecodeIRKeepsDeadUnprovenNumericAddModArithmetic(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(2, NumberValue(4))
	desc := builder.addNumericAddModOp(numericAddModOp{
		mul:  builder.addConstant(NumberValue(3)),
		idiv: builder.addConstant(NumberValue(2)),
		mod:  builder.addConstant(NumberValue(17)),
	})
	builder.emit(instruction{op: opAddNumericModK, a: 1, b: 2, c: desc})
	builder.emitLoadConst(3, NumberValue(9))
	builder.emit(instruction{op: opReturnOne, a: 3})

	builder.optimize(optimizationOptions{})
	got := assembleBytecodeIR(builder.ir)
	want := []instruction{
		{op: opLoadConst, a: 2, b: 0},
		{op: opAddNumericModK, a: 1, b: 2, c: desc},
		{op: opLoadConst, a: 3, b: 4},
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
		{name: "table insert", ins: instruction{op: opTableInsert, a: 4, b: 2, d: 1}, want: []int{4, 5, 6}},
		{name: "table remove", ins: instruction{op: opTableRemove, a: 4, b: 1, d: 1}, want: []int{4, 5}},
		{name: "coroutine resume", ins: instruction{op: opCoroutineResume, a: 4, b: 2, d: 2}, want: []int{4, 5, 6}},
		{name: "math min", ins: instruction{op: opMathMin, a: 4, b: 2, d: 1}, want: []int{4, 5, 6}},
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
		{name: "get field", ins: instruction{op: opGetField, a: 8, b: 4, c: 0}, want: []int{4}},
		{name: "set field", ins: instruction{op: opSetField, a: 4, b: 0, c: 6}, want: []int{4, 6}},
		{name: "get index", ins: instruction{op: opGetIndex, a: 8, b: 4, c: 6}, want: []int{4, 6}},
		{name: "set index", ins: instruction{op: opSetIndex, a: 4, b: 5, c: 6}, want: []int{4, 5, 6}},
		{name: "get string field", ins: instruction{op: opGetStringField, a: 8, b: 4, c: 0}, want: []int{4}},
		{name: "set string field", ins: instruction{op: opSetStringField, a: 4, b: 0, c: 6}, want: []int{4, 6}},
		{name: "get row string field", ins: instruction{op: opGetRowStringField, a: 8, b: 4, c: 0, d: 1}, want: []int{4}},
		{name: "set row string field", ins: instruction{op: opSetRowStringField, a: 4, b: 0, c: 6, d: 1}, want: []int{4, 6}},
		{name: "get string field2", ins: instruction{op: opGetStringField2, a: 8, b: 4, c: 0, d: 1}, want: []int{4}},
		{name: "set string field2", ins: instruction{op: opSetStringField2, a: 4, b: 0, c: 1, d: 6}, want: []int{4, 6}},
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
		{name: "not less register", ins: instruction{op: opJumpIfNotLess, a: 8, b: 1, d: 20}, want: []int{1, 8}},
		{name: "not greater register", ins: instruction{op: opJumpIfNotGreater, a: 8, b: 1, d: 20}, want: []int{1, 8}},
		{name: "mod not equal constants", ins: instruction{op: opJumpIfModKNotEqualK, a: 8, b: 1, c: 2, d: 20}, want: []int{8}},
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
		{name: "string field not equal constant", ins: instruction{op: opJumpIfStringFieldNotEqualK, a: 8, b: 1, c: 2, d: 20}, want: []int{8}},
		{name: "row string field not equal constant", ins: instruction{op: opJumpIfRowStringFieldNotEqualK, a: 8, b: 1, d: 20}, want: []int{8}},
		{name: "row string field not equal field", ins: instruction{op: opJumpIfRowStringFieldNotEqualField, a: 8, b: 1, c: 2, d: 20}, want: []int{2, 8}},
		{name: "row string field equal field", ins: instruction{op: opJumpIfRowStringFieldEqualField, a: 8, b: 1, c: 2, d: 20}, want: []int{2, 8}},
		{name: "string field not greater constant", ins: instruction{op: opJumpIfStringFieldNotGreaterK, a: 8, b: 1, c: 2, d: 20}, want: []int{8}},
		{name: "string field greater constant", ins: instruction{op: opJumpIfStringFieldGreaterK, a: 8, b: 1, c: 2, d: 20}, want: []int{8}},
		{name: "row string field not greater constant", ins: instruction{op: opJumpIfRowStringFieldNotGreaterK, a: 8, b: 1, d: 20}, want: []int{8}},
		{name: "row string field greater constant", ins: instruction{op: opJumpIfRowStringFieldGreaterK, a: 8, b: 1, d: 20}, want: []int{8}},
		{name: "string field not greater register", ins: instruction{op: opJumpIfStringFieldNotGreaterR, a: 8, b: 1, c: 2, d: 20}, want: []int{2, 8}},
		{name: "row string field not greater register", ins: instruction{op: opJumpIfRowStringFieldNotGreaterR, a: 8, b: 1, c: 2, d: 20}, want: []int{2, 8}},
		{name: "row string field not less field", ins: instruction{op: opJumpIfRowStringFieldNotLessField, a: 8, b: 1, d: 20}, want: []int{8}},
		{name: "string field false", ins: instruction{op: opJumpIfStringFieldFalse, a: 8, b: 1, c: 2, d: 20}, want: []int{8}},
		{name: "string field nil", ins: instruction{op: opJumpIfStringFieldNil, a: 8, b: 1, c: 2, d: 20}, want: []int{8}},
		{name: "string field true", ins: instruction{op: opJumpIfStringFieldTrue, a: 8, b: 1, c: 2, d: 20}, want: []int{8}},
		{name: "string field not nil", ins: instruction{op: opJumpIfStringFieldNotNil, a: 8, b: 1, c: 2, d: 20}, want: []int{8}},
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

func TestOptimizeBytecodeIRRemovesDeadLoadAroundRowStringFieldOps(t *testing.T) {
	var builder bytecodeBuilder
	field := builder.addConstant(StringValue("hp"))
	builder.emitLoadConst(9, NumberValue(99))
	builder.emit(instruction{op: opGetRowStringField, a: 1, b: 0, c: field, d: 0})
	builder.emitLoadConst(2, NumberValue(7))
	builder.emit(instruction{op: opSetRowStringField, a: 0, b: field, c: 2, d: 0})
	builder.emit(instruction{op: opReturnOne, a: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opGetRowStringField, a: 1, b: 0, c: field, d: 0},
		{op: opLoadConst, a: 2, b: 2},
		{op: opSetRowStringField, a: 0, b: field, c: 2, d: 0},
		{op: opReturnOne, a: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRRemovesDeadLoadAroundStringFieldPairOps(t *testing.T) {
	var builder bytecodeBuilder
	first := builder.addConstant(StringValue("stats"))
	second := builder.addConstant(StringValue("hp"))
	builder.emitLoadConst(9, NumberValue(99))
	builder.emit(instruction{op: opGetStringField2, a: 1, b: 0, c: first, d: second})
	builder.emitLoadConst(2, NumberValue(7))
	builder.emit(instruction{op: opSetStringField2, a: 0, b: first, c: second, d: 2})
	builder.emit(instruction{op: opReturnOne, a: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opGetStringField2, a: 1, b: 0, c: first, d: second},
		{op: opLoadConst, a: 2, b: 3},
		{op: opSetStringField2, a: 0, b: first, c: second, d: 2},
		{op: opReturnOne, a: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
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

func TestOptimizeBytecodeIRRemovesDeadLoadAroundTablePredicateBranch(t *testing.T) {
	var builder bytecodeBuilder
	field := builder.addConstant(StringValue("alive"))
	builder.emitLoadConst(9, NumberValue(99))
	jumpEnd := builder.emit(instruction{op: opJumpIfStringFieldFalse, a: 0, b: field, c: 0})
	builder.emit(instruction{op: opReturnOne, a: 0})
	end := builder.pc()
	builder.patchJumpD(jumpEnd, end)

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opJumpIfStringFieldFalse, a: 0, b: field, c: 0, d: 2},
		{op: opReturnOne, a: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("optimized bytecode = %#v, want %#v", got, want)
	}
}

func TestOptimizeBytecodeIRKeepsIntrinsicArgumentLoads(t *testing.T) {
	var builder bytecodeBuilder
	builder.emitLoadConst(1, NumberValue(4))
	builder.emitLoadConst(2, NumberValue(7))
	builder.emit(instruction{op: opMathMin, a: 1, b: 1, d: 1})
	builder.emit(instruction{op: opReturnOne, a: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 1, b: 0},
		{op: opLoadConst, a: 2, b: 1},
		{op: opMathMin, a: 1, b: 1, d: 1},
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
	builder.emit(instruction{op: opMathMin, a: 1, b: 1, d: 1})
	builder.emit(instruction{op: opReturnOne, a: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opLoadConst, a: 1, b: 1},
		{op: opLoadConst, a: 2, b: 2},
		{op: opMathMin, a: 1, b: 1, d: 1},
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

func TestOptimizeBytecodeIRRemovesDeadLoadAroundTableFieldRead(t *testing.T) {
	var builder bytecodeBuilder
	field := builder.addConstant(StringValue("hp"))
	builder.emitLoadConst(9, NumberValue(99))
	builder.emit(instruction{op: opGetField, a: 1, b: 0, c: field})
	builder.emit(instruction{op: opReturnOne, a: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opGetField, a: 1, b: 0, c: field},
		{op: opReturnOne, a: 1},
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

func TestCompileRunRowStringFieldDCEPreservesEffects(t *testing.T) {
	proto, err := Compile(`
local rows = {
	{hp = 10},
	{hp = 20},
}
local dead = 99
local total = 0
for _, row in rows do
	row.hp = row.hp + 1
	total = total + row.hp
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "GET_ROW_STRING_FIELD") || !strings.Contains(joined, "ADD_STRING_FIELD") {
		t.Fatalf("compiled row field program is missing row field ops:\n%s", joined)
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 32 {
		t.Fatalf("Run result is %v (%t), want number 32", got, ok)
	}
}

func TestCompileRunNestedStringFieldDCEPreservesEffects(t *testing.T) {
	proto, err := Compile(`
local row = {stats = {hp = 10}}
local dead = 99
row.stats.hp = 12
local got = row.stats.hp
return got
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	joined := strings.Join(disassembleProto(proto), "\n")
	if !strings.Contains(joined, "GET_STRING_FIELD2") && !strings.Contains(joined, "GET_STRING_FIELD_INDEX") {
		t.Fatalf("compiled nested field program is missing nested field read ops:\n%s", joined)
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
	if !strings.Contains(joined, "JUMP_IF_STRING_FIELD_FALSE") {
		t.Fatalf("compiled table predicate program is missing field predicate branch:\n%s", joined)
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
	builder.emit(instruction{op: opTableInsert, a: 1, b: 1, d: 1})
	builder.emit(instruction{op: opReturnOne, a: 1})

	optimized := optimizeBytecodeIR(builder.ir, optimizationOptions{})
	got := assembleBytecodeIR(optimized)
	want := []instruction{
		{op: opNewTable, a: 1},
		{op: opLoadConst, a: 2, b: 0},
		{op: opTableInsert, a: 1, b: 1, d: 1},
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
	for op := opcode(0); op < opcodeCount; op++ {
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
		if meta.mayCall != wantOpcodeMayCall(op) {
			t.Fatalf("opcode metadata mayCall for %s is %t, want %t", opcodeName(op), meta.mayCall, wantOpcodeMayCall(op))
		}
		if meta.mayYield != wantOpcodeMayYield(op) {
			t.Fatalf("opcode metadata mayYield for %s is %t, want %t", opcodeName(op), meta.mayYield, wantOpcodeMayYield(op))
		}
		if meta.mayYield && !meta.mayCall {
			t.Fatalf("opcode metadata %s may yield without call risk", opcodeName(op))
		}
		if meta.readsTable != wantOpcodeReadsTable(op) {
			t.Fatalf("opcode metadata readsTable for %s is %t, want %t", opcodeName(op), meta.readsTable, wantOpcodeReadsTable(op))
		}
		if meta.writesTable != wantOpcodeWritesTable(op) {
			t.Fatalf("opcode metadata writesTable for %s is %t, want %t", opcodeName(op), meta.writesTable, wantOpcodeWritesTable(op))
		}
		if meta.readsGlobal != (op == opLoadGlobal) {
			t.Fatalf("opcode metadata readsGlobal for %s is %t, want %t", opcodeName(op), meta.readsGlobal, op == opLoadGlobal)
		}
		if meta.writesGlobal != (op == opSetGlobal) {
			t.Fatalf("opcode metadata writesGlobal for %s is %t, want %t", opcodeName(op), meta.writesGlobal, op == opSetGlobal)
		}
		if meta.allocates != wantOpcodeAllocates(op) {
			t.Fatalf("opcode metadata allocates for %s is %t, want %t", opcodeName(op), meta.allocates, wantOpcodeAllocates(op))
		}
		if meta.writesTable && meta.readsGlobal {
			t.Fatalf("opcode metadata %s mixes table write and global read effects", opcodeName(op))
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
		mutate func(*[opcodeCount]opcodeMetadataEntry)
		want   string
	}{
		{
			name: "empty name",
			mutate: func(table *[opcodeCount]opcodeMetadataEntry) {
				table[opAdd].name = ""
			},
			want: "missing name",
		},
		{
			name: "empty operands",
			mutate: func(table *[opcodeCount]opcodeMetadataEntry) {
				table[opAdd].operands = opcodeOperandShape{}
			},
			want: "missing operand shape",
		},
		{
			name: "branch without jump target",
			mutate: func(table *[opcodeCount]opcodeMetadataEntry) {
				table[opJumpIfFalse].jumpTarget = opcodeJumpTargetNone
			},
			want: "control flow without jump target",
		},
		{
			name: "yield without call",
			mutate: func(table *[opcodeCount]opcodeMetadataEntry) {
				table[opCall].mayCall = false
			},
			want: "may yield without call risk",
		},
		{
			name: "jump slot without operand",
			mutate: func(table *[opcodeCount]opcodeMetadataEntry) {
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

func wantOpcodeReadsTable(op opcode) bool {
	switch op {
	case opSetIndex,
		opGetField,
		opGetStringField,
		opGetRowStringField,
		opGetStringField2,
		opGetStringFieldIndex,
		opAddStringField,
		opSubStringField,
		opSubAddStringField,
		opAddSubStringField2,
		opGetIndex,
		opPrepareIter,
		opArrayNext,
		opArrayNextJump2,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualField,
		opJumpIfRowStringFieldEqualField,
		opJumpIfStringFieldNotGreaterK,
		opJumpIfStringFieldGreaterK,
		opJumpIfRowStringFieldNotGreaterK,
		opJumpIfRowStringFieldGreaterK,
		opJumpIfStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotLessField,
		opJumpIfStringFieldFalse,
		opJumpIfStringFieldNil,
		opJumpIfStringFieldTrue,
		opJumpIfStringFieldNotNil,
		opTableInsert,
		opTableRemove,
		opCallMethodOne,
		opCallTableFieldKeyOne:
		return true
	default:
		return false
	}
}

func wantOpcodeWritesTable(op opcode) bool {
	switch op {
	case opSetField,
		opSetStringField,
		opSetRowStringField,
		opSetStringField2,
		opSetStringFieldIndex,
		opAddStringField,
		opSubStringField,
		opSubAddStringField,
		opAddSubStringField2,
		opSetIndex,
		opTableInsert,
		opTableRemove:
		return true
	default:
		return false
	}
}

func wantOpcodeAllocates(op opcode) bool {
	switch op {
	case opNewTable,
		opClosure,
		opVararg,
		opConcat,
		opCoroutineResume,
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallUpvalueOne,
		opCallUpvalueSelfOne,
		opCallUpvalueSelfKOne,
		opCallUpvalueSelfAddKOne,
		opCallMethodOne,
		opCallTableFieldKeyOne:
		return true
	default:
		return false
	}
}

func wantOpcodeMayCall(op opcode) bool {
	switch op {
	case opCoroutineResume,
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallUpvalueOne,
		opCallUpvalueSelfOne,
		opCallUpvalueSelfKOne,
		opCallUpvalueSelfAddKOne,
		opCallMethodOne,
		opCallTableFieldKeyOne:
		return true
	default:
		return false
	}
}

func wantOpcodeMayYield(op opcode) bool {
	return wantOpcodeMayCall(op)
}

func wantDirectFrameOpcodeSupported(op opcode) bool {
	switch op {
	case opLoadConst,
		opLoadGlobal,
		opNewTable,
		opSetField,
		opGetField,
		opSetStringField,
		opSetRowStringField,
		opSetStringField2,
		opSetStringFieldIndex,
		opGetStringField,
		opGetRowStringField,
		opGetStringField2,
		opGetStringFieldIndex,
		opAddStringField,
		opSubStringField,
		opSubAddStringField,
		opAddSubStringField2,
		opSetIndex,
		opGetIndex,
		opClosure,
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
		opAddNumericModK,
		opNeg,
		opEqual,
		opNotEqual,
		opLess,
		opLessEqual,
		opGreater,
		opGreaterEqual,
		opNumericForCheck,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfModKNotEqualK,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualField,
		opJumpIfRowStringFieldEqualField,
		opJumpIfStringFieldNotGreaterK,
		opJumpIfStringFieldGreaterK,
		opJumpIfRowStringFieldNotGreaterK,
		opJumpIfRowStringFieldGreaterK,
		opJumpIfStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotLessField,
		opJumpIfStringFieldFalse,
		opJumpIfStringFieldNil,
		opJumpIfStringFieldTrue,
		opJumpIfStringFieldNotNil,
		opTableInsert,
		opTableRemove,
		opMathMin,
		opJumpIfFalse,
		opCall,
		opCallOne,
		opCallLocalOne,
		opCallTableFieldKeyOne,
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
	case opJump:
		return opcodeControlJump
	case opArrayNextJump2,
		opNumericForCheck,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfNotLess,
		opJumpIfNotGreater,
		opJumpIfModKNotEqualK,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualField,
		opJumpIfRowStringFieldEqualField,
		opJumpIfStringFieldNotGreaterK,
		opJumpIfStringFieldGreaterK,
		opJumpIfRowStringFieldNotGreaterK,
		opJumpIfRowStringFieldGreaterK,
		opJumpIfStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotLessField,
		opJumpIfStringFieldFalse,
		opJumpIfStringFieldNil,
		opJumpIfStringFieldTrue,
		opJumpIfStringFieldNotNil,
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

func TestRunDirectLeafCallOnePreservesSemantics(t *testing.T) {
	proto, err := Compile(`
local function add(a, b)
	return a + b
end
local function first(a, b)
	if b == nil then
		return a
	end
	return b
end
local total = 0
for i = 1, 8 do
	total = total + add(i, 2)
end
return total, first(9)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) < 2 || !proto.prototypes[0].directLeafCallOne || !proto.prototypes[1].directLeafCallOne {
		t.Fatalf("compiled closures are not direct leaf-call eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 52 {
		t.Fatalf("first result is %v (%t), want number 52", results[0], ok)
	}
	if got, ok := results[1].Number(); !ok || got != 9 {
		t.Fatalf("second result is %v (%t), want number 9", results[1], ok)
	}
}

func TestRunDirectLeafCallOneCountersRecordReusableFrame(t *testing.T) {
	proto, err := Compile(`
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
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) == 0 || !proto.prototypes[0].directLeafCallOne {
		t.Fatalf("compiled closures are not direct leaf-call eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 52 {
		t.Fatalf("first result is %v (%t), want number 52", results[0], ok)
	}
	if counts.fixedCallFrameReuses != 8 {
		t.Fatalf("fixed-call frame reuses = %d, want 8", counts.fixedCallFrameReuses)
	}
	if counts.fixedCallArgCopies != 16 {
		t.Fatalf("fixed-call arg copies = %d, want 16", counts.fixedCallArgCopies)
	}
	if counts.fixedCallFrameMaterializations != 0 {
		t.Fatalf("fixed-call frame materializations = %d, want 0", counts.fixedCallFrameMaterializations)
	}
}

func TestRunDirectLeafCallOneCountersRecordFallbackMaterialization(t *testing.T) {
	proto, err := Compile(`
local function read(t)
	return t.x
end
local proxy = setmetatable({}, {
	__index = function()
		return 41
	end,
})
local value = read(proxy)
return value
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) == 0 || !proto.prototypes[0].directLeafCallOne {
		t.Fatalf("compiled read closure is not direct leaf-call eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 41 {
		t.Fatalf("first result is %v (%t), want number 41", results[0], ok)
	}
	if counts.fixedCallFrameReuses != 1 {
		t.Fatalf("fixed-call frame reuses = %d, want 1", counts.fixedCallFrameReuses)
	}
	if counts.fixedCallFrameMaterializations == 0 {
		t.Fatalf("fixed-call frame materializations = 0, want direct leaf side-exit materialization")
	}
	if counts.fixedCallRegisterCopies == 0 {
		t.Fatalf("fixed-call register copies = 0, want side-exit materialization copies")
	}
}

func TestRunDirectFrameTableFieldKeyCallUsesFastMethodFieldAdd(t *testing.T) {
	proto, err := Compile(`
local handlers = {}
function handlers.bump(state, amount)
	state.score = state.score + amount
	return state.score
end
local state = {score = 0}
local event = {kind = "bump", amount = 3}
local total = 0
for i = 1, 4 do
	total = total + handlers[event.kind](state, event.amount)
end
return total, state.score
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) == 0 || !proto.prototypes[0].hasFastMethodFieldAdd {
		t.Fatalf("compiled handler is not fast field-add eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	if joined := strings.Join(disassembleProto(proto), "\n"); !strings.Contains(joined, "CALL_TABLE_FIELD_KEY_ONE") {
		t.Fatalf("compiled dynamic handler call is missing table field-key call:\n%s", joined)
	}

	var counts directFramePICCounts
	thread := newVMThread(runtimeGlobals(nil))
	thread.directFramePICCounts = &counts
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 30 {
		t.Fatalf("first result is %v (%t), want number 30", results[0], ok)
	}
	if got, ok := results[1].Number(); !ok || got != 12 {
		t.Fatalf("second result is %v (%t), want number 12", results[1], ok)
	}
	if counts.fixedCallFrameReuses != 0 || counts.fixedCallArgCopies != 0 {
		t.Fatalf("fixed-call counters = reuse %d arg copies %d, want table field-key fast add to avoid script call frames", counts.fixedCallFrameReuses, counts.fixedCallArgCopies)
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

func TestRunDirectFrameRuntimePathCacheIsEnabledWithoutCounters(t *testing.T) {
	proto, err := Compile(`
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
		t.Fatalf("Compile returned error: %v", err)
	}
	if facts := strings.Join(disassembleProtoFacts(proto), "\n"); !strings.Contains(facts, "path_plan") {
		t.Fatalf("compiled path program is missing path plan:\n%s", facts)
	}

	thread := newVMThread(runtimeGlobals(nil))
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 36 {
		t.Fatalf("result is %v (%t), want number 36", results[0], ok)
	}
	if thread.runtimePathCount == 0 {
		t.Fatal("runtime path cache count is 0, want normal run to populate path-plan cache without counters")
	}
}

func TestRunDirectLeafCallOneFallsBackAcrossProtectedBoundary(t *testing.T) {
	proto, err := Compile(`
local function bad(t)
	return t.missing.value
end
local ok, message = pcall(function()
	return bad({})
end)
return ok, type(message)
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) == 0 || !proto.prototypes[0].directLeafCallOne {
		t.Fatalf("compiled bad closure is not direct leaf-call eligible:\n%s", strings.Join(disassembleProtoFacts(proto), "\n"))
	}
	results, err := Run(proto)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, ok := results[0].Bool(); !ok || got {
		t.Fatalf("first result is %v (%t), want false", results[0], ok)
	}
	if got, ok := results[1].String(); !ok || got != "string" {
		t.Fatalf("second result is %v (%t), want string", results[1], ok)
	}
}
