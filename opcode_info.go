package ember

type opcodeControlFlowKind int

const (
	opcodeControlNone opcodeControlFlowKind = iota
	opcodeControlJump
	opcodeControlBranch
	opcodeControlReturn
)

type opcodeJumpTargetSlot int

const (
	opcodeJumpTargetNone opcodeJumpTargetSlot = iota
	opcodeJumpTargetB
	opcodeJumpTargetD
)

func opcodeControlFlow(op opcode) opcodeControlFlowKind {
	switch op {
	case opJump:
		return opcodeControlJump
	case opJumpIfFalse,
		opNumericForCheck,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfModKNotEqualK,
		opJumpIfStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualK,
		opJumpIfStringFieldNotGreaterK,
		opJumpIfStringFieldGreaterK,
		opJumpIfStringFieldFalse:
		return opcodeControlBranch
	case opReturnOne, opReturn:
		return opcodeControlReturn
	default:
		return opcodeControlNone
	}
}

func opcodeTransfersControl(op opcode) bool {
	return opcodeControlFlow(op) != opcodeControlNone
}

func opcodeJumpTarget(op opcode) opcodeJumpTargetSlot {
	switch op {
	case opJump, opJumpIfFalse:
		return opcodeJumpTargetB
	case opNumericForCheck,
		opJumpIfNotEqualK,
		opJumpIfNotLessK,
		opJumpIfModKNotEqualK,
		opJumpIfStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualK,
		opJumpIfStringFieldNotGreaterK,
		opJumpIfStringFieldGreaterK,
		opJumpIfStringFieldFalse:
		return opcodeJumpTargetD
	default:
		return opcodeJumpTargetNone
	}
}

func opcodeHasJumpTarget(op opcode) bool {
	return opcodeJumpTarget(op) != opcodeJumpTargetNone
}

func instructionJumpTarget(ins instruction) (int, bool) {
	switch opcodeJumpTarget(ins.op) {
	case opcodeJumpTargetB:
		return ins.b, true
	case opcodeJumpTargetD:
		return ins.d, true
	default:
		return 0, false
	}
}
