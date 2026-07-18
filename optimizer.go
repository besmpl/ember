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
	constantPool      *bytecodeBuilder
	optimizationPool  *optimizationConstantPool
}

// optimizationConstantPool tracks constants created while folding IR. Its
// seed aliases the durable builder pool so existing operand indices remain
// valid, while new values stay local until the final compaction step.
type optimizationConstantPool struct {
	seed        []Value
	pending     []Value
	pendingBase int
	pendingCap  int
}

func newOptimizationConstantPool(seed []Value, pendingCapacity int) *optimizationConstantPool {
	pool := &optimizationConstantPool{
		seed:        seed,
		pendingBase: len(seed),
		pendingCap:  pendingCapacity,
	}
	return pool
}

func (pool *optimizationConstantPool) intern(value Value) int {
	if pool == nil {
		return -1
	}
	if !isScalarConstant(value) {
		return -1
	}
	for index, existing := range pool.seed {
		if scalarConstantsEqual(existing, value) {
			return index
		}
	}
	index := pool.pendingBase + len(pool.pending)
	if pool.pending == nil && pool.pendingCap > 0 {
		pool.pending = make([]Value, 0, pool.pendingCap)
	}
	pool.pending = append(pool.pending, value)
	return index
}

func (pool *optimizationConstantPool) valueAt(index int) (Value, bool) {
	if pool == nil || index < 0 {
		return Value{}, false
	}
	if index < len(pool.seed) {
		return pool.seed[index], true
	}
	pending := index - pool.pendingBase
	if pending < 0 || pending >= len(pool.pending) {
		return Value{}, false
	}
	return pool.pending[pending], true
}

func compactOptimizationConstantPool(ir []bytecodeIRInstruction, pool *optimizationConstantPool, durable *bytecodeBuilder) ([]bytecodeIRInstruction, []Value) {
	if pool == nil {
		return ir, nil
	}
	total := len(pool.seed) + len(pool.pending)
	used := make([]bool, total)
	for _, instruction := range ir {
		for iterator := instruction.operandsIter(); ; {
			_, kind, value, ok := iterator.next()
			if !ok {
				break
			}
			if kind == bytecodeOperandConstant && value >= 0 && value < total {
				used[value] = true
			}
		}
	}
	oldToNew := make([]int, total)
	for index := range oldToNew {
		oldToNew[index] = -1
	}
	if durable != nil {
		durable.resetConstants(nil)
	}
	canonical := bytecodeBuilder{}
	if durable == nil {
		durable = &canonical
	}
	for oldIndex := 0; oldIndex < total; oldIndex++ {
		if !used[oldIndex] {
			continue
		}
		value, ok := pool.valueAt(oldIndex)
		if !ok {
			continue
		}
		canonicalIndex := durable.addConstant(value)
		oldToNew[oldIndex] = canonicalIndex
	}
	optimized := ir
	changed := false
	for instructionIndex, instruction := range ir {
		rewrite := func(kind bytecodeOperandKind, value int, assign func(int)) {
			if kind != bytecodeOperandConstant || value < 0 || value >= len(oldToNew) || oldToNew[value] < 0 {
				return
			}
			if value != oldToNew[value] {
				if !changed {
					optimized = append([]bytecodeIRInstruction(nil), ir...)
					changed = true
				}
				assign(oldToNew[value])
			}
		}
		rewrite(instruction.operandKind(bytecodeIROperandSlotA), instruction.operandValue(bytecodeIROperandSlotA), func(value int) { optimized[instructionIndex].setOperandValue(bytecodeIROperandSlotA, value) })
		rewrite(instruction.operandKind(bytecodeIROperandSlotB), instruction.operandValue(bytecodeIROperandSlotB), func(value int) { optimized[instructionIndex].setOperandValue(bytecodeIROperandSlotB, value) })
		rewrite(instruction.operandKind(bytecodeIROperandSlotC), instruction.operandValue(bytecodeIROperandSlotC), func(value int) { optimized[instructionIndex].setOperandValue(bytecodeIROperandSlotC, value) })
		rewrite(instruction.operandKind(bytecodeIROperandSlotD), instruction.operandValue(bytecodeIROperandSlotD), func(value int) { optimized[instructionIndex].setOperandValue(bytecodeIROperandSlotD, value) })
	}
	return optimized, durable.constants
}

type bytecodeIROptimizerPlan struct {
	runControlFlow bool
	runMoves       bool
	runLoop        bool
}

func optimizerPlanForIR(ir []bytecodeIRInstruction) bytecodeIROptimizerPlan {
	return optimizerPlanForFunction(newFunctionIR(ir))
}

func optimizerPlanForFunction(function *functionIR) bytecodeIROptimizerPlan {
	features := function.currentFeatures()
	return bytecodeIROptimizerPlan{
		runControlFlow: features.hasControlFlow,
		runMoves:       features.hasMove,
		runLoop:        features.hasBackedge,
	}
}

