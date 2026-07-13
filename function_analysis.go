package ember

type functionIR struct {
	instructions []bytecodeIRInstruction
	revision     uint64

	// The caches have separate generations because a constant/source rewrite
	// does not change the CFG or register liveness, while a register/count
	// rewrite changes liveness without changing the CFG.
	cfgRevision      uint64
	livenessRevision uint64
	analysis         *functionAnalysis
	features         bytecodeIRFeatures
	featuresValid    bool
	assembled        []instruction
	assembledValid   bool
}

type bytecodeIRChangeClass uint8

const (
	bytecodeIRChangeNone bytecodeIRChangeClass = iota
	bytecodeIRChangeSource
	bytecodeIRChangeValue
	bytecodeIRChangeRegisterEffects
	bytecodeIRChangeControlFlow
)

func (class bytecodeIRChangeClass) merge(other bytecodeIRChangeClass) bytecodeIRChangeClass {
	if other > class {
		return other
	}
	return class
}

type bytecodeIRFeatures struct {
	hasMove        bool
	hasControlFlow bool
	hasBackedge    bool
	hasCall        bool
}

type functionAnalysis struct {
	revision         uint64
	cfgRevision      uint64
	livenessRevision uint64
	blocks           []bytecodeIRBlock
	successors       [][]int
	predecessors     [][]int
	reachable        []bool
	use              []registerSet
	def              []registerSet
	liveness         []bytecodeIRLivenessBlock
	// liveAfter is ephemeral finalization data, never a Proto side table.
	liveAfter []registerSet
	effects   []opcodeEffects
}

func newFunctionIR(ir []bytecodeIRInstruction) *functionIR {
	return &functionIR{instructions: append([]bytecodeIRInstruction(nil), ir...)}
}

func (function *functionIR) replace(ir []bytecodeIRInstruction) {
	function.replaceOwned(append([]bytecodeIRInstruction(nil), ir...))
}

// replaceOwned accepts a slice that the caller will not mutate after this
// call. Optimizer transformations use it to avoid an extra copy; tests and
// external callers should use replace, which takes ownership by cloning.
func (function *functionIR) replaceOwned(ir []bytecodeIRInstruction) {
	if function == nil {
		return
	}
	change := classifyBytecodeIRChange(function.instructions, ir)
	if change == bytecodeIRChangeNone {
		return
	}
	function.revision++
	if change != bytecodeIRChangeSource {
		function.assembledValid = false
	}
	switch change {
	case bytecodeIRChangeSource, bytecodeIRChangeValue:
		// Semantic analysis and opcode features remain valid.
	case bytecodeIRChangeRegisterEffects:
		function.livenessRevision++
	case bytecodeIRChangeControlFlow:
		function.cfgRevision++
		function.livenessRevision++
		function.featuresValid = false
	}
	function.instructions = ir
}

// currentCode returns a shared, read-only assembled view. Mutating passes must
// clone it before changing any element.
func (function *functionIR) currentCode() []instruction {
	if function == nil {
		return nil
	}
	if !function.assembledValid {
		function.assembled = materializeBytecodeIR(function.instructions)
		function.assembledValid = true
	}
	return function.assembled
}

func materializeBytecodeIR(ir []bytecodeIRInstruction) []instruction {
	code := make([]instruction, len(ir))
	for index, instruction := range ir {
		code[index] = assembleBytecodeIRInstruction(instruction)
	}
	return code
}

func classifyBytecodeIRChange(before, after []bytecodeIRInstruction) bytecodeIRChangeClass {
	if len(before) != len(after) {
		return bytecodeIRChangeControlFlow
	}
	change := bytecodeIRChangeNone
	for index := range before {
		left, right := before[index], after[index]
		if left.opcodeValue() != right.opcodeValue() {
			return bytecodeIRChangeControlFlow
		}
		if left.sourceStart != right.sourceStart || left.sourceEnd != right.sourceEnd {
			change = change.merge(bytecodeIRChangeSource)
		}
		for slot := bytecodeIROperandSlotA; slot <= bytecodeIROperandSlotD; slot++ {
			if left.operandValue(slot) == right.operandValue(slot) {
				continue
			}
			kind := left.operandKind(slot)
			if kind != right.operandKind(slot) {
				return bytecodeIRChangeControlFlow
			}
			switch kind {
			case bytecodeOperandJumpTarget:
				return bytecodeIRChangeControlFlow
			case bytecodeOperandRegister, bytecodeOperandCount:
				change = change.merge(bytecodeIRChangeRegisterEffects)
			default:
				change = change.merge(bytecodeIRChangeValue)
			}
		}
	}
	return change
}

