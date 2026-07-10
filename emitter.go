package ember

import (
	"fmt"
	"sort"
)

type compiler struct {
	bytecodeBuilder
	bind                     bindResult
	sourceLines              sourceLineMap
	symbolRegisters          map[int]int
	locals                   map[string]int
	localStringSlots         map[int]map[string]int
	localRowStringSlots      map[int]map[string]int
	localArrayElemSlots      map[int]map[string]int
	localFieldArrayElemSlots map[int]map[string]map[string]int
	localArrayElemFieldSlots map[int]map[string]map[string]int
	parent                   *compiler
	selfFunctionSymbol       int
	selfNumericPairAdd       bool
	selfNumericPairBase      float64
	variadic                 bool
	upvalues                 map[string]int
	upvaluesByID             map[int]int
	upvalueDescs             []upvalueDesc
	loops                    []loopContext
	prototypeDrafts          []*functionDraft
	nextReg                  int
	freeTemps                []int
	suppressTagChains        bool
	options                  compilerOptions
}

type variableKind int

const (
	variableLocal variableKind = iota
	variableUpvalue
)

type variableRef struct {
	kind  variableKind
	index int
}

type loopContext struct {
	breakJumps     []int
	continueTarget int
	continueJumps  []int
}

func compileProgram(source sourceArtifact) (*Proto, error) {
	return compileProgramWithOptions(source, defaultCompilerOptions())
}

func compileProgramWithOptions(source sourceArtifact, options compilerOptions) (*Proto, error) {
	c := compiler{
		bind:                     source.bind,
		sourceLines:              newSourceLineMap(source.source.Text),
		symbolRegisters:          make(map[int]int),
		locals:                   make(map[string]int),
		localStringSlots:         make(map[int]map[string]int),
		localRowStringSlots:      make(map[int]map[string]int),
		localArrayElemSlots:      make(map[int]map[string]int),
		localFieldArrayElemSlots: make(map[int]map[string]map[string]int),
		localArrayElemFieldSlots: make(map[int]map[string]map[string]int),
		selfFunctionSymbol:       -1,
		options:                  options,
	}
	c.sourceText = source.source.Text

	if err := c.compileStatements(source.program.statements); err != nil {
		return nil, err
	}
	if !statementsHaveReturn(source.program.statements) {
		c.emit(instruction{op: opReturn})
	}

	c.optimizeFunction(options.optimizations)
	draft := c.buildFunctionDraft(nil, 0, false)
	return sealFunctionDraft(draft)
}

func (c *compiler) buildFunctionDraft(upvalues []upvalueDesc, params int, variadic bool) *functionDraft {
	c.shrinkCompiledFrameRegisters(params, variadic)
	assembly := assembleFunctionBytecode(c.sourceLines, c.ir)
	registers := compactedCompiledRegisterCount(assembly.code, c.prototypeDrafts, c.nextReg, params)
	return newFunctionDraft(c.constants, assembly, c.prototypeDrafts, upvalues, registers, params, variadic)
}

func (c *compiler) shrinkCompiledFrameRegisters(params int, variadic bool) {
	if c == nil ||
		c.parent != nil ||
		variadic ||
		len(c.prototypeDrafts) != 0 ||
		len(c.upvalueDescs) != 0 ||
		c.selfFunctionSymbol >= 0 ||
		!bytecodeIRFrameShrinkSafe(c.ir) {
		return
	}
	remap, ok := bytecodeIRLivenessRegisterRemap(c.ir, params)
	if !ok {
		return
	}
	for i := range c.ir {
		remapBytecodeIRRegisterOperands(&c.ir[i].operands, remap)
	}
}

func bytecodeIRFrameShrinkSafe(ir []bytecodeIRInstruction) bool {
	for _, ins := range assembleBytecodeIR(ir) {
		switch ins.op {
		case opLoadConst, opLoadGlobal, opSetGlobal, opMove,
			opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opConcat,
			opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual,
			opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
			opNeg, opLen,
			opReturnOne:
			continue
		default:
			return false
		}
	}
	return true
}

type registerLiveInterval struct {
	register int
	start    int
	end      int
	color    int
}

func bytecodeIRLivenessRegisterRemap(ir []bytecodeIRInstruction, params int) ([]int, bool) {
	code := assembleBytecodeIR(ir)
	intervalByRegister := make(map[int]*registerLiveInterval)
	touch := func(register int, pc int) {
		if register < 0 {
			return
		}
		interval := intervalByRegister[register]
		if interval == nil {
			interval = &registerLiveInterval{register: register, start: pc, end: pc, color: register}
			intervalByRegister[register] = interval
			return
		}
		if pc < interval.start {
			interval.start = pc
		}
		if pc > interval.end {
			interval.end = pc
		}
	}
	for register := 0; register < params; register++ {
		touch(register, 0)
	}
	for pc, ins := range code {
		registers := instructionRegisters(ins, instructionRegisterReadWrite)
		for register, ok := registers.next(); ok; register, ok = registers.next() {
			touch(register, pc)
		}
	}
	if len(intervalByRegister) == 0 {
		return nil, false
	}
	intervals := make([]*registerLiveInterval, 0, len(intervalByRegister))
	maxRegister := -1
	for _, interval := range intervalByRegister {
		intervals = append(intervals, interval)
		if interval.register > maxRegister {
			maxRegister = interval.register
		}
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].start != intervals[j].start {
			return intervals[i].start < intervals[j].start
		}
		return intervals[i].register < intervals[j].register
	})

	var active []*registerLiveInterval
	for _, interval := range intervals {
		if interval.register < params {
			interval.color = interval.register
			active = append(active, interval)
			continue
		}
		active = liveIntervalsActiveAt(active, interval.start)
		used := make(map[int]bool, len(active))
		for _, existing := range active {
			used[existing.color] = true
		}
		color := params
		for used[color] {
			color++
		}
		interval.color = color
		active = append(active, interval)
	}

	remap := make([]int, maxRegister+1)
	changed := false
	for register := range remap {
		remap[register] = register
	}
	for _, interval := range intervals {
		remap[interval.register] = interval.color
		if interval.register != interval.color {
			changed = true
		}
	}
	return remap, changed
}

func liveIntervalsActiveAt(active []*registerLiveInterval, pc int) []*registerLiveInterval {
	kept := active[:0]
	for _, interval := range active {
		if interval.end >= pc {
			kept = append(kept, interval)
		}
	}
	return kept
}

func remapBytecodeIRRegisterOperands(operands *bytecodeOperands, remap []int) {
	remapOperand := func(operand *bytecodeOperand) {
		if operand.kind != bytecodeOperandRegister || operand.value < 0 || operand.value >= len(remap) {
			return
		}
		operand.value = remap[operand.value]
	}
	remapOperand(&operands.a)
	remapOperand(&operands.b)
	remapOperand(&operands.c)
	remapOperand(&operands.d)
}

func compactedCompiledRegisterCount(code []instruction, children []*functionDraft, allocated int, params int) int {
	limit := allocated
	if limit < params {
		limit = params
	}
	maxRegister := params - 1
	for _, ins := range code {
		registers := instructionRegisters(ins, instructionRegisterReadWrite)
		for register, ok := registers.next(); ok; register, ok = registers.next() {
			if register < limit && register > maxRegister {
				maxRegister = register
			}
		}
	}
	for _, child := range children {
		for _, desc := range child.upvalues {
			if desc.local && desc.index > maxRegister {
				maxRegister = desc.index
			}
		}
	}
	if maxRegister < 0 {
		return 0
	}
	return maxRegister + 1
}

func (c *compiler) addConstant(value Value) int {
	return c.bytecodeBuilder.addConstant(value)
}

