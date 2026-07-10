package ember

type instructionRegisterAccess uint8

const (
	instructionRegisterRead instructionRegisterAccess = 1 << iota
	instructionRegisterWrite
	instructionRegisterReadWrite = instructionRegisterRead | instructionRegisterWrite
)

func (access instructionRegisterAccess) matches(reads bool, writes bool) bool {
	return access&instructionRegisterRead != 0 && reads || access&instructionRegisterWrite != 0 && writes
}

func (access instructionRegisterAccess) String() string {
	switch access {
	case instructionRegisterRead:
		return "read"
	case instructionRegisterWrite:
		return "write"
	case instructionRegisterReadWrite:
		return "read/write"
	default:
		return "none"
	}
}

type instructionRegisterIterator struct {
	ins      instruction
	access   instructionRegisterAccess
	nextReg  int
	limitReg int
}

func instructionRegisters(ins instruction, access instructionRegisterAccess) instructionRegisterIterator {
	return instructionRegisterIterator{
		ins:      ins,
		access:   access,
		limitReg: instructionRegisterLimit(ins),
	}
}

func (iterator *instructionRegisterIterator) next() (int, bool) {
	for iterator.nextReg < iterator.limitReg {
		register := iterator.nextReg
		iterator.nextReg++
		if iterator.access.matches(
			instructionReadsRegister(iterator.ins, register),
			instructionWritesRegister(iterator.ins, register),
		) {
			return register, true
		}
	}
	return 0, false
}

func instructionRegisterLimit(ins instruction) int {
	limit := 0
	if meta, ok := opcodeMetadata(ins.op); ok {
		operands := [...]struct {
			kind  bytecodeOperandKind
			value int
		}{
			{kind: meta.operands.a, value: ins.a},
			{kind: meta.operands.b, value: ins.b},
			{kind: meta.operands.c, value: ins.c},
			{kind: meta.operands.d, value: ins.d},
		}
		for _, operand := range operands {
			if operand.kind == bytecodeOperandRegister {
				limit = maxRegisterLimit(limit, operand.value+1)
			}
		}
	}

	switch ins.op {
	case opCall, opCallOne:
		argumentCount := ins.c
		if argumentCount < 0 {
			argumentCount = -argumentCount - 1
		}
		limit = maxRegisterLimit(limit, ins.b+argumentCount+1)
		if ins.d > 0 {
			limit = maxRegisterLimit(limit, ins.a+ins.d)
		}
	case opCallLocalOne, opCallUpvalueOne:
		limit = maxRegisterLimit(limit, ins.c+ins.d)
	case opCallMethodOne:
		limit = maxRegisterLimit(limit, ins.a+ins.d+2)
	case opCoroutineResume:
		limit = maxRegisterLimit(limit, ins.a+ins.b+1)
	case opFastCall:
		limit = maxRegisterLimit(limit, ins.a+maxRegisterLimit(ins.c, ins.d))
	case opArrayNext:
		limit = maxRegisterLimit(limit, ins.a+ins.d)
	case opArrayNextJump2:
		limit = maxRegisterLimit(limit, ins.a+2)
	case opVararg:
		if ins.b > 0 {
			limit = maxRegisterLimit(limit, ins.a+ins.b)
		}
	case opConcatChain:
		limit = maxRegisterLimit(limit, ins.b+ins.c)
	case opReturn:
		count := ins.b
		if count < 0 {
			count = -count - 1
		}
		limit = maxRegisterLimit(limit, ins.a+count)
	}
	return limit
}

func maxRegisterLimit(current int, candidate int) int {
	if candidate > current {
		return candidate
	}
	return current
}

