package ember

import "math"

type optimizationCategory string

const (
	optimizationHIRSimplify      optimizationCategory = "hir-simplify"
	optimizationBytecodePeephole optimizationCategory = "bytecode-peephole"
)

type compilerOptions struct {
	optimizations optimizationOptions
}

type optimizationOptions struct {
	disableAll         bool
	disabledCategories map[optimizationCategory]bool
}

func defaultCompilerOptions() compilerOptions {
	return compilerOptions{}
}

func (o optimizationOptions) enabled(category optimizationCategory) bool {
	if o.disableAll {
		return false
	}
	return !o.disabledCategories[category]
}

func optimizeBytecodeIR(ir []bytecodeIRInstruction, options optimizationOptions) []bytecodeIRInstruction {
	return optimizeBytecodeIRWithConstants(ir, nil, options)
}

func optimizeBytecodeIRWithConstants(ir []bytecodeIRInstruction, constants []Value, options optimizationOptions) []bytecodeIRInstruction {
	return optimizeBytecodeIRWithFacts(ir, bytecodeIROptimizationFacts{constants: constants}, options)
}

type bytecodeIROptimizationFacts struct {
	constants         []Value
	capturedRegisters []bool
}

func optimizeBytecodeIRWithFacts(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts, options optimizationOptions) []bytecodeIRInstruction {
	if !options.enabled(optimizationBytecodePeephole) {
		return append([]bytecodeIRInstruction(nil), ir...)
	}
	function := newFunctionIR(append([]bytecodeIRInstruction(nil), ir...))
	function.replace(applyBytecodeIRRemovalSet(
		function.instructions,
		bytecodeIRPeepholeRemovalSet(function.instructions, assembleBytecodeIRRaw(function.instructions), function.currentAnalysis()),
	))
	function.replace(simplifyBytecodeIRControlFlow(function.instructions, facts))
	function.replace(fuseBytecodeIRRowFieldArrayIndex(function.instructions))
	function.replace(propagateBytecodeIRSingleUseMoves(function.instructions, function.currentAnalysis()))
	function.replace(coalesceBytecodeIRMoveProducers(function.instructions, facts.capturedRegisters, function.currentAnalysis()))
	function.replace(hoistBytecodeIRLoopInvariantHeaderLoads(function.instructions))
	function.replace(applyBytecodeIRRemovalSet(
		function.instructions,
		bytecodeIRDeadCodeRemovalSet(function.instructions, facts, function.currentAnalysis()),
	))
	function.replace(simplifyBytecodeIRControlFlow(function.instructions, facts))
	return function.instructions
}

func applyBytecodeIRRemovalSet(ir []bytecodeIRInstruction, remove []bool) []bytecodeIRInstruction {
	if !hasRemovedInstructions(remove) {
		return ir
	}
	optimized := make([]bytecodeIRInstruction, 0, len(ir))
	for i := 0; i < len(ir); i++ {
		if remove[i] {
			continue
		}
		optimized = append(optimized, ir[i])
	}
	remapBytecodeIRJumpTargets(optimized, oldPCToNewPC(remove))
	return optimized
}

func fuseBytecodeIRRowFieldArrayIndex(ir []bytecodeIRInstruction) []bytecodeIRInstruction {
	return ir
}

func bytecodeIRDeadCodeRemovalSet(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts, analysis *functionAnalysis) []bool {
	code := assembleBytecodeIRRaw(ir)
	remove := make([]bool, len(ir))
	numberFacts := bytecodeIRNumberFactsBefore(code, facts, analysis.blocks)
	for _, live := range analysis.liveness {
		if !bytecodeIRBlockAllowsDeadCodeCleanup(code, live.block) {
			continue
		}
		liveRegisters := live.liveOut.copy()
		for pc := live.block.end - 1; pc >= live.block.start; pc-- {
			ins := code[pc]
			if instructionWritesOnlyDeadRegisters(ins, liveRegisters) && instructionCanRemoveWhenResultDead(ins, numberFacts[pc], facts) {
				remove[pc] = true
				continue
			}
			writes := instructionRegisters(ins, instructionRegisterWrite)
			for register, ok := writes.next(); ok; register, ok = writes.next() {
				liveRegisters.remove(register)
			}
			reads := instructionRegisters(ins, instructionRegisterRead)
			for register, ok := reads.next(); ok; register, ok = reads.next() {
				liveRegisters.add(register)
			}
		}
	}
	return remove
}