func (c *compiler) compileStatements(statements []statement) error {
	for _, stmt := range statements {
		if err := c.compileStatement(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileStatement(stmt statement) error {
	return c.compileLoweredStatement(lowerStatement(stmt))
}

func (c *compiler) compileLoweredStatement(stmt loweredStatement) error {
	switch stmt.kind {
	case loweredStatementLocal:
		return c.compileLoweredLocal(*stmt.local)
	case loweredStatementLocalFunction:
		return c.compileLocalFunction(*stmt.localFunction)
	case loweredStatementFunctionDeclaration:
		return c.compileFunctionDeclaration(*stmt.functionDeclaration)
	case loweredStatementAssignment:
		return c.compileLoweredAssignment(*stmt.assignment)
	case loweredStatementCall:
		return c.compileLoweredCallStatement(*stmt.call)
	case loweredStatementIf:
		return c.compileLoweredIf(*stmt.ifStatement)
	case loweredStatementWhile:
		return c.compileWhile(*stmt.while)
	case loweredStatementNumericFor:
		return c.compileFor(*stmt.numericFor)
	case loweredStatementGenericFor:
		return c.compileGenericFor(*stmt.genericFor)
	case loweredStatementRepeat:
		return c.compileRepeat(*stmt.repeat)
	case loweredStatementBlock:
		return c.compileLoweredBlock(*stmt.block)
	case loweredStatementTypeAlias:
		return nil
	case loweredStatementBreak:
		return c.compileBreak()
	case loweredStatementContinue:
		return c.compileContinue()
	case loweredStatementReturn:
		return c.compileLoweredReturn(*stmt.ret)
	case loweredStatementEmpty:
		return fmt.Errorf("compile: empty statement")
	default:
		return fmt.Errorf("compile: unknown lowered statement kind %d", stmt.kind)
	}
}

func (c *compiler) compileLocal(stmt localStatement) error {
	return c.compileLoweredLocal(lowerLocal(stmt))
}

func (c *compiler) compileLoweredLocal(lowered loweredLocal) error {
	if len(lowered.names) == 0 {
		return fmt.Errorf("compile: local statement has no names")
	}

	first := c.allocReg()
	targets := make([]int, len(lowered.names))
	for i := range targets {
		targets[i] = first + i
	}
	c.reserveRegistersThrough(first + len(targets))

	if err := c.compileLoweredValueListTo(lowered.values, lowered.sources, targets); err != nil {
		return err
	}
	for i, name := range lowered.names {
		c.locals[name] = targets[i]
		if i < len(lowered.values.items) {
			item := lowered.values.items[i]
			if item.kind == loweredValueSingle && item.source >= 0 {
				if slots, ok := expressionNamedTableFieldSlots(lowered.sources[item.source]); ok {
					c.localStringSlots[targets[i]] = slots
				}
				if slots, ok := expressionArrayElementNamedTableFieldSlots(lowered.sources[item.source]); ok {
					c.localArrayElemSlots[targets[i]] = slots
				}
				if slots, ok := expressionArrayElementFieldArrayElementSlots(lowered.sources[item.source]); ok {
					c.localArrayElemFieldSlots[targets[i]] = slots
				}
				if slots, ok := c.expressionIndexedLocalArrayElementSlots(lowered.sources[item.source]); ok {
					c.localStringSlots[targets[i]] = slots
					c.localRowStringSlots[targets[i]] = slots
				}
				if slots, ok := c.expressionIndexedLocalArrayElementFieldSlots(lowered.sources[item.source]); ok {
					c.localFieldArrayElemSlots[targets[i]] = slots
				}
				if slots, ok := c.expressionLocalFieldArrayElementSlots(lowered.sources[item.source]); ok {
					c.localArrayElemSlots[targets[i]] = slots
				}
			}
		}
		if symbol, ok := c.claimSymbol(syntaxNameID(lowered.nameID, i), symbolLocal); ok {
			c.symbolRegisters[symbol.id] = targets[i]
		}
	}
	return nil
}

func (c *compiler) compileReturn(stmt returnStatement) error {
	return c.compileLoweredReturn(lowerReturn(stmt))
}

func (c *compiler) compileLoweredReturn(lowered loweredReturn) error {
	if len(lowered.sources) == 0 {
		c.emit(instruction{op: opReturn})
		return nil
	}

	list := lowered.values
	if len(list.items) == 1 && list.items[0].kind == loweredValueSingle {
		if ref, ok := c.expressionLocalRef(lowered.sources[list.items[0].source]); ok {
			c.emit(instruction{op: opReturnOne, a: ref.index})
			return nil
		}
	}
	first := c.allocReg()
	for i, item := range list.items {
		target := first + i
		c.reserveRegistersThrough(target + 1)
		switch item.kind {
		case loweredValueExpanded:
			if vararg, ok := expressionSingleVararg(lowered.sources[item.source]); ok {
				if err := c.compileVarargToResults(vararg, target, item.resultCount); err != nil {
					return err
				}
			} else if call, ok := expressionSingleCall(lowered.sources[item.source]); ok {
				if err := c.compileCallToResults(call, target, item.resultCount); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("compile: expanded return value is not a call or vararg")
			}
			c.emit(instruction{op: opReturn, a: first, b: -(i + 1)})
			return nil
		case loweredValueSingle:
			if err := c.compileExpressionTo(lowered.sources[item.source], target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("compile: unknown lowered value kind %d", item.kind)
		}
	}
	c.reserveRegistersThrough(first + len(list.items))
	if len(list.items) == 1 {
		c.emit(instruction{op: opReturnOne, a: first})
		return nil
	}
	c.emit(instruction{op: opReturn, a: first, b: len(list.items)})
	return nil
}

func (c *compiler) compileCallStatement(stmt term) error {
	return c.compileLoweredCallStatement(lowerCallStatement(stmt))
}

func (c *compiler) compileLoweredCallStatement(lowered loweredCallStatement) error {
	result := c.allocReg()
	return c.compileLoweredCallToResults(lowered.call, lowered.args, result, lowered.resultCount)
}

func (c *compiler) compileExpressionListTo(values []expression, targets []int) error {
	if len(targets) == 0 {
		return nil
	}
	return c.compileLoweredValueListTo(lowerFixedValueList(values, len(targets)), values, targets)
}

func (c *compiler) compileLoweredValueListTo(list loweredValueList, values []expression, targets []int) error {
	for i, item := range list.items {
		target := targets[i]
		c.reserveRegistersThrough(target + 1)
		switch item.kind {
		case loweredValueNil:
			c.compileNilTo(target)
			continue
		case loweredValueExpanded:
			if vararg, ok := expressionSingleVararg(values[item.source]); ok {
				return c.compileVarargToResults(vararg, target, item.resultCount)
			}
			if call, ok := expressionSingleCall(values[item.source]); ok {
				return c.compileCallToResults(call, target, item.resultCount)
			}
			return fmt.Errorf("compile: expanded value is not a call or vararg")
		case loweredValueSingle:
			if err := c.compileExpressionTo(values[item.source], target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("compile: unknown lowered value kind %d", item.kind)
		}
	}
	return nil
}

func (c *compiler) compileNilTo(target int) {
	c.emitLoadConst(target, NilValue())
}

func (c *compiler) compileLocalFunction(stmt localFunctionStatement) error {
	closure := lowerLocalFunctionClosure(stmt)
	target := c.allocReg()
	c.locals[stmt.name] = target
	selfFunctionSymbol := -1
	if symbol, ok := c.claimSymbol(stmt.nameID, symbolLocalFunction); ok {
		c.symbolRegisters[symbol.id] = target
		selfFunctionSymbol = symbol.id
	}
	if err := c.compileClosureToSelf(closure, target, selfFunctionSymbol); err != nil {
		return err
	}
	return nil
}

func (c *compiler) compileFunctionDeclaration(stmt functionDeclarationStatement) error {
	closure := lowerFunctionDeclarationClosure(stmt)

	value := c.allocReg()
	if err := c.compileClosureTo(closure, value); err != nil {
		return err
	}
	return c.compileAssignTargetFromRegister(stmt.target, value)
}

func (c *compiler) compileFunctionDraft(closure loweredClosure, selfFunctionSymbol int) (*functionDraft, error) {
	selfNumericPairBase, selfNumericPairAdd := selfNumericPairAddClosureBase(closure)
	fn := compiler{
		bind:                     c.bind,
		sourceLines:              c.sourceLines,
		symbolRegisters:          make(map[int]int),
		locals:                   make(map[string]int),
		localStringSlots:         make(map[int]map[string]int),
		localRowStringSlots:      make(map[int]map[string]int),
		localArrayElemSlots:      make(map[int]map[string]int),
		localFieldArrayElemSlots: make(map[int]map[string]map[string]int),
		localArrayElemFieldSlots: make(map[int]map[string]map[string]int),
		parent:                   c,
		selfFunctionSymbol:       selfFunctionSymbol,
		selfNumericPairAdd:       selfNumericPairAdd,
		selfNumericPairBase:      selfNumericPairBase,
		variadic:                 closure.variadic,
		upvalues:                 make(map[string]int),
		upvaluesByID:             make(map[int]int),
		nextReg:                  len(closure.params),
		options:                  c.options,
	}
	fn.sourceText = c.sourceText
	for i, param := range closure.params {
		fn.locals[param] = i
		if symbol, ok := fn.claimSymbol(syntaxNameID(closure.paramID, i), symbolParameter); ok {
			fn.symbolRegisters[symbol.id] = i
		}
	}
	if err := fn.compileStatements(closure.body); err != nil {
		return nil, err
	}
	if !statementsHaveReturn(closure.body) {
		fn.emit(instruction{op: opReturn})
	}

	fn.optimizeFunction(c.options.optimizations)
	return fn.buildFunctionDraft(fn.upvalueDescs, len(closure.params), closure.variadic), nil
}

func (c *compiler) compileExpression(expr expression) (int, error) {
	target := c.allocReg()
	if err := c.compileExpressionTo(expr, target); err != nil {
		return 0, err
	}
	return target, nil
}

func (c *compiler) compileExpressionTo(expr expression, target int) error {
	c.claimRegister(target)
	source := expressionRange(expr)
	return c.withSourceRange(source, func() error {
		expr = optimizeExpression(expr, c.options.optimizations)
		if len(expr.terms) == 0 {
			return fmt.Errorf("compile: empty expression")
		}

		if err := c.compileAndExpressionTo(expr.terms[0], target); err != nil {
			return err
		}

		for _, term := range expr.terms[1:] {
			jumpIfFalse := c.emitJumpIfFalse(target)
			jumpEnd := c.emitJump()

			c.patchJump(jumpIfFalse, c.pc())
			if err := c.compileAndExpressionTo(term, target); err != nil {
				return err
			}
			c.patchJump(jumpEnd, c.pc())
		}

		return nil
	})
}

func (c *compiler) compileAndExpressionTo(expr andExpression, target int) error {
	if len(expr.terms) == 0 {
		return fmt.Errorf("compile: empty expression")
	}

	if err := c.compileComparisonExpressionTo(expr.terms[0], target); err != nil {
		return err
	}

	for _, term := range expr.terms[1:] {
		jumpEnd := c.emitJumpIfFalse(target)
		if err := c.compileComparisonExpressionTo(term, target); err != nil {
			return err
		}
		c.patchJump(jumpEnd, c.pc())
	}

	return nil
}

func (c *compiler) compileComparisonExpressionTo(expr comparisonExpression, target int) error {
	if expr.op == "" {
		return c.compileConcatExpressionTo(expr.left, target)
	}

	if expr.right == nil {
		return fmt.Errorf("compile: missing comparison right operand")
	}

	if err := c.compileConcatExpressionTo(expr.left, target); err != nil {
		return err
	}

	right := c.allocTemp()
	if err := c.compileConcatExpressionTo(*expr.right, right); err != nil {
		c.releaseTemp(right)
		return err
	}

	switch expr.op {
	case comparisonEqual:
		c.emit(instruction{op: opEqual, a: target, b: target, c: right})
	case comparisonNotEqual:
		c.emit(instruction{op: opNotEqual, a: target, b: target, c: right})
	case comparisonLess:
		c.emit(instruction{op: opLess, a: target, b: target, c: right})
	case comparisonLessEqual:
		c.emit(instruction{op: opLessEqual, a: target, b: target, c: right})
	case comparisonGreater:
		c.emit(instruction{op: opGreater, a: target, b: target, c: right})
	case comparisonGreaterEqual:
		c.emit(instruction{op: opGreaterEqual, a: target, b: target, c: right})
	default:
		c.releaseTemp(right)
		return fmt.Errorf("compile: unsupported comparison %q", expr.op)
	}

	c.releaseTemp(right)
	return nil
}

func (c *compiler) compileConcatExpressionTo(expr concatExpression, target int) error {
	operandCount := 1 + len(expr.rest)
	if operandCount >= 3 && target+1 >= c.nextReg {
		return c.compileConcatChainExpressionTo(expr, target, operandCount)
	}

	if err := c.compileAdditiveExpressionTo(expr.first, target); err != nil {
		return err
	}

	for _, part := range expr.rest {
		right := c.allocTemp()
		if err := c.compileAdditiveExpressionTo(part, right); err != nil {
			c.releaseTemp(right)
			return err
		}
		c.emit(instruction{op: opConcat, a: target, b: target, c: right})
		c.releaseTemp(right)
	}

	return nil
}

func (c *compiler) compileConcatChainExpressionTo(expr concatExpression, target int, operandCount int) error {
	end := target + operandCount
	c.reserveRegistersThrough(end)
	c.claimRegisterRange(target, end)

	if err := c.compileAdditiveExpressionTo(expr.first, target); err != nil {
		return err
	}
	for index, part := range expr.rest {
		register := target + index + 1
		if err := c.compileAdditiveExpressionTo(part, register); err != nil {
			return err
		}
	}

	c.emit(instruction{op: opConcatChain, a: target, b: target, c: operandCount})
	for register := target + 1; register < end; register++ {
		c.releaseTemp(register)
	}
	return nil
}

func (c *compiler) compileAdditiveExpressionTo(expr additiveExpression, target int) error {
	if err := c.compileMultiplicativeExpressionTo(expr.first, target); err != nil {
		return err
	}

	for _, part := range expr.rest {
		if right, ok := foldNumberMultiplicative(part.value); ok {
			constant := c.addConstant(NumberValue(right))
			switch part.op {
			case additiveAdd:
				c.emit(instruction{op: opAddK, a: target, b: target, c: constant})
				continue
			case additiveSubtract:
				c.emit(instruction{op: opSubK, a: target, b: target, c: constant})
				continue
			}
		}
		right := c.allocArithmeticOperandRegister(part.value)
		if err := c.compileMultiplicativeExpressionTo(part.value, right); err != nil {
			c.releaseTemp(right)
			return err
		}
		switch part.op {
		case additiveAdd:
			c.emit(instruction{op: opAdd, a: target, b: target, c: right})
		case additiveSubtract:
			c.emit(instruction{op: opSub, a: target, b: target, c: right})
		default:
			c.releaseTemp(right)
			return fmt.Errorf("compile: unsupported additive operator %q", part.op)
		}
		c.releaseTemp(right)
	}

	return nil
}

func (c *compiler) allocArithmeticOperandRegister(expr multiplicativeExpression) int {
	if _, ok := multiplicativeSingleCall(expr); ok {
		return c.allocReg()
	}
	return c.allocTemp()
}

func multiplicativeSingleCall(expr multiplicativeExpression) (callExpression, bool) {
	if len(expr.rest) != 0 {
		return callExpression{}, false
	}
	value := termWithoutCasts(expr.first)
	if value.call == nil || len(value.selectors) != 0 {
		return callExpression{}, false
	}
	return *value.call, true
}

func (c *compiler) compileMultiplicativeExpressionTo(expr multiplicativeExpression, target int) error {
	if err := c.compileTermTo(expr.first, target); err != nil {
		return err
	}

	for _, part := range expr.rest {
		if right, ok := foldNumberTerm(part.value); ok {
			constant := c.addConstant(NumberValue(right))
			switch part.op {
			case multiplicativeMultiply:
				c.emit(instruction{op: opMulK, a: target, b: target, c: constant})
				continue
			case multiplicativeDivide:
				c.emit(instruction{op: opDivK, a: target, b: target, c: constant})
				continue
			case multiplicativeModulo:
				c.emit(instruction{op: opModK, a: target, b: target, c: constant})
				continue
			case multiplicativeFloorDiv:
				c.emit(instruction{op: opIDivK, a: target, b: target, c: constant})
				continue
			}
		}
		right := c.allocTemp()
		if err := c.compileTermTo(part.value, right); err != nil {
			c.releaseTemp(right)
			return err
		}
		switch part.op {
		case multiplicativeMultiply:
			c.emit(instruction{op: opMul, a: target, b: target, c: right})
		case multiplicativeDivide:
			c.emit(instruction{op: opDiv, a: target, b: target, c: right})
		case multiplicativeModulo:
			c.emit(instruction{op: opMod, a: target, b: target, c: right})
		case multiplicativeFloorDiv:
			c.emit(instruction{op: opIDiv, a: target, b: target, c: right})
		default:
			c.releaseTemp(right)
			return fmt.Errorf("compile: unsupported multiplicative operator %q", part.op)
		}
		c.releaseTemp(right)
	}

	return nil
}

func (c *compiler) compileTermTo(term term, target int) error {
	if len(term.selectors) > 0 {
		base := term
		base.selectors = nil
		if ref, ok := c.termLocalRef(base); ok {
			return c.compileSelectorsFromBaseTo(ref.index, term.selectors, target)
		}
		if isNamedTerm(base) {
			if err := c.compileNamedTermTo(base, target); err != nil {
				return err
			}
		} else {
			if err := c.compileTermTo(base, target); err != nil {
				return err
			}
		}
		return c.compileSelectorsTo(term.selectors, target)
	}

	if term.power != nil {
		return c.compilePowerTo(*term.power, target)
	}
	if term.number != nil {
		c.emitLoadConst(target, NumberValue(*term.number))
		return nil
	}
	if term.lit != nil {
		c.emitLoadConst(target, *term.lit)
		return nil
	}
	if term.table != nil {
		return c.compileTableTo(*term.table, target)
	}
	if term.function != nil {
		return c.compileClosureTo(lowerClosure(*term.function), target)
	}
	if term.ifExpr != nil {
		return c.compileIfExpressionTo(*term.ifExpr, target)
	}
	if term.call != nil {
		return c.compileCallTo(*term.call, target)
	}
	if term.vararg {
		return c.compileVarargToResults(term, target, 1)
	}
	if term.unaryNot != nil {
		return c.compileNotTo(*term.unaryNot, target)
	}
	if term.unaryMinus != nil {
		return c.compileUnaryMinusTo(*term.unaryMinus, target)
	}
	if term.unaryLen != nil {
		return c.compileLengthTo(*term.unaryLen, target)
	}
	if term.group != nil {
		return c.compileExpressionTo(*term.group, target)
	}

	return c.compileNamedTermTo(term, target)
}

func (c *compiler) compilePowerTo(power powerExpression, target int) error {
	if err := c.compileTermTo(power.base, target); err != nil {
		return err
	}
	exponent := c.allocTemp()
	if err := c.compileTermTo(power.exponent, exponent); err != nil {
		c.releaseTemp(exponent)
		return err
	}
	c.emit(instruction{op: opPow, a: target, b: target, c: exponent})
	c.releaseTemp(exponent)
	return nil
}

func (c *compiler) compileClosureTo(closure loweredClosure, target int) error {
	return c.compileClosureToSelf(closure, target, -1)
}

func (c *compiler) compileClosureToSelf(closure loweredClosure, target int, selfFunctionSymbol int) error {
	draft, err := c.compileFunctionDraft(closure, selfFunctionSymbol)
	if err != nil {
		return err
	}

	protoIndex := c.addFunctionDraft(draft)
	c.emit(instruction{op: opClosure, a: target, b: protoIndex})
	return nil
}

func (c *compiler) compileSelectorsTo(selectors []selector, target int) error {
	for len(selectors) > 0 {
		if len(selectors) >= 2 && selectors[0].field != "" && selectors[1].field != "" {
			firstKey := c.addConstant(StringValue(selectors[0].field))
			secondKey := c.addConstant(StringValue(selectors[1].field))
			c.emit(instruction{op: opGetStringField, a: target, b: target, c: firstKey})
			c.emit(instruction{op: opGetStringField, a: target, b: target, c: secondKey})
			selectors = selectors[2:]
			continue
		}
		if len(selectors) >= 2 && selectors[0].field != "" && selectors[1].index != nil {
			firstKey := c.addConstant(StringValue(selectors[0].field))
			key := c.allocReg()
			if err := c.compileExpressionTo(*selectors[1].index, key); err != nil {
				return err
			}
			c.emit(instruction{op: opGetStringFieldIndex, a: target, b: target, c: firstKey, d: key})
			selectors = selectors[2:]
			continue
		}

		selector := selectors[0]
		if selector.field != "" {
			key := c.addConstant(StringValue(selector.field))
			c.emit(instruction{op: opGetStringField, a: target, b: target, c: key})
			selectors = selectors[1:]
			continue
		}

		key := c.allocReg()
		if err := c.compileExpressionTo(*selector.index, key); err != nil {
			return err
		}
		c.emit(instruction{op: opGetIndex, a: target, b: target, c: key})
		selectors = selectors[1:]
	}
	return nil
}

func (c *compiler) compileSelectorsFromBaseTo(base int, selectors []selector, target int) error {
	if len(selectors) == 0 {
		if target != base {
			c.emit(instruction{op: opMove, a: target, b: base})
		}
		return nil
	}
	first := selectors[0]
	if len(selectors) >= 2 && first.field != "" && selectors[1].field != "" {
		firstKey := c.addConstant(StringValue(first.field))
		secondKey := c.addConstant(StringValue(selectors[1].field))
		c.emit(instruction{op: opGetStringField, a: target, b: base, c: firstKey})
		c.emit(instruction{op: opGetStringField, a: target, b: target, c: secondKey})
		return c.compileSelectorsTo(selectors[2:], target)
	}
	if len(selectors) >= 2 && first.field != "" && selectors[1].index != nil {
		firstKey := c.addConstant(StringValue(first.field))
		key := c.allocReg()
		if err := c.compileExpressionTo(*selectors[1].index, key); err != nil {
			return err
		}
		c.emit(instruction{op: opGetStringFieldIndex, a: target, b: base, c: firstKey, d: key})
		return c.compileSelectorsTo(selectors[2:], target)
	}
	if first.field != "" {
		key := c.addConstant(StringValue(first.field))
		c.emit(instruction{op: opGetStringField, a: target, b: base, c: key})
		return c.compileSelectorsTo(selectors[1:], target)
	}
	key := c.allocReg()
	if err := c.compileExpressionTo(*first.index, key); err != nil {
		return err
	}
	c.emit(instruction{op: opGetIndex, a: target, b: base, c: key})
	return c.compileSelectorsTo(selectors[1:], target)
}

func isNamedTerm(term term) bool {
	return term.name != "" &&
		term.number == nil &&
		term.lit == nil &&
		term.table == nil &&
		term.function == nil &&
		term.ifExpr == nil &&
		term.call == nil &&
		!term.vararg &&
		term.unaryNot == nil &&
		term.unaryMinus == nil &&
		term.unaryLen == nil &&
		term.group == nil &&
		len(term.selectors) == 0
}

func (c *compiler) compileCallTargetTo(term term, target int) error {
	if isNamedTerm(term) {
		return c.compileNamedTermTo(term, target)
	}
	return c.compileTermTo(term, target)
}

func (c *compiler) compileNotTo(term term, target int) error {
	if err := c.compileTermTo(term, target); err != nil {
		return err
	}

	jumpIfFalse := c.emitJumpIfFalse(target)

	c.emitLoadConst(target, BoolValue(false))

	jumpEnd := c.emitJump()

	c.patchJump(jumpIfFalse, c.pc())
	c.emitLoadConst(target, BoolValue(true))

	c.patchJump(jumpEnd, c.pc())
	return nil
}

func (c *compiler) compileUnaryMinusTo(term term, target int) error {
	if err := c.compileTermTo(term, target); err != nil {
		return err
	}
	c.emit(instruction{op: opNeg, a: target, b: target})
	return nil
}

func (c *compiler) compileLengthTo(term term, target int) error {
	if err := c.compileTermTo(term, target); err != nil {
		return err
	}
	c.emit(instruction{op: opLen, a: target, b: target})
	return nil
}

func (c *compiler) compileAssignment(stmt assignStatement) error {
	return c.compileLoweredAssignment(lowerAssignment(stmt))
}

func (c *compiler) compileLoweredAssignment(lowered loweredAssignment) error {
	if len(lowered.targets) == 0 {
		return fmt.Errorf("compile: assignment has no targets")
	}

	if c.canCompileSingleLocalAssignmentInPlace(lowered) {
		target := lowered.targets[0]
		ref, _ := c.resolveAssignTarget(target)
		return c.compileExpressionTo(lowered.sources[lowered.values.items[0].source], ref.index)
	}

	if addField, ok := c.addStringFieldAssignment(lowered); ok {
		return c.compileAddStringFieldAssignment(addField)
	}
	if subField, ok := c.subStringFieldAssignment(lowered); ok {
		return c.compileSubStringFieldAssignment(subField)
	}
	first := c.allocReg()
	values := make([]int, len(lowered.targets))
	for i := range values {
		values[i] = first + i
	}
	c.reserveRegistersThrough(first + len(values))
	if err := c.compileLoweredValueListTo(lowered.values, lowered.sources, values); err != nil {
		return err
	}

	for i, target := range lowered.targets {
		if err := c.compileAssignTargetFromRegister(target, values[i]); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) canCompileSingleLocalAssignmentInPlace(lowered loweredAssignment) bool {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) {
		return false
	}
	if len(lowered.targets) != 1 || len(lowered.values.items) != 1 {
		return false
	}
	target := lowered.targets[0]
	if len(target.selectors) != 0 {
		return false
	}
	item := lowered.values.items[0]
	if item.kind != loweredValueSingle {
		return false
	}
	ref, ok := c.resolveAssignTarget(target)
	if !ok || ref.kind != variableLocal {
		return false
	}
	return expressionCanAssignToNameInPlace(lowered.sources[item.source], target.name)
}

func expressionCanAssignToNameInPlace(expr expression, name string) bool {
	if !expressionReferencesName(expr, name) {
		return true
	}
	if len(expr.terms) == 0 {
		return true
	}
	if !andExpressionCanAssignToNameInPlace(expr.terms[0], name) {
		return false
	}
	for _, term := range expr.terms[1:] {
		if andExpressionReferencesName(term, name) {
			return false
		}
	}
	return true
}

func andExpressionCanAssignToNameInPlace(expr andExpression, name string) bool {
	if !andExpressionReferencesName(expr, name) {
		return true
	}
	if len(expr.terms) == 0 {
		return true
	}
	if !comparisonExpressionCanAssignToNameInPlace(expr.terms[0], name) {
		return false
	}
	for _, term := range expr.terms[1:] {
		if comparisonExpressionReferencesName(term, name) {
			return false
		}
	}
	return true
}

func comparisonExpressionCanAssignToNameInPlace(expr comparisonExpression, name string) bool {
	if !comparisonExpressionReferencesName(expr, name) {
		return true
	}
	if !concatExpressionCanAssignToNameInPlace(expr.left, name) {
		return false
	}
	return expr.right == nil || !concatExpressionReferencesName(*expr.right, name)
}

func concatExpressionCanAssignToNameInPlace(expr concatExpression, name string) bool {
	if !concatExpressionReferencesName(expr, name) {
		return true
	}
	if !additiveExpressionCanAssignToNameInPlace(expr.first, name) {
		return false
	}
	for _, part := range expr.rest {
		if additiveExpressionReferencesName(part, name) {
			return false
		}
	}
	return true
}

func additiveExpressionCanAssignToNameInPlace(expr additiveExpression, name string) bool {
	if !additiveExpressionReferencesName(expr, name) {
		return true
	}
	if !multiplicativeExpressionCanAssignToNameInPlace(expr.first, name) {
		return false
	}
	for _, part := range expr.rest {
		if multiplicativeExpressionReferencesName(part.value, name) {
			return false
		}
	}
	return true
}

func multiplicativeExpressionCanAssignToNameInPlace(expr multiplicativeExpression, name string) bool {
	if !multiplicativeExpressionReferencesName(expr, name) {
		return true
	}
	if !termCanAssignToNameInPlace(expr.first, name) {
		return false
	}
	for _, part := range expr.rest {
		if termReferencesName(part.value, name) {
			return false
		}
	}
	return true
}

func termCanAssignToNameInPlace(term term, name string) bool {
	if !termReferencesName(term, name) {
		return true
	}
	if term.name == name {
		for _, selector := range term.selectors {
			if selector.index != nil && expressionReferencesName(*selector.index, name) {
				return false
			}
		}
		return true
	}
	if term.unaryNot != nil {
		return termCanAssignToNameInPlace(*term.unaryNot, name)
	}
	if term.unaryMinus != nil {
		return termCanAssignToNameInPlace(*term.unaryMinus, name)
	}
	if term.unaryLen != nil {
		return termCanAssignToNameInPlace(*term.unaryLen, name)
	}
	if term.power != nil {
		return termCanAssignToNameInPlace(term.power.base, name) &&
			!termReferencesName(term.power.exponent, name)
	}
	if term.group != nil {
		return expressionCanAssignToNameInPlace(*term.group, name)
	}
	return false
}

func expressionReferencesName(expr expression, name string) bool {
	for _, term := range expr.terms {
		if andExpressionReferencesName(term, name) {
			return true
		}
	}
	return false
}

func andExpressionReferencesName(expr andExpression, name string) bool {
	for _, term := range expr.terms {
		if comparisonExpressionReferencesName(term, name) {
			return true
		}
	}
	return false
}

func comparisonExpressionReferencesName(expr comparisonExpression, name string) bool {
	if concatExpressionReferencesName(expr.left, name) {
		return true
	}
	return expr.right != nil && concatExpressionReferencesName(*expr.right, name)
}

func concatExpressionReferencesName(expr concatExpression, name string) bool {
	if additiveExpressionReferencesName(expr.first, name) {
		return true
	}
	for _, part := range expr.rest {
		if additiveExpressionReferencesName(part, name) {
			return true
		}
	}
	return false
}

func additiveExpressionReferencesName(expr additiveExpression, name string) bool {
	if multiplicativeExpressionReferencesName(expr.first, name) {
		return true
	}
	for _, part := range expr.rest {
		if multiplicativeExpressionReferencesName(part.value, name) {
			return true
		}
	}
	return false
}

func multiplicativeExpressionReferencesName(expr multiplicativeExpression, name string) bool {
	if termReferencesName(expr.first, name) {
		return true
	}
	for _, part := range expr.rest {
		if termReferencesName(part.value, name) {
			return true
		}
	}
	return false
}

func termReferencesName(term term, name string) bool {
	if term.name == name {
		return true
	}
	if term.table != nil && tableExpressionReferencesName(*term.table, name) {
		return true
	}
	if term.ifExpr != nil &&
		(expressionReferencesName(term.ifExpr.condition, name) ||
			expressionReferencesName(term.ifExpr.thenValue, name) ||
			expressionReferencesName(term.ifExpr.elseValue, name)) {
		return true
	}
	if term.call != nil && callExpressionReferencesName(*term.call, name) {
		return true
	}
	if term.unaryNot != nil && termReferencesName(*term.unaryNot, name) {
		return true
	}
	if term.unaryMinus != nil && termReferencesName(*term.unaryMinus, name) {
		return true
	}
	if term.unaryLen != nil && termReferencesName(*term.unaryLen, name) {
		return true
	}
	if term.power != nil &&
		(termReferencesName(term.power.base, name) || termReferencesName(term.power.exponent, name)) {
		return true
	}
	if term.group != nil && expressionReferencesName(*term.group, name) {
		return true
	}
	for _, selector := range term.selectors {
		if selector.index != nil && expressionReferencesName(*selector.index, name) {
			return true
		}
	}
	return false
}

func tableExpressionReferencesName(table tableExpression, name string) bool {
	for _, field := range table.fields {
		if field.key != nil && expressionReferencesName(*field.key, name) {
			return true
		}
		if expressionReferencesName(field.value, name) {
			return true
		}
	}
	return false
}

func callExpressionReferencesName(call callExpression, name string) bool {
	if termReferencesName(call.target, name) {
		return true
	}
	if call.receiver != nil && termReferencesName(*call.receiver, name) {
		return true
	}
	for _, arg := range call.args {
		if expressionReferencesName(arg, name) {
			return true
		}
	}
	return false
}

type addStringFieldAssignment struct {
	table   int
	field   string
	operand expression
	slot    int
}

type subStringFieldAssignment struct {
	table   int
	field   string
	operand expression
	slot    int
}

func (c *compiler) addStringFieldAssignment(lowered loweredAssignment) (addStringFieldAssignment, bool) {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) {
		return addStringFieldAssignment{}, false
	}
	if len(lowered.targets) != 1 || len(lowered.values.items) != 1 {
		return addStringFieldAssignment{}, false
	}
	item := lowered.values.items[0]
	if item.kind != loweredValueSingle {
		return addStringFieldAssignment{}, false
	}
	target := lowered.targets[0]
	if len(target.selectors) != 1 || target.selectors[0].field == "" {
		return addStringFieldAssignment{}, false
	}
	ref, ok := c.resolveAssignTarget(target)
	if !ok || ref.kind != variableLocal {
		return addStringFieldAssignment{}, false
	}
	operand, ok := fieldAddAssignmentOperand(lowered.sources[item.source], target)
	if !ok {
		return addStringFieldAssignment{}, false
	}
	slot := -1
	if slots, ok := c.localStringSlots[ref.index]; ok {
		if fieldSlot, ok := slots[target.selectors[0].field]; ok {
			slot = fieldSlot
		}
	}
	return addStringFieldAssignment{
		table:   ref.index,
		field:   target.selectors[0].field,
		operand: operand,
		slot:    slot,
	}, true
}

func (c *compiler) subStringFieldAssignment(lowered loweredAssignment) (subStringFieldAssignment, bool) {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) {
		return subStringFieldAssignment{}, false
	}
	if len(lowered.targets) != 1 || len(lowered.values.items) != 1 {
		return subStringFieldAssignment{}, false
	}
	item := lowered.values.items[0]
	if item.kind != loweredValueSingle {
		return subStringFieldAssignment{}, false
	}
	target := lowered.targets[0]
	if len(target.selectors) != 1 || target.selectors[0].field == "" {
		return subStringFieldAssignment{}, false
	}
	ref, ok := c.resolveAssignTarget(target)
	if !ok || ref.kind != variableLocal {
		return subStringFieldAssignment{}, false
	}
	operand, ok := fieldSubAssignmentOperand(lowered.sources[item.source], target)
	if !ok {
		return subStringFieldAssignment{}, false
	}
	slot := -1
	if slots, ok := c.localStringSlots[ref.index]; ok {
		if fieldSlot, ok := slots[target.selectors[0].field]; ok {
			slot = fieldSlot
		}
	}
	return subStringFieldAssignment{
		table:   ref.index,
		field:   target.selectors[0].field,
		operand: operand,
		slot:    slot,
	}, true
}

func fieldAddAssignmentOperand(expr expression, target assignTarget) (expression, bool) {
	return fieldAddSubAssignmentOperand(expr, target, additiveAdd)
}

func fieldSubAssignmentOperand(expr expression, target assignTarget) (expression, bool) {
	return fieldAddSubAssignmentOperand(expr, target, additiveSubtract)
}

func fieldAddSubAssignmentOperand(expr expression, target assignTarget, op additiveOperator) (expression, bool) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return expression{}, false
	}
	comparison := expr.terms[0].terms[0]
	if comparison.op != "" || comparison.right != nil || len(comparison.left.rest) != 0 {
		return expression{}, false
	}
	additive := comparison.left.first
	if len(additive.rest) != 1 || additive.rest[0].op != op {
		return expression{}, false
	}
	if !multiplicativeMatchesAssignTarget(additive.first, target) {
		return expression{}, false
	}
	operand := additive.rest[0].value
	if !multiplicativeIsSideEffectFreeSingleValue(operand) {
		return expression{}, false
	}
	return expression{
		terms: []andExpression{{
			terms: []comparisonExpression{{
				left: concatExpression{
					first: additiveExpression{
						first: operand,
					},
				},
			}},
		}},
	}, true
}

func multiplicativeMatchesAssignTarget(expr multiplicativeExpression, target assignTarget) bool {
	if len(expr.rest) != 0 {
		return false
	}
	value := termWithoutCastsAndGroups(expr.first)
	if value.name != target.name || len(value.selectors) != len(target.selectors) {
		return false
	}
	for i, selector := range value.selectors {
		targetSelector := target.selectors[i]
		if selector.field != targetSelector.field || selector.index != nil || targetSelector.index != nil {
			return false
		}
	}
	return true
}

func multiplicativeIsSideEffectFreeSingleValue(expr multiplicativeExpression) bool {
	if len(expr.rest) != 0 {
		return false
	}
	value := termWithoutCastsAndGroups(expr.first)
	if value.name != "" && len(value.selectors) == 0 {
		return true
	}
	return value.number != nil || value.lit != nil
}

func multiplicativeLocalStringField(expr multiplicativeExpression) (string, string, bool) {
	if len(expr.rest) != 0 {
		return "", "", false
	}
	value := termWithoutCastsAndGroups(expr.first)
	if value.name == "" || len(value.selectors) != 1 {
		return "", "", false
	}
	field := value.selectors[0]
	if field.field == "" || field.index != nil {
		return "", "", false
	}
	return value.name, field.field, true
}

func expressionSingleMultiplicative(expr expression) (multiplicativeExpression, bool) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return multiplicativeExpression{}, false
	}
	comparison := expr.terms[0].terms[0]
	if comparison.op != "" || comparison.right != nil || len(comparison.left.rest) != 0 {
		return multiplicativeExpression{}, false
	}
	additive := comparison.left.first
	if len(additive.rest) != 0 {
		return multiplicativeExpression{}, false
	}
	return additive.first, true
}

