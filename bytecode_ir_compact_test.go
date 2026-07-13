package ember

import (
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

func TestBytecodeIRCompactLayout(t *testing.T) {
	if got := unsafe.Sizeof(bytecodeIRInstruction{}); got > 32 {
		t.Fatalf("bytecodeIRInstruction=%d bytes, want <=32", got)
	}
}

func TestBytecodeIRCheckedConversionsRejectMalformedValues(t *testing.T) {
	max := int(1<<31 - 1)
	one := uint64(1)
	if _, err := lowerInstructionToBytecodeIRChecked(instruction{op: opCall, a: max, b: max, c: -3, d: -1}, sourceRange{start: max, end: max}); err != nil {
		t.Fatalf("max int32 instruction rejected: %v", err)
	}
	cases := []struct {
		name string
		ins  instruction
		src  sourceRange
		want string
	}{
		{name: "negative source", ins: instruction{op: opMove}, src: sourceRange{start: -1}, want: "source start"},
		{name: "invalid opcode", ins: instruction{op: opcode(255)}, want: "invalid opcode"},
	}
	if strconv.IntSize == 64 {
		cases = append(cases,
			struct {
				name string
				ins  instruction
				src  sourceRange
				want string
			}{name: "operand overflow", ins: instruction{op: opMove, a: max + 1}, want: "operand A"},
			struct {
				name string
				ins  instruction
				src  sourceRange
				want string
			}{name: "operand underflow", ins: instruction{op: opMove, a: -max - 2}, want: "operand A"},
			struct {
				name string
				ins  instruction
				src  sourceRange
				want string
			}{name: "source uint32 overflow", ins: instruction{op: opMove}, src: sourceRange{start: int(one << 32)}, want: "source start"},
		)
	}

	maxSource := sourceRange{}
	if strconv.IntSize == 64 {
		maxUint32 := int((one << 32) - 1)
		maxSource = sourceRange{start: maxUint32, end: maxUint32}
	}
	if ir, err := lowerInstructionToBytecodeIRChecked(instruction{op: opMove, a: max, b: max}, maxSource); err != nil {
		t.Fatalf("max register/source instruction rejected: %v", err)
	} else if assembled, err := assembleBytecodeIRInstructionChecked(ir); err != nil {
		t.Fatalf("max register/source assembly rejected: %v", err)
	} else {
		if assembled.a != max || assembled.b != max {
			t.Fatalf("max register roundtrip=%+v", assembled)
		}
		if ir.operandKind(bytecodeIROperandSlotA) != bytecodeOperandRegister || ir.operandKind(bytecodeIROperandSlotB) != bytecodeOperandRegister {
			t.Fatalf("register operand kinds=%v,%v", ir.operandKind(bytecodeIROperandSlotA), ir.operandKind(bytecodeIROperandSlotB))
		}
		if strconv.IntSize == 64 {
			if span := ir.sourceSpan(); span != maxSource {
				t.Fatalf("max source roundtrip=%+v, want %+v", span, maxSource)
			}
		}
	}
	for _, tc := range []struct {
		name string
		ins  instruction
		kind bytecodeOperandKind
		slot bytecodeIROperandSlot
	}{
		{name: "constant", ins: instruction{op: opLoadConst, a: max, b: max}, kind: bytecodeOperandConstant, slot: bytecodeIROperandSlotB},
		{name: "prototype", ins: instruction{op: opClosure, a: max, b: max}, kind: bytecodeOperandPrototype, slot: bytecodeIROperandSlotB},
		{name: "B jump", ins: instruction{op: opJumpIfFalse, a: max, b: max}, kind: bytecodeOperandJumpTarget, slot: bytecodeIROperandSlotB},
		{name: "D jump", ins: instruction{op: opNumericForLoop, a: max, b: max, d: max}, kind: bytecodeOperandJumpTarget, slot: bytecodeIROperandSlotD},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ir, err := lowerInstructionToBytecodeIRChecked(tc.ins, sourceRange{})
			if err != nil {
				t.Fatalf("lower rejected max value: %v", err)
			}
			assembled, err := assembleBytecodeIRInstructionChecked(ir)
			if err != nil {
				t.Fatalf("assemble rejected max value: %v", err)
			}
			if assembled != tc.ins {
				t.Fatalf("roundtrip=%+v, want %+v", assembled, tc.ins)
			}
			if ir.operandKind(tc.slot) != tc.kind || ir.operandValue(tc.slot) != max {
				t.Fatalf("operand slot=%v kind=%v value=%d", tc.slot, ir.operandKind(tc.slot), ir.operandValue(tc.slot))
			}
		})
	}
	for _, tc := range []struct {
		name string
		ins  instruction
	}{
		{name: "vararg count", ins: instruction{op: opVararg, a: 0, b: -1}},
		{name: "call counts", ins: instruction{op: opCall, a: 0, b: 1, c: -3, d: -1}},
		{name: "return count", ins: instruction{op: opReturn, a: 0, b: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ir, err := lowerInstructionToBytecodeIRChecked(tc.ins, sourceRange{})
			if err != nil {
				t.Fatalf("lower rejected negative marker: %v", err)
			}
			assembled, err := assembleBytecodeIRInstructionChecked(ir)
			if err != nil || assembled != tc.ins {
				t.Fatalf("negative marker roundtrip=%+v err=%v, want %+v", assembled, err, tc.ins)
			}
		})
	}
	if _, err := assembleBytecodeIRInstructionChecked(bytecodeIRInstruction{op: opcode(255)}); err == nil || !strings.Contains(err.Error(), "invalid IR opcode") {
		t.Fatalf("invalid checked assembly error=%v", err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := lowerInstructionToBytecodeIRChecked(tc.ins, tc.src); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want substring %q", err, tc.want)
			}
		})
	}

	ir := lowerInstructionToBytecodeIR(instruction{op: opMove, a: 1, b: 2}, sourceRange{})
	before := ir
	if strconv.IntSize == 64 {
		if ir.setOperandValueChecked(bytecodeIROperandSlotA, max+1) == nil {
			t.Fatal("setter accepted int32 overflow")
		}
		if ir != before {
			t.Fatalf("overflow setter mutated IR: before=%+v after=%+v", before, ir)
		}
	}
	malformed := bytecodeIRInstruction{op: opMove, sourceStart: ^uint32(0), sourceEnd: ^uint32(0)}
	if _, err := malformed.sourceSpanChecked(); strconv.IntSize == 32 && err == nil {
		t.Fatal("source span overflow on 32-bit int was not rejected")
	} else if strconv.IntSize == 64 && err != nil {
		t.Fatalf("max uint32 source span rejected on 64-bit int: %v", err)
	}
}