func bytecodeIRBlockAllowsDeadCodeCleanup(code []instruction, block bytecodeIRBlock) bool {
	for pc := block.start; pc < block.end; pc++ {
		if !instructionAllowsDeadCodeCleanupInBlock(code[pc]) {
			return false
		}
	}
	return true
}

func instructionAllowsDeadCodeCleanupInBlock(ins instruction) bool {
	switch ins.op {
	case opLoadConst, opMove, opJumpIfFalse, opJump, opReturnOne, opReturn,
		opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opNeg,
		opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
		opCoroutineResume, opFastCall,
		opPrepareIter, opArrayNext, opArrayNextJump2,
		opNumericForCheck, opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK,
		opJumpIfLessK, opJumpIfGreaterK, opJumpIfNotLess, opJumpIfNotGreater,
		opJumpIfLess, opJumpIfGreater, opJumpIfModKNotEqualK,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK,
		opJumpIfStringFieldFalse, opJumpIfStringFieldNil,
		opJumpIfStringFieldTrue, opJumpIfStringFieldNotNil,
		opGetField, opSetField, opGetIndex, opSetIndex, opGetStringField, opSetStringField,
		opGetStringFieldIndex, opSetStringFieldIndex,
		opAddStringField, opSubStringField:
		return true
	case opCall:
		return true
	case opCallOne:
		return ins.c >= 0 && ins.d >= 0
	case opVararg:
		return true
	default:
		return false
	}
}

func instructionWritesOnlyDeadRegisters(ins instruction, liveRegisters registerSet) bool {
	hasWrite := false
	writes := instructionRegisters(ins, instructionRegisterWrite)
	for register, ok := writes.next(); ok; register, ok = writes.next() {
		hasWrite = true
		if liveRegisters.contains(register) {
			return false
		}
	}
	return hasWrite
}

func instructionCanRemoveWhenResultDead(ins instruction, numberFacts registerSet, facts bytecodeIROptimizationFacts) bool {
	effect := opcodeEffect(ins.op)
	if !effect.classified ||
		opcodeTransfersControl(ins.op) ||
		effect.invokesScriptOrHostCode ||
		effect.mayYield ||
		effect.mayError ||
		effect.allocatesOrObservesIdentity ||
		effect.readsGlobals || effect.writesGlobals ||
		effect.readsUpvalues || effect.writesUpvalues ||
		effect.readsTables || effect.writesTables ||
		effect.readsUnknownHeap || effect.writesUnknownHeap {
		return false
	}
	switch ins.op {
	case opLoadConst, opMove:
		return true
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		return numberFacts.contains(ins.b) && numberFacts.contains(ins.c)
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return numberFacts.contains(ins.b) && constantIsNumber(facts, ins.c)
	case opNeg:
		return numberFacts.contains(ins.b)
	default:
		return false
	}
}

func bytecodeIRNumberFactsBefore(code []instruction, facts bytecodeIROptimizationFacts, blocks []bytecodeIRBlock) []registerSet {
	factsBefore := make([]registerSet, len(code))
	for _, block := range blocks {
		numberFacts := registerSet{}
		for pc := block.start; pc < block.end; pc++ {
			factsBefore[pc] = numberFacts.copy()
			applyInstructionNumberFacts(numberFacts, code[pc], facts)
		}
	}
	return factsBefore
}

func applyInstructionNumberFacts(numberFacts registerSet, ins instruction, facts bytecodeIROptimizationFacts) {
	if instructionClearsAllNumberFacts(ins) {
		numberFacts.clear()
		return
	}
	producesNumber := instructionProducesNumber(ins, numberFacts, facts)
	writes := instructionRegisters(ins, instructionRegisterWrite)
	for register, ok := writes.next(); ok; register, ok = writes.next() {
		numberFacts.remove(register)
	}
	if producesNumber {
		numberFacts.add(ins.a)
	}
}

func instructionClearsAllNumberFacts(ins instruction) bool {
	return (ins.op == opCall && ins.d < 0) || (ins.op == opVararg && ins.b < 0)
}

func instructionProducesNumber(ins instruction, numberFacts registerSet, facts bytecodeIROptimizationFacts) bool {
	switch ins.op {
	case opLoadConst:
		return constantIsNumber(facts, ins.b)
	case opMove:
		return numberFacts.contains(ins.b)
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		return numberFacts.contains(ins.b) && numberFacts.contains(ins.c)
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return numberFacts.contains(ins.b) && constantIsNumber(facts, ins.c)
	case opNeg:
		return numberFacts.contains(ins.b)
	default:
		return false
	}
}

