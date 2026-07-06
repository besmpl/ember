package ember

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

func optimizeBytecode(code []instruction, options optimizationOptions) []instruction {
	if !options.enabled(optimizationBytecodePeephole) {
		return append([]instruction(nil), code...)
	}
	return peepholeBytecode(code)
}

func optimizeBytecodeIR(ir []bytecodeIRInstruction, options optimizationOptions) []bytecodeIRInstruction {
	return optimizeBytecodeIRWithConstants(ir, nil, options)
}

func optimizeBytecodeIRWithConstants(ir []bytecodeIRInstruction, constants []Value, options optimizationOptions) []bytecodeIRInstruction {
	return optimizeBytecodeIRWithFacts(ir, bytecodeIROptimizationFacts{constants: constants}, options)
}

type bytecodeIROptimizationFacts struct {
	constants        []Value
	numericAddModOps []numericAddModOp
}

func optimizeBytecodeIRWithFacts(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts, options optimizationOptions) []bytecodeIRInstruction {
	if !options.enabled(optimizationBytecodePeephole) {
		return append([]bytecodeIRInstruction(nil), ir...)
	}
	optimized := append([]bytecodeIRInstruction(nil), ir...)
	optimized = applyBytecodeIRRemovalSet(optimized, bytecodeIRPeepholeRemovalSet(optimized, assembleBytecodeIR(optimized)))
	optimized = applyBytecodeIRRemovalSet(optimized, bytecodeIRDeadCodeRemovalSet(optimized, facts))
	return optimized
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

func bytecodeIRDeadCodeRemovalSet(ir []bytecodeIRInstruction, facts bytecodeIROptimizationFacts) []bool {
	code := assembleBytecodeIR(ir)
	remove := make([]bool, len(ir))
	numberFacts := bytecodeIRNumberFactsBefore(code, facts, bytecodeIRBlockOrder(ir))
	liveness := bytecodeIRLiveness(ir)
	for _, live := range liveness {
		if !bytecodeIRBlockAllowsDeadCodeCleanup(code, live.block) {
			continue
		}
		liveRegisters := live.liveOut.copy()
		for pc := live.block.end - 1; pc >= live.block.start; pc-- {
			ins := code[pc]
			writes := bytecodeIRWrittenRegisters(ir[pc])
			reads := bytecodeIRReadRegisters(ir[pc])
			if len(writes) > 0 && instructionWritesOnlyDeadRegisters(writes, liveRegisters) && instructionCanRemoveWhenResultDead(ins, numberFacts[pc], facts) {
				remove[pc] = true
				continue
			}
			for _, register := range writes {
				delete(liveRegisters, register)
			}
			for _, register := range reads {
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
		opAddK, opSubK, opMulK, opDivK, opModK, opIDivK, opAddNumericModK,
		opTableInsert, opTableRemove, opCoroutineResume, opMathMin,
		opPrepareIter, opArrayNext, opArrayNextJump2,
		opNumericForCheck, opJumpIfNotEqualK, opJumpIfNotLessK,
		opJumpIfNotLess, opJumpIfNotGreater, opJumpIfModKNotEqualK,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK, opJumpIfRowStringFieldNotEqualK,
		opJumpIfRowStringFieldNotEqualField, opJumpIfRowStringFieldEqualField,
		opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK,
		opJumpIfRowStringFieldNotGreaterK, opJumpIfRowStringFieldGreaterK,
		opJumpIfStringFieldNotGreaterR, opJumpIfRowStringFieldNotGreaterR,
		opJumpIfRowStringFieldNotLessField,
		opJumpIfStringFieldFalse, opJumpIfStringFieldNil,
		opJumpIfStringFieldTrue, opJumpIfStringFieldNotNil,
		opGetField, opSetField, opGetIndex, opSetIndex, opGetStringField, opSetStringField,
		opGetRowStringField, opSetRowStringField, opGetStringField2, opSetStringField2,
		opGetStringFieldIndex, opSetStringFieldIndex:
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

func instructionWritesOnlyDeadRegisters(writes []int, liveRegisters registerSet) bool {
	for _, register := range writes {
		if liveRegisters[register] {
			return false
		}
	}
	return true
}

func instructionCanRemoveWhenResultDead(ins instruction, numberFacts registerSet, facts bytecodeIROptimizationFacts) bool {
	if opcodeTransfersControl(ins.op) ||
		opcodeMayCall(ins.op) ||
		opcodeReadsTable(ins.op) ||
		opcodeWritesTable(ins.op) ||
		opcodeReadsGlobal(ins.op) ||
		opcodeWritesGlobal(ins.op) ||
		opcodeAllocates(ins.op) {
		return false
	}
	switch ins.op {
	case opLoadConst, opMove:
		return true
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		return numberFacts[ins.b] && numberFacts[ins.c]
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return numberFacts[ins.b] && constantIsNumber(facts, ins.c)
	case opAddNumericModK:
		return numberFacts[ins.a] && numberFacts[ins.b] && numericAddModConstantsAreNumbers(facts, ins.c)
	case opNeg:
		return numberFacts[ins.b]
	default:
		return false
	}
}

func bytecodeIRNumberFactsBefore(code []instruction, facts bytecodeIROptimizationFacts, blocks []bytecodeIRBlock) []registerSet {
	factsBefore := make([]registerSet, len(code))
	for _, block := range blocks {
		numberFacts := make(registerSet)
		for pc := block.start; pc < block.end; pc++ {
			factsBefore[pc] = numberFacts.copy()
			applyInstructionNumberFacts(numberFacts, code[pc], facts)
		}
	}
	for pc := range factsBefore {
		if factsBefore[pc] == nil {
			factsBefore[pc] = make(registerSet)
		}
	}
	return factsBefore
}

func applyInstructionNumberFacts(numberFacts registerSet, ins instruction, facts bytecodeIROptimizationFacts) {
	if instructionClearsAllNumberFacts(ins) {
		for register := range numberFacts {
			delete(numberFacts, register)
		}
		return
	}
	producesNumber := instructionProducesNumber(ins, numberFacts, facts)
	writes := registersMatching(ins, func(register int) bool {
		return instructionWritesRegister(ins, register)
	})
	for _, register := range writes {
		delete(numberFacts, register)
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
		return numberFacts[ins.b]
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		return numberFacts[ins.b] && numberFacts[ins.c]
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return numberFacts[ins.b] && constantIsNumber(facts, ins.c)
	case opAddNumericModK:
		return numberFacts[ins.a] && numberFacts[ins.b] && numericAddModConstantsAreNumbers(facts, ins.c)
	case opNeg:
		return numberFacts[ins.b]
	default:
		return false
	}
}

func numericAddModConstantsAreNumbers(facts bytecodeIROptimizationFacts, index int) bool {
	if index < 0 || index >= len(facts.numericAddModOps) {
		return false
	}
	desc := facts.numericAddModOps[index]
	return constantIsNumber(facts, desc.mul) && constantIsNumber(facts, desc.idiv) && constantIsNumber(facts, desc.mod)
}

func constantIsNumber(facts bytecodeIROptimizationFacts, index int) bool {
	return index >= 0 && index < len(facts.constants) && facts.constants[index].kind == NumberKind
}

func bytecodeIRPeepholeRemovalSet(ir []bytecodeIRInstruction, code []instruction) []bool {
	remove := make([]bool, len(ir))
	liveness := bytecodeIRLiveness(ir)
	for _, live := range liveness {
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
	return !liveOut[register]
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

func optimizeExpression(expr expression, options optimizationOptions) expression {
	if !options.enabled(optimizationHIRSimplify) {
		return expr
	}
	if number, ok := foldNumberExpression(expr); ok {
		return numberLiteralExpression(number)
	}
	return expr
}

func numberLiteralExpression(number float64) expression {
	return expression{
		terms: []andExpression{
			{
				terms: []comparisonExpression{
					{
						left: concatExpression{
							first: additiveExpression{
								first: multiplicativeExpression{
									first: term{number: &number},
								},
							},
						},
					},
				},
			},
		},
	}
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

func peepholeBytecode(code []instruction) []instruction {
	if bytecodeHasControlTransfers(code) {
		return append([]instruction(nil), code...)
	}

	optimized := make([]instruction, 0, len(code))
	for i := 0; i < len(code); i++ {
		ins := code[i]
		if ins.op == opMove && ins.a == ins.b {
			continue
		}
		if i+1 < len(code) && isDeadMoveRoundTrip(code, i) {
			i++
			continue
		}
		optimized = append(optimized, ins)
	}
	return optimized
}

func bytecodeHasControlTransfers(code []instruction) bool {
	for _, ins := range code {
		if opcodeHasJumpTarget(ins.op) {
			return true
		}
	}
	return false
}

func isDeadMoveRoundTrip(code []instruction, first int) bool {
	left := code[first]
	right := code[first+1]
	if left.op != opMove || right.op != opMove {
		return false
	}
	if left.a != right.b || left.b != right.a || left.a == left.b {
		return false
	}
	return registerDeadAfter(code[first+2:], left.a)
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

func instructionReadsRegister(ins instruction, register int) bool {
	switch ins.op {
	case opMove:
		return ins.b == register
	case opSetGlobal:
		return ins.b == register
	case opSetField, opSetStringField, opSetRowStringField:
		return ins.a == register || ins.c == register
	case opSetStringField2:
		return ins.a == register || ins.d == register
	case opSetStringFieldIndex:
		return ins.a == register || ins.c == register || ins.d == register
	case opGetField, opGetStringField, opGetRowStringField, opGetStringField2:
		return ins.b == register
	case opGetStringFieldIndex:
		return ins.b == register || ins.d == register
	case opAddStringField, opSubStringField, opSubAddStringField:
		return ins.a == register || ins.c == register
	case opAddSubStringField2:
		return ins.a == register
	case opSetIndex:
		return ins.a == register || ins.b == register || ins.c == register
	case opGetIndex:
		return ins.b == register || ins.c == register
	case opSetUpvalue:
		return ins.b == register
	case opPrepareIter:
		return ins.a == register
	case opArrayNext:
		return ins.a == register || ins.b == register || ins.c == register
	case opArrayNextJump2:
		return ins.a == register || ins.b == register || ins.c == register
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat,
		opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		return ins.b == register || ins.c == register
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return ins.b == register
	case opAddNumericModK:
		return ins.a == register || ins.b == register
	case opNumericForCheck:
		return ins.a == register || ins.b == register || ins.c == register
	case opJumpIfNotLess, opJumpIfNotGreater:
		return ins.a == register || ins.b == register
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfModKNotEqualK,
		opJumpIfTableHasMetatable,
		opJumpIfStringFieldNotEqualK, opJumpIfRowStringFieldNotEqualK,
		opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK,
		opJumpIfRowStringFieldNotGreaterK, opJumpIfRowStringFieldGreaterK,
		opJumpIfStringFieldFalse, opJumpIfStringFieldNil, opJumpIfStringFieldTrue, opJumpIfStringFieldNotNil:
		return ins.a == register
	case opJumpIfRowStringFieldNotEqualField, opJumpIfRowStringFieldEqualField:
		return ins.a == register || ins.c == register
	case opJumpIfStringFieldNotGreaterR, opJumpIfRowStringFieldNotGreaterR:
		return ins.a == register || ins.c == register
	case opJumpIfRowStringFieldNotLessField:
		return ins.a == register
	case opNeg, opLen:
		return ins.b == register
	case opTableInsert, opTableRemove, opCoroutineResume, opMathMin:
		return register >= ins.a && register <= ins.a+ins.b
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
		return ins.b == register || (register >= ins.c && register < ins.c+ins.d)
	case opCallUpvalueOne:
		return register >= ins.c && register < ins.c+ins.d
	case opCallMethodOne:
		return ins.b == register || (register >= ins.a+2 && register <= ins.a+1+ins.d)
	case opCallTableFieldKeyOne:
		argCount := tableFieldKeyCallArgCount(ins.d)
		return ins.b == register ||
			(register >= ins.a+1 && register <= ins.a+argCount+1)
	case opJumpIfFalse:
		return ins.a == register
	case opReturnOne:
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
	case opLoadConst, opLoadGlobal, opMove, opNewTable, opGetField, opGetStringField,
		opGetStringField2, opGetStringFieldIndex, opGetIndex,
		opClosure, opGetUpvalue, opVararg, opAdd, opSub, opMul, opDiv, opMod,
		opIDiv, opPow, opNeg, opLen, opConcat, opEqual, opNotEqual, opLess,
		opLessEqual, opGreater, opGreaterEqual, opAddK, opSubK, opMulK,
		opDivK, opModK, opIDivK, opAddNumericModK, opCoroutineResume, opMathMin, opSelectVarargCount:
		if ins.op == opVararg && ins.b > 0 {
			return register >= ins.a && register < ins.a+ins.b
		}
		return ins.a == register
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
	case opCallTableFieldKeyOne:
		return register == ins.a
	default:
		return false
	}
}
