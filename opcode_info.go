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

func opcodeEffect(op opcode) opcodeEffects {
	meta, ok := opcodeMetadata(op)
	if !ok {
		return opcodeEffects{}
	}
	return meta.effects
}

func opcodeMayCall(op opcode) bool {
	return opcodeEffect(op).invokesScriptOrHostCode
}

func opcodeMayYield(op opcode) bool {
	return opcodeEffect(op).mayYield
}

func opcodeReadsTable(op opcode) bool {
	return opcodeEffect(op).readsTables
}

func opcodeWritesTable(op opcode) bool {
	return opcodeEffect(op).writesTables
}

func opcodeReadsGlobal(op opcode) bool {
	return opcodeEffect(op).readsGlobals
}

func opcodeWritesGlobal(op opcode) bool {
	return opcodeEffect(op).writesGlobals
}

func opcodeAllocates(op opcode) bool {
	return opcodeEffect(op).allocatesOrObservesIdentity
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