func constantIsNumber(facts bytecodeIROptimizationFacts, index int) bool {
	return index >= 0 && index < len(facts.constants) && facts.constants[index].kind == NumberKind
}

func bytecodeIRPeepholeRemovalSet(ir []bytecodeIRInstruction, code []instruction, analysis *functionAnalysis) []bool {
	remove := make([]bool, len(ir))
	for _, live := range analysis.liveness {
		block := live.block
		for pc := block.start; pc < block.end; pc++ {
			ins := code[pc]
			if ins.op == opMove && ins.a == ins.b {
				remove[pc] = true
				continue
			}
			if pc+1 < block.end && isDeadMoveRoundTripInBlock(code, pc, block.end, live.liveOut) {
				remove[pc] = true
				remove[pc+1] = true
				pc++
			}
		}
	}
	return remove
}

func simplifyBytecodeIRControlFlow(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts) []bytecodeIRInstruction {
	if len(ir) == 0 {
		return ir
	}
	if !bytecodeIRHasControlFlowSimplificationWork(ir) {
		return ir
	}
	optimized := append([]bytecodeIRInstruction(nil), ir...)
	foldBytecodeIRConstantBranches(optimized, facts)
	threadBytecodeIRJumpTargetsMemoized(optimized)
	remove := bytecodeIRReachabilityRemovalSet(optimized)
	markBytecodeIRJumpsToNextSurvivor(optimized, remove)
	if hasRemovedInstructions(remove) {
		return applyBytecodeIRRemovalSet(optimized, remove)
	}
	return optimized
}

func bytecodeIRHasControlFlowSimplificationWork(ir []bytecodeIRInstruction) bool {
	for pc, ins := range ir {
		switch opcodeControlFlow(ins.op) {
		case opcodeControlJump, opcodeControlBranch:
			return true
		case opcodeControlReturn:
			if pc+1 < len(ir) {
				return true
			}
		}
	}
	return false
}

func threadBytecodeIRJumpTargetsMemoized(ir []bytecodeIRInstruction) {
	resolver := bytecodeIRJumpResolver{
		ir:      ir,
		state:   make([]byte, len(ir)),
		targets: make([]int, len(ir)),
		valid:   make([]bool, len(ir)),
	}
	for pc := range ir {
		target, ok := bytecodeIRJumpTarget(ir[pc])
		if !ok {
			continue
		}
		threaded, ok := resolver.resolve(target)
		if ok && threaded != target {
			setBytecodeIRJumpTarget(&ir[pc], threaded)
		}
	}
}

type bytecodeIRJumpResolver struct {
	ir      []bytecodeIRInstruction
	state   []byte
	targets []int
	valid   []bool
}

func (resolver *bytecodeIRJumpResolver) resolve(pc int) (int, bool) {
	if pc < 0 || pc >= len(resolver.ir) {
		return pc, false
	}
	switch resolver.state[pc] {
	case 1:
		return pc, false
	case 2:
		return resolver.targets[pc], resolver.valid[pc]
	}
	resolver.state[pc] = 1
	target := pc
	valid := true
	if resolver.ir[pc].op == opJump {
		next, ok := bytecodeIRJumpTarget(resolver.ir[pc])
		if !ok {
			valid = false
		} else {
			target, valid = resolver.resolve(next)
		}
	}
	resolver.state[pc] = 2
	resolver.targets[pc] = target
	resolver.valid[pc] = valid
	return target, valid
}

func foldBytecodeIRConstantBranches(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts) bool {
	if len(facts.constants) == 0 || !bytecodeIRHasJumpIfFalse(ir) {
		return false
	}
	constantFacts := bytecodeIRConstantFactsBefore(ir, facts)
	changed := false
	for pc, ins := range ir {
		if ins.op != opJumpIfFalse {
			continue
		}
		constant, ok := constantFacts[pc][ins.operands.a.value]
		if !ok || constant < 0 || constant >= len(facts.constants) {
			continue
		}
		target, ok := bytecodeIRJumpTarget(ins)
		if !ok {
			continue
		}
		if facts.constants[constant].truthy() {
			target = pc + 1
		}
		ir[pc] = lowerInstructionToBytecodeIR(instruction{op: opJump, b: target}, ins.source)
		changed = true
	}
	return changed
}