func (function *functionIR) currentFeatures() bytecodeIRFeatures {
	if function == nil {
		return bytecodeIRFeatures{}
	}
	if !function.featuresValid {
		features := bytecodeIRFeatures{}
		for pc, instruction := range function.instructions {
			if instruction.opcodeValue() == opMove {
				features.hasMove = true
			}
			meta, known := opcodeMetadata(instruction.opcodeValue())
			if known && meta.effects.invokesScriptOrHostCode {
				features.hasCall = true
			}
			if known {
				switch meta.controlFlow {
				case opcodeControlJump, opcodeControlBranch:
					features.hasControlFlow = true
				case opcodeControlReturn:
					// A return followed by another instruction still needs the
					// control-flow cleanup pass to remove unreachable code.
					if pc+1 < len(function.instructions) {
						features.hasControlFlow = true
					}
				}
				var target int
				hasTarget := false
				switch meta.jumpTarget {
				case opcodeJumpTargetB:
					target, hasTarget = instruction.operandValue(bytecodeIROperandSlotB), instruction.operandKind(bytecodeIROperandSlotB) == bytecodeOperandJumpTarget
				case opcodeJumpTargetD:
					target, hasTarget = instruction.operandValue(bytecodeIROperandSlotD), instruction.operandKind(bytecodeIROperandSlotD) == bytecodeOperandJumpTarget
				}
				if hasTarget && target >= 0 && target <= pc && target < len(function.instructions) {
					features.hasBackedge = true
				}
			}
		}
		function.features = features
		function.featuresValid = true
	}
	return function.features
}

func (function *functionIR) currentAnalysis() *functionAnalysis {
	if function == nil {
		return nil
	}
	if function.analysis == nil || function.analysis.cfgRevision != function.cfgRevision {
		function.analysis = analyzeBytecodeIR(function.instructions, function.revision)
		function.analysis.cfgRevision = function.cfgRevision
		function.analysis.livenessRevision = function.livenessRevision
	} else if function.analysis.livenessRevision != function.livenessRevision {
		previous := function.analysis
		function.analysis = &functionAnalysis{
			revision:         function.revision,
			cfgRevision:      function.cfgRevision,
			livenessRevision: function.livenessRevision,
			blocks:           previous.blocks,
			successors:       previous.successors,
			predecessors:     previous.predecessors,
			reachable:        previous.reachable,
			effects:          previous.effects,
		}
		refreshFunctionAnalysisLiveness(function.instructions, function.analysis)
	}
	return function.analysis
}

func analyzeBytecodeIR(ir []bytecodeIRInstruction, revision uint64) *functionAnalysis {
	blocks := bytecodeIRBlockOrder(ir)
	successors := bytecodeIRBlockSuccessors(ir, blocks)
	analysis := &functionAnalysis{
		revision:     revision,
		blocks:       blocks,
		successors:   successors,
		predecessors: bytecodeIRBlockPredecessors(successors),
		reachable:    bytecodeIRReachableBlocks(successors),
		effects:      make([]opcodeEffects, len(ir)),
	}
	for pc, ins := range ir {
		analysis.effects[pc] = opcodeEffect(ins.opcodeValue())
	}
	refreshFunctionAnalysisLiveness(ir, analysis)
	return analysis
}

func refreshFunctionAnalysisLiveness(ir []bytecodeIRInstruction, analysis *functionAnalysis) {
	if analysis == nil {
		return
	}
	liveness := bytecodeIRLivenessForGraph(ir, analysis.blocks, analysis.successors)
	analysis.use = make([]registerSet, len(liveness))
	analysis.def = make([]registerSet, len(liveness))
	analysis.liveness = liveness
	analysis.liveAfter = bytecodeIRLiveAfter(ir, analysis.blocks, liveness)
	for block := range liveness {
		analysis.use[block] = liveness[block].use
		analysis.def[block] = liveness[block].def
	}
}