func expressionNamedTableFieldSlots(expr expression) (map[string]int, bool) {
	multiplicative, ok := expressionSingleMultiplicative(expr)
	if !ok {
		return nil, false
	}
	term := termWithoutCastsAndGroups(multiplicative.first)
	if term.table == nil || len(term.selectors) != 0 {
		return nil, false
	}
	slots := make(map[string]int)
	for _, field := range term.table.fields {
		if field.name == "" || field.key != nil || field.arrayIndex != 0 {
			return nil, false
		}
		if _, exists := slots[field.name]; !exists {
			slots[field.name] = len(slots)
		}
	}
	if len(slots) == 0 {
		return nil, false
	}
	return slots, true
}

func expressionArrayElementNamedTableFieldSlots(expr expression) (map[string]int, bool) {
	multiplicative, ok := expressionSingleMultiplicative(expr)
	if !ok {
		return nil, false
	}
	term := termWithoutCastsAndGroups(multiplicative.first)
	if term.table == nil || len(term.selectors) != 0 {
		return nil, false
	}
	shape := make(map[string]int)
	for _, field := range term.table.fields {
		if field.arrayIndex == 0 || field.name != "" || field.key != nil {
			return nil, false
		}
		slots, ok := expressionNamedTableFieldSlots(field.value)
		if !ok {
			return nil, false
		}
		for name, slot := range slots {
			if _, exists := shape[name]; !exists {
				shape[name] = slot
			}
		}
	}
	if len(shape) == 0 {
		return nil, false
	}
	return shape, true
}

func expressionArrayElementFieldArrayElementSlots(expr expression) (map[string]map[string]int, bool) {
	multiplicative, ok := expressionSingleMultiplicative(expr)
	if !ok {
		return nil, false
	}
	term := termWithoutCastsAndGroups(multiplicative.first)
	if term.table == nil || len(term.selectors) != 0 {
		return nil, false
	}
	shape := make(map[string]map[string]int)
	for _, field := range term.table.fields {
		if field.arrayIndex == 0 || field.name != "" || field.key != nil {
			return nil, false
		}
		rowFields, ok := expressionNamedTableFieldArrayElementSlots(field.value)
		if !ok {
			continue
		}
		for name, slots := range rowFields {
			mergeStringSlotMap(shape, name, slots)
		}
	}
	if len(shape) == 0 {
		return nil, false
	}
	return shape, true
}

func expressionNamedTableFieldArrayElementSlots(expr expression) (map[string]map[string]int, bool) {
	multiplicative, ok := expressionSingleMultiplicative(expr)
	if !ok {
		return nil, false
	}
	term := termWithoutCastsAndGroups(multiplicative.first)
	if term.table == nil || len(term.selectors) != 0 {
		return nil, false
	}
	slots := make(map[string]map[string]int)
	for _, field := range term.table.fields {
		if field.name == "" || field.key != nil || field.arrayIndex != 0 {
			return nil, false
		}
		elemSlots, ok := expressionArrayElementNamedTableFieldSlots(field.value)
		if !ok {
			continue
		}
		slots[field.name] = elemSlots
	}
	if len(slots) == 0 {
		return nil, false
	}
	return slots, true
}