func bytecodeIRHasJumpIfFalse(ir []bytecodeIRInstruction) bool {
	for _, ins := range ir {
		if ins.op == opJumpIfFalse {
			return true
		}
	}
	return false
}

func bytecodeIRConstantFactsBefore(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts) []map[int]int {
	code := assembleBytecodeIRRaw(ir)
	factsBefore := make([]map[int]int, len(ir))
	for _, block := range bytecodeIRBlockOrder(ir) {
		registerConstants := make(map[int]int)
		for pc := block.start; pc < block.end; pc++ {
			factsBefore[pc] = copyRegisterConstants(registerConstants)
			applyInstructionConstantFacts(registerConstants, code[pc], facts)
		}
	}
	for pc := range factsBefore {
		if factsBefore[pc] == nil {
			factsBefore[pc] = make(map[int]int)
		}
	}
	return factsBefore
}

func applyInstructionConstantFacts(registerConstants map[int]int, ins instruction, facts bytecodeIROptimizationFacts) {
	if instructionClearsAllNumberFacts(ins) {
		clear(registerConstants)
		return
	}
	sourceConstant, sourceKnown := registerConstants[ins.b]
	writes := instructionRegisters(ins, instructionRegisterWrite)
	for register, ok := writes.next(); ok; register, ok = writes.next() {
		delete(registerConstants, register)
	}
	if opcodeMayCall(ins.op) {
		for register := range registerConstants {
			if register >= 0 && register < len(facts.capturedRegisters) && facts.capturedRegisters[register] {
				delete(registerConstants, register)
			}
		}
	}
	switch ins.op {
	case opLoadConst:
		registerConstants[ins.a] = ins.b
	case opMove:
		if sourceKnown {
			registerConstants[ins.a] = sourceConstant
		}
	}
}

func copyRegisterConstants(registerConstants map[int]int) map[int]int {
	copied := make(map[int]int, len(registerConstants))
	for register, constant := range registerConstants {
		copied[register] = constant
	}
	return copied
}

func bytecodeIRReachabilityRemovalSet(ir []bytecodeIRInstruction) []bool {
	remove := make([]bool, len(ir))
	if len(ir) == 0 {
		return remove
	}
	for pc := range remove {
		remove[pc] = true
	}
	worklist := make([]int, 1, len(ir))
	worklist[0] = 0
	for len(worklist) != 0 {
		last := len(worklist) - 1
		pc := worklist[last]
		worklist = worklist[:last]
		if pc < 0 || pc >= len(ir) || !remove[pc] {
			continue
		}
		remove[pc] = false
		ins := ir[pc]
		target, hasTarget := bytecodeIRJumpTarget(ins)
		switch opcodeControlFlow(ins.op) {
		case opcodeControlJump:
			if hasTarget {
				worklist = append(worklist, target)
			}
		case opcodeControlBranch:
			if hasTarget {
				worklist = append(worklist, target)
			}
			worklist = append(worklist, pc+1)
		case opcodeControlReturn:
		default:
			worklist = append(worklist, pc+1)
		}
	}
	return remove
}

func markBytecodeIRJumpsToNextSurvivor(ir []bytecodeIRInstruction, remove []bool) {
	if len(ir) == 0 || len(remove) != len(ir) {
		return
	}
	oldToNew := oldPCToNewPC(remove)
	for pc, ins := range ir {
		if remove[pc] || ins.op != opJump {
			continue
		}
		target, ok := bytecodeIRJumpTarget(ins)
		if !ok || target < 0 || target >= len(oldToNew) {
			continue
		}
		if oldToNew[target] == oldToNew[pc]+1 {
			remove[pc] = true
		}
	}
}

func setBytecodeIRJumpTarget(ins *bytecodeIRInstruction, target int) bool {
	switch opcodeJumpTarget(ins.op) {
	case opcodeJumpTargetB:
		if ins.operands.b.kind != bytecodeOperandJumpTarget {
			return false
		}
		ins.operands.b.value = target
		return true
	case opcodeJumpTargetD:
		if ins.operands.d.kind != bytecodeOperandJumpTarget {
			return false
		}
		ins.operands.d.value = target
		return true
	default:
		return false
	}
}

