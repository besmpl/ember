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
	if !options.enabled(optimizationBytecodePeephole) {
		return append([]bytecodeIRInstruction(nil), ir...)
	}
	code := assembleBytecodeIR(ir)
	if bytecodeHasControlTransfers(code) {
		return append([]bytecodeIRInstruction(nil), ir...)
	}

	optimized := make([]bytecodeIRInstruction, 0, len(ir))
	for i := 0; i < len(ir); i++ {
		ins := code[i]
		if ins.op == opMove && ins.a == ins.b {
			continue
		}
		if i+1 < len(code) && isDeadMoveRoundTrip(code, i) {
			i++
			continue
		}
		optimized = append(optimized, ir[i])
	}
	return optimized
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
		switch ins.op {
		case opJump, opJumpIfFalse, opNumericForCheck, opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfStringFieldNotEqualK, opJumpIfRowStringFieldNotEqualK, opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK, opJumpIfStringFieldFalse:
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
	case opSetField, opSetStringField:
		return ins.a == register || ins.c == register
	case opSetStringField2:
		return ins.a == register || ins.d == register
	case opGetField, opGetStringField, opGetStringField2:
		return ins.b == register
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
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat,
		opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		return ins.b == register || ins.c == register
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return ins.b == register
	case opAddNumericModK:
		return ins.a == register || ins.b == register
	case opNumericForCheck:
		return ins.a == register || ins.b == register || ins.c == register
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfStringFieldNotEqualK, opJumpIfRowStringFieldNotEqualK, opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK, opJumpIfStringFieldFalse:
		return ins.a == register
	case opNeg, opLen:
		return ins.b == register
	case opCall, opCallOne:
		if ins.b == register {
			return true
		}
		if ins.c < 0 {
			return register > ins.b
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
		opGetStringField2, opGetIndex,
		opClosure, opGetUpvalue, opVararg, opAdd, opSub, opMul, opDiv, opMod,
		opIDiv, opPow, opNeg, opLen, opConcat, opEqual, opNotEqual, opLess,
		opLessEqual, opGreater, opGreaterEqual, opAddK, opSubK, opMulK,
		opDivK, opModK, opIDivK, opAddNumericModK, opCoroutineResume, opMathMin, opSelectVarargCount:
		return ins.a == register
	case opPrepareIter:
		return ins.a == register || ins.b == register || ins.c == register
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