func (c *compiler) expressionIndexedLocalArrayElementSlots(expr expression) (map[string]int, bool) {
	value, ok := expressionSingleTerm(expr)
	if !ok || value.name == "" || len(value.selectors) != 1 {
		return nil, false
	}
	selector := value.selectors[0]
	if selector.field != "" || selector.index == nil {
		return nil, false
	}
	base := value
	base.selectors = nil
	ref, ok := c.termLocalRef(base)
	if !ok {
		return nil, false
	}
	slots, ok := c.localArrayElemSlots[ref.index]
	return slots, ok
}

func (c *compiler) expressionIndexedLocalArrayElementFieldSlots(expr expression) (map[string]map[string]int, bool) {
	value, ok := expressionSingleTerm(expr)
	if !ok || value.name == "" || len(value.selectors) != 1 {
		return nil, false
	}
	selector := value.selectors[0]
	if selector.field != "" || selector.index == nil {
		return nil, false
	}
	base := value
	base.selectors = nil
	ref, ok := c.termLocalRef(base)
	if !ok {
		return nil, false
	}
	slots, ok := c.localArrayElemFieldSlots[ref.index]
	return slots, ok
}

func (c *compiler) expressionLocalFieldArrayElementSlots(expr expression) (map[string]int, bool) {
	value, ok := expressionSingleTerm(expr)
	if !ok || value.name == "" || len(value.selectors) != 1 {
		return nil, false
	}
	selector := value.selectors[0]
	if selector.field == "" || selector.index != nil {
		return nil, false
	}
	base := value
	base.selectors = nil
	ref, ok := c.termLocalRef(base)
	if !ok {
		return nil, false
	}
	fields, ok := c.localFieldArrayElemSlots[ref.index]
	if !ok {
		return nil, false
	}
	slots, ok := fields[selector.field]
	return slots, ok
}

func (c *compiler) expressionArrayElementSlots(expr expression) (map[string]int, bool) {
	if ref, ok := c.expressionLocalRef(expr); ok {
		slots, ok := c.localArrayElemSlots[ref.index]
		return slots, ok
	}
	if slots, ok := c.expressionLocalFieldArrayElementSlots(expr); ok {
		return slots, true
	}
	return expressionArrayElementNamedTableFieldSlots(expr)
}

func (c *compiler) expressionArrayElementFieldSlots(expr expression) (map[string]map[string]int, bool) {
	if ref, ok := c.expressionLocalRef(expr); ok {
		slots, ok := c.localArrayElemFieldSlots[ref.index]
		return slots, ok
	}
	return expressionArrayElementFieldArrayElementSlots(expr)
}

func mergeStringSlotMap(target map[string]map[string]int, name string, slots map[string]int) {
	existing, ok := target[name]
	if !ok {
		existing = make(map[string]int, len(slots))
		target[name] = existing
	}
	for field, slot := range slots {
		if _, exists := existing[field]; !exists {
			existing[field] = slot
		}
	}
}

func (c *compiler) compileAddStringFieldAssignment(addField addStringFieldAssignment) error {
	operand := c.allocTemp()
	if err := c.compileExpressionTo(addField.operand, operand); err != nil {
		c.releaseTemp(operand)
		return err
	}
	key := c.addConstant(StringValue(addField.field))
	c.emit(instruction{op: opAddStringField, a: addField.table, b: key, c: operand, d: addField.slot})
	c.releaseTemp(operand)
	return nil
}

func (c *compiler) compileSubStringFieldAssignment(subField subStringFieldAssignment) error {
	operand := c.allocTemp()
	if err := c.compileExpressionTo(subField.operand, operand); err != nil {
		c.releaseTemp(operand)
		return err
	}
	key := c.addConstant(StringValue(subField.field))
	c.emit(instruction{op: opSubStringField, a: subField.table, b: key, c: operand, d: subField.slot})
	c.releaseTemp(operand)
	return nil
}

func (c *compiler) compileAssignTargetFromRegister(target assignTarget, value int) error {
	if len(target.selectors) == 0 {
		ref, ok := c.resolveAssignTarget(target)
		if !ok {
			name := c.addConstant(StringValue(target.name))
			c.emit(instruction{op: opSetGlobal, a: name, b: value})
			return nil
		}
		if ref.kind == variableLocal {
			c.emit(instruction{op: opMove, a: ref.index, b: value})
			return nil
		}

		c.emit(instruction{op: opSetUpvalue, a: ref.index, b: value})
		return nil
	}

	if ref, ok := c.resolveAssignTarget(target); ok && ref.kind == variableLocal && len(target.selectors) == 1 {
		last := target.selectors[0]
		if last.field != "" {
			key := c.addConstant(StringValue(last.field))
			c.emit(instruction{op: opSetStringField, a: ref.index, b: key, c: value})
			return nil
		}
		key := c.allocReg()
		if err := c.compileExpressionTo(*last.index, key); err != nil {
			return err
		}
		c.emit(instruction{op: opSetIndex, a: ref.index, b: key, c: value})
		return nil
	}

	if ref, ok := c.resolveAssignTarget(target); ok && ref.kind == variableLocal && len(target.selectors) == 2 {
		first := target.selectors[0]
		second := target.selectors[1]
		if first.field != "" && second.field != "" {
			firstKey := c.addConstant(StringValue(first.field))
			secondKey := c.addConstant(StringValue(second.field))
			table := c.allocTemp()
			c.emit(instruction{op: opGetStringField, a: table, b: ref.index, c: firstKey})
			c.emit(instruction{op: opSetStringField, a: table, b: secondKey, c: value})
			c.releaseTemp(table)
			return nil
		}
		if first.field != "" && second.index != nil {
			firstKey := c.addConstant(StringValue(first.field))
			key := c.allocReg()
			if err := c.compileExpressionTo(*second.index, key); err != nil {
				return err
			}
			c.emit(instruction{op: opSetStringFieldIndex, a: ref.index, b: firstKey, c: key, d: value})
			return nil
		}
	}

	table := c.allocReg()
	if err := c.compileAssignTargetBaseTo(target, table); err != nil {
		return err
	}

	receivers := target.selectors[:len(target.selectors)-1]
	if err := c.compileSelectorsTo(receivers, table); err != nil {
		return err
	}

	last := target.selectors[len(target.selectors)-1]
	if last.field != "" {
		key := c.addConstant(StringValue(last.field))
		c.emit(instruction{op: opSetStringField, a: table, b: key, c: value})
		return nil
	}

	key := c.allocReg()
	if err := c.compileExpressionTo(*last.index, key); err != nil {
		return err
	}
	c.emit(instruction{op: opSetIndex, a: table, b: key, c: value})
	return nil
}

func (c *compiler) compileIf(stmt ifStatement) error {
	return c.compileLoweredIf(lowerIfStatement(stmt))
}

func (c *compiler) compileLoweredIf(branch loweredIfStatement) error {
	if !c.suppressTagChains {
		if ok, err := c.compileStringTagElseIfChain(branch); ok || err != nil {
			return err
		}
	}
	return c.compileLoweredIfDefault(branch)
}

func (c *compiler) compileLoweredIfSlowPath(branch loweredIfStatement) error {
	previous := c.suppressTagChains
	c.suppressTagChains = true
	defer func() {
		c.suppressTagChains = previous
	}()
	return c.compileLoweredIfDefault(branch)
}

func (c *compiler) compileLoweredIfDefault(branch loweredIfStatement) error {
	jumpIfFalse, ok, err := c.compileConditionJumpIfFalse(branch.condition)
	if err != nil {
		return err
	}
	if !ok {
		condition, err := c.compileExpression(branch.condition)
		if err != nil {
			return err
		}
		jumpIfFalse = c.emitJumpIfFalse(condition)
		c.releaseTemp(condition)
	}

	outerLocals := copyLocals(c.locals)
	if err := c.compileStatements(branch.thenBody); err != nil {
		return err
	}
	c.locals = copyLocals(outerLocals)

	jumpEnd := c.emitJump()

	elseStart := c.pc()
	c.patchJump(jumpIfFalse, elseStart)

	if len(branch.elseBody) > 0 {
		if err := c.compileStatements(branch.elseBody); err != nil {
			return err
		}
		c.locals = copyLocals(outerLocals)
	}

	c.patchJump(jumpEnd, c.pc())
	return nil
}

type stringTagElseIfArm struct {
	value  string
	guards []comparisonExpression
	body   []statement
}

type stringTagElseIfChain struct {
	table    int
	field    string
	slot     int
	arms     []stringTagElseIfArm
	elseBody []statement
}

func (c *compiler) compileStringTagElseIfChain(branch loweredIfStatement) (bool, error) {
	chain, ok := c.stringTagElseIfChain(branch)
	if !ok {
		return false, nil
	}

	outerLocals := copyLocals(c.locals)
	metatableJump := c.emit(instruction{op: opJumpIfTableHasMetatable, a: chain.table})
	tag := c.allocTemp()
	field := c.addConstant(StringValue(chain.field))
	c.emit(instruction{op: opGetStringField, a: tag, b: chain.table, c: field})

	endJumps := make([]int, 0, len(chain.arms)+1)
	for _, arm := range chain.arms {
		value := c.addConstant(StringValue(arm.value))
		nextArmJumps := []int{c.emit(instruction{op: opJumpIfNotEqualK, a: tag, b: value})}
		if len(arm.guards) > 0 {
			guardJump, ok, err := c.compileConditionJumpIfFalse(expression{
				terms: []andExpression{{terms: arm.guards}},
			})
			if err != nil {
				c.releaseTemp(tag)
				return true, err
			}
			if !ok {
				guardCondition, err := c.compileExpression(expression{
					terms: []andExpression{{terms: arm.guards}},
				})
				if err != nil {
					c.releaseTemp(tag)
					return true, err
				}
				guardJump = c.emitJumpIfFalse(guardCondition)
				c.releaseTemp(guardCondition)
			}
			nextArmJumps = append(nextArmJumps, guardJump)
		}
		if err := c.compileStatements(arm.body); err != nil {
			c.releaseTemp(tag)
			return true, err
		}
		c.locals = copyLocals(outerLocals)
		endJumps = append(endJumps, c.emitJump())
		nextArm := c.pc()
		for _, jump := range nextArmJumps {
			c.patchJump(jump, nextArm)
		}
	}
	if len(chain.elseBody) > 0 {
		if err := c.compileStatements(chain.elseBody); err != nil {
			c.releaseTemp(tag)
			return true, err
		}
		c.locals = copyLocals(outerLocals)
	}
	endJumps = append(endJumps, c.emitJump())

	slowStart := c.pc()
	c.patchJump(metatableJump, slowStart)
	c.releaseTemp(tag)
	if err := c.compileLoweredIfSlowPath(branch); err != nil {
		return true, err
	}
	end := c.pc()
	for _, jump := range endJumps {
		c.patchJump(jump, end)
	}
	c.locals = copyLocals(outerLocals)
	return true, nil
}

func (c *compiler) stringTagElseIfChain(branch loweredIfStatement) (stringTagElseIfChain, bool) {
	first, firstGuards, ok := c.stringTagArmCondition(branch.condition)
	if !ok {
		return stringTagElseIfChain{}, false
	}
	firstValue, ok := first.value.String()
	if !ok {
		return stringTagElseIfChain{}, false
	}
	chain := stringTagElseIfChain{
		table: first.table,
		field: first.field,
		slot:  first.slot,
		arms: []stringTagElseIfArm{{
			value:  firstValue,
			guards: firstGuards,
			body:   branch.thenBody,
		}},
	}
	elseBody := branch.elseBody
	for len(elseBody) == 1 && elseBody[0].ifStmt != nil {
		nextBranch := lowerIfStatement(*elseBody[0].ifStmt)
		condition, guards, ok := c.stringTagArmCondition(nextBranch.condition)
		if !ok ||
			condition.table != chain.table ||
			condition.field != chain.field ||
			condition.slot != chain.slot {
			return stringTagElseIfChain{}, false
		}
		conditionValue, ok := condition.value.String()
		if !ok {
			return stringTagElseIfChain{}, false
		}
		chain.arms = append(chain.arms, stringTagElseIfArm{
			value:  conditionValue,
			guards: guards,
			body:   nextBranch.thenBody,
		})
		elseBody = nextBranch.elseBody
	}
	if len(chain.arms) < 3 {
		return stringTagElseIfChain{}, false
	}
	chain.elseBody = elseBody
	return chain, true
}

func (c *compiler) stringTagArmCondition(expr expression) (stringFieldEqualityCondition, []comparisonExpression, bool) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) == 0 {
		return stringFieldEqualityCondition{}, nil, false
	}
	condition, ok := c.stringFieldEqualityCondition(expr.terms[0].terms[0])
	if !ok {
		return stringFieldEqualityCondition{}, nil, false
	}
	guards := append([]comparisonExpression(nil), expr.terms[0].terms[1:]...)
	return condition, guards, true
}

func (c *compiler) singleStringFieldEqualityCondition(expr expression) (stringFieldEqualityCondition, bool) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return stringFieldEqualityCondition{}, false
	}
	return c.stringFieldEqualityCondition(expr.terms[0].terms[0])
}

func (c *compiler) compileIfExpressionTo(expr ifExpression, target int) error {
	branch := lowerIfExpression(expr)
	jumpIfFalse, ok, err := c.compileConditionJumpIfFalse(branch.condition)
	if err != nil {
		return err
	}
	if !ok {
		condition, err := c.compileExpression(branch.condition)
		if err != nil {
			return err
		}
		jumpIfFalse = c.emitJumpIfFalse(condition)
		c.releaseTemp(condition)
	}

	if err := c.compileExpressionTo(branch.thenValue, target); err != nil {
		return err
	}

	jumpEnd := c.emitJump()

	c.patchJump(jumpIfFalse, c.pc())
	if err := c.compileExpressionTo(branch.elseValue, target); err != nil {
		return err
	}

	c.patchJump(jumpEnd, c.pc())
	return nil
}

func (c *compiler) compileConditionJumpIfFalse(expr expression) (int, bool, error) {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) {
		return 0, false, nil
	}
	expr = optimizeExpression(expr, c.options.optimizations)
	if jump, ok, err := c.compileRowStringFieldPairEqualityJumpIfFalse(expr); ok || err != nil {
		return jump, ok, err
	}
	if jump, ok, err := c.compileStringFieldEqualityJumpIfFalse(expr); ok || err != nil {
		return jump, ok, err
	}
	if jump, ok, err := c.compileStringFieldNumericJumpIfFalse(expr); ok || err != nil {
		return jump, ok, err
	}
	if jump, ok, err := c.compileRowStringFieldPairNumericJumpIfFalse(expr); ok || err != nil {
		return jump, ok, err
	}
	if jump, ok, err := c.compileRegisterStringFieldNumericJumpIfFalse(expr); ok || err != nil {
		return jump, ok, err
	}
	if jump, ok, err := c.compileStringFieldTruthyJumpIfFalse(expr); ok || err != nil {
		return jump, ok, err
	}
	if jump, ok, err := c.compileStringFieldNotJumpIfFalse(expr); ok || err != nil {
		return jump, ok, err
	}
	if jump, ok, err := c.compileStringFieldNilJumpIfFalse(expr); ok || err != nil {
		return jump, ok, err
	}
	if jump, ok, err := c.compileAndChainJumpIfFalse(expr); ok || err != nil {
		return jump, ok, err
	}
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return 0, false, nil
	}
	comparison := expr.terms[0].terms[0]
	if comparison.right == nil {
		return 0, false, nil
	}
	right, ok := foldNumberConcat(*comparison.right)
	if !ok {
		return c.compileRegisterNumericJumpIfFalse(comparison)
	}
	if condition, ok := c.moduloConstantEqualityCondition(comparison, right); ok {
		mod := c.addConstant(NumberValue(condition.mod))
		value := c.addConstant(NumberValue(condition.value))
		jump := c.emit(instruction{op: opJumpIfModKNotEqualK, a: condition.source.index, b: mod, c: value})
		return jump, true, nil
	}
	left, releaseLeft, err := c.compileConditionLeftRegister(comparison.left)
	if err != nil {
		return 0, false, err
	}
	constant := c.addConstant(NumberValue(right))
	switch comparison.op {
	case comparisonEqual:
		jump := c.emit(instruction{op: opJumpIfNotEqualK, a: left, b: constant})
		releaseLeft()
		return jump, true, nil
	case comparisonLess:
		jump := c.emit(instruction{op: opJumpIfNotLessK, a: left, b: constant})
		releaseLeft()
		return jump, true, nil
	case comparisonGreater:
		jump := c.emit(instruction{op: opJumpIfNotGreaterK, a: left, b: constant})
		releaseLeft()
		return jump, true, nil
	case comparisonLessEqual:
		jump := c.emit(instruction{op: opJumpIfGreaterK, a: left, b: constant})
		releaseLeft()
		return jump, true, nil
	case comparisonGreaterEqual:
		jump := c.emit(instruction{op: opJumpIfLessK, a: left, b: constant})
		releaseLeft()
		return jump, true, nil
	default:
		releaseLeft()
		return 0, false, nil
	}
}

