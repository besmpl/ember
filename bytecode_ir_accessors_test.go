package ember

import (
	"reflect"
	"testing"
)

func TestBytecodeIRAccessorsRoundTripAllOpcodes(t *testing.T) {
	for _, op := range allOpcodes {
		for variant := 0; variant < 2; variant++ {
			raw := sampleInstructionForIRAccessors(op, variant)
			source := sourceRange{start: 7, end: 19}
			ir := lowerInstructionToBytecodeIR(raw, source)
			if got := ir.opcodeValue(); got != op {
				t.Fatalf("%s opcode=%v", opcodeName(op), got)
			}
			if got := ir.sourceSpan(); got != source {
				t.Fatalf("%s source=%+v, want %+v", opcodeName(op), got, source)
			}
			wantKinds := expectedIRAccessorKinds(op)
			for slot := bytecodeIROperandSlotA; slot <= bytecodeIROperandSlotD; slot++ {
				if got := ir.operandKind(slot); got != wantKinds[slot] {
					t.Errorf("%s slot %d kind=%v, want %v", opcodeName(op), slot, got, wantKinds[slot])
				}
			}
			assembled := assembleBytecodeIRInstruction(ir)
			if assembled != raw {
				t.Errorf("%s assembled=%+v, want %+v", opcodeName(op), assembled, raw)
			}
		}
	}
}

func TestBytecodeIRAccessorSetterAndIterator(t *testing.T) {
	raw := instruction{op: opCall, a: 4, b: 2, c: -3, d: -1}
	ir := lowerInstructionToBytecodeIR(raw, sourceRange{})
	for slot := bytecodeIROperandSlotA; slot <= bytecodeIROperandSlotD; slot++ {
		if !ir.setOperandValue(slot, ir.operandValue(slot)+10) {
			t.Fatalf("set slot %d rejected", slot)
		}
	}
	for slot := bytecodeIROperandSlotA; slot <= bytecodeIROperandSlotD; slot++ {
		if got := ir.operandValue(slot); got != rawOperandValue(raw, slot)+10 {
			t.Errorf("slot %d value=%d", slot, got)
		}
	}
	if ir.setOperandValue(bytecodeIROperandSlot(99), 1) {
		t.Fatal("invalid slot accepted")
	}
	if ir.setOperandValue(bytecodeIROperandSlotC, 1) != true {
		t.Fatal("used slot rejected")
	}
	unused := lowerInstructionToBytecodeIR(instruction{op: opReturnOne, a: 1}, sourceRange{})
	if unused.setOperandValue(bytecodeIROperandSlotB, 1) {
		t.Fatal("unused slot accepted")
	}
	var nilIR *bytecodeIRInstruction
	if nilIR.setOperandValue(bytecodeIROperandSlotA, 1) {
		t.Fatal("nil IR accepted")
	}

	iterator := ir.operandsIter()
	var gotSlots []bytecodeIROperandSlot
	for slot, kind, value, ok := iterator.next(); ok; slot, kind, value, ok = iterator.next() {
		gotSlots = append(gotSlots, slot)
		if kind != ir.operandKind(slot) || value != ir.operandValue(slot) {
			t.Fatalf("iterator slot %d mismatch", slot)
		}
	}
	if !reflect.DeepEqual(gotSlots, []bytecodeIROperandSlot{bytecodeIROperandSlotA, bytecodeIROperandSlotB, bytecodeIROperandSlotC, bytecodeIROperandSlotD}) {
		t.Fatalf("iterator slots=%v", gotSlots)
	}
	short := lowerInstructionToBytecodeIR(instruction{op: opLoadConst, a: 1, b: 2}, sourceRange{})
	shortIterator := short.operandsIter()
	var shortSlots []bytecodeIROperandSlot
	for slot, _, _, ok := shortIterator.next(); ok; slot, _, _, ok = shortIterator.next() {
		shortSlots = append(shortSlots, slot)
	}
	if !reflect.DeepEqual(shortSlots, []bytecodeIROperandSlot{bytecodeIROperandSlotA, bytecodeIROperandSlotB}) {
		t.Fatalf("iterator included unused slots: %v", shortSlots)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		iterator := ir.operandsIter()
		for _, _, _, ok := iterator.next(); ok; _, _, _, ok = iterator.next() {
		}
	}); allocs != 0 {
		t.Fatalf("iterator allocations=%v", allocs)
	}
}

func rawOperandValue(raw instruction, slot bytecodeIROperandSlot) int {
	switch slot {
	case bytecodeIROperandSlotA:
		return raw.a
	case bytecodeIROperandSlotB:
		return raw.b
	case bytecodeIROperandSlotC:
		return raw.c
	case bytecodeIROperandSlotD:
		return raw.d
	default:
		return 0
	}
}