func bytecodeIRLiveAfter(ir []bytecodeIRInstruction, blocks []bytecodeIRBlock, liveness []bytecodeIRLivenessBlock) []registerSet {
	if len(ir) == 0 {
		return nil
	}
	liveAfter := make([]registerSet, len(ir))
	for index, block := range blocks {
		if index < 0 || index >= len(liveness) {
			continue
		}
		live := liveness[index].liveOut.copy()
		for pc := block.end - 1; pc >= block.start; pc-- {
			liveAfter[pc].assign(live)
			raw := assembleBytecodeIRInstruction(ir[pc])
			writes := instructionRegisters(raw, instructionRegisterWrite)
			for register, ok := writes.next(); ok; register, ok = writes.next() {
				live.remove(register)
			}
			reads := instructionRegisters(raw, instructionRegisterRead)
			for register, ok := reads.next(); ok; register, ok = reads.next() {
				live.add(register)
			}
		}
	}
	return liveAfter
}

// fixedCallBorrowFact is an ephemeral proof used while finalizing code. The
// marker is the only persistent output reserved for a future runtime-window
// activation.
type fixedCallBorrowFact struct {
	pc            int
	op            opcode
	argumentStart int
	argumentCount int
	result        int
	resultCount   int
	openResults   bool
	openArguments bool
	prefixCount   int
	suffixStart   int
	suffixEnd     int
	eligible      bool
	reason        string
}

func openArgumentCallPrefix(ins instruction) (prefixCount int, ok bool) {
	if ins.op != opCall || ins.c >= 0 {
		return 0, false
	}
	if prefixCount, marked := decodeOpenArgumentCallMarker(ins.c); marked {
		return prefixCount, true
	}
	prefixCount = -ins.c - 1
	return prefixCount, prefixCount >= 0
}

func fixedCallBorrowShape(ins instruction) (argumentStart, argumentCount, result, rawCount int, ok bool) {
	switch ins.op {
	case opCall:
		// Generic CALL is eligible only for fixed arguments and a fixed
		// multi-result destination.  A one-result CALL has no marker space
		// distinct from its ordinary encoding and keeps the existing path.
		if ins.c < 0 || (ins.d < 2 && ins.d >= 0) {
			return 0, 0, 0, 0, false
		}
		return ins.b + 1, ins.c, ins.a, ins.c, true
	case opCallOne:
		argumentCount, _ = decodeFixedCallCount(ins.c)
		return ins.b + 1, argumentCount, ins.a, ins.c, true
	case opCallLocalOne, opCallUpvalueOne:
		argumentCount, _ = decodeFixedCallCount(ins.d)
		return ins.c, argumentCount, ins.a, ins.d, true
	case opCallMethodOne:
		explicitCount, _ := decodeFixedCallCount(ins.d)
		// The VM stages receiver/self at A+1 before the explicit arguments.
		// The result at A remains outside the borrowed callee window.
		return ins.a + 1, explicitCount + 1, ins.a, ins.d, true
	default:
		return 0, 0, 0, 0, false
	}
}

func fixedCallBorrowFactForInstruction(ins instruction, pc int, registers int, capturedLocals []bool, liveAfter registerSet) fixedCallBorrowFact {
	fact := fixedCallBorrowFact{pc: pc, op: ins.op, suffixEnd: registers, resultCount: 1}
	argumentStart, argumentCount, result, rawCount, ok := fixedCallBorrowShape(ins)
	if !ok {
		return fact
	}
	fact.argumentStart = argumentStart
	fact.argumentCount = argumentCount
	fact.result = result
	if ins.op == opCall {
		fact.openResults = ins.d < 0
		if fact.openResults {
			fact.resultCount = -1
		} else {
			fact.resultCount = ins.d
		}
	}
	fact.suffixStart = argumentStart + argumentCount
	if rawCount < -32768 || rawCount > 32767 {
		fact.reason = "argument count is outside packed int16 range"
		return fact
	}
	if registers < 0 || argumentStart < 0 || argumentStart > registers || argumentCount < 0 || argumentCount > registers-argumentStart {
		fact.reason = "argument window is outside caller registers"
		return fact
	}
	if result < 0 || result >= registers {
		fact.reason = "result destination is outside caller registers"
		return fact
	}
	if fact.openResults {
		if result >= argumentStart {
			fact.reason = "open result destination overlaps the scratch suffix"
			return fact
		}
	} else {
		if fact.resultCount < 1 || result+fact.resultCount > registers {
			fact.reason = "result destination is outside caller registers"
			return fact
		}
		if (ins.op != opCall && result+fact.resultCount > fact.suffixStart) ||
			(ins.op == opCall && result >= argumentStart) {
			fact.reason = "result destination overlaps the scratch suffix"
			return fact
		}
	}
	resultEnd := result + fact.resultCount
	if fact.openResults {
		// An open result may occupy any caller register through the static
		// register limit. Treat that whole span as the destination for capture
		// safety and liveness; the runtime stores the values in the owner-backed
		// range and only publishes the first value into A.
		resultEnd = registers
	}
	for destination := result; destination < resultEnd; destination++ {
		if len(capturedLocals) > destination && capturedLocals[destination] {
			fact.reason = "result destination is captured"
			return fact
		}
	}
	for register := argumentStart; register < registers; register++ {
		if register < len(capturedLocals) && capturedLocals[register] {
			fact.reason = "borrowed suffix contains a captured local"
			return fact
		}
		if !fact.openResults && (register < result || register >= resultEnd) {
			if liveAfter.contains(register) {
				fact.reason = "borrowed suffix remains live after the call"
				return fact
			}
		}
	}
	fact.eligible = true
	return fact
}