func (c *compiler) compileRegisterNumericJumpIfFalse(comparison comparisonExpression) (int, bool, error) {
	if comparison.right == nil {
		return 0, false, nil
	}
	var op opcode
	switch comparison.op {
	case comparisonLess:
		op = opJumpIfNotLess
	case comparisonGreater:
		op = opJumpIfNotGreater
	case comparisonLessEqual:
		op = opJumpIfGreater
	case comparisonGreaterEqual:
		op = opJumpIfLess
	default:
		return 0, false, nil
	}
	left, releaseLeft, err := c.compileConditionLeftRegister(comparison.left)
	if err != nil {
		return 0, false, err
	}
	right, releaseRight, err := c.compileConditionLeftRegister(*comparison.right)
	if err != nil {
		releaseLeft()
		return 0, false, err
	}
	jump := c.emit(instruction{op: op, a: left, b: right})
	releaseRight()
	releaseLeft()
	return jump, true, nil
}

type andChainBranchPlan struct {
	op         opcode
	a          int
	b          int
	constant   float64
	field      string
	slot       int
	rightField string
	rightSlot  int
}

func (c *compiler) compileAndChainJumpIfFalse(expr expression) (int, bool, error) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) < 2 {
		return 0, false, nil
	}
	plans := make([]andChainBranchPlan, 0, len(expr.terms[0].terms))
	for _, comparison := range expr.terms[0].terms {
		plan, ok := c.andChainBranchPlan(comparison)
		if !ok {
			return 0, false, nil
		}
		plans = append(plans, plan)
	}
	falseJumps := make([]int, 0, len(plans))
	for _, plan := range plans {
		switch plan.op {
		case opJumpIfStringFieldFalse, opJumpIfStringFieldTrue, opJumpIfStringFieldNil, opJumpIfStringFieldNotNil:
			field := c.addConstant(StringValue(plan.field))
			falseJumps = append(falseJumps, c.emit(instruction{
				op: plan.op,
				a:  plan.a,
				b:  field,
				c:  plan.slot,
			}))
		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			if plan.field != "" {
				falseJumps = append(falseJumps, c.emitAndChainFieldPairBranch(plan))
			} else {
				falseJumps = append(falseJumps, c.emit(instruction{
					op: plan.op,
					a:  plan.a,
					b:  plan.b,
				}))
			}
		case opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			constant := c.addConstant(NumberValue(plan.constant))
			falseJumps = append(falseJumps, c.emit(instruction{
				op: plan.op,
				a:  plan.a,
				b:  constant,
			}))
		case opJumpIfStringFieldNotGreaterK, opJumpIfStringFieldGreaterK:
			field := c.addConstant(StringValue(plan.field))
			value := c.addConstant(NumberValue(plan.constant))
			falseJumps = append(falseJumps, c.emit(instruction{
				op: plan.op,
				a:  plan.a,
				b:  field,
				c:  value,
			}))
		default:
			return 0, false, nil
		}
	}
	passJump := c.emitJump()
	falseTarget := c.pc()
	for _, jump := range falseJumps {
		c.patchJump(jump, falseTarget)
	}
	exitJump := c.emitJump()
	c.patchJump(passJump, c.pc())
	return exitJump, true, nil
}

func (c *compiler) andChainBranchPlan(comparison comparisonExpression) (andChainBranchPlan, bool) {
	if comparison.op == "" && comparison.right == nil {
		if table, field, ok := c.concatLocalStringFieldRef(comparison.left); ok {
			return andChainBranchPlan{
				op:    opJumpIfStringFieldFalse,
				a:     table.index,
				field: field,
				slot:  c.localRowStringFieldSlot(table.index, field),
			}, true
		}
		if table, field, ok := c.concatUnaryNotLocalStringFieldRef(comparison.left); ok {
			return andChainBranchPlan{
				op:    opJumpIfStringFieldTrue,
				a:     table.index,
				field: field,
				slot:  c.localRowStringFieldSlot(table.index, field),
			}, true
		}
		return andChainBranchPlan{}, false
	}
	if comparison.right == nil {
		return andChainBranchPlan{}, false
	}
	if table, field, ok := c.concatLocalStringFieldRef(comparison.left); ok && concatNilLiteral(*comparison.right) {
		switch comparison.op {
		case comparisonNotEqual:
			return andChainBranchPlan{
				op:    opJumpIfStringFieldNil,
				a:     table.index,
				field: field,
				slot:  c.localRowStringFieldSlot(table.index, field),
			}, true
		case comparisonEqual:
			return andChainBranchPlan{
				op:    opJumpIfStringFieldNotNil,
				a:     table.index,
				field: field,
				slot:  c.localRowStringFieldSlot(table.index, field),
			}, true
		}
	}
	if plan, ok := c.andChainStringFieldNumericPlan(comparison); ok {
		return plan, true
	}
	if plan, ok := c.andChainStringFieldPairNumericPlan(comparison); ok {
		return plan, true
	}
	var op opcode
	switch comparison.op {
	case comparisonLess:
		op = opJumpIfNotLess
	case comparisonGreater:
		op = opJumpIfNotGreater
	case comparisonLessEqual:
		op = opJumpIfGreater
	case comparisonGreaterEqual:
		op = opJumpIfLess
	default:
		return andChainBranchPlan{}, false
	}
	left, ok := c.concatLocalRef(comparison.left)
	if !ok {
		return andChainBranchPlan{}, false
	}
	if right, ok := c.concatLocalRef(*comparison.right); ok {
		return andChainBranchPlan{
			op: op,
			a:  left.index,
			b:  right.index,
		}, true
	}
	right, ok := foldNumberConcat(*comparison.right)
	if !ok {
		return andChainBranchPlan{}, false
	}
	switch comparison.op {
	case comparisonLess:
		op = opJumpIfNotLessK
	case comparisonGreater:
		op = opJumpIfNotGreaterK
	case comparisonLessEqual:
		op = opJumpIfGreaterK
	case comparisonGreaterEqual:
		op = opJumpIfLessK
	default:
		return andChainBranchPlan{}, false
	}
	return andChainBranchPlan{
		op:       op,
		a:        left.index,
		constant: right,
	}, true
}

func (c *compiler) emitAndChainFieldPairBranch(plan andChainBranchPlan) int {
	left := c.allocTemp()
	c.emitLocalStringFieldLoad(left, plan.a, plan.field, plan.slot)
	right := c.allocTemp()
	c.emitLocalStringFieldLoad(right, plan.b, plan.rightField, plan.rightSlot)
	jump := c.emit(instruction{op: plan.op, a: left, b: right})
	c.releaseTemp(right)
	c.releaseTemp(left)
	return jump
}

func (c *compiler) emitLocalStringFieldLoad(target int, table int, field string, slot int) {
	_ = slot
	key := c.addConstant(StringValue(field))
	c.emit(instruction{op: opGetStringField, a: target, b: table, c: key})
}

func (c *compiler) andChainStringFieldNumericPlan(comparison comparisonExpression) (andChainBranchPlan, bool) {
	if comparison.right == nil {
		return andChainBranchPlan{}, false
	}
	table, field, ok := c.concatLocalStringFieldRef(comparison.left)
	if !ok {
		return andChainBranchPlan{}, false
	}
	right, ok := foldNumberConcat(*comparison.right)
	if !ok {
		return andChainBranchPlan{}, false
	}
	var op opcode
	switch comparison.op {
	case comparisonGreater:
		op = opJumpIfStringFieldNotGreaterK
	case comparisonLessEqual:
		op = opJumpIfStringFieldGreaterK
	default:
		return andChainBranchPlan{}, false
	}
	return andChainBranchPlan{
		op:       op,
		a:        table.index,
		constant: right,
		field:    field,
		slot:     -1,
	}, true
}

func (c *compiler) andChainStringFieldPairNumericPlan(comparison comparisonExpression) (andChainBranchPlan, bool) {
	if comparison.right == nil {
		return andChainBranchPlan{}, false
	}
	leftTable, leftField, ok := c.concatLocalStringFieldRef(comparison.left)
	if !ok {
		return andChainBranchPlan{}, false
	}
	rightTable, rightField, ok := c.concatLocalStringFieldRef(*comparison.right)
	if !ok {
		return andChainBranchPlan{}, false
	}
	var op opcode
	switch comparison.op {
	case comparisonLess:
		op = opJumpIfNotLess
	case comparisonGreater:
		op = opJumpIfNotGreater
	case comparisonLessEqual:
		op = opJumpIfGreater
	case comparisonGreaterEqual:
		op = opJumpIfLess
	default:
		return andChainBranchPlan{}, false
	}
	return andChainBranchPlan{
		op:         op,
		a:          leftTable.index,
		b:          rightTable.index,
		field:      leftField,
		slot:       c.localRowStringFieldSlot(leftTable.index, leftField),
		rightField: rightField,
		rightSlot:  c.localRowStringFieldSlot(rightTable.index, rightField),
	}, true
}

func (c *compiler) localRowStringFieldSlot(register int, field string) int {
	if slots, ok := c.localRowStringSlots[register]; ok {
		if slot, ok := slots[field]; ok {
			return slot
		}
	}
	if slots, ok := c.localStringSlots[register]; ok {
		if slot, ok := slots[field]; ok {
			return slot
		}
	}
	return -1
}

type moduloConstantEqualityCondition struct {
	source variableRef
	mod    float64
	value  float64
}

func (c *compiler) moduloConstantEqualityCondition(comparison comparisonExpression, right float64) (moduloConstantEqualityCondition, bool) {
	if comparison.op != comparisonEqual || comparison.right == nil || len(comparison.left.rest) != 0 {
		return moduloConstantEqualityCondition{}, false
	}
	additive := comparison.left.first
	if len(additive.rest) != 0 || len(additive.first.rest) != 1 {
		return moduloConstantEqualityCondition{}, false
	}
	modPart := additive.first.rest[0]
	if modPart.op != multiplicativeModulo {
		return moduloConstantEqualityCondition{}, false
	}
	mod, ok := foldNumberTerm(modPart.value)
	if !ok {
		return moduloConstantEqualityCondition{}, false
	}
	source, ok := c.termLocalRef(additive.first.first)
	if !ok || source.kind != variableLocal {
		return moduloConstantEqualityCondition{}, false
	}
	return moduloConstantEqualityCondition{source: source, mod: mod, value: right}, true
}

type stringFieldEqualityCondition struct {
	table int
	field string
	value Value
	slot  int
}

func (c *compiler) compileStringFieldEqualityJumpIfFalse(expr expression) (int, bool, error) {
	if len(expr.terms) == 0 || len(expr.terms) > 2 {
		return 0, false, nil
	}
	conditions := make([]stringFieldEqualityCondition, 0, len(expr.terms))
	for _, term := range expr.terms {
		if len(term.terms) != 1 {
			return 0, false, nil
		}
		condition, ok := c.stringFieldEqualityCondition(term.terms[0])
		if !ok {
			return 0, false, nil
		}
		if len(conditions) > 0 &&
			(conditions[0].table != condition.table || conditions[0].field != condition.field) {
			return 0, false, nil
		}
		conditions = append(conditions, condition)
	}
	if len(conditions) == 0 {
		return 0, false, nil
	}
	firstJump := c.emitStringFieldEqualityJump(conditions[0])
	if len(conditions) == 1 {
		return firstJump, true, nil
	}
	jumpThen := c.emitJump()
	secondStart := c.pc()
	c.patchJump(firstJump, secondStart)
	secondJump := c.emitStringFieldEqualityJump(conditions[1])
	c.patchJump(jumpThen, c.pc())
	return secondJump, true, nil
}

func (c *compiler) emitStringFieldEqualityJump(condition stringFieldEqualityCondition) int {
	field := c.addConstant(StringValue(condition.field))
	value := c.addConstant(condition.value)
	return c.emit(instruction{op: opJumpIfStringFieldNotEqualK, a: condition.table, b: field, c: value})
}

type rowStringFieldPairEqualityCondition struct {
	leftTable  int
	rightTable int
	leftField  string
	rightField string
	leftSlot   int
	rightSlot  int
	op         comparisonOperator
}

func (c *compiler) compileRowStringFieldPairEqualityJumpIfFalse(expr expression) (int, bool, error) {
	_ = expr
	return 0, false, nil
}

func (c *compiler) compileRowStringFieldPairEqualityJumpIfFalseOld(expr expression) (int, bool, error) {
	_ = expr
	return 0, false, nil
}

func (c *compiler) emitRowStringFieldPairEqualityJump(condition rowStringFieldPairEqualityCondition) int {
	_ = condition
	return 0
}

func (c *compiler) rowStringFieldPairEqualityCondition(expr comparisonExpression) (rowStringFieldPairEqualityCondition, bool) {
	if (expr.op != comparisonEqual && expr.op != comparisonNotEqual) || expr.right == nil {
		return rowStringFieldPairEqualityCondition{}, false
	}
	leftTable, leftField, ok := c.concatLocalStringFieldRef(expr.left)
	if !ok {
		return rowStringFieldPairEqualityCondition{}, false
	}
	rightTable, rightField, ok := c.concatLocalStringFieldRef(*expr.right)
	if !ok {
		return rowStringFieldPairEqualityCondition{}, false
	}
	leftSlot := -1
	if slots, ok := c.localRowStringSlots[leftTable.index]; ok {
		if slot, ok := slots[leftField]; ok {
			leftSlot = slot
		}
	}
	rightSlot := -1
	if slots, ok := c.localRowStringSlots[rightTable.index]; ok {
		if slot, ok := slots[rightField]; ok {
			rightSlot = slot
		}
	}
	if leftSlot < 0 || rightSlot < 0 {
		return rowStringFieldPairEqualityCondition{}, false
	}
	return rowStringFieldPairEqualityCondition{
		leftTable:  leftTable.index,
		rightTable: rightTable.index,
		leftField:  leftField,
		rightField: rightField,
		leftSlot:   leftSlot,
		rightSlot:  rightSlot,
		op:         expr.op,
	}, true
}

func (c *compiler) stringFieldEqualityCondition(expr comparisonExpression) (stringFieldEqualityCondition, bool) {
	if expr.op != comparisonEqual || expr.right == nil {
		return stringFieldEqualityCondition{}, false
	}
	table, field, ok := c.concatLocalStringFieldRef(expr.left)
	if !ok {
		return stringFieldEqualityCondition{}, false
	}
	value, ok := concatNonNilLiteral(*expr.right)
	if !ok {
		return stringFieldEqualityCondition{}, false
	}
	slot := -1
	if slots, ok := c.localRowStringSlots[table.index]; ok {
		if fieldSlot, ok := slots[field]; ok {
			slot = fieldSlot
		}
	}
	return stringFieldEqualityCondition{
		table: table.index,
		field: field,
		value: value,
		slot:  slot,
	}, true
}

func (c *compiler) concatLocalStringFieldRef(expr concatExpression) (variableRef, string, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return variableRef{}, "", false
	}
	term := termWithoutCastsAndGroups(expr.first.first.first)
	if len(term.selectors) != 1 || term.selectors[0].field == "" || term.selectors[0].index != nil {
		return variableRef{}, "", false
	}
	field := term.selectors[0].field
	term.selectors = nil
	ref, ok := c.termLocalRef(term)
	return ref, field, ok
}

func concatStringLiteral(expr concatExpression) (string, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return "", false
	}
	term := termWithoutCastsAndGroups(expr.first.first.first)
	if !isNamedTerm(term) && term.lit != nil && len(term.selectors) == 0 {
		value, ok := term.lit.String()
		return value, ok
	}
	return "", false
}