func propagateBytecodeIRSingleUseMoves(ir []bytecodeIRInstruction, analysis *functionAnalysis) []bytecodeIRInstruction {
	if len(ir) == 0 {
		return ir
	}
	optimized := append([]bytecodeIRInstruction(nil), ir...)
	code := assembleBytecodeIRRaw(optimized)
	remove := make([]bool, len(ir))
	for _, live := range analysis.liveness {
		block := live.block
		for pc := block.start; pc < block.end; pc++ {
			move := code[pc]
			if move.op != opMove || move.a == move.b {
				continue
			}
			usePC, ok := singleUseMoveReadPC(code, pc+1, block.end, live.liveOut, move.a, move.b)
			if !ok {
				continue
			}
			rewritten, ok := replaceInstructionReadRegister(code[usePC], move.a, move.b)
			if !ok {
				continue
			}
			code[usePC] = rewritten
			optimized[usePC] = lowerInstructionToBytecodeIR(rewritten, optimized[usePC].source)
			remove[pc] = true
		}
	}
	return applyBytecodeIRRemovalSet(optimized, remove)
}

func singleUseMoveReadPC(code []instruction, start int, end int, liveOut registerSet, target int, source int) (int, bool) {
	usePC := -1
	for pc := start; pc < end; pc++ {
		ins := code[pc]
		if usePC < 0 && instructionWritesRegister(ins, source) {
			return -1, false
		}
		if instructionReadsRegister(ins, target) {
			if usePC >= 0 {
				return -1, false
			}
			usePC = pc
		}
		if instructionWritesRegister(ins, target) {
			if usePC < 0 {
				return -1, false
			}
			return usePC, true
		}
	}
	if usePC < 0 || liveOut.contains(target) {
		return -1, false
	}
	return usePC, true
}

func replaceInstructionReadRegister(ins instruction, from int, to int) (instruction, bool) {
	replace := func(slot *int) bool {
		if *slot != from {
			return false
		}
		*slot = to
		return true
	}
	changed := false
	switch ins.op {
	case opJumpIfFalse, opReturnOne:
		changed = replace(&ins.a)
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat,
		opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		if ins.a == ins.b || ins.a == ins.c {
			return ins, false
		}
		changed = replace(&ins.b) || changed
		changed = replace(&ins.c) || changed
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		if ins.a == ins.b {
			return ins, false
		}
		changed = replace(&ins.b)
	case opReturn:
		if ins.b < 0 {
			return ins, false
		}
		if from >= ins.a && from < ins.a+ins.b {
			return ins, false
		}
	default:
		return ins, false
	}
	if !changed {
		return ins, false
	}
	return ins, true
}

func coalesceBytecodeIRMoveProducers(ir []bytecodeIRInstruction, capturedRegisters []bool, analysis *functionAnalysis) []bytecodeIRInstruction {
	if len(ir) < 2 {
		return ir
	}
	optimized := append([]bytecodeIRInstruction(nil), ir...)
	code := assembleBytecodeIRRaw(optimized)
	remove := make([]bool, len(ir))
	for _, live := range analysis.liveness {
		block := live.block
		for pc := block.start + 1; pc < block.end; pc++ {
			move := code[pc]
			if move.op != opMove || move.a == move.b {
				continue
			}
			if move.b >= 0 && move.b < len(capturedRegisters) && capturedRegisters[move.b] {
				continue
			}
			if !registerDeadAfterMoveInBlock(code, pc, block.end, live.liveOut, move.b) {
				continue
			}
			producerPC := pc - 1
			producer := code[producerPC]
			if instructionReadsRegister(producer, move.a) || instructionWritesRegister(producer, move.a) {
				continue
			}
			rewritten, ok := replaceInstructionWrittenRegister(producer, move.b, move.a)
			if !ok {
				continue
			}
			code[producerPC] = rewritten
			optimized[producerPC] = lowerInstructionToBytecodeIR(rewritten, optimized[producerPC].source)
			remove[pc] = true
		}
	}
	return applyBytecodeIRRemovalSet(optimized, remove)
}

func registerDeadAfterMoveInBlock(code []instruction, movePC int, blockEnd int, liveOut registerSet, register int) bool {
	if killed, known := registerKilledBeforeRead(code[movePC+1:blockEnd], register); known {
		return killed
	}
	return !liveOut.contains(register)
}