func openArgumentCallBorrowFactForInstruction(code []instruction, ins instruction, pc int, registers int, capturedLocals []bool, liveAfter registerSet) fixedCallBorrowFact {
	prefixCount, ok := openArgumentCallPrefix(ins)
	fact := fixedCallBorrowFact{pc: pc, op: ins.op, suffixEnd: registers, resultCount: 1, openArguments: ok, prefixCount: prefixCount}
	if !ok {
		return fact
	}
	fact.argumentStart = ins.b + 1
	fact.argumentCount = prefixCount
	openArgStart := fact.argumentStart + prefixCount
	scratchStart := registers - prefixCount
	fact.suffixStart = scratchStart
	fact.result = ins.a
	_, fixedMultiResults := decodeFixedMultiResultCount(ins.d, registers)
	fact.openResults = ins.d < 0 && !fixedMultiResults
	if fact.openResults {
		fact.resultCount = -1
	} else if fixedMultiResults {
		fact.resultCount, _ = decodeFixedMultiResultCount(ins.d, registers)
	} else if ins.d == 0 {
		fact.resultCount = 1
	} else {
		fact.resultCount = ins.d
	}
	if !openArgumentCallHasProducer(code, pc, openArgStart, registers) {
		fact.reason = "open argument call has no matching preceding producer"
		return fact
	}
	if registers < 0 || fact.argumentStart < 0 || openArgStart < fact.argumentStart || openArgStart >= registers {
		fact.reason = "open argument window is outside caller registers"
		return fact
	}
	if fact.result < 0 || fact.result >= registers {
		fact.reason = "result destination is outside caller registers"
		return fact
	}
	if fact.openResults {
		if fact.result >= scratchStart {
			fact.reason = "open result destination overlaps the scratch suffix"
			return fact
		}
	} else {
		if fact.resultCount < 0 || fact.result+fact.resultCount > registers || (fact.resultCount > 0 && fact.result >= scratchStart) {
			fact.reason = "result destination overlaps the scratch suffix"
			return fact
		}
	}
	resultEnd := fact.result + fact.resultCount
	if fact.openResults {
		resultEnd = registers
	}
	for destination := fact.result; destination < resultEnd; destination++ {
		if len(capturedLocals) > destination && capturedLocals[destination] {
			fact.reason = "result destination is captured"
			return fact
		}
	}
	for register := scratchStart; register < registers; register++ {
		if register < len(capturedLocals) && capturedLocals[register] {
			fact.reason = "borrowed suffix contains a captured local"
			return fact
		}
		if !fact.openResults && (register < fact.result || register >= resultEnd) && liveAfter.contains(register) {
			fact.reason = "borrowed suffix remains live after the call"
			return fact
		}
	}
	fact.eligible = true
	return fact
}