func concatNonNilLiteral(expr concatExpression) (Value, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return NilValue(), false
	}
	term := termWithoutCastsAndGroups(expr.first.first.first)
	if isNamedTerm(term) || len(term.selectors) != 0 {
		return NilValue(), false
	}
	if term.number != nil {
		return NumberValue(*term.number), true
	}
	if term.lit == nil || term.lit.kind == NilKind {
		return NilValue(), false
	}
	switch term.lit.kind {
	case BoolKind, NumberKind, StringKind:
		return *term.lit, true
	default:
		return NilValue(), false
	}
}

func (c *compiler) compileStringFieldNumericJumpIfFalse(expr expression) (int, bool, error) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return 0, false, nil
	}
	comparison := expr.terms[0].terms[0]
	if comparison.right == nil {
		return 0, false, nil
	}
	table, field, ok := c.concatLocalStringFieldRef(comparison.left)
	if !ok {
		return 0, false, nil
	}
	right, ok := foldNumberConcat(*comparison.right)
	if !ok {
		return 0, false, nil
	}
	fieldConstant := c.addConstant(StringValue(field))
	valueConstant := c.addConstant(NumberValue(right))
	switch comparison.op {
	case comparisonGreater:
		jump := c.emit(instruction{op: opJumpIfStringFieldNotGreaterK, a: table.index, b: fieldConstant, c: valueConstant})
		return jump, true, nil
	case comparisonLessEqual:
		jump := c.emit(instruction{op: opJumpIfStringFieldGreaterK, a: table.index, b: fieldConstant, c: valueConstant})
		return jump, true, nil
	default:
		return 0, false, nil
	}
}

func (c *compiler) compileRegisterStringFieldNumericJumpIfFalse(expr expression) (int, bool, error) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return 0, false, nil
	}
	comparison := expr.terms[0].terms[0]
	if comparison.op != comparisonLess || comparison.right == nil {
		return 0, false, nil
	}
	table, field, ok := c.concatLocalStringFieldRef(*comparison.right)
	if !ok {
		return 0, false, nil
	}
	left, releaseLeft, err := c.compileConditionLeftRegister(comparison.left)
	if err != nil {
		return 0, false, err
	}
	fieldConstant := c.addConstant(StringValue(field))
	jump := c.emit(instruction{op: opJumpIfStringFieldNotGreaterR, a: table.index, b: fieldConstant, c: left})
	releaseLeft()
	return jump, true, nil
}

func (c *compiler) compileRowStringFieldPairNumericJumpIfFalse(expr expression) (int, bool, error) {
	_ = expr
	return 0, false, nil
}

func (c *compiler) compileRowStringFieldPairNumericJumpIfFalseOld(expr expression) (int, bool, error) {
	_ = expr
	return 0, false, nil
}

func (c *compiler) compileStringFieldTruthyJumpIfFalse(expr expression) (int, bool, error) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return 0, false, nil
	}
	comparison := expr.terms[0].terms[0]
	if comparison.op != "" || comparison.right != nil {
		return 0, false, nil
	}
	table, field, ok := c.concatLocalStringFieldRef(comparison.left)
	if !ok {
		return 0, false, nil
	}
	fieldConstant := c.addConstant(StringValue(field))
	slot := -1
	if slots, ok := c.localRowStringSlots[table.index]; ok {
		if fieldSlot, ok := slots[field]; ok {
			slot = fieldSlot
		}
	}
	jump := c.emit(instruction{op: opJumpIfStringFieldFalse, a: table.index, b: fieldConstant, c: slot})
	return jump, true, nil
}

func (c *compiler) compileStringFieldNotJumpIfFalse(expr expression) (int, bool, error) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return 0, false, nil
	}
	comparison := expr.terms[0].terms[0]
	if comparison.op != "" || comparison.right != nil {
		return 0, false, nil
	}
	table, field, ok := c.concatUnaryNotLocalStringFieldRef(comparison.left)
	if !ok {
		return 0, false, nil
	}
	fieldConstant := c.addConstant(StringValue(field))
	slot := -1
	if slots, ok := c.localRowStringSlots[table.index]; ok {
		if fieldSlot, ok := slots[field]; ok {
			slot = fieldSlot
		}
	}
	jump := c.emit(instruction{op: opJumpIfStringFieldTrue, a: table.index, b: fieldConstant, c: slot})
	return jump, true, nil
}

func (c *compiler) concatUnaryNotLocalStringFieldRef(expr concatExpression) (variableRef, string, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return variableRef{}, "", false
	}
	term := termWithoutCastsAndGroups(expr.first.first.first)
	if term.unaryNot == nil || len(term.selectors) != 0 {
		return variableRef{}, "", false
	}
	inner := termWithoutCastsAndGroups(*term.unaryNot)
	if len(inner.selectors) != 1 || inner.selectors[0].field == "" || inner.selectors[0].index != nil {
		return variableRef{}, "", false
	}
	field := inner.selectors[0].field
	inner.selectors = nil
	ref, ok := c.termLocalRef(inner)
	return ref, field, ok
}

func (c *compiler) compileStringFieldNilJumpIfFalse(expr expression) (int, bool, error) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return 0, false, nil
	}
	comparison := expr.terms[0].terms[0]
	if comparison.right == nil {
		return 0, false, nil
	}
	table, field, ok := c.concatLocalStringFieldRef(comparison.left)
	if !ok || !concatNilLiteral(*comparison.right) {
		return 0, false, nil
	}
	fieldConstant := c.addConstant(StringValue(field))
	slot := -1
	if slots, ok := c.localRowStringSlots[table.index]; ok {
		if fieldSlot, ok := slots[field]; ok {
			slot = fieldSlot
		}
	}
	switch comparison.op {
	case comparisonNotEqual:
		jump := c.emit(instruction{op: opJumpIfStringFieldNil, a: table.index, b: fieldConstant, c: slot})
		return jump, true, nil
	case comparisonEqual:
		jump := c.emit(instruction{op: opJumpIfStringFieldNotNil, a: table.index, b: fieldConstant, c: slot})
		return jump, true, nil
	default:
		return 0, false, nil
	}
}

func concatNilLiteral(expr concatExpression) bool {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return false
	}
	term := termWithoutCastsAndGroups(expr.first.first.first)
	return !isNamedTerm(term) && term.lit != nil && term.lit.kind == NilKind && len(term.selectors) == 0
}

func (c *compiler) compileConditionLeftRegister(expr concatExpression) (int, func(), error) {
	if ref, ok := c.concatLocalRef(expr); ok {
		return ref.index, func() {}, nil
	}
	left := c.allocTemp()
	if err := c.compileConcatExpressionTo(expr, left); err != nil {
		c.releaseTemp(left)
		return 0, nil, err
	}
	return left, func() { c.releaseTemp(left) }, nil
}

func (c *compiler) concatLocalRef(expr concatExpression) (variableRef, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return variableRef{}, false
	}
	term := expr.first.first.first
	if !isNamedTerm(term) {
		return variableRef{}, false
	}
	if use, ok := c.bind.use(term.id); ok {
		if ref, ok := c.resolveSymbol(use.symbol); ok && ref.kind == variableLocal {
			return ref, true
		}
	}
	ref, ok := c.resolveVariable(term.name)
	return ref, ok && ref.kind == variableLocal
}

func (c *compiler) expressionLocalRef(expr expression) (variableRef, bool) {
	expr = optimizeExpression(expr, c.options.optimizations)
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return variableRef{}, false
	}
	comparison := expr.terms[0].terms[0]
	if comparison.op != "" || comparison.right != nil {
		return variableRef{}, false
	}
	return c.concatLocalRef(comparison.left)
}

func (c *compiler) compileBreak() error {
	if len(c.loops) == 0 {
		return fmt.Errorf("compile: break outside loop")
	}

	jump := c.emitJump()
	currentLoop := len(c.loops) - 1
	c.loops[currentLoop].breakJumps = append(c.loops[currentLoop].breakJumps, jump)
	return nil
}

func (c *compiler) compileContinue() error {
	if len(c.loops) == 0 {
		return fmt.Errorf("compile: continue outside loop")
	}

	target := c.loops[len(c.loops)-1].continueTarget
	jump := c.emit(instruction{op: opJump, b: target})
	if target < 0 {
		currentLoop := len(c.loops) - 1
		c.loops[currentLoop].continueJumps = append(c.loops[currentLoop].continueJumps, jump)
	}
	return nil
}

func (c *compiler) compileWhile(stmt whileStatement) error {
	loopShape := lowerWhileLoop(stmt)
	if loopShape.kind != loweredLoopPreTest || loopShape.continueTarget != loweredLoopContinueCondition {
		return fmt.Errorf("compile: invalid while loop lowering")
	}
	conditionStart := c.pc()

	jumpIfFalse, ok, err := c.compileConditionJumpIfFalse(loopShape.condition)
	if err != nil {
		return err
	}
	if !ok {
		condition, err := c.compileExpression(loopShape.condition)
		if err != nil {
			return err
		}
		jumpIfFalse = c.emitJumpIfFalse(condition)
		c.releaseTemp(condition)
	}

	outerLocals := copyLocals(c.locals)
	c.loops = append(c.loops, loopContext{continueTarget: conditionStart})
	if err := c.compileStatements(loopShape.body); err != nil {
		return err
	}
	loop := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]
	c.locals = copyLocals(outerLocals)

	c.emit(instruction{op: opJump, b: conditionStart})
	c.patchJump(jumpIfFalse, c.pc())
	for _, jump := range loop.breakJumps {
		c.patchJump(jump, c.pc())
	}
	return nil
}

func (c *compiler) compileFor(stmt forStatement) error {
	loopShape := lowerNumericForLoop(stmt)
	if loopShape.continueTarget != loweredNumericForContinueIncrement {
		return fmt.Errorf("compile: invalid numeric for loop lowering")
	}
	loopVar := c.allocReg()
	limit := c.allocReg()
	step := c.allocReg()

	if err := c.compileExpressionTo(loopShape.start, loopVar); err != nil {
		return err
	}
	if err := c.compileExpressionTo(loopShape.limit, limit); err != nil {
		return err
	}
	if !loopShape.defaultStep {
		if err := c.compileExpressionTo(*loopShape.step, step); err != nil {
			return err
		}
	} else {
		c.emitLoadConst(step, NumberValue(1))
	}

	zero := c.addConstant(NumberValue(0))
	c.emit(instruction{op: opAddK, a: loopVar, b: loopVar, c: zero})
	c.emit(instruction{op: opAddK, a: limit, b: limit, c: zero})
	c.emit(instruction{op: opAddK, a: step, b: step, c: zero})

	conditionStart := c.pc()
	jumpExit := c.emit(instruction{op: opNumericForCheck, a: loopVar, b: limit, c: step})

	outerLocals := copyLocals(c.locals)
	c.locals[loopShape.name] = loopVar
	c.loops = append(c.loops, loopContext{continueTarget: -1})
	if err := c.compileStatements(loopShape.body); err != nil {
		return err
	}
	loop := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]
	c.locals = copyLocals(outerLocals)

	incrementStart := c.pc()
	for _, jump := range loop.continueJumps {
		c.patchJump(jump, incrementStart)
	}
	c.emit(instruction{op: opNumericForLoop, a: loopVar, b: step, d: conditionStart})

	exit := c.pc()
	c.patchJumpD(jumpExit, exit)
	for _, jump := range loop.breakJumps {
		c.patchJump(jump, exit)
	}
	return nil
}

func (c *compiler) compileGenericFor(stmt genericForStatement) error {
	loopShape := lowerGenericForLoop(stmt)
	if loopShape.continueTarget != loweredGenericForContinueIterator {
		return fmt.Errorf("compile: invalid generic for loop lowering")
	}
	if len(loopShape.names) == 0 {
		return fmt.Errorf("compile: generic for has no names")
	}

	generator := c.allocReg()
	state := c.allocReg()
	control := c.allocReg()
	targets := []int{generator, state, control}
	if err := c.compileExpressionListTo(loopShape.values, targets); err != nil {
		return err
	}
	if loopShape.prepareDirectIterator {
		c.emit(instruction{op: opPrepareIter, a: generator, b: state, c: control})
	}

	resultStart := control
	c.reserveRegistersThrough(resultStart + 4)
	c.reserveRegistersThrough(resultStart + len(loopShape.names))
	c.claimRegisterRange(resultStart, resultStart+len(loopShape.names))
	loopStart := c.pc()
	var jumpExit int
	if len(loopShape.names) == 2 {
		jumpExit = c.emit(instruction{op: opArrayNextJump2, a: resultStart, b: generator, c: state})
	} else {
		nilReg := c.allocReg()
		c.compileNilTo(nilReg)
		condition := c.allocReg()
		c.emit(instruction{op: opArrayNext, a: resultStart, b: generator, c: state, d: len(loopShape.names)})
		c.emit(instruction{op: opMove, a: control, b: resultStart})
		c.emit(instruction{op: opNotEqual, a: condition, b: resultStart, c: nilReg})
		jumpExit = c.emitJumpIfFalse(condition)
	}

	outerLocals := copyLocals(c.locals)
	outerStringSlots := copyLocalStringSlots(c.localStringSlots)
	outerRowStringSlots := copyLocalStringSlots(c.localRowStringSlots)
	outerFieldArrayElemSlots := copyLocalFieldArrayElemSlots(c.localFieldArrayElemSlots)
	for i, name := range loopShape.names {
		register := resultStart + i
		c.locals[name] = register
		if i == 1 && len(loopShape.values) == 1 {
			if slots, ok := c.expressionArrayElementSlots(loopShape.values[0]); ok {
				c.localStringSlots[register] = slots
				c.localRowStringSlots[register] = slots
			}
			if slots, ok := c.expressionArrayElementFieldSlots(loopShape.values[0]); ok {
				c.localFieldArrayElemSlots[register] = slots
			}
		}
	}
	c.loops = append(c.loops, loopContext{continueTarget: loopStart})
	if err := c.compileStatements(loopShape.body); err != nil {
		return err
	}
	loop := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]
	c.locals = copyLocals(outerLocals)
	c.localStringSlots = copyLocalStringSlots(outerStringSlots)
	c.localRowStringSlots = copyLocalStringSlots(outerRowStringSlots)
	c.localFieldArrayElemSlots = copyLocalFieldArrayElemSlots(outerFieldArrayElemSlots)

	c.emit(instruction{op: opJump, b: loopStart})
	exit := c.pc()
	c.patchJump(jumpExit, exit)
	for _, jump := range loop.breakJumps {
		c.patchJump(jump, exit)
	}
	return nil
}

func (c *compiler) compileRepeat(stmt repeatStatement) error {
	loopShape := lowerRepeatLoop(stmt)
	if loopShape.kind != loweredLoopPostTest || loopShape.continueTarget != loweredLoopContinueCondition {
		return fmt.Errorf("compile: invalid repeat loop lowering")
	}
	bodyStart := c.pc()

	outerLocals := copyLocals(c.locals)
	c.loops = append(c.loops, loopContext{continueTarget: -1})
	if err := c.compileStatements(loopShape.body); err != nil {
		return err
	}
	loop := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]

	conditionStart := c.pc()
	for _, jump := range loop.continueJumps {
		c.patchJump(jump, conditionStart)
	}

	condition, err := c.compileExpression(loopShape.condition)
	if err != nil {
		return err
	}
	c.locals = copyLocals(outerLocals)

	c.emit(instruction{op: opJumpIfFalse, a: condition, b: bodyStart})
	c.releaseTemp(condition)
	exit := c.pc()
	for _, jump := range loop.breakJumps {
		c.patchJump(jump, exit)
	}
	return nil
}

func (c *compiler) compileBlock(stmt blockStatement) error {
	return c.compileLoweredBlock(lowerBlock(stmt))
}

func (c *compiler) compileLoweredBlock(lowered loweredBlock) error {
	var outerLocals map[string]int
	if lowered.lexicalScope {
		outerLocals = copyLocals(c.locals)
	}
	if err := c.compileStatements(lowered.body); err != nil {
		return err
	}
	if lowered.lexicalScope {
		c.locals = copyLocals(outerLocals)
	}
	return nil
}