func instructionReadsRegister(ins instruction, register int) bool {
	switch ins.op {
	case opMove:
		return ins.b == register
	case opSetGlobal:
		return ins.b == register
	case opSetField, opSetStringField:
		return ins.a == register || ins.c == register
	case opGetField, opGetStringField:
		return ins.b == register
	case opSetStringFieldIndex:
		return ins.a == register || ins.c == register || ins.d == register
	case opGetStringFieldIndex:
		return ins.b == register || ins.d == register
	case opAddStringField, opSubStringField:
		return ins.a == register || ins.c == register
	case opSetIndex:
		return ins.a == register || ins.b == register || ins.c == register
	case opGetIndex:
		return ins.b == register || ins.c == register
	case opSetUpvalue:
		return ins.b == register
	case opPrepareIter:
		return ins.a == register
	case opArrayNext, opArrayNextJump2:
		return ins.a == register || ins.b == register || ins.c == register
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat,
		opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		return ins.b == register || ins.c == register
	case opConcatChain:
		return register >= ins.b && register < ins.b+ins.c
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return ins.b == register
	case opNumericForCheck:
		return ins.a == register || ins.b == register || ins.c == register
	case opNumericForLoop:
		return ins.a == register || ins.b == register
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		return ins.a == register || ins.b == register
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK,
		opJumpIfModKNotEqualK,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK, opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK,
		opJumpIfStringFieldFalse, opJumpIfStringFieldNil, opJumpIfStringFieldTrue, opJumpIfStringFieldNotNil:
		return ins.a == register
	case opJumpIfStringFieldNotGreaterR:
		return ins.a == register || ins.c == register
	case opNeg, opLen:
		return ins.b == register
	case opCoroutineResume:
		return register >= ins.a && register <= ins.a+ins.b
	case opFastCall:
		return register >= ins.a && register < ins.a+ins.c
	case opCall, opCallOne:
		if ins.b == register {
			return true
		}
		if ins.c < 0 {
			prefixCount := -ins.c - 1
			return register > ins.b && register <= ins.b+prefixCount
		}
		return register > ins.b && register <= ins.b+ins.c
	case opCallLocalOne:
		return ins.b == register || register >= ins.c && register < ins.c+ins.d
	case opCallUpvalueOne:
		return register >= ins.c && register < ins.c+ins.d
	case opCallMethodOne:
		return ins.b == register || register >= ins.a+2 && register <= ins.a+1+ins.d
	case opJumpIfFalse, opReturnOne:
		return ins.a == register
	case opReturn:
		if ins.b < 0 {
			prefixCount := -ins.b - 1
			return register >= ins.a && register < ins.a+prefixCount
		}
		return register >= ins.a && register < ins.a+ins.b
	default:
		return false
	}
}

func instructionWritesRegister(ins instruction, register int) bool {
	switch ins.op {
	case opLoadConst, opLoadGlobal, opMove, opNewTable, opGetField, opGetStringField, opGetStringFieldIndex,
		opClosure, opGetUpvalue, opVararg, opAdd, opSub, opMul, opDiv, opMod,
		opIDiv, opPow, opNeg, opLen, opConcat, opConcatChain, opEqual, opNotEqual, opLess,
		opLessEqual, opGreater, opGreaterEqual, opAddK, opSubK, opMulK,
		opDivK, opModK, opIDivK, opCoroutineResume, opFastCall:
		if ins.op == opVararg && ins.b > 0 {
			return register >= ins.a && register < ins.a+ins.b
		}
		return ins.a == register
	case opNumericForLoop:
		return register == ins.a
	case opPrepareIter:
		return ins.a == register || ins.b == register || ins.c == register
	case opArrayNext:
		return register >= ins.a && register < ins.a+ins.d
	case opArrayNextJump2:
		return register == ins.a || register == ins.a+1
	case opCall:
		resultCount := ins.d
		if resultCount == 0 {
			resultCount = 1
		}
		if resultCount < 0 {
			return register >= ins.a
		}
		return register >= ins.a && register < ins.a+resultCount
	case opCallOne, opCallLocalOne, opCallUpvalueOne:
		return register == ins.a
	case opCallMethodOne:
		return register == ins.a || register == ins.a+1
	default:
		return false
	}
}