func replaceInstructionWrittenRegister(ins instruction, from int, to int) (instruction, bool) {
	if from == to {
		return ins, false
	}
	if !singleResultProducerCanRetarget(ins) || ins.a != from {
		return ins, false
	}
	if instructionReadsRegister(ins, to) {
		return ins, false
	}
	ins.a = to
	return ins, true
}

func singleResultProducerCanRetarget(ins instruction) bool {
	switch ins.op {
	case opLoadConst, opLoadGlobal, opMove,
		opNewTable, opClosure, opGetUpvalue,
		opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat,
		opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual,
		opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
		opNeg, opLen:
		return true
	default:
		return false
	}
}

func hoistBytecodeIRLoopInvariantHeaderLoads(ir []bytecodeIRInstruction) []bytecodeIRInstruction {
	if len(ir) < 3 {
		return ir
	}
	optimized := append([]bytecodeIRInstruction(nil), ir...)
	code := assembleBytecodeIRRaw(optimized)
	for loopEnd, backedge := range code {
		loopStart, ok := loopLocalPathBackedgeTarget(backedge, loopEnd)
		if !ok || loopStart < 1 || loopStart+1 >= loopEnd {
			continue
		}
		load := code[loopStart]
		if load.op != opGetStringField {
			continue
		}
		if !loopHeaderLoadHasNoMetatableGuard(code, loopStart, loopEnd, load.b) {
			continue
		}
		if loopHasInvariantHeaderLoadBarrier(code, loopStart, loopEnd, load) {
			continue
		}
		rewritten := backedge
		switch backedge.op {
		case opJump:
			rewritten.b = loopStart + 1
		case opNumericForLoop:
			rewritten.d = loopStart + 1
		default:
			continue
		}
		code[loopEnd] = rewritten
		optimized[loopEnd] = lowerInstructionToBytecodeIR(rewritten, optimized[loopEnd].source)
	}
	return optimized
}

func loopLocalPathBackedgeTarget(ins instruction, loopEnd int) (int, bool) {
	var target int
	switch ins.op {
	case opJump:
		target = ins.b
	case opNumericForLoop:
		target = ins.d
	default:
		return 0, false
	}
	return target, target >= 0 && target < loopEnd
}

func loopHeaderLoadHasNoMetatableGuard(code []instruction, loopStart int, loopEnd int, base int) bool {
	if loopStart <= 0 {
		return false
	}
	guard := code[loopStart-1]
	if guard.op != opJumpIfTableHasMetatable || guard.a != base {
		return false
	}
	target, ok := instructionJumpTarget(guard)
	return ok && target > loopEnd
}

func loopHasInvariantHeaderLoadBarrier(code []instruction, loopStart int, loopEnd int, load instruction) bool {
	for pc := loopStart + 1; pc < loopEnd; pc++ {
		ins := code[pc]
		effect := opcodeEffect(ins.op)
		if !effect.classified ||
			effect.invokesScriptOrHostCode || effect.mayYield || effect.mayError ||
			effect.allocatesOrObservesIdentity ||
			effect.writesGlobals || effect.writesUpvalues || effect.writesTables ||
			effect.readsUnknownHeap || effect.writesUnknownHeap {
			return true
		}
		if effect.readsTables {
			return true
		}
		if instructionWritesRegister(ins, load.a) || instructionWritesRegister(ins, load.b) {
			return true
		}
	}
	return false
}

func hasRemovedInstructions(remove []bool) bool {
	for _, removed := range remove {
		if removed {
			return true
		}
	}
	return false
}

func oldPCToNewPC(remove []bool) []int {
	remap := make([]int, len(remove)+1)
	next := 0
	for pc, removed := range remove {
		remap[pc] = next
		if !removed {
			next++
		}
	}
	remap[len(remove)] = next
	return remap
}

func remapBytecodeIRJumpTargets(ir []bytecodeIRInstruction, oldToNew []int) {
	for i := range ir {
		switch opcodeJumpTarget(ir[i].op) {
		case opcodeJumpTargetB:
			target := ir[i].operands.b
			if target.kind == bytecodeOperandJumpTarget && target.value >= 0 && target.value < len(oldToNew) {
				ir[i].operands.b.value = oldToNew[target.value]
			}
		case opcodeJumpTargetD:
			target := ir[i].operands.d
			if target.kind == bytecodeOperandJumpTarget && target.value >= 0 && target.value < len(oldToNew) {
				ir[i].operands.d.value = oldToNew[target.value]
			}
		}
	}
}