func TestBytecodeBuilderLatchesLoweringError(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("int32 overflow case requires 64-bit int")
	}
	var builder bytecodeBuilder
	one := uint64(1)
	if got := builder.emit(instruction{op: opMove, a: int(one << 31)}); got != -1 {
		t.Fatalf("overflow emit index=%d, want -1", got)
	}
	if len(builder.ir) != 0 {
		t.Fatalf("overflow emit appended %d IR instructions", len(builder.ir))
	}
	builder.patchJump(-1, 1)
	if _, err := builder.finalizeProto(nil, 1, 0, false); err == nil || !strings.Contains(err.Error(), "lower MOVE") {
		t.Fatalf("finalize error=%v, want lowering error", err)
	}
	if got := builder.proto(nil, 1, 0, false); got != nil {
		t.Fatal("proto published after conversion error")
	}
}

func TestBytecodeBuilderRejectsInvalidJumpPatch(t *testing.T) {
	var builder bytecodeBuilder
	if got := builder.emit(instruction{op: opMove, a: 0, b: 1}); got != 0 {
		t.Fatalf("emit index=%d, want 0", got)
	}
	builder.patchJump(0, 1)
	if builder.conversionErr == nil || !strings.Contains(builder.conversionErr.Error(), "opcode has no jump target") {
		t.Fatalf("patch error=%v, want no-jump-target error", builder.conversionErr)
	}
	if got := builder.emit(instruction{op: opJump}); got != -1 {
		t.Fatalf("emit after patch error index=%d, want -1", got)
	}
}