func analyzeFixedCallBorrowFacts(code []instruction, registers int, capturedLocals []bool) []fixedCallBorrowFact {
	if len(code) == 0 {
		return nil
	}
	ir := lowerInstructionsToBytecodeIR(code)
	analysis := analyzeBytecodeIR(ir, 0)
	facts := make([]fixedCallBorrowFact, 0)
	for pc, ins := range code {
		if _, ok := openArgumentCallPrefix(ins); ok {
			var liveAfter registerSet
			if pc < len(analysis.liveAfter) {
				liveAfter = analysis.liveAfter[pc]
			}
			fact := openArgumentCallBorrowFactForInstruction(code, ins, pc, registers, capturedLocals, liveAfter)
			if fact.openResults && !openResultCallHasImmediateConsumer(code, pc, ins.a) {
				fact.eligible = false
				fact.reason = "open result call is not consumed by a matching open return"
			}
			facts = append(facts, fact)
			continue
		}
		if _, _, _, _, ok := fixedCallBorrowShape(ins); !ok {
			continue
		}
		var liveAfter registerSet
		if pc < len(analysis.liveAfter) {
			liveAfter = analysis.liveAfter[pc]
		}
		fact := fixedCallBorrowFactForInstruction(ins, pc, registers, capturedLocals, liveAfter)
		if fact.openResults {
			// An open destination is only safe for the compact record bridge when
			// the call immediately feeds an open RETURN.  This proves that no
			// unrelated suffix locals survive the dynamic result span.
			if !openResultCallHasImmediateConsumer(code, pc, ins.a) {
				fact.eligible = false
				fact.reason = "open result call is not consumed by an open return"
			}
		}
		facts = append(facts, fact)
	}
	return facts
}

// markBorrowableFixedCallWindows encodes a borrow hint only after liveness and
// capture analysis prove that the fixed-call register suffix is dead. All four
// runtime fixed-call forms decode the hint and retain guarded cold fallbacks.
func markBorrowableFixedCallWindows(code []instruction, registers int, capturedLocals []bool) []instruction {
	hasFixedCall := false
	for _, ins := range code {
		if _, open := openArgumentCallPrefix(ins); open {
			hasFixedCall = true
			break
		}
		if _, _, _, _, ok := fixedCallBorrowShape(ins); ok {
			hasFixedCall = true
			break
		}
	}
	if !hasFixedCall {
		return code
	}
	marked := append([]instruction(nil), code...)
	for _, fact := range analyzeFixedCallBorrowFacts(code, registers, capturedLocals) {
		if !fact.eligible || fact.pc < 0 || fact.pc >= len(marked) {
			continue
		}
		switch marked[fact.pc].op {
		case opCall:
			if fact.openArguments {
				marker, representable := encodeOpenArgumentCallMarker(fact.prefixCount)
				if !representable {
					continue
				}
				marked[fact.pc].c = marker
				if fact.openResults {
					marked[fact.pc].d = encodeOpenResultCallMarker()
				}
			} else {
				marker := encodeFixedMultiResultCount(fact.resultCount, registers)
				if fact.openResults {
					marker = encodeOpenResultCallMarker()
				}
				if marker >= -32768 && marker <= 32767 {
					marked[fact.pc].d = marker
				}
			}
		case opCallOne:
			marked[fact.pc].c = encodeFixedCallCount(fact.argumentCount, true)
		case opCallLocalOne, opCallUpvalueOne:
			marked[fact.pc].d = encodeFixedCallCount(fact.argumentCount, true)
		case opCallMethodOne:
			marked[fact.pc].d = encodeFixedCallCount(fact.argumentCount-1, true)
		}
	}
	return marked
}

func bytecodeIRBlockPredecessors(successors [][]int) [][]int {
	predecessors := make([][]int, len(successors))
	for block, next := range successors {
		for _, successor := range next {
			if successor >= 0 && successor < len(predecessors) {
				predecessors[successor] = append(predecessors[successor], block)
			}
		}
	}
	return predecessors
}

func bytecodeIRReachableBlocks(successors [][]int) []bool {
	if len(successors) == 0 {
		return nil
	}
	reachable := make([]bool, len(successors))
	worklist := []int{0}
	reachable[0] = true
	for len(worklist) != 0 {
		last := len(worklist) - 1
		block := worklist[last]
		worklist = worklist[:last]
		for _, successor := range successors[block] {
			if successor < 0 || successor >= len(reachable) || reachable[successor] {
				continue
			}
			reachable[successor] = true
			worklist = append(worklist, successor)
		}
	}
	return reachable
}