func sampleInstructionForIRAccessors(op opcode, variant int) instruction {
	values := [4]int{1, 2, 3, 4}
	if variant != 0 {
		values = [4]int{-11, -22, -33, -44}
	}
	ins := instruction{op: op}
	kinds := expectedIRAccessorKinds(op)
	if kinds[bytecodeIROperandSlotA] != bytecodeOperandUnused {
		ins.a = values[0]
	}
	if kinds[bytecodeIROperandSlotB] != bytecodeOperandUnused {
		ins.b = values[1]
	}
	if kinds[bytecodeIROperandSlotC] != bytecodeOperandUnused {
		ins.c = values[2]
	}
	if kinds[bytecodeIROperandSlotD] != bytecodeOperandUnused {
		ins.d = values[3]
	}
	switch op {
	case opVararg:
		ins.b = -1 - variant
	case opCall:
		ins.c, ins.d = -3-variant, -1-variant
	case opReturn:
		ins.b = -1 - variant
	}
	return ins
}

func expectedIRAccessorKinds(op opcode) [4]bytecodeOperandKind {
	u, r, c, p, v, j, n, g, id := bytecodeOperandUnused, bytecodeOperandRegister, bytecodeOperandConstant, bytecodeOperandPrototype, bytecodeOperandUpvalue, bytecodeOperandJumpTarget, bytecodeOperandCount, bytecodeOperandGlobalSlot, bytecodeOperandNativeID
	switch op {
	case opLoadConst:
		return [4]bytecodeOperandKind{r, c, u, u}
	case opLoadGlobal:
		return [4]bytecodeOperandKind{r, c, g, u}
	case opSetGlobal:
		return [4]bytecodeOperandKind{c, r, g, u}
	case opMove:
		return [4]bytecodeOperandKind{r, r, u, u}
	case opNewTable:
		return [4]bytecodeOperandKind{r, n, n, u}
	case opSetField, opSetStringField:
		return [4]bytecodeOperandKind{r, c, r, u}
	case opSetStringFieldIndex:
		return [4]bytecodeOperandKind{r, c, r, r}
	case opGetStringField:
		return [4]bytecodeOperandKind{r, r, c, u}
	case opGetStringFieldIndex:
		return [4]bytecodeOperandKind{r, r, c, r}
	case opSetIndex, opGetIndex:
		return [4]bytecodeOperandKind{r, r, r, u}
	case opClosure:
		return [4]bytecodeOperandKind{r, p, u, u}
	case opGetUpvalue:
		return [4]bytecodeOperandKind{r, v, u, u}
	case opSetUpvalue:
		return [4]bytecodeOperandKind{v, r, u, u}
	case opVararg:
		return [4]bytecodeOperandKind{r, n, u, u}
	case opPrepareIter:
		return [4]bytecodeOperandKind{r, r, r, u}
	case opArrayNext:
		return [4]bytecodeOperandKind{r, r, r, n}
	case opArrayNextJump2:
		return [4]bytecodeOperandKind{r, r, r, j}
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat, opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		return [4]bytecodeOperandKind{r, r, r, u}
	case opConcatChain:
		return [4]bytecodeOperandKind{r, r, n, u}
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return [4]bytecodeOperandKind{r, r, c, u}
	case opNeg, opLen:
		return [4]bytecodeOperandKind{r, r, u, u}
	case opNumericForCheck:
		return [4]bytecodeOperandKind{r, r, r, j}
	case opNumericForLoop:
		return [4]bytecodeOperandKind{r, r, u, j}
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
		return [4]bytecodeOperandKind{r, c, u, j}
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		return [4]bytecodeOperandKind{r, r, u, j}
	case opJumpIfTableHasMetatable:
		return [4]bytecodeOperandKind{r, u, u, j}
	case opFastCall:
		return [4]bytecodeOperandKind{r, id, n, n}
	case opCall, opCallOne:
		return [4]bytecodeOperandKind{r, r, n, n}
	case opCallLocalOne:
		return [4]bytecodeOperandKind{r, r, r, n}
	case opCallUpvalueOne:
		return [4]bytecodeOperandKind{r, v, r, n}
	case opCallMethodOne:
		return [4]bytecodeOperandKind{r, r, c, n}
	case opJumpIfFalse:
		return [4]bytecodeOperandKind{r, j, u, u}
	case opJump:
		return [4]bytecodeOperandKind{u, j, u, u}
	case opReturnOne:
		return [4]bytecodeOperandKind{r, u, u, u}
	case opReturn:
		return [4]bytecodeOperandKind{r, n, u, u}
	default:
		panic("missing opcode accessor test shape")
	}
}