func (c *compiler) compileTableTo(table tableExpression, target int) error {
	lowered := lowerTable(table)
	arrayCapacity, fieldCapacity := loweredTableCapacity(lowered)
	c.emit(instruction{op: opNewTable, a: target, b: arrayCapacity, c: fieldCapacity})
	for _, field := range lowered.fields {
		value := c.allocTemp()
		if err := c.compileExpressionTo(field.value, value); err != nil {
			c.releaseTemp(value)
			return err
		}
		switch field.kind {
		case loweredTableFieldComputed:
			key := c.allocTemp()
			if err := c.compileExpressionTo(*field.key, key); err != nil {
				c.releaseTemp(key)
				c.releaseTemp(value)
				return err
			}
			c.emit(instruction{op: opSetIndex, a: target, b: key, c: value})
			c.releaseTemp(key)
		case loweredTableFieldArray:
			key := c.addConstant(NumberValue(float64(field.arrayIndex)))
			c.emit(instruction{op: opSetField, a: target, b: key, c: value})
		case loweredTableFieldNamed:
			key := c.addConstant(StringValue(field.name))
			c.emit(instruction{op: opSetStringField, a: target, b: key, c: value})
		default:
			c.releaseTemp(value)
			return fmt.Errorf("compile: unknown lowered table field kind %d", field.kind)
		}
		c.releaseTemp(value)
	}
	return nil
}

func loweredTableCapacity(table loweredTable) (int, int) {
	arrayCapacity := 0
	fieldCapacity := 0
	for _, field := range table.fields {
		switch field.kind {
		case loweredTableFieldArray:
			if field.arrayIndex > arrayCapacity {
				arrayCapacity = field.arrayIndex
			}
		case loweredTableFieldNamed, loweredTableFieldComputed:
			fieldCapacity++
		}
	}
	return arrayCapacity, fieldCapacity
}

func (c *compiler) compileNamedValueTo(name string, target int) error {
	if ref, ok := c.resolveVariable(name); ok {
		return c.compileVariableRefTo(ref, target)
	}

	constant := c.addConstant(StringValue(name))
	c.emit(instruction{op: opLoadGlobal, a: target, b: constant})
	return nil
}

func (c *compiler) compileNamedTermTo(term term, target int) error {
	if use, ok := c.bind.use(term.id); ok {
		if ref, ok := c.resolveSymbol(use.symbol); ok {
			return c.compileVariableRefTo(ref, target)
		}
	}
	return c.compileNamedValueTo(term.name, target)
}

func (c *compiler) termLocalRef(term term) (variableRef, bool) {
	if !isNamedTerm(term) {
		return variableRef{}, false
	}
	if use, ok := c.bind.use(term.id); ok {
		if ref, ok := c.resolveSymbol(use.symbol); ok && ref.kind == variableLocal {
			return ref, true
		}
	}
	ref, ok := c.resolveVariable(term.name)
	return ref, ok && ref.kind == variableLocal
}

func (c *compiler) compileAssignTargetBaseTo(target assignTarget, register int) error {
	if use, ok := c.bind.use(target.id); ok {
		if ref, ok := c.resolveSymbol(use.symbol); ok {
			return c.compileVariableRefTo(ref, register)
		}
	}
	return c.compileNamedValueTo(target.name, register)
}

func (c *compiler) resolveAssignTarget(target assignTarget) (variableRef, bool) {
	if use, ok := c.bind.use(target.id); ok {
		if ref, ok := c.resolveSymbol(use.symbol); ok {
			return ref, true
		}
	}
	return c.resolveVariable(target.name)
}

func (c *compiler) compileVariableRefTo(ref variableRef, target int) error {
	switch ref.kind {
	case variableLocal:
		if target == ref.index {
			return nil
		}
		c.emit(instruction{op: opMove, a: target, b: ref.index})
	case variableUpvalue:
		c.emit(instruction{op: opGetUpvalue, a: target, b: ref.index})
	default:
		return fmt.Errorf("compile: unknown variable kind")
	}
	return nil
}

func (c *compiler) resolveVariable(name string) (variableRef, bool) {
	if register, ok := c.locals[name]; ok {
		return variableRef{kind: variableLocal, index: register}, true
	}
	upvalue, ok := c.resolveUpvalue(name)
	if !ok {
		return variableRef{}, false
	}
	return variableRef{kind: variableUpvalue, index: upvalue}, true
}

func (c *compiler) resolveSymbol(symbolID int) (variableRef, bool) {
	if register, ok := c.symbolRegisters[symbolID]; ok {
		return variableRef{kind: variableLocal, index: register}, true
	}
	upvalue, ok := c.resolveSymbolUpvalue(symbolID)
	if !ok {
		return variableRef{}, false
	}
	return variableRef{kind: variableUpvalue, index: upvalue}, true
}

func (c *compiler) resolveSymbolUpvalue(symbolID int) (int, bool) {
	if c.upvaluesByID != nil {
		if upvalue, ok := c.upvaluesByID[symbolID]; ok {
			return upvalue, true
		}
	}
	if c.parent == nil {
		return 0, false
	}

	if register, ok := c.parent.symbolRegisters[symbolID]; ok {
		return c.addSymbolUpvalue(symbolID, upvalueDesc{local: true, index: register, copy: c.canCopyParentLocalUpvalue(symbolID)}), true
	}
	parentUpvalue, ok := c.parent.resolveSymbolUpvalue(symbolID)
	if !ok {
		return 0, false
	}
	return c.addSymbolUpvalue(symbolID, upvalueDesc{local: false, index: parentUpvalue}), true
}

func (c *compiler) resolveUpvalue(name string) (int, bool) {
	if c.upvalues != nil {
		if upvalue, ok := c.upvalues[name]; ok {
			return upvalue, true
		}
	}
	if c.parent == nil {
		return 0, false
	}

	if register, ok := c.parent.locals[name]; ok {
		return c.addUpvalue(name, upvalueDesc{local: true, index: register}), true
	}
	parentUpvalue, ok := c.parent.resolveUpvalue(name)
	if !ok {
		return 0, false
	}
	return c.addUpvalue(name, upvalueDesc{local: false, index: parentUpvalue}), true
}

func (c *compiler) addUpvalue(name string, desc upvalueDesc) int {
	if c.upvalues == nil {
		c.upvalues = make(map[string]int)
	}
	upvalue := len(c.upvalueDescs)
	c.upvalues[name] = upvalue
	c.upvalueDescs = append(c.upvalueDescs, desc)
	return upvalue
}

func (c *compiler) addSymbolUpvalue(symbolID int, desc upvalueDesc) int {
	if c.upvaluesByID == nil {
		c.upvaluesByID = make(map[int]int)
	}
	upvalue := len(c.upvalueDescs)
	c.upvaluesByID[symbolID] = upvalue
	c.upvalueDescs = append(c.upvalueDescs, desc)
	return upvalue
}

func (c *compiler) canCopyParentLocalUpvalue(symbolID int) bool {
	if c == nil || c.parent == nil {
		return false
	}
	symbol, ok := c.bindSymbol(symbolID)
	if !ok {
		return false
	}
	if symbol.kind != symbolLocal && symbol.kind != symbolParameter {
		return false
	}
	return symbolID < len(c.bind.symbols) && c.bind.symbols[symbolID].facts.immutableCopyEligible
}

func (c *compiler) bindSymbol(symbolID int) (boundSymbol, bool) {
	if c == nil || symbolID < 0 || symbolID >= len(c.bind.symbols) {
		return boundSymbol{}, false
	}
	return c.bind.symbols[symbolID], true
}

func (c *compiler) claimSymbol(node syntaxID, kind symbolKind) (boundSymbol, bool) {
	symbol, ok := c.bind.definition(node)
	return symbol, ok && symbol.kind == kind
}

func (c *compiler) compileCallTo(call callExpression, target int) error {
	return c.compileCallToResults(call, target, 1)
}

func (c *compiler) compileCallToResults(call callExpression, target int, resultCount int) error {
	lowered := lowerCall(call)
	return c.compileLoweredCallToResults(lowered, call.args, target, resultCount)
}

func (c *compiler) compileLoweredCallToResults(lowered loweredCall, args []expression, target int, resultCount int) error {
	if c.callNeedsScratch(target, resultCount) {
		scratch := c.nextReg
		if err := c.compileLoweredCallToResultsDirect(lowered, args, scratch, resultCount); err != nil {
			return err
		}
		for i := 0; i < resultCount; i++ {
			c.emit(instruction{op: opMove, a: target + i, b: scratch + i})
		}
		c.claimRegisterRange(target, target+resultCount)
		return nil
	}
	return c.compileLoweredCallToResultsDirect(lowered, args, target, resultCount)
}

func (c *compiler) callNeedsScratch(target int, resultCount int) bool {
	if resultCount <= 0 {
		return false
	}
	if c.registerIsLocal(target) {
		return true
	}
	return target+1 < c.nextReg
}

func (c *compiler) registerIsLocal(register int) bool {
	for _, local := range c.locals {
		if local == register {
			return true
		}
	}
	return false
}

func (c *compiler) compileLoweredCallToResultsDirect(lowered loweredCall, args []expression, target int, resultCount int) error {
	if c.selectVarargCountCall(lowered, args, resultCount) {
		return c.compileSelectVarargCountToResults(target, resultCount)
	}
	if c.rawLenIntrinsicCall(lowered) {
		return c.compileBaseIntrinsicCallToResults(nativeFuncRawLen, lowered, args, target, resultCount)
	}
	if intrinsic, ok := c.tableIntrinsicCall(lowered); ok {
		return c.compileBaseIntrinsicCallToResults(intrinsic, lowered, args, target, resultCount)
	}
	if intrinsic, ok := c.coroutineIntrinsicCall(lowered); ok {
		return c.compileBaseIntrinsicCallToResults(intrinsic, lowered, args, target, resultCount)
	}
	if intrinsic, ok := c.mathIntrinsicCall(lowered); ok {
		return c.compileBaseIntrinsicCallToResults(intrinsic, lowered, args, target, resultCount)
	}
	if method, ok := c.methodOneResultCall(lowered, resultCount); ok {
		return c.compileMethodOneResultCallToResults(method, lowered, args, target)
	}
	if local, ok := c.localOneResultCall(lowered, resultCount); ok {
		return c.compileLocalOneResultCallToResults(local, lowered, args, target)
	}
	if upvalue, ok := c.upvalueOneResultCall(lowered, resultCount); ok {
		return c.compileUpvalueOneResultCallToResults(upvalue, lowered, args, target)
	}
	return c.compileLoweredCallToResultsGeneric(lowered, args, target, resultCount)
}

type methodOneResultCall struct {
	receiver int
	field    string
}

type tableFieldKeyOneResultCall struct {
	table    int
	keyBase  term
	keyField string
	keySlot  int
}

func (c *compiler) methodOneResultCall(lowered loweredCall, resultCount int) (methodOneResultCall, bool) {
	if resultCount != 1 || lowered.receiver == nil {
		return methodOneResultCall{}, false
	}
	target := lowered.target
	if len(target.selectors) != 1 ||
		target.selectors[0].field == "" ||
		target.selectors[0].index != nil {
		return methodOneResultCall{}, false
	}
	base := target
	base.selectors = nil
	receiver, ok := c.termLocalRef(*lowered.receiver)
	if !ok {
		return methodOneResultCall{}, false
	}
	targetBase, ok := c.termLocalRef(base)
	if !ok || targetBase.index != receiver.index {
		return methodOneResultCall{}, false
	}
	for _, item := range lowered.args.items {
		if item.kind != loweredValueSingle {
			return methodOneResultCall{}, false
		}
	}
	return methodOneResultCall{
		receiver: receiver.index,
		field:    target.selectors[0].field,
	}, true
}

func (c *compiler) compileMethodOneResultCallToResults(
	method methodOneResultCall,
	lowered loweredCall,
	args []expression,
	target int,
) error {
	span := len(args) + 2
	c.reserveRegistersThrough(target + span)
	for i, item := range lowered.args.items {
		if err := c.compileExpressionTo(args[item.source], target+2+i); err != nil {
			return err
		}
	}
	c.claimRegister(target)
	key := c.addConstant(StringValue(method.field))
	c.emit(instruction{op: opCallMethodOne, a: target, b: method.receiver, c: key, d: len(args)})
	return nil
}

func (c *compiler) tableFieldKeyOneResultCall(lowered loweredCall, resultCount int) (tableFieldKeyOneResultCall, bool) {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) ||
		resultCount != 1 ||
		lowered.receiver != nil {
		return tableFieldKeyOneResultCall{}, false
	}
	for _, item := range lowered.args.items {
		if item.kind != loweredValueSingle {
			return tableFieldKeyOneResultCall{}, false
		}
	}
	target := lowered.target
	if len(target.selectors) != 1 || target.selectors[0].index == nil || target.selectors[0].field != "" {
		return tableFieldKeyOneResultCall{}, false
	}
	base := target
	base.selectors = nil
	table, ok := c.termLocalRef(base)
	if !ok {
		return tableFieldKeyOneResultCall{}, false
	}
	keyTerm, ok := expressionSingleTerm(*target.selectors[0].index)
	if !ok || len(keyTerm.selectors) != 1 || keyTerm.selectors[0].field == "" || keyTerm.selectors[0].index != nil {
		return tableFieldKeyOneResultCall{}, false
	}
	keyBase := keyTerm
	keyBase.selectors = nil
	keyBaseRef, ok := c.termLocalRef(keyBase)
	if !ok {
		return tableFieldKeyOneResultCall{}, false
	}
	return tableFieldKeyOneResultCall{
		table:    table.index,
		keyBase:  keyBase,
		keyField: keyTerm.selectors[0].field,
		keySlot:  c.localStringFieldSlot(keyBaseRef.index, keyTerm.selectors[0].field),
	}, true
}

func (c *compiler) localStringFieldSlot(register int, field string) int {
	if slots, ok := c.localStringSlots[register]; ok {
		if slot, ok := slots[field]; ok {
			return slot
		}
	}
	return -1
}

func (c *compiler) compileTableFieldKeyOneResultCallToResults(
	call tableFieldKeyOneResultCall,
	lowered loweredCall,
	args []expression,
	target int,
) error {
	argCount := len(args)
	keySource := target + argCount + 1
	c.reserveRegistersThrough(keySource + 1)
	for i, item := range lowered.args.items {
		if err := c.compileExpressionTo(args[item.source], target+1+i); err != nil {
			return err
		}
	}
	if err := c.compileTermTo(call.keyBase, keySource); err != nil {
		return err
	}
	c.claimRegister(target)
	key := c.addConstant(StringValue(call.keyField))
	c.emit(instruction{op: opGetStringField, a: keySource, b: keySource, c: key})
	c.emit(instruction{op: opGetIndex, a: target, b: call.table, c: keySource})
	c.emit(instruction{op: opCallOne, a: target, b: target, c: argCount})
	return nil
}

func (c *compiler) selectVarargCountCall(lowered loweredCall, args []expression, resultCount int) bool {
	if resultCount == 0 || lowered.receiver != nil || !c.variadic {
		return false
	}
	if !c.isUnboundGlobalName(lowered.target, "select") {
		return false
	}
	if len(args) != 2 || len(lowered.args.items) != 2 {
		return false
	}
	if marker, ok := expressionStringLiteral(args[0]); !ok || marker != "#" {
		return false
	}
	if _, ok := expressionSingleVararg(args[1]); !ok {
		return false
	}
	return lowered.args.items[0].kind == loweredValueSingle &&
		lowered.args.items[1].kind == loweredValueExpanded
}

func (c *compiler) compileSelectVarargCountToResults(target int, resultCount int) error {
	c.reserveRegistersThrough(target + 1)
	c.claimRegister(target)
	c.emit(instruction{op: opFastCall, a: target, b: int(nativeFuncSelect), c: 0, d: resultCount})
	return nil
}

func (c *compiler) rawLenIntrinsicCall(lowered loweredCall) bool {
	return c.options.optimizations.enabled(optimizationBytecodePeephole) &&
		lowered.receiver == nil &&
		c.isUnboundGlobalName(lowered.target, "rawlen")
}

func (c *compiler) isUnboundGlobalName(term term, name string) bool {
	if !isNamedTerm(term) || term.name != name {
		return false
	}
	if use, ok := c.bind.use(term.id); ok {
		if _, resolved := c.resolveSymbol(use.symbol); resolved {
			return false
		}
	}
	if _, ok := c.resolveVariable(term.name); ok {
		return false
	}
	return true
}

func expressionStringLiteral(expr expression) (string, bool) {
	value, ok := expressionSingleTerm(expr)
	if !ok || value.lit == nil {
		return "", false
	}
	return value.lit.String()
}

