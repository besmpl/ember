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
	meta, ok := opcodeMetadata(op)
	if !ok {
		return opcodeControlNone
	}
	return meta.controlFlow
}

func opcodeTransfersControl(op opcode) bool {
	return opcodeControlFlow(op) != opcodeControlNone
}

func opcodeJumpTarget(op opcode) opcodeJumpTargetSlot {
	meta, ok := opcodeMetadata(op)
	if !ok {
		return opcodeJumpTargetNone
	}
	return meta.jumpTarget
}

func opcodeHasJumpTarget(op opcode) bool {
	return opcodeJumpTarget(op) != opcodeJumpTargetNone
}

func opcodeMayCall(op opcode) bool {
	meta, ok := opcodeMetadata(op)
	return ok && meta.mayCall
}

func opcodeMayYield(op opcode) bool {
	meta, ok := opcodeMetadata(op)
	return ok && meta.mayYield
}

func opcodeReadsTable(op opcode) bool {
	meta, ok := opcodeMetadata(op)
	return ok && meta.readsTable
}

func opcodeWritesTable(op opcode) bool {
	meta, ok := opcodeMetadata(op)
	return ok && meta.writesTable
}

func opcodeReadsGlobal(op opcode) bool {
	meta, ok := opcodeMetadata(op)
	return ok && meta.readsGlobal
}

func opcodeWritesGlobal(op opcode) bool {
	meta, ok := opcodeMetadata(op)
	return ok && meta.writesGlobal
}

func opcodeAllocates(op opcode) bool {
	meta, ok := opcodeMetadata(op)
	return ok && meta.allocates
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