func isDeadMoveRoundTripInBlock(code []instruction, first int, blockEnd int, liveOut registerSet) bool {
	if first+1 >= blockEnd || !isDeadMoveRoundTripPair(code[first], code[first+1]) {
		return false
	}
	register := code[first].a
	if killed, known := registerKilledBeforeRead(code[first+2:blockEnd], register); known {
		return killed
	}
	return !liveOut.contains(register)
}

func isDeadMoveRoundTripPair(left instruction, right instruction) bool {
	if left.op != opMove || right.op != opMove {
		return false
	}
	return left.a == right.b && left.b == right.a && left.a != left.b
}

func registerKilledBeforeRead(code []instruction, register int) (bool, bool) {
	for _, ins := range code {
		if instructionReadsRegister(ins, register) {
			return false, true
		}
		if instructionWritesRegister(ins, register) {
			return true, true
		}
	}
	return false, false
}

func foldConstantExpression(expr expression) (Value, bool) {
	if len(expr.terms) != 1 {
		return NilValue(), false
	}
	and := expr.terms[0]
	if len(and.terms) != 1 {
		return NilValue(), false
	}
	comparison := and.terms[0]
	if comparison.op != "" || comparison.right != nil {
		return NilValue(), false
	}
	return foldConstantConcat(comparison.left)
}

func foldConstantConcat(expr concatExpression) (Value, bool) {
	value, ok := foldConstantAdditive(expr.first)
	if !ok {
		return NilValue(), false
	}
	if len(expr.rest) == 0 {
		return value, true
	}
	for _, part := range expr.rest {
		right, ok := foldConstantAdditive(part)
		if !ok {
			return NilValue(), false
		}
		text, err := valuesConcat(value, right)
		if err != nil {
			return NilValue(), false
		}
		value = StringValue(text)
	}
	return value, true
}

func foldConstantAdditive(expr additiveExpression) (Value, bool) {
	value, ok := foldConstantMultiplicative(expr.first)
	if !ok {
		return NilValue(), false
	}
	if len(expr.rest) == 0 {
		return value, true
	}
	left, ok := numericOperandValue(value)
	if !ok {
		return NilValue(), false
	}
	for _, part := range expr.rest {
		rightValue, ok := foldConstantMultiplicative(part.value)
		if !ok {
			return NilValue(), false
		}
		right, ok := numericOperandValue(rightValue)
		if !ok {
			return NilValue(), false
		}
		switch part.op {
		case additiveAdd:
			left += right
		case additiveSubtract:
			left -= right
		default:
			return NilValue(), false
		}
	}
	return NumberValue(left), true
}

func foldConstantMultiplicative(expr multiplicativeExpression) (Value, bool) {
	value, ok := foldConstantTerm(expr.first)
	if !ok {
		return NilValue(), false
	}
	if len(expr.rest) == 0 {
		return value, true
	}
	left, ok := numericOperandValue(value)
	if !ok {
		return NilValue(), false
	}
	for _, part := range expr.rest {
		rightValue, ok := foldConstantTerm(part.value)
		if !ok {
			return NilValue(), false
		}
		right, ok := numericOperandValue(rightValue)
		if !ok {
			return NilValue(), false
		}
		switch part.op {
		case multiplicativeMultiply:
			left *= right
		case multiplicativeDivide:
			left /= right
		case multiplicativeModulo:
			left = left - math.Floor(left/right)*right
		case multiplicativeFloorDiv:
			left = math.Floor(left / right)
		default:
			return NilValue(), false
		}
	}
	return NumberValue(left), true
}

func foldConstantTerm(expr term) (Value, bool) {
	if len(expr.selectors) != 0 {
		return NilValue(), false
	}
	if expr.power != nil {
		base, ok := foldConstantTerm(expr.power.base)
		if !ok {
			return NilValue(), false
		}
		exponent, ok := foldConstantTerm(expr.power.exponent)
		if !ok {
			return NilValue(), false
		}
		baseNumber, baseOK := numericOperandValue(base)
		exponentNumber, exponentOK := numericOperandValue(exponent)
		if !baseOK || !exponentOK {
			return NilValue(), false
		}
		return NumberValue(math.Pow(baseNumber, exponentNumber)), true
	}
	if expr.number != nil {
		return NumberValue(*expr.number), true
	}
	if expr.lit != nil {
		return *expr.lit, true
	}
	if expr.unaryNot != nil {
		value, ok := foldConstantTerm(*expr.unaryNot)
		if !ok {
			return NilValue(), false
		}
		return BoolValue(!value.truthy()), true
	}
	if expr.unaryMinus != nil {
		value, ok := foldConstantTerm(*expr.unaryMinus)
		if !ok {
			return NilValue(), false
		}
		number, ok := numericOperandValue(value)
		if !ok {
			return NilValue(), false
		}
		return NumberValue(-number), true
	}
	if expr.unaryLen != nil {
		return foldConstantLength(*expr.unaryLen)
	}
	if expr.group != nil {
		return foldConstantExpression(*expr.group)
	}
	return NilValue(), false
}