func (c *compiler) upvalueOneResultCall(lowered loweredCall, resultCount int) (int, bool) {
	if resultCount != 1 || lowered.receiver != nil {
		return 0, false
	}
	target := lowered.target
	if !isNamedTerm(target) || len(target.selectors) != 0 {
		return 0, false
	}
	for _, item := range lowered.args.items {
		if item.kind != loweredValueSingle {
			return 0, false
		}
	}
	if use, ok := c.bind.use(target.id); ok {
		ref, ok := c.resolveSymbol(use.symbol)
		return ref.index, ok && ref.kind == variableUpvalue
	}
	ref, ok := c.resolveVariable(target.name)
	return ref.index, ok && ref.kind == variableUpvalue
}

func (c *compiler) selfUpvalueOneResultCall(lowered loweredCall, resultCount int) (int, bool) {
	if c.selfFunctionSymbol < 0 || resultCount != 1 || lowered.receiver != nil {
		return 0, false
	}
	target := lowered.target
	if !isNamedTerm(target) || len(target.selectors) != 0 {
		return 0, false
	}
	for _, item := range lowered.args.items {
		if item.kind != loweredValueSingle {
			return 0, false
		}
	}
	use, ok := c.bind.use(target.id)
	if !ok || use.symbol != c.selfFunctionSymbol {
		return 0, false
	}
	ref, ok := c.resolveSymbol(use.symbol)
	return ref.index, ok && ref.kind == variableUpvalue
}

func (c *compiler) localOneResultCall(lowered loweredCall, resultCount int) (int, bool) {
	if resultCount != 1 || lowered.receiver != nil {
		return 0, false
	}
	target := lowered.target
	if !isNamedTerm(target) || len(target.selectors) != 0 {
		return 0, false
	}
	for _, item := range lowered.args.items {
		if item.kind != loweredValueSingle {
			return 0, false
		}
	}
	if use, ok := c.bind.use(target.id); ok {
		ref, ok := c.resolveSymbol(use.symbol)
		return ref.index, ok && ref.kind == variableLocal
	}
	ref, ok := c.resolveVariable(target.name)
	return ref.index, ok && ref.kind == variableLocal
}

func (c *compiler) compileLocalOneResultCallToResults(local int, lowered loweredCall, args []expression, target int) error {
	span := len(args)
	if span <= 0 {
		span = 1
	}
	c.reserveRegistersThrough(target + span)
	for i, item := range lowered.args.items {
		if err := c.compileExpressionTo(args[item.source], target+i); err != nil {
			return err
		}
	}
	c.claimRegister(target)
	c.emit(instruction{op: opCallLocalOne, a: target, b: local, c: target, d: len(args)})
	return nil
}

func (c *compiler) compileUpvalueOneResultCallToResults(upvalue int, lowered loweredCall, args []expression, target int) error {
	span := len(args)
	if span <= 0 {
		span = 1
	}
	c.reserveRegistersThrough(target + span)
	for i, item := range lowered.args.items {
		if err := c.compileExpressionTo(args[item.source], target+i); err != nil {
			return err
		}
	}
	c.claimRegister(target)
	c.emit(instruction{op: opCallUpvalueOne, a: target, b: upvalue, c: target, d: len(args)})
	return nil
}

func (c *compiler) compileSelfUpvalueOneResultCallToResults(upvalue int, lowered loweredCall, args []expression, target int) error {
	if source, constant, ok := c.selfCallSubtractConstantArg(args); ok {
		c.reserveRegistersThrough(target + 1)
		c.claimRegister(target)
		c.emit(instruction{op: opMove, a: target, b: source})
		c.emit(instruction{op: opSubK, a: target, b: target, c: constant})
		c.emit(instruction{op: opCallUpvalueOne, a: target, b: upvalue, c: target, d: 1})
		return nil
	}
	span := len(args)
	if span <= 0 {
		span = 1
	}
	c.reserveRegistersThrough(target + span)
	for i, item := range lowered.args.items {
		if err := c.compileExpressionTo(args[item.source], target+i); err != nil {
			return err
		}
	}
	c.claimRegister(target)
	c.emit(instruction{op: opCallUpvalueOne, a: target, b: upvalue, c: target, d: len(args)})
	return nil
}

type selfUpvaluePairAddReturn struct {
	upvalue   int
	source    int
	baseLess  int
	firstSub  int
	secondSub int
}

func (c *compiler) selfUpvaluePairAddReturn(expr expression) (selfUpvaluePairAddReturn, bool) {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) ||
		c.selfFunctionSymbol < 0 ||
		!c.selfNumericPairAdd ||
		len(expr.terms) != 1 ||
		len(expr.terms[0].terms) != 1 {
		return selfUpvaluePairAddReturn{}, false
	}
	comparison := expr.terms[0].terms[0]
	if comparison.op != "" || comparison.right != nil || len(comparison.left.rest) != 0 {
		return selfUpvaluePairAddReturn{}, false
	}
	additive := comparison.left.first
	if len(additive.rest) != 1 || additive.rest[0].op != additiveAdd {
		return selfUpvaluePairAddReturn{}, false
	}
	firstCall, ok := multiplicativeSingleCall(additive.first)
	if !ok {
		return selfUpvaluePairAddReturn{}, false
	}
	secondCall, ok := multiplicativeSingleCall(additive.rest[0].value)
	if !ok {
		return selfUpvaluePairAddReturn{}, false
	}
	first, ok := c.selfCallSubtractConstantCall(firstCall)
	if !ok {
		return selfUpvaluePairAddReturn{}, false
	}
	second, ok := c.selfCallSubtractConstantCall(secondCall)
	if !ok ||
		first.upvalue != second.upvalue ||
		first.source != second.source {
		return selfUpvaluePairAddReturn{}, false
	}
	return selfUpvaluePairAddReturn{
		upvalue:   first.upvalue,
		source:    first.source,
		baseLess:  c.addConstant(NumberValue(c.selfNumericPairBase)),
		firstSub:  first.constant,
		secondSub: second.constant,
	}, true
}

type selfCallSubtractConstantCall struct {
	upvalue  int
	source   int
	constant int
}

func (c *compiler) selfCallSubtractConstantCall(call callExpression) (selfCallSubtractConstantCall, bool) {
	if call.receiver != nil ||
		len(call.args) != 1 ||
		!isNamedTerm(call.target) ||
		len(call.target.selectors) != 0 {
		return selfCallSubtractConstantCall{}, false
	}
	use, ok := c.bind.use(call.target.id)
	if !ok || use.symbol != c.selfFunctionSymbol {
		return selfCallSubtractConstantCall{}, false
	}
	ref, ok := c.resolveSymbol(use.symbol)
	if !ok || ref.kind != variableUpvalue {
		return selfCallSubtractConstantCall{}, false
	}
	source, constant, ok := c.selfCallSubtractConstantArg(call.args)
	if !ok {
		return selfCallSubtractConstantCall{}, false
	}
	return selfCallSubtractConstantCall{
		upvalue:  ref.index,
		source:   source,
		constant: constant,
	}, true
}

func (c *compiler) selfCallSubtractConstantArg(args []expression) (int, int, bool) {
	if len(args) != 1 {
		return 0, 0, false
	}
	expr := args[0]
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return 0, 0, false
	}
	comparison := expr.terms[0].terms[0]
	if comparison.op != "" || comparison.right != nil || len(comparison.left.rest) != 0 {
		return 0, 0, false
	}
	additive := comparison.left.first
	if len(additive.rest) != 1 || additive.rest[0].op != additiveSubtract {
		return 0, 0, false
	}
	if len(additive.first.rest) != 0 {
		return 0, 0, false
	}
	ref, ok := c.termLocalRef(termWithoutCastsAndGroups(additive.first.first))
	if !ok {
		return 0, 0, false
	}
	number, ok := foldNumberMultiplicative(additive.rest[0].value)
	if !ok {
		return 0, 0, false
	}
	return ref.index, c.addConstant(NumberValue(number)), true
}

func (c *compiler) tableIntrinsicCall(lowered loweredCall) (nativeFuncID, bool) {
	return c.baseFieldIntrinsicCall(lowered, "table")
}

func (c *compiler) coroutineIntrinsicCall(lowered loweredCall) (nativeFuncID, bool) {
	return c.baseFieldIntrinsicCall(lowered, "coroutine")
}

func (c *compiler) mathIntrinsicCall(lowered loweredCall) (nativeFuncID, bool) {
	return c.baseFieldIntrinsicCall(lowered, "math")
}

func (c *compiler) baseFieldIntrinsicCall(lowered loweredCall, globalName string) (nativeFuncID, bool) {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) ||
		lowered.receiver != nil ||
		!c.isUnboundBaseField(lowered.target, globalName) {
		return nativeFuncUnknown, false
	}
	field := lowered.target.selectors[0].field
	intrinsic, ok := baseFieldIntrinsic(globalName, field)
	if !ok {
		return nativeFuncUnknown, false
	}
	return intrinsic.nativeID, true
}

func selfNumericPairAddClosureBase(closure loweredClosure) (float64, bool) {
	if len(closure.params) != 1 ||
		closure.variadic ||
		len(closure.body) != 2 ||
		closure.body[0].ifStmt == nil ||
		closure.body[1].ret == nil {
		return 0, false
	}
	param := closure.params[0]
	ifStmt := closure.body[0].ifStmt
	if len(ifStmt.thenStatements) != 1 ||
		ifStmt.thenStatements[0].ret == nil ||
		len(ifStmt.elseStatements) != 0 {
		return 0, false
	}
	base, ok := lessThanNumberCondition(ifStmt.condition, param)
	if !ok {
		return 0, false
	}
	if !singleNameReturn(*ifStmt.thenStatements[0].ret, param) {
		return 0, false
	}
	return base, true
}

func lessThanNumberCondition(expr expression, name string) (float64, bool) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return 0, false
	}
	comparison := expr.terms[0].terms[0]
	if comparison.op != comparisonLess || comparison.right == nil || len(comparison.left.rest) != 0 {
		return 0, false
	}
	left := comparison.left.first
	if len(left.rest) != 0 || len(left.first.rest) != 0 {
		return 0, false
	}
	value := termWithoutCastsAndGroups(left.first.first)
	if !isNamedTerm(value) || value.name != name || len(value.selectors) != 0 {
		return 0, false
	}
	return foldNumberConcat(*comparison.right)
}

func singleNameReturn(stmt returnStatement, name string) bool {
	if len(stmt.values) != 1 {
		return false
	}
	value, ok := expressionSingleTerm(stmt.values[0])
	return ok && isNamedTerm(value) && value.name == name && len(value.selectors) == 0
}

func (c *compiler) isUnboundBaseField(term term, name string) bool {
	base := term
	base.selectors = nil
	if !isNamedTerm(base) || base.name != name ||
		len(term.selectors) != 1 ||
		term.selectors[0].field == "" ||
		term.selectors[0].index != nil {
		return false
	}
	if use, ok := c.bind.use(term.id); ok {
		if _, resolved := c.resolveSymbol(use.symbol); resolved {
			return false
		}
	}
	if _, ok := c.resolveVariable(base.name); ok {
		return false
	}
	return true
}

func (c *compiler) compileBaseIntrinsicCallToResults(
	nativeID nativeFuncID,
	lowered loweredCall,
	args []expression,
	target int,
	resultCount int,
) error {
	for _, item := range lowered.args.items {
		if item.kind != loweredValueSingle {
			return c.compileLoweredCallToResultsGeneric(lowered, args, target, resultCount)
		}
	}

	span := len(args)
	if resultCount > span {
		span = resultCount
	}
	if span <= 0 {
		span = 1
	}
	c.reserveRegistersThrough(target + span)
	for i, item := range lowered.args.items {
		if err := c.compileExpressionTo(args[item.source], target+i); err != nil {
			return err
		}
	}
	if resultCount > 0 {
		c.claimRegisterRange(target, target+resultCount)
	} else {
		c.claimRegister(target)
	}
	c.emit(instruction{op: opFastCall, a: target, b: int(nativeID), c: len(args), d: resultCount})
	return nil
}

func (c *compiler) compileLoweredCallToResultsGeneric(lowered loweredCall, args []expression, target int, resultCount int) error {
	if err := c.compileCallTargetTo(lowered.target, target); err != nil {
		return err
	}

	firstArg := target + 1
	fixedArgCount := 0
	if lowered.receiver != nil {
		c.reserveRegistersThrough(target + 2 + len(args))
		if err := c.compileCallTargetTo(*lowered.receiver, firstArg); err != nil {
			return err
		}
		firstArg++
		fixedArgCount += lowered.fixedArgCount
	} else {
		c.reserveRegistersThrough(target + 1 + len(args))
	}

	argCount := fixedArgCount
	for _, item := range lowered.args.items {
		argRegister := firstArg + item.source
		switch item.kind {
		case loweredValueExpanded:
			openTarget := argRegister
			c.reserveRegistersThrough(openTarget + 1)
			if vararg, ok := expressionSingleVararg(args[item.source]); ok {
				if err := c.compileVarargToResults(vararg, openTarget, item.resultCount); err != nil {
					return err
				}
			} else if nestedCall, ok := expressionSingleCall(args[item.source]); ok {
				if err := c.compileCallToResults(nestedCall, openTarget, item.resultCount); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("compile: expanded call argument is not a call or vararg")
			}
			argCount = -(fixedArgCount + 1)
		case loweredValueSingle:
			if err := c.compileExpressionTo(args[item.source], argRegister); err != nil {
				return err
			}
			fixedArgCount++
			argCount = fixedArgCount
		default:
			return fmt.Errorf("compile: unknown lowered value kind %d", item.kind)
		}
	}
	if resultCount > 0 {
		c.claimRegisterRange(target, target+resultCount)
	} else {
		c.claimRegister(target)
	}
	op := opCall
	if resultCount == 1 && argCount >= 0 {
		op = opCallOne
	}
	c.emit(instruction{op: op, a: target, b: target, c: argCount, d: resultCount})
	return nil
}

func (c *compiler) compileVarargToResults(_ term, target int, resultCount int) error {
	if !c.variadic {
		return fmt.Errorf("compile: vararg outside variadic function")
	}
	if resultCount > 0 {
		c.reserveRegistersThrough(target + resultCount)
		c.claimRegisterRange(target, target+resultCount)
	} else {
		c.reserveRegistersThrough(target + 1)
		c.claimRegister(target)
	}
	c.emit(instruction{op: opVararg, a: target, b: resultCount})
	return nil
}

func (c *compiler) allocReg() int {
	register := c.nextReg
	c.nextReg++
	return register
}

func (c *compiler) allocTemp() int {
	if len(c.freeTemps) == 0 {
		return c.allocReg()
	}
	last := len(c.freeTemps) - 1
	register := c.freeTemps[last]
	c.freeTemps = c.freeTemps[:last]
	return register
}

func (c *compiler) releaseTemp(register int) {
	if register < 0 {
		return
	}
	for _, existing := range c.freeTemps {
		if existing == register {
			return
		}
	}
	c.freeTemps = append(c.freeTemps, register)
}

func (c *compiler) claimRegister(register int) {
	for i, existing := range c.freeTemps {
		if existing == register {
			c.freeTemps = append(c.freeTemps[:i], c.freeTemps[i+1:]...)
			return
		}
	}
}

func (c *compiler) claimRegisterRange(start int, end int) {
	for register := start; register < end; register++ {
		c.claimRegister(register)
	}
}

func (c *compiler) reserveRegistersThrough(nextReg int) {
	if c.nextReg < nextReg {
		c.nextReg = nextReg
	}
}

func copyLocals(locals map[string]int) map[string]int {
	copied := make(map[string]int, len(locals))
	for name, register := range locals {
		copied[name] = register
	}
	return copied
}

func copyLocalStringSlots(slots map[int]map[string]int) map[int]map[string]int {
	copied := make(map[int]map[string]int, len(slots))
	for register, registerSlots := range slots {
		slotCopy := make(map[string]int, len(registerSlots))
		for field, slot := range registerSlots {
			slotCopy[field] = slot
		}
		copied[register] = slotCopy
	}
	return copied
}

func copyLocalFieldArrayElemSlots(slots map[int]map[string]map[string]int) map[int]map[string]map[string]int {
	copied := make(map[int]map[string]map[string]int, len(slots))
	for register, fieldSlots := range slots {
		fieldCopy := make(map[string]map[string]int, len(fieldSlots))
		for field, elemSlots := range fieldSlots {
			elemCopy := make(map[string]int, len(elemSlots))
			for elemField, slot := range elemSlots {
				elemCopy[elemField] = slot
			}
			fieldCopy[field] = elemCopy
		}
		copied[register] = fieldCopy
	}
	return copied
}