func optimizeBytecodeIRWithFacts(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts, options optimizationOptions) []bytecodeIRInstruction {
	if !options.enabled(optimizationBytecodePeephole) {
		return append([]bytecodeIRInstruction(nil), ir...)
	}
	if facts.constantPool != nil {
		facts.optimizationPool = newOptimizationConstantPool(facts.constants, len(ir))
	}
	function := newFunctionIR(ir)
	// The optional passes only remove or rewrite their corresponding opcode
	// families; they do not introduce a family that was absent at the start of
	// this pipeline. Keep one stage-local plan instead of rescanning the IR
	// before every pass.
	plan := optimizerPlanForFunction(function)
	if plan.runMoves {
		function.replaceOwned(applyBytecodeIRRemovalSet(
			function.instructions,
			bytecodeIRPeepholeRemovalSetCompact(function.instructions, function.currentAnalysis()),
		))
	}
	if plan.runControlFlow {
		function.replaceOwned(simplifyBytecodeIRControlFlow(function.instructions, bytecodeIROptimizationFacts{}))
	}
	function.replaceOwned(propagateBytecodeIRScalarConstants(function.instructions, facts))
	// Folding can turn a long straight-line chain into independent loads. Drop
	// those dead producers before move/coalescing inspect it, but pay for this
	// extra analysis only when folding actually created pending constants.
	if facts.optimizationPool != nil && len(facts.optimizationPool.pending) != 0 && len(function.instructions) > 256 {
		function.replaceOwned(applyBytecodeIRRemovalSet(
			function.instructions,
			bytecodeIRDeadCodeRemovalSetCompact(function.instructions, facts, function.currentAnalysis()),
		))
	}
	if plan.runMoves {
		function.replaceOwned(propagateBytecodeIRSingleUseMovesWithCode(function.instructions, function.currentAnalysis(), function.currentCode()))
		function.replaceOwned(coalesceBytecodeIRMoveProducersWithCode(function.instructions, facts.capturedRegisters, function.currentAnalysis(), function.currentCode()))
	}
	if plan.runLoop {
		function.replaceOwned(hoistBytecodeIRLoopInvariantHeaderLoadsWithCode(function.instructions, function.currentCode()))
	}
	function.replaceOwned(applyBytecodeIRRemovalSet(
		function.instructions,
		bytecodeIRDeadCodeRemovalSetCompact(function.instructions, facts, function.currentAnalysis()),
	))
	if plan.runControlFlow {
		function.replaceOwned(simplifyBytecodeIRControlFlow(function.instructions, bytecodeIROptimizationFacts{}))
	}
	if facts.optimizationPool != nil {
		if len(facts.optimizationPool.pending) != 0 {
			compactedIR, compactedConstants := compactOptimizationConstantPool(function.instructions, facts.optimizationPool, facts.constantPool)
			function.replaceOwned(compactedIR)
			if facts.constantPool != nil {
				facts.constantPool.constants = compactedConstants
			}
		} else if facts.constantPool != nil {
			compactedIR, compactedConstants := compactBytecodeIRConstants(function.instructions, facts.optimizationPool.seed)
			function.replaceOwned(compactedIR)
			if !sameValueSlices(compactedConstants, facts.optimizationPool.seed) {
				facts.constantPool.resetConstants(compactedConstants)
			}
		}
	}
	return function.instructions
}