func foldConstantLength(expr term) (Value, bool) {
	if len(expr.selectors) != 0 {
		return NilValue(), false
	}
	if expr.lit != nil && expr.lit.kind == StringKind {
		return NumberValue(float64(len(expr.lit.stringText()))), true
	}
	if expr.table != nil {
		length, ok := foldConstantTableLength(*expr.table)
		if ok {
			return NumberValue(float64(length)), true
		}
	}
	if expr.group != nil {
		value, ok := foldConstantExpression(*expr.group)
		if ok && value.kind == StringKind {
			return NumberValue(float64(len(value.stringText()))), true
		}
	}
	return NilValue(), false
}

func foldConstantTableLength(table tableExpression) (int, bool) {
	if len(table.fields) == 0 {
		return 0, true
	}
	for index, field := range table.fields {
		if field.key != nil || field.name != "" || field.arrayIndex != index+1 {
			return 0, false
		}
		value, ok := foldConstantExpression(field.value)
		if !ok || value.kind == NilKind {
			return 0, false
		}
	}
	return len(table.fields), true
}

func foldNumberExpression(expr expression) (float64, bool) {
	if len(expr.terms) != 1 {
		return 0, false
	}
	and := expr.terms[0]
	if len(and.terms) != 1 {
		return 0, false
	}
	comparison := and.terms[0]
	if comparison.op != "" || comparison.right != nil {
		return 0, false
	}
	return foldNumberConcat(comparison.left)
}

func foldNumberConcat(expr concatExpression) (float64, bool) {
	if len(expr.rest) != 0 {
		return 0, false
	}
	return foldNumberAdditive(expr.first)
}

func foldNumberAdditive(expr additiveExpression) (float64, bool) {
	value, ok := foldNumberMultiplicative(expr.first)
	if !ok {
		return 0, false
	}
	for _, part := range expr.rest {
		right, ok := foldNumberMultiplicative(part.value)
		if !ok {
			return 0, false
		}
		switch part.op {
		case additiveAdd:
			value += right
		case additiveSubtract:
			value -= right
		default:
			return 0, false
		}
	}
	return value, true
}

func foldNumberMultiplicative(expr multiplicativeExpression) (float64, bool) {
	value, ok := foldNumberTerm(expr.first)
	if !ok {
		return 0, false
	}
	for _, part := range expr.rest {
		right, ok := foldNumberTerm(part.value)
		if !ok {
			return 0, false
		}
		switch part.op {
		case multiplicativeMultiply:
			value *= right
		case multiplicativeDivide:
			value /= right
		case multiplicativeModulo:
			value = value - math.Floor(value/right)*right
		case multiplicativeFloorDiv:
			value = math.Floor(value / right)
		default:
			return 0, false
		}
	}
	return value, true
}

func foldNumberTerm(expr term) (float64, bool) {
	if len(expr.selectors) != 0 {
		return 0, false
	}
	if expr.power != nil {
		base, ok := foldNumberTerm(expr.power.base)
		if !ok {
			return 0, false
		}
		exponent, ok := foldNumberTerm(expr.power.exponent)
		if !ok {
			return 0, false
		}
		return math.Pow(base, exponent), true
	}
	if expr.number != nil {
		return *expr.number, true
	}
	if expr.unaryMinus != nil {
		value, ok := foldNumberTerm(*expr.unaryMinus)
		return -value, ok
	}
	if expr.group != nil {
		return foldNumberExpression(*expr.group)
	}
	return 0, false
}

func registerDeadAfter(code []instruction, register int) bool {
	for _, ins := range code {
		if instructionReadsRegister(ins, register) {
			return false
		}
		if instructionWritesRegister(ins, register) {
			return true
		}
	}
	return true
}