func sameValueSlices(left, right []Value) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func compactBytecodeIRConstants(ir []bytecodeIRInstruction, constants []Value) ([]bytecodeIRInstruction, []Value) {
	if len(constants) == 0 {
		return ir, constants
	}
	used := make([]bool, len(constants))
	for _, ins := range ir {
		for iterator := ins.operandsIter(); ; {
			_, kind, value, ok := iterator.next()
			if !ok {
				break
			}
			if kind == bytecodeOperandConstant && value >= 0 && value < len(used) {
				used[value] = true
			}
		}
	}
	oldToNew := make([]int, len(constants))
	compacted := make([]Value, 0, len(constants))
	for index, value := range constants {
		oldToNew[index] = -1
		if used[index] {
			oldToNew[index] = len(compacted)
			compacted = append(compacted, value)
		}
	}
	if len(compacted) == len(constants) {
		return ir, constants
	}
	optimized := append([]bytecodeIRInstruction(nil), ir...)
	for index := range optimized {
		for slot := bytecodeIROperandSlotA; slot <= bytecodeIROperandSlotD; slot++ {
			kind := optimized[index].operandKind(slot)
			value := optimized[index].operandValue(slot)
			if kind == bytecodeOperandConstant && value >= 0 && value < len(oldToNew) {
				optimized[index].setOperandValue(slot, oldToNew[value])
			}
		}
	}
	return optimized, compacted
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

func bytecodeIRDeadCodeRemovalSetCompact(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts, analysis *functionAnalysis) []bool {
	remove := make([]bool, len(ir))
	numberFacts := bytecodeIRNumberFactsBeforeCompact(ir, facts, analysis.blocks)
	for _, live := range analysis.liveness {
		if !bytecodeIRBlockAllowsDeadCodeCleanupCompact(ir, live.block) {
			continue
		}
		liveRegisters := live.liveOut.copy()
		for pc := live.block.end - 1; pc >= live.block.start; pc-- {
			ins := assembleBytecodeIRInstruction(ir[pc])
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

func bytecodeIRBlockAllowsDeadCodeCleanupCompact(ir []bytecodeIRInstruction, block bytecodeIRBlock) bool {
	for pc := block.start; pc < block.end; pc++ {
		if !instructionAllowsDeadCodeCleanupInBlock(assembleBytecodeIRInstruction(ir[pc])) {
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
		opFastCall,
		opPrepareIter, opArrayNext, opArrayNextJump2,
		opNumericForCheck, opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK,
		opJumpIfLessK, opJumpIfGreaterK, opJumpIfNotLess, opJumpIfNotGreater,
		opJumpIfLess, opJumpIfGreater,
		opJumpIfTableHasMetatable,
		opSetField, opGetIndex, opSetIndex, opGetStringField, opSetStringField,
		opGetStringFieldIndex, opSetStringFieldIndex:
		return true
	case opCall:
		return true
	case opCallOne:
		_, borrowHint := decodeFixedCallCount(ins.c)
		return (ins.c >= 0 || borrowHint) && ins.d >= 0
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
	if instructionWritesCapturedRegister(ins, facts.capturedRegisters) {
		return false
	}
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

func instructionWritesCapturedRegister(ins instruction, capturedRegisters []bool) bool {
	writes := instructionRegisters(ins, instructionRegisterWrite)
	for register, ok := writes.next(); ok; register, ok = writes.next() {
		if register >= 0 && register < len(capturedRegisters) && capturedRegisters[register] {
			return true
		}
	}
	return false
}

func bytecodeIRNumberFactsBeforeCompact(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts, blocks []bytecodeIRBlock) []registerSet {
	factsBefore := make([]registerSet, len(ir))
	for _, block := range blocks {
		numberFacts := registerSet{}
		for pc := block.start; pc < block.end; pc++ {
			factsBefore[pc] = numberFacts.copy()
			applyInstructionNumberFacts(numberFacts, assembleBytecodeIRInstruction(ir[pc]), facts)
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
	value, ok := facts.scalarConstantAt(index)
	return ok && valueKind(value) == NumberKind
}

func bytecodeIRPeepholeRemovalSetCompact(ir []bytecodeIRInstruction, analysis *functionAnalysis) []bool {
	remove := make([]bool, len(ir))
	for _, live := range analysis.liveness {
		block := live.block
		for pc := block.start; pc < block.end; pc++ {
			ins := assembleBytecodeIRInstruction(ir[pc])
			if ins.op == opMove && ins.a == ins.b {
				remove[pc] = true
				continue
			}
			if pc+1 < block.end && isDeadMoveRoundTripInIR(ir, pc, block.end, live.liveOut) {
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
		switch opcodeControlFlow(ins.opcodeValue()) {
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
	if resolver.ir[pc].opcodeValue() == opJump {
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
	if len(facts.scalarConstants()) == 0 || !bytecodeIRHasJumpIfFalse(ir) {
		return false
	}
	constantFacts := bytecodeIRConstantFactsBefore(ir, facts)
	changed := false
	for pc, ins := range ir {
		if ins.opcodeValue() != opJumpIfFalse {
			continue
		}
		constant, ok := constantFacts[pc][ins.operandValue(bytecodeIROperandSlotA)]
		value, valid := facts.scalarConstantAt(constant)
		if !ok || !valid {
			continue
		}
		target, ok := bytecodeIRJumpTarget(ins)
		if !ok {
			continue
		}
		if value.truthy() {
			target = pc + 1
		}
		ir[pc] = lowerInstructionToBytecodeIR(instruction{op: opJump, b: target}, ins.sourceSpan())
		changed = true
	}
	return changed
}

func bytecodeIRHasJumpIfFalse(ir []bytecodeIRInstruction) bool {
	for _, ins := range ir {
		if ins.opcodeValue() == opJumpIfFalse {
			return true
		}
	}
	return false
}

func bytecodeIRConstantFactsBefore(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts) []map[int]int {
	factsBefore := make([]map[int]int, len(ir))
	for _, block := range bytecodeIRBlockOrder(ir) {
		registerConstants := make(map[int]int)
		for pc := block.start; pc < block.end; pc++ {
			factsBefore[pc] = copyRegisterConstants(registerConstants)
			applyInstructionConstantFacts(registerConstants, assembleBytecodeIRInstruction(ir[pc]), facts)
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

type scalarLatticeValue int

const (
	scalarVarying   scalarLatticeValue = -2
	scalarUnreached scalarLatticeValue = -1
)

func propagateBytecodeIRScalarConstants(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts) []bytecodeIRInstruction {
	if len(ir) == 0 || len(facts.scalarConstants()) == 0 {
		return ir
	}
	if !bytecodeIRHasScalarControlFlow(ir) {
		if len(ir) <= 256 && !straightLineBytecodeIRMayFoldScalarConstants(ir, facts) {
			return ir
		}
		return propagateStraightLineBytecodeIRScalarConstants(ir, facts)
	}
	blocks := bytecodeIRBlockOrder(ir)
	registerCount := bytecodeIRScalarRegisterCount(ir, len(facts.capturedRegisters))
	blockByStart := make(map[int]int, len(blocks))
	for _, block := range blocks {
		blockByStart[block.start] = block.id
	}
	successors := bytecodeIRBlockSuccessors(ir, blocks)
	entries := make([]scalarLatticeValue, len(blocks)*registerCount)
	for index := range entries {
		entries[index] = scalarUnreached
	}
	executable := make([]bool, len(blocks))
	inWorklist := make([]bool, len(blocks))

	entry := bytecodeIRScalarBlockState(entries, 0, registerCount)
	for register := range entry {
		entry[register] = scalarVarying
	}
	executable[0] = true
	worklist := []int{0}
	inWorklist[0] = true
	state := make([]scalarLatticeValue, registerCount)

	for len(worklist) != 0 {
		blockID := worklist[0]
		worklist = worklist[1:]
		inWorklist[blockID] = false
		copy(state, bytecodeIRScalarBlockState(entries, blockID, registerCount))
		block := blocks[blockID]
		for pc := block.start; pc < block.end; pc++ {
			applyBytecodeIRScalarTransfer(state, assembleBytecodeIRInstruction(ir[pc]), facts)
		}
		for _, successor := range bytecodeIRScalarSuccessors(ir, block, successors[blockID], blockByStart, state, facts) {
			if successor < 0 || successor >= len(entries) {
				continue
			}
			changed := false
			destination := bytecodeIRScalarBlockState(entries, successor, registerCount)
			if !executable[successor] {
				copy(destination, state)
				executable[successor] = true
				changed = true
			} else {
				changed = mergeBytecodeIRScalarState(destination, state)
			}
			if changed && !inWorklist[successor] {
				worklist = append(worklist, successor)
				inWorklist[successor] = true
			}
		}
	}

	optimized := ir
	changed := false
	rewriteState := make([]scalarLatticeValue, registerCount)
	for blockID, block := range blocks {
		if !executable[blockID] {
			continue
		}
		copy(rewriteState, bytecodeIRScalarBlockState(entries, blockID, registerCount))
		for pc := block.start; pc < block.end; pc++ {
			ins := assembleBytecodeIRInstruction(ir[pc])
			if value, ok := bytecodeIRScalarInstructionValue(ins, rewriteState, facts); ok && ins.op != opLoadConst && ins.op != opMove {
				if constant, ok := facts.internScalarConstant(value); ok {
					if !changed {
						optimized = append([]bytecodeIRInstruction(nil), ir...)
					}
					optimized[pc] = lowerInstructionToBytecodeIR(instruction{op: opLoadConst, a: ins.a, b: constant}, ir[pc].sourceSpan())
					changed = true
				}
			} else if taken, ok := bytecodeIRScalarBranchDecision(ins, rewriteState, facts); ok {
				if !changed {
					optimized = append([]bytecodeIRInstruction(nil), ir...)
				}
				target := pc + 1
				if taken {
					if jumpTarget, hasTarget := instructionJumpTarget(ins); hasTarget {
						target = jumpTarget
					}
				}
				optimized[pc] = lowerInstructionToBytecodeIR(instruction{op: opJump, b: target}, ir[pc].sourceSpan())
				changed = true
			}
			applyBytecodeIRScalarTransfer(rewriteState, ins, facts)
		}
	}
	if !changed {
		return ir
	}
	return simplifyBytecodeIRControlFlow(optimized, bytecodeIROptimizationFacts{})
}

func straightLineBytecodeIRMayFoldScalarConstants(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts) bool {
	registerCount := bytecodeIRScalarRegisterCount(ir, 0)
	var inline [64]bool
	known := inline[:min(registerCount, len(inline))]
	if registerCount > len(inline) {
		known = make([]bool, registerCount)
	}
	for _, raw := range ir {
		ins := assembleBytecodeIRInstruction(raw)
		switch ins.op {
		case opNeg, opLen:
			if ins.b >= 0 && ins.b < len(known) && known[ins.b] {
				return true
			}
		case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat,
			opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
			if ins.b >= 0 && ins.b < len(known) && known[ins.b] &&
				ins.c >= 0 && ins.c < len(known) && known[ins.c] {
				return true
			}
		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			if ins.b >= 0 && ins.b < len(known) && known[ins.b] {
				return true
			}
		}

		sourceKnown := ins.op == opMove && ins.b >= 0 && ins.b < len(known) && known[ins.b]
		if instructionClearsAllNumberFacts(ins) {
			clear(known)
		} else {
			writes := instructionRegisters(ins, instructionRegisterWrite)
			for register, ok := writes.next(); ok; register, ok = writes.next() {
				if register >= 0 && register < len(known) {
					known[register] = false
				}
			}
		}
		switch ins.op {
		case opLoadConst:
			if ins.a >= 0 && ins.a < len(known) {
				_, known[ins.a] = facts.scalarConstantAt(ins.b)
			}
		case opMove:
			if ins.a >= 0 && ins.a < len(known) {
				known[ins.a] = sourceKnown
			}
		}
	}
	return false
}

func bytecodeIRHasScalarControlFlow(ir []bytecodeIRInstruction) bool {
	for _, ins := range ir {
		switch opcodeControlFlow(ins.opcodeValue()) {
		case opcodeControlJump, opcodeControlBranch:
			return true
		}
	}
	return false
}

func propagateStraightLineBytecodeIRScalarConstants(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts) []bytecodeIRInstruction {
	state := make([]scalarLatticeValue, bytecodeIRScalarRegisterCount(ir, len(facts.capturedRegisters)))
	for register := range state {
		state[register] = scalarVarying
	}
	optimized := ir
	changed := false
	for pc, raw := range ir {
		ins := assembleBytecodeIRInstruction(raw)
		if value, ok := bytecodeIRScalarInstructionValue(ins, state, facts); ok && ins.op != opLoadConst && ins.op != opMove {
			if constant, ok := facts.internScalarConstant(value); ok {
				if !changed {
					optimized = append([]bytecodeIRInstruction(nil), ir...)
				}
				optimized[pc] = lowerInstructionToBytecodeIR(instruction{op: opLoadConst, a: ins.a, b: constant}, raw.sourceSpan())
				changed = true
			}
		}
		applyBytecodeIRScalarTransfer(state, ins, facts)
	}
	if !changed {
		return ir
	}
	return optimized
}

func bytecodeIRScalarBlockState(states []scalarLatticeValue, block int, registerCount int) []scalarLatticeValue {
	start := block * registerCount
	return states[start : start+registerCount]
}

func (facts bytecodeIROptimizationFacts) scalarConstants() []Value {
	if facts.optimizationPool != nil {
		return facts.optimizationPool.seed
	}
	if facts.constantPool != nil {
		return facts.constantPool.constants
	}
	return facts.constants
}

func (facts bytecodeIROptimizationFacts) scalarConstantAt(index int) (Value, bool) {
	if facts.optimizationPool != nil {
		return facts.optimizationPool.valueAt(index)
	}
	constants := facts.scalarConstants()
	if index < 0 || index >= len(constants) || !isScalarConstant(constants[index]) {
		return Value{}, false
	}
	return constants[index], true
}

func (facts bytecodeIROptimizationFacts) internScalarConstant(value Value) (int, bool) {
	if !isScalarConstant(value) {
		return 0, false
	}
	if facts.optimizationPool != nil {
		return facts.optimizationPool.intern(value), true
	}
	for index, constant := range facts.constants {
		if scalarConstantsEqual(constant, value) {
			return index, true
		}
	}
	return 0, false
}

func isScalarConstant(value Value) bool {
	switch valueKind(value) {
	case NilKind, BoolKind, NumberKind, StringKind:
		return true
	default:
		return false
	}
}

func scalarConstantsEqual(left Value, right Value) bool {
	leftKind := valueKind(left)
	if leftKind != valueKind(right) {
		return false
	}
	switch leftKind {
	case NilKind:
		return true
	case BoolKind:
		return valueBool(left) == valueBool(right)
	case NumberKind:
		return math.Float64bits(valueNumber(left)) == math.Float64bits(valueNumber(right))
	case StringKind:
		return left.stringText() == right.stringText()
	default:
		return false
	}
}

func bytecodeIRScalarRegisterCount(ir []bytecodeIRInstruction, minimum int) int {
	count := minimum
	for _, raw := range ir {
		ins := assembleBytecodeIRInstruction(raw)
		if limit := instructionRegisterStaticBound(ins); limit > count {
			count = limit
		}
	}
	return count
}

func mergeBytecodeIRScalarState(destination []scalarLatticeValue, incoming []scalarLatticeValue) bool {
	changed := false
	for register := range destination {
		joined := joinBytecodeIRScalarValue(destination[register], incoming[register])
		if joined != destination[register] {
			destination[register] = joined
			changed = true
		}
	}
	return changed
}

func joinBytecodeIRScalarValue(left scalarLatticeValue, right scalarLatticeValue) scalarLatticeValue {
	if left == scalarUnreached {
		return right
	}
	if right == scalarUnreached {
		return left
	}
	if left == right {
		return left
	}
	return scalarVarying
}

func bytecodeIRScalarSuccessors(
	ir []bytecodeIRInstruction,
	block bytecodeIRBlock,
	successors []int,
	blockByStart map[int]int,
	state []scalarLatticeValue,
	facts bytecodeIROptimizationFacts,
) []int {
	if block.end <= block.start || block.end > len(ir) {
		return successors
	}
	ins := assembleBytecodeIRInstruction(ir[block.end-1])
	taken, known := bytecodeIRScalarBranchDecision(ins, state, facts)
	if !known {
		return successors
	}
	nextPC := block.end
	if taken {
		var ok bool
		nextPC, ok = instructionJumpTarget(ins)
		if !ok {
			return successors
		}
	}
	next, ok := blockByStart[nextPC]
	if !ok {
		return nil
	}
	return []int{next}
}

func applyBytecodeIRScalarTransfer(state []scalarLatticeValue, ins instruction, facts bytecodeIROptimizationFacts) {
	value, hasValue := bytecodeIRScalarInstructionValue(ins, state, facts)
	constant := 0
	if hasValue {
		constant, hasValue = facts.internScalarConstant(value)
	}
	_, branchKnown := bytecodeIRScalarBranchDecision(ins, state, facts)
	if instructionClearsAllNumberFacts(ins) {
		for register := range state {
			state[register] = scalarVarying
		}
	} else if opcodeMayCall(ins.op) && !hasValue && !branchKnown {
		for register, captured := range facts.capturedRegisters {
			if captured && register < len(state) {
				state[register] = scalarVarying
			}
		}
	}
	markBytecodeIRScalarWritesVarying(state, ins)
	if hasValue && ins.a >= 0 && ins.a < len(state) {
		state[ins.a] = scalarLatticeValue(constant)
	}
}

func markBytecodeIRScalarWritesVarying(state []scalarLatticeValue, ins instruction) {
	writes := instructionRegistersBounded(ins, instructionRegisterWrite, len(state))
	for register, ok := writes.next(); ok; register, ok = writes.next() {
		state[register] = scalarVarying
	}
}

func bytecodeIRScalarInstructionValue(ins instruction, state []scalarLatticeValue, facts bytecodeIROptimizationFacts) (Value, bool) {
	register := func(index int) (Value, bool) {
		if index < 0 || index >= len(state) || state[index] < 0 {
			return Value{}, false
		}
		return facts.scalarConstantAt(int(state[index]))
	}
	number := func(index int) (float64, bool) {
		value, ok := register(index)
		return valueNumber(value), ok && valueKind(value) == NumberKind
	}
	constantNumber := func(index int) (float64, bool) {
		value, ok := facts.scalarConstantAt(index)
		return valueNumber(value), ok && valueKind(value) == NumberKind
	}

	switch ins.op {
	case opLoadConst:
		return facts.scalarConstantAt(ins.b)
	case opMove:
		return register(ins.b)
	case opNeg:
		operand, ok := number(ins.b)
		if ok {
			return NumberValue(-operand), true
		}
	case opLen:
		operand, ok := register(ins.b)
		if ok && valueKind(operand) == StringKind {
			return NumberValue(float64(len(operand.stringText()))), true
		}
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		left, leftOK := number(ins.b)
		right, rightOK := number(ins.c)
		if leftOK && rightOK {
			return foldBytecodeIRScalarArithmetic(ins.op, left, right), true
		}
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		left, leftOK := number(ins.b)
		right, rightOK := constantNumber(ins.c)
		if leftOK && rightOK {
			return foldBytecodeIRScalarArithmetic(ins.op, left, right), true
		}
	case opConcat:
		left, leftOK := register(ins.b)
		right, rightOK := register(ins.c)
		if leftOK && rightOK {
			text, err := valuesConcat(left, right)
			if err == nil {
				return StringValue(text), true
			}
		}
	case opEqual, opNotEqual:
		left, leftOK := register(ins.b)
		right, rightOK := register(ins.c)
		if leftOK && rightOK {
			equal := valuesEqual(left, right)
			if ins.op == opNotEqual {
				equal = !equal
			}
			return BoolValue(equal), true
		}
	case opLess, opLessEqual, opGreater, opGreaterEqual:
		left, leftOK := register(ins.b)
		right, rightOK := register(ins.c)
		if leftOK && rightOK {
			if result, ok := foldBytecodeIRScalarOrdering(ins.op, left, right); ok {
				return BoolValue(result), true
			}
		}
	}
	return Value{}, false
}

func foldBytecodeIRScalarArithmetic(op opcode, left float64, right float64) Value {
	switch op {
	case opAdd, opAddK:
		return NumberValue(left + right)
	case opSub, opSubK:
		return NumberValue(left - right)
	case opMul, opMulK:
		return NumberValue(left * right)
	case opDiv, opDivK:
		return NumberValue(left / right)
	case opMod, opModK:
		return NumberValue(left - math.Floor(left/right)*right)
	case opIDiv, opIDivK:
		return NumberValue(math.Floor(left / right))
	case opPow:
		return NumberValue(math.Pow(left, right))
	default:
		return Value{}
	}
}

func foldBytecodeIRScalarOrdering(op opcode, left Value, right Value) (bool, bool) {
	var less bool
	var equal bool
	leftKind := valueKind(left)
	if leftKind != valueKind(right) {
		return false, false
	}
	switch leftKind {
	case NumberKind:
		leftNumber, rightNumber := valueNumber(left), valueNumber(right)
		if math.IsNaN(leftNumber) || math.IsNaN(rightNumber) {
			return false, false
		}
		less = leftNumber < rightNumber
		equal = leftNumber == rightNumber
	case StringKind:
		less = left.stringText() < right.stringText()
		equal = left.stringText() == right.stringText()
	default:
		return false, false
	}
	switch op {
	case opLess:
		return less, true
	case opLessEqual:
		return less || equal, true
	case opGreater:
		return !less && !equal, true
	case opGreaterEqual:
		return !less, true
	default:
		return false, false
	}
}

func bytecodeIRScalarBranchDecision(ins instruction, state []scalarLatticeValue, facts bytecodeIROptimizationFacts) (bool, bool) {
	register := func(index int) (Value, bool) {
		if index < 0 || index >= len(state) || state[index] < 0 {
			return Value{}, false
		}
		return facts.scalarConstantAt(int(state[index]))
	}
	left, leftOK := register(ins.a)
	switch ins.op {
	case opJumpIfFalse:
		return !left.truthy(), leftOK
	case opJumpIfNotEqualK:
		right, rightOK := facts.scalarConstantAt(ins.b)
		if leftOK && rightOK {
			return !valuesEqual(left, right), true
		}
	case opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
		right, rightOK := facts.scalarConstantAt(ins.b)
		if leftOK && rightOK {
			op := opLess
			if ins.op == opJumpIfNotGreaterK || ins.op == opJumpIfGreaterK {
				op = opGreater
			}
			result, ok := foldBytecodeIRScalarOrdering(op, left, right)
			if ok {
				if ins.op == opJumpIfNotLessK || ins.op == opJumpIfNotGreaterK {
					result = !result
				}
				return result, true
			}
		}
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		right, rightOK := register(ins.b)
		if leftOK && rightOK {
			op := opLess
			if ins.op == opJumpIfNotGreater || ins.op == opJumpIfGreater {
				op = opGreater
			}
			result, ok := foldBytecodeIRScalarOrdering(op, left, right)
			if ok {
				if ins.op == opJumpIfNotLess || ins.op == opJumpIfNotGreater {
					result = !result
				}
				return result, true
			}
		}
	}
	return false, false
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
		switch opcodeControlFlow(ins.opcodeValue()) {
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
		if remove[pc] || ins.opcodeValue() != opJump {
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
	switch opcodeJumpTarget(ins.opcodeValue()) {
	case opcodeJumpTargetB:
		if ins.operandKind(bytecodeIROperandSlotB) != bytecodeOperandJumpTarget {
			return false
		}
		return ins.setOperandValue(bytecodeIROperandSlotB, target)
	case opcodeJumpTargetD:
		if ins.operandKind(bytecodeIROperandSlotD) != bytecodeOperandJumpTarget {
			return false
		}
		return ins.setOperandValue(bytecodeIROperandSlotD, target)
	default:
		return false
	}
}

func propagateBytecodeIRSingleUseMoves(ir []bytecodeIRInstruction, analysis *functionAnalysis) []bytecodeIRInstruction {
	return propagateBytecodeIRSingleUseMovesWithCode(ir, analysis, materializeBytecodeIR(ir))
}

func propagateBytecodeIRSingleUseMovesWithCode(ir []bytecodeIRInstruction, analysis *functionAnalysis, sourceCode []instruction) []bytecodeIRInstruction {
	if len(ir) == 0 {
		return ir
	}
	var optimized []bytecodeIRInstruction
	var code []instruction
	var remove []bool
	for _, live := range analysis.liveness {
		block := live.block
		for pc := block.start; pc < block.end; pc++ {
			view := sourceCode
			if code != nil {
				view = code
			}
			move := view[pc]
			if move.op != opMove || move.a == move.b {
				continue
			}
			usePC, ok := singleUseMoveReadPC(view, pc+1, block.end, live.liveOut, move.a, move.b)
			if !ok {
				continue
			}
			rewritten, ok := replaceInstructionReadRegister(view[usePC], move.a, move.b)
			if !ok {
				continue
			}
			if optimized == nil {
				optimized = append([]bytecodeIRInstruction(nil), ir...)
				code = append([]instruction(nil), sourceCode...)
				remove = make([]bool, len(ir))
			}
			code[usePC] = rewritten
			optimized[usePC] = lowerInstructionToBytecodeIR(rewritten, optimized[usePC].sourceSpan())
			remove[pc] = true
		}
	}
	if optimized == nil {
		return ir
	}
	return applyBytecodeIRRemovalSet(optimized, remove)
}

func singleUseMoveReadPC(code []instruction, start int, end int, liveOut registerSet, target int, source int) (int, bool) {
	usePC := -1
	for pc := start; pc < end; pc++ {
		ins := code[pc]
		if usePC < 0 && instructionHasRegisterEffect(ins, source, instructionRegisterWrite) {
			return -1, false
		}
		if instructionHasRegisterEffect(ins, target, instructionRegisterRead) {
			if usePC >= 0 {
				return -1, false
			}
			usePC = pc
		}
		if instructionHasRegisterEffect(ins, target, instructionRegisterWrite) {
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
	return coalesceBytecodeIRMoveProducersWithCode(ir, capturedRegisters, analysis, materializeBytecodeIR(ir))
}

func coalesceBytecodeIRMoveProducersWithCode(ir []bytecodeIRInstruction, capturedRegisters []bool, analysis *functionAnalysis, sourceCode []instruction) []bytecodeIRInstruction {
	if len(ir) < 2 {
		return ir
	}
	var optimized []bytecodeIRInstruction
	var code []instruction
	var remove []bool
	for _, live := range analysis.liveness {
		block := live.block
		for pc := block.start + 1; pc < block.end; pc++ {
			view := sourceCode
			if code != nil {
				view = code
			}
			move := view[pc]
			if move.op != opMove || move.a == move.b {
				continue
			}
			if move.b >= 0 && move.b < len(capturedRegisters) && capturedRegisters[move.b] {
				continue
			}
			if !registerDeadAfterMoveInBlock(view, pc, block.end, live.liveOut, move.b) {
				continue
			}
			producerPC := pc - 1
			producer := view[producerPC]
			if instructionHasRegisterEffect(producer, move.a, instructionRegisterRead) || instructionHasRegisterEffect(producer, move.a, instructionRegisterWrite) {
				continue
			}
			rewritten, ok := replaceInstructionWrittenRegister(producer, move.b, move.a)
			if !ok {
				continue
			}
			if optimized == nil {
				optimized = append([]bytecodeIRInstruction(nil), ir...)
				code = append([]instruction(nil), sourceCode...)
				remove = make([]bool, len(ir))
			}
			code[producerPC] = rewritten
			optimized[producerPC] = lowerInstructionToBytecodeIR(rewritten, optimized[producerPC].sourceSpan())
			remove[pc] = true
		}
	}
	if optimized == nil {
		return ir
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
	if instructionHasRegisterEffect(ins, to, instructionRegisterRead) {
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
	return hoistBytecodeIRLoopInvariantHeaderLoadsWithCode(ir, materializeBytecodeIR(ir))
}

func hoistBytecodeIRLoopInvariantHeaderLoadsWithCode(ir []bytecodeIRInstruction, sourceCode []instruction) []bytecodeIRInstruction {
	if len(ir) < 3 {
		return ir
	}
	var optimized []bytecodeIRInstruction
	var code []instruction
	for loopEnd, sourceBackedge := range sourceCode {
		backedge := sourceBackedge
		if code != nil {
			backedge = code[loopEnd]
		}
		loopStart, ok := loopLocalPathBackedgeTarget(backedge, loopEnd)
		if !ok || loopStart < 1 || loopStart+1 >= loopEnd {
			continue
		}
		view := sourceCode
		if code != nil {
			view = code
		}
		load := view[loopStart]
		if load.op != opGetStringField {
			continue
		}
		if !loopHeaderLoadHasNoMetatableGuard(view, loopStart, loopEnd, load.b) {
			continue
		}
		if loopHasInvariantHeaderLoadBarrier(view, loopStart, loopEnd, load) {
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
		if optimized == nil {
			optimized = append([]bytecodeIRInstruction(nil), ir...)
			code = append([]instruction(nil), sourceCode...)
		}
		code[loopEnd] = rewritten
		optimized[loopEnd] = lowerInstructionToBytecodeIR(rewritten, optimized[loopEnd].sourceSpan())
	}
	if optimized == nil {
		return ir
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
		if instructionHasRegisterEffect(ins, load.a, instructionRegisterWrite) || instructionHasRegisterEffect(ins, load.b, instructionRegisterWrite) {
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
		switch opcodeJumpTarget(ir[i].opcodeValue()) {
		case opcodeJumpTargetB:
			if ir[i].operandKind(bytecodeIROperandSlotB) == bytecodeOperandJumpTarget {
				target := ir[i].operandValue(bytecodeIROperandSlotB)
				if target >= 0 && target < len(oldToNew) {
					ir[i].setOperandValue(bytecodeIROperandSlotB, oldToNew[target])
				}
			}
		case opcodeJumpTargetD:
			if ir[i].operandKind(bytecodeIROperandSlotD) == bytecodeOperandJumpTarget {
				target := ir[i].operandValue(bytecodeIROperandSlotD)
				if target >= 0 && target < len(oldToNew) {
					ir[i].setOperandValue(bytecodeIROperandSlotD, oldToNew[target])
				}
			}
		}
	}
}

func isDeadMoveRoundTripInIR(ir []bytecodeIRInstruction, first int, blockEnd int, liveOut registerSet) bool {
	if first+1 >= blockEnd || !isDeadMoveRoundTripPair(assembleBytecodeIRInstruction(ir[first]), assembleBytecodeIRInstruction(ir[first+1])) {
		return false
	}
	register := ir[first].operandValue(bytecodeIROperandSlotA)
	if killed, known := registerKilledBeforeReadIR(ir, first+2, blockEnd, register); known {
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
		if instructionHasRegisterEffect(ins, register, instructionRegisterRead) {
			return false, true
		}
		if instructionHasRegisterEffect(ins, register, instructionRegisterWrite) {
			return true, true
		}
	}
	return false, false
}

func registerKilledBeforeReadIR(ir []bytecodeIRInstruction, start int, end int, register int) (bool, bool) {
	for pc := start; pc < end; pc++ {
		ins := assembleBytecodeIRInstruction(ir[pc])
		if instructionHasRegisterEffect(ins, register, instructionRegisterRead) {
			return false, true
		}
		if instructionHasRegisterEffect(ins, register, instructionRegisterWrite) {
			return true, true
		}
	}
	return false, false
}

func foldConstantExpression(tree syntaxTree, expr expressionID) (Value, bool) {
	terms, ok := tree.expressionTerms(expr)
	if !ok || len(terms) != 1 {
		return NilValue(), false
	}
	and := terms[0]
	comparisons, ok := tree.andTerms(and)
	if !ok || len(comparisons) != 1 {
		return NilValue(), false
	}
	comparison := comparisons[0]
	if tree.comparisonOperator(comparison) != "" || tree.comparisonRight(comparison) != 0 {
		return NilValue(), false
	}
	return foldConstantConcat(tree, tree.comparisonLeft(comparison))
}

func foldConstantConcat(tree syntaxTree, expr concatExpressionID) (Value, bool) {
	value, ok := foldConstantAdditive(tree, tree.concatFirst(expr))
	if !ok {
		return NilValue(), false
	}
	rest, _ := tree.concatRest(expr)
	if len(rest) == 0 {
		return value, true
	}
	for _, part := range rest {
		right, ok := foldConstantAdditive(tree, part)
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

func foldConstantAdditive(tree syntaxTree, expr additiveExpressionID) (Value, bool) {
	value, ok := foldConstantMultiplicative(tree, tree.additiveFirst(expr))
	if !ok {
		return NilValue(), false
	}
	rest, _ := tree.additiveRest(expr)
	if len(rest) == 0 {
		return value, true
	}
	left, ok := numericOperandValue(value)
	if !ok {
		return NilValue(), false
	}
	for _, part := range rest {
		rightValue, ok := foldConstantMultiplicative(tree, part.value)
		if !ok {
			return NilValue(), false
		}
		right, ok := numericOperandValue(rightValue)
		if !ok {
			return NilValue(), false
		}
		switch part.op {
		case arenaAdditiveAdd:
			left += right
		case arenaAdditiveSubtract:
			left -= right
		default:
			return NilValue(), false
		}
	}
	return NumberValue(left), true
}

func foldConstantMultiplicative(tree syntaxTree, expr multiplicativeExpressionID) (Value, bool) {
	value, ok := foldConstantTerm(tree, tree.multiplicativeFirst(expr))
	if !ok {
		return NilValue(), false
	}
	rest, _ := tree.multiplicativeRest(expr)
	if len(rest) == 0 {
		return value, true
	}
	left, ok := numericOperandValue(value)
	if !ok {
		return NilValue(), false
	}
	for _, part := range rest {
		rightValue, ok := foldConstantTerm(tree, part.value)
		if !ok {
			return NilValue(), false
		}
		right, ok := numericOperandValue(rightValue)
		if !ok {
			return NilValue(), false
		}
		switch part.op {
		case arenaMultiplicativeMultiply:
			left *= right
		case arenaMultiplicativeDivide:
			left /= right
		case arenaMultiplicativeModulo:
			left = left - math.Floor(left/right)*right
		case arenaMultiplicativeFloorDiv:
			left = math.Floor(left / right)
		default:
			return NilValue(), false
		}
	}
	return NumberValue(left), true
}

func foldConstantTerm(tree syntaxTree, expr termID) (Value, bool) {
	selectors, _ := tree.termSelectors(expr)
	if len(selectors) != 0 {
		return NilValue(), false
	}
	if power, ok := tree.termPower(expr); ok {
		base, ok := foldConstantTerm(tree, tree.powerBase(power))
		if !ok {
			return NilValue(), false
		}
		exponent, ok := foldConstantTerm(tree, tree.powerExponent(power))
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
	if number, ok := tree.termNumber(expr); ok {
		return NumberValue(number), true
	}
	if literal, ok := tree.termLiteral(expr); ok {
		return literal, true
	}
	if unary, ok := tree.termChild(expr); ok && tree.termKind(expr) == syntaxTermUnaryNot {
		value, ok := foldConstantTerm(tree, unary)
		if !ok {
			return NilValue(), false
		}
		return BoolValue(!value.truthy()), true
	}
	if unary, ok := tree.termChild(expr); ok && tree.termKind(expr) == syntaxTermUnaryMinus {
		value, ok := foldConstantTerm(tree, unary)
		if !ok {
			return NilValue(), false
		}
		number, ok := numericOperandValue(value)
		if !ok {
			return NilValue(), false
		}
		return NumberValue(-number), true
	}
	if unary, ok := tree.termChild(expr); ok && tree.termKind(expr) == syntaxTermUnaryLength {
		return foldConstantLength(tree, unary)
	}
	if group, ok := tree.termGroup(expr); ok {
		return foldConstantExpression(tree, group)
	}
	return NilValue(), false
}

func foldConstantLength(tree syntaxTree, expr termID) (Value, bool) {
	selectors, _ := tree.termSelectors(expr)
	if len(selectors) != 0 {
		return NilValue(), false
	}
	if literal, ok := tree.termLiteral(expr); ok && valueKind(literal) == StringKind {
		return NumberValue(float64(len(literal.stringText()))), true
	}
	if table, ok := tree.termTable(expr); ok {
		length, ok := foldConstantTableLength(tree, table)
		if ok {
			return NumberValue(float64(length)), true
		}
	}
	if group, ok := tree.termGroup(expr); ok {
		value, ok := foldConstantExpression(tree, group)
		if ok && valueKind(value) == StringKind {
			return NumberValue(float64(len(value.stringText()))), true
		}
	}
	return NilValue(), false
}

func foldConstantTableLength(tree syntaxTree, table arenaTableID) (int, bool) {
	fields, _ := tree.tableFields(table)
	if len(fields) == 0 {
		return 0, true
	}
	for index := range fields {
		field := fields[index]
		if tree.tableFieldKey(field) != 0 || tree.tableFieldName(field) != "" || tree.tableFieldArrayIndex(field) != index+1 {
			return 0, false
		}
		value, ok := foldConstantExpression(tree, tree.tableFieldValue(field))
		if !ok || valueKind(value) == NilKind {
			return 0, false
		}
	}
	return len(fields), true
}

func foldNumberExpression(tree syntaxTree, expr expressionID) (float64, bool) {
	terms, ok := tree.expressionTerms(expr)
	if !ok || len(terms) != 1 {
		return 0, false
	}
	and := terms[0]
	comparisons, ok := tree.andTerms(and)
	if !ok || len(comparisons) != 1 {
		return 0, false
	}
	comparison := comparisons[0]
	if tree.comparisonOperator(comparison) != "" || tree.comparisonRight(comparison) != 0 {
		return 0, false
	}
	return foldNumberConcat(tree, tree.comparisonLeft(comparison))
}

func foldNumberConcat(tree syntaxTree, expr concatExpressionID) (float64, bool) {
	rest, _ := tree.concatRest(expr)
	if len(rest) != 0 {
		return 0, false
	}
	return foldNumberAdditive(tree, tree.concatFirst(expr))
}

func foldNumberAdditive(tree syntaxTree, expr additiveExpressionID) (float64, bool) {
	value, ok := foldNumberMultiplicative(tree, tree.additiveFirst(expr))
	if !ok {
		return 0, false
	}
	rest, _ := tree.additiveRest(expr)
	for _, part := range rest {
		right, ok := foldNumberMultiplicative(tree, part.value)
		if !ok {
			return 0, false
		}
		switch part.op {
		case arenaAdditiveAdd:
			value += right
		case arenaAdditiveSubtract:
			value -= right
		default:
			return 0, false
		}
	}
	return value, true
}

func foldNumberMultiplicative(tree syntaxTree, expr multiplicativeExpressionID) (float64, bool) {
	value, ok := foldNumberTerm(tree, tree.multiplicativeFirst(expr))
	if !ok {
		return 0, false
	}
	rest, _ := tree.multiplicativeRest(expr)
	for _, part := range rest {
		right, ok := foldNumberTerm(tree, part.value)
		if !ok {
			return 0, false
		}
		switch part.op {
		case arenaMultiplicativeMultiply:
			value *= right
		case arenaMultiplicativeDivide:
			value /= right
		case arenaMultiplicativeModulo:
			value = value - math.Floor(value/right)*right
		case arenaMultiplicativeFloorDiv:
			value = math.Floor(value / right)
		default:
			return 0, false
		}
	}
	return value, true
}

func foldNumberTerm(tree syntaxTree, expr termID) (float64, bool) {
	selectors, _ := tree.termSelectors(expr)
	if len(selectors) != 0 {
		return 0, false
	}
	if power, ok := tree.termPower(expr); ok {
		base, ok := foldNumberTerm(tree, tree.powerBase(power))
		if !ok {
			return 0, false
		}
		exponent, ok := foldNumberTerm(tree, tree.powerExponent(power))
		if !ok {
			return 0, false
		}
		return math.Pow(base, exponent), true
	}
	if number, ok := tree.termNumber(expr); ok {
		return number, true
	}
	if unary, ok := tree.termChild(expr); ok && tree.termKind(expr) == syntaxTermUnaryMinus {
		value, ok := foldNumberTerm(tree, unary)
		return -value, ok
	}
	if group, ok := tree.termGroup(expr); ok {
		return foldNumberExpression(tree, group)
	}
	return 0, false
}

func registerDeadAfter(code []instruction, register int) bool {
	for _, ins := range code {
		if instructionHasRegisterEffect(ins, register, instructionRegisterRead) {
			return false
		}
		if instructionHasRegisterEffect(ins, register, instructionRegisterWrite) {
			return true
		}
	}
	return true
}
