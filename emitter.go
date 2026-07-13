package ember

import (
	"fmt"
	"sort"
	"strings"
)

type compiler struct {
	bytecodeBuilder
	bind               bindResult
	sourceLines        sourceLineMap
	symbolRegisters    []int
	localRegisters     registerSet
	parent             *compiler
	selfFunctionSymbol int
	variadic           bool
	upvaluesByID       []int
	upvalueDescs       []upvalueDesc
	loops              []loopContext
	prototypeDrafts    []*functionDraft
	nextReg            int
	freeTemps          []int
	suppressTagChains  bool
	options            compilerOptions
	sourceName         string
	functionName       string
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

func newDenseSymbolSlots(count int) []int {
	slots := make([]int, count)
	for i := range slots {
		slots[i] = -1
	}
	return slots
}

func denseSymbolSlot(slots []int, symbolID int) (int, bool) {
	if symbolID < 0 || symbolID >= len(slots) || slots[symbolID] < 0 {
		return 0, false
	}
	return slots[symbolID], true
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
	sourceName := source.source.Name
	if sourceName == "" {
		sourceName = "<string>"
	} else {
		sourceName = strings.Clone(sourceName)
	}
	c := compiler{
		bind:               source.bind,
		sourceLines:        newSourceLineMap(source.source.Text),
		symbolRegisters:    newDenseSymbolSlots(len(source.bind.symbols)),
		selfFunctionSymbol: -1,
		options:            options,
		sourceName:         sourceName,
		functionName:       "<module>",
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
	draft := newFunctionDraft(c.constants, assembly, c.prototypeDrafts, upvalues, registers, params, variadic)
	draft.sourceName = c.sourceName
	if draft.sourceName == "" {
		draft.sourceName = "<string>"
	}
	draft.functionName = strings.Clone(c.functionName)
	if draft.functionName == "" {
		draft.functionName = "<module>"
	}
	return draft
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

func (c *compiler) addStringConstant(value string) int {
	return c.bytecodeBuilder.addStringConstant(value)
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
	switch {
	case stmt.local != nil:
		return c.compileLocal(*stmt.local)
	case stmt.localFunc != nil:
		return c.compileLocalFunction(*stmt.localFunc)
	case stmt.funcDecl != nil:
		return c.compileFunctionDeclaration(*stmt.funcDecl)
	case stmt.assign != nil:
		return c.compileAssignment(*stmt.assign)
	case stmt.call != nil:
		return c.compileCallStatement(*stmt.call)
	case stmt.ifStmt != nil:
		return c.compileIf(*stmt.ifStmt)
	case stmt.while != nil:
		return c.compileWhile(*stmt.while)
	case stmt.forLoop != nil:
		return c.compileFor(*stmt.forLoop)
	case stmt.genericFor != nil:
		return c.compileGenericFor(*stmt.genericFor)
	case stmt.repeat != nil:
		return c.compileRepeat(*stmt.repeat)
	case stmt.block != nil:
		return c.compileBlock(*stmt.block)
	case stmt.typeAlias != nil:
		return nil
	case stmt.breaking:
		return c.compileBreak()
	case stmt.continues:
		return c.compileContinue()
	case stmt.ret != nil:
		return c.compileReturn(*stmt.ret)
	default:
		return fmt.Errorf("compile: empty statement")
	}
}

func (c *compiler) compileLocal(stmt localStatement) error {
	if len(stmt.names) == 0 {
		return fmt.Errorf("compile: local statement has no names")
	}

	first := c.allocReg()
	targets := make([]int, len(stmt.names))
	for i := range targets {
		targets[i] = first + i
	}
	c.reserveRegistersThrough(first + len(targets))

	plan := fixedValueListPlan(stmt.values, len(targets))
	if err := c.compileValueListTo(plan, targets); err != nil {
		return err
	}
	for i := range stmt.names {
		if err := c.assignDefinition(syntaxNameID(stmt.nameID, i), symbolLocal, targets[i]); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileReturn(stmt returnStatement) error {
	if len(stmt.values) == 0 {
		c.emit(instruction{op: opReturn})
		return nil
	}

	plan := openValueListPlan(stmt.values)
	if plan.len() == 1 && plan.item(0).kind == valuePlanSingle {
		if ref, ok := c.expressionLocalRef(stmt.values[0]); ok {
			c.emit(instruction{op: opReturnOne, a: ref.index})
			return nil
		}
	}
	first := c.allocReg()
	for i := 0; i < plan.len(); i++ {
		item := plan.item(i)
		target := first + i
		c.reserveRegistersThrough(target + 1)
		switch item.kind {
		case valuePlanExpanded:
			if vararg, ok := expressionSingleVararg(stmt.values[item.source]); ok {
				if err := c.compileVarargToResults(vararg, target, item.resultCount); err != nil {
					return err
				}
			} else if call, ok := expressionSingleCall(stmt.values[item.source]); ok {
				if err := c.compileCallToResults(call, target, item.resultCount); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("compile: expanded return value is not a call or vararg")
			}
			c.emit(instruction{op: opReturn, a: first, b: -(i + 1)})
			return nil
		case valuePlanSingle:
			if err := c.compileExpressionTo(stmt.values[item.source], target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("compile: unknown value plan kind %d", item.kind)
		}
	}
	c.reserveRegistersThrough(first + plan.len())
	if plan.len() == 1 {
		c.emit(instruction{op: opReturnOne, a: first})
		return nil
	}
	c.emit(instruction{op: opReturn, a: first, b: plan.len()})
	return nil
}

func (c *compiler) compileCallStatement(stmt term) error {
	if stmt.call == nil {
		return fmt.Errorf("compile: call statement has no call")
	}
	result := c.allocReg()
	return c.compilePlannedCallToResults(planCall(*stmt.call), stmt.call.args, result, 1)
}

func (c *compiler) compileExpressionListTo(values []expression, targets []int) error {
	if len(targets) == 0 {
		return nil
	}
	return c.compileValueListTo(fixedValueListPlan(values, len(targets)), targets)
}

func (c *compiler) compileValueListTo(plan valueListPlan, targets []int) error {
	for i := 0; i < plan.len(); i++ {
		item := plan.item(i)
		target := targets[i]
		c.reserveRegistersThrough(target + 1)
		switch item.kind {
		case valuePlanNil:
			c.compileNilTo(target)
			continue
		case valuePlanExpanded:
			if vararg, ok := expressionSingleVararg(plan.values[item.source]); ok {
				return c.compileVarargToResults(vararg, target, item.resultCount)
			}
			if call, ok := expressionSingleCall(plan.values[item.source]); ok {
				return c.compileCallToResults(call, target, item.resultCount)
			}
			return fmt.Errorf("compile: expanded value is not a call or vararg")
		case valuePlanSingle:
			if err := c.compileExpressionTo(plan.values[item.source], target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("compile: unknown value plan kind %d", item.kind)
		}
	}
	return nil
}

func (c *compiler) compileNilTo(target int) {
	c.emitLoadConst(target, NilValue())
}

func (c *compiler) compileLocalFunction(stmt localFunctionStatement) error {
	closure := planLocalFunction(stmt)
	target := c.allocReg()
	selfFunctionSymbol := -1
	symbol, err := c.claimSymbol(stmt.nameID, symbolLocalFunction)
	if err != nil {
		return err
	}
	if err := c.assignSymbolRegister(symbol.id, target); err != nil {
		return err
	}
	selfFunctionSymbol = symbol.id
	if err := c.compileClosureToSelf(closure, target, selfFunctionSymbol); err != nil {
		return err
	}
	return nil
}

func (c *compiler) compileFunctionDeclaration(stmt functionDeclarationStatement) error {
	closure := planFunctionDeclaration(stmt)

	value := c.allocReg()
	if err := c.compileClosureTo(closure, value); err != nil {
		return err
	}
	return c.compileAssignTargetFromRegister(stmt.target, value)
}

func (c *compiler) compileFunctionDraft(closure closurePlan, selfFunctionSymbol int) (*functionDraft, error) {
	fn := compiler{
		bind:               c.bind,
		sourceLines:        c.sourceLines,
		symbolRegisters:    newDenseSymbolSlots(len(c.bind.symbols)),
		parent:             c,
		selfFunctionSymbol: selfFunctionSymbol,
		variadic:           closure.variadic,
		upvaluesByID:       newDenseSymbolSlots(len(c.bind.symbols)),
		nextReg:            closure.paramCount(),
		options:            c.options,
		sourceName:         c.sourceName,
		functionName:       closure.functionName,
	}
	fn.sourceText = c.sourceText
	for i := 0; i < closure.paramCount(); i++ {
		_, paramID := closure.param(i)
		if err := fn.assignDefinition(paramID, symbolParameter, i); err != nil {
			return nil, err
		}
	}
	if err := fn.compileStatements(closure.body); err != nil {
		return nil, err
	}
	if !statementsHaveReturn(closure.body) {
		fn.emit(instruction{op: opReturn})
	}

	fn.optimizeFunction(c.options.optimizations)
	return fn.buildFunctionDraft(fn.upvalueDescs, closure.paramCount(), closure.variadic), nil
}

func (c *compiler) compileTempExpression(expr expression) (int, error) {
	target := c.allocTemp()
	if err := c.compileExpressionTo(expr, target); err != nil {
		c.releaseTemp(target)
		return 0, err
	}
	return target, nil
}

func (c *compiler) compileExpressionTo(expr expression, target int) error {
	c.claimRegister(target)
	source := expressionRange(expr)
	return c.withSourceRange(source, func() error {
		if c.options.optimizations.enabled(optimizationHIRSimplify) {
			if value, ok := foldConstantExpression(expr); ok {
				c.emitLoadConst(target, value)
				return nil
			}
		}
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
		return c.compileClosureTo(planFunctionExpression(*term.function), target)
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

func (c *compiler) compileClosureTo(closure closurePlan, target int) error {
	return c.compileClosureToSelf(closure, target, -1)
}

func (c *compiler) compileClosureToSelf(closure closurePlan, target int, selfFunctionSymbol int) error {
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
			firstKey := c.addStringConstant(selectors[0].field)
			secondKey := c.addStringConstant(selectors[1].field)
			c.emit(instruction{op: opGetStringField, a: target, b: target, c: firstKey})
			c.emit(instruction{op: opGetStringField, a: target, b: target, c: secondKey})
			selectors = selectors[2:]
			continue
		}
		if len(selectors) >= 2 && selectors[0].field != "" && selectors[1].index != nil {
			firstKey := c.addStringConstant(selectors[0].field)
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
			key := c.addStringConstant(selector.field)
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
		firstKey := c.addStringConstant(first.field)
		secondKey := c.addStringConstant(selectors[1].field)
		c.emit(instruction{op: opGetStringField, a: target, b: base, c: firstKey})
		c.emit(instruction{op: opGetStringField, a: target, b: target, c: secondKey})
		return c.compileSelectorsTo(selectors[2:], target)
	}
	if len(selectors) >= 2 && first.field != "" && selectors[1].index != nil {
		firstKey := c.addStringConstant(first.field)
		key := c.allocReg()
		if err := c.compileExpressionTo(*selectors[1].index, key); err != nil {
			return err
		}
		c.emit(instruction{op: opGetStringFieldIndex, a: target, b: base, c: firstKey, d: key})
		return c.compileSelectorsTo(selectors[2:], target)
	}
	if first.field != "" {
		key := c.addStringConstant(first.field)
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
	if len(stmt.targets) == 0 {
		return fmt.Errorf("compile: assignment has no targets")
	}
	plan := fixedValueListPlan(stmt.values, len(stmt.targets))

	if c.canCompileSingleLocalAssignmentInPlace(stmt, plan) {
		target := stmt.targets[0]
		ref, _ := c.resolveAssignTarget(target)
		return c.compileExpressionTo(stmt.values[plan.item(0).source], ref.index)
	}

	if addField, ok := c.addStringFieldAssignment(stmt, plan); ok {
		return c.compileAddStringFieldAssignment(addField)
	}
	if subField, ok := c.subStringFieldAssignment(stmt, plan); ok {
		return c.compileSubStringFieldAssignment(subField)
	}
	first := c.allocReg()
	values := make([]int, len(stmt.targets))
	for i := range values {
		values[i] = first + i
	}
	c.reserveRegistersThrough(first + len(values))
	if err := c.compileValueListTo(plan, values); err != nil {
		return err
	}

	for i, target := range stmt.targets {
		if err := c.compileAssignTargetFromRegister(target, values[i]); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) canCompileSingleLocalAssignmentInPlace(stmt assignStatement, plan valueListPlan) bool {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) {
		return false
	}
	if len(stmt.targets) != 1 || plan.len() != 1 {
		return false
	}
	target := stmt.targets[0]
	if len(target.selectors) != 0 {
		return false
	}
	item := plan.item(0)
	if item.kind != valuePlanSingle {
		return false
	}
	ref, ok := c.resolveAssignTarget(target)
	if !ok || ref.kind != variableLocal {
		return false
	}
	return expressionCanAssignToNameInPlace(stmt.values[item.source], target.name)
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
}

type subStringFieldAssignment struct {
	table   int
	field   string
	operand expression
}

func (c *compiler) addStringFieldAssignment(stmt assignStatement, plan valueListPlan) (addStringFieldAssignment, bool) {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) {
		return addStringFieldAssignment{}, false
	}
	if len(stmt.targets) != 1 || plan.len() != 1 {
		return addStringFieldAssignment{}, false
	}
	item := plan.item(0)
	if item.kind != valuePlanSingle {
		return addStringFieldAssignment{}, false
	}
	target := stmt.targets[0]
	if len(target.selectors) != 1 || target.selectors[0].field == "" {
		return addStringFieldAssignment{}, false
	}
	ref, ok := c.resolveAssignTarget(target)
	if !ok || ref.kind != variableLocal {
		return addStringFieldAssignment{}, false
	}
	operand, ok := fieldAddAssignmentOperand(stmt.values[item.source], target)
	if !ok {
		return addStringFieldAssignment{}, false
	}
	return addStringFieldAssignment{
		table:   ref.index,
		field:   target.selectors[0].field,
		operand: operand,
	}, true
}

func (c *compiler) subStringFieldAssignment(stmt assignStatement, plan valueListPlan) (subStringFieldAssignment, bool) {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) {
		return subStringFieldAssignment{}, false
	}
	if len(stmt.targets) != 1 || plan.len() != 1 {
		return subStringFieldAssignment{}, false
	}
	item := plan.item(0)
	if item.kind != valuePlanSingle {
		return subStringFieldAssignment{}, false
	}
	target := stmt.targets[0]
	if len(target.selectors) != 1 || target.selectors[0].field == "" {
		return subStringFieldAssignment{}, false
	}
	ref, ok := c.resolveAssignTarget(target)
	if !ok || ref.kind != variableLocal {
		return subStringFieldAssignment{}, false
	}
	operand, ok := fieldSubAssignmentOperand(stmt.values[item.source], target)
	if !ok {
		return subStringFieldAssignment{}, false
	}
	return subStringFieldAssignment{
		table:   ref.index,
		field:   target.selectors[0].field,
		operand: operand,
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

func (c *compiler) compileAddStringFieldAssignment(addField addStringFieldAssignment) error {
	operand := c.allocTemp()
	if err := c.compileExpressionTo(addField.operand, operand); err != nil {
		c.releaseTemp(operand)
		return err
	}
	key := c.addStringConstant(addField.field)
	fieldValue := c.allocTemp()
	c.emit(instruction{op: opGetStringField, a: fieldValue, b: addField.table, c: key})
	c.emit(instruction{op: opAdd, a: fieldValue, b: fieldValue, c: operand})
	c.emit(instruction{op: opSetStringField, a: addField.table, b: key, c: fieldValue})
	c.releaseTemp(fieldValue)
	c.releaseTemp(operand)
	return nil
}

func (c *compiler) compileSubStringFieldAssignment(subField subStringFieldAssignment) error {
	operand := c.allocTemp()
	if err := c.compileExpressionTo(subField.operand, operand); err != nil {
		c.releaseTemp(operand)
		return err
	}
	key := c.addStringConstant(subField.field)
	fieldValue := c.allocTemp()
	c.emit(instruction{op: opGetStringField, a: fieldValue, b: subField.table, c: key})
	c.emit(instruction{op: opSub, a: fieldValue, b: fieldValue, c: operand})
	c.emit(instruction{op: opSetStringField, a: subField.table, b: key, c: fieldValue})
	c.releaseTemp(fieldValue)
	c.releaseTemp(operand)
	return nil
}

func (c *compiler) compileAssignTargetFromRegister(target assignTarget, value int) error {
	if len(target.selectors) == 0 {
		ref, bound, err := c.resolveBoundUse(target.id)
		if err != nil {
			return err
		}
		if !bound {
			name := c.addStringConstant(target.name)
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
			key := c.addStringConstant(last.field)
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
			firstKey := c.addStringConstant(first.field)
			secondKey := c.addStringConstant(second.field)
			table := c.allocTemp()
			c.emit(instruction{op: opGetStringField, a: table, b: ref.index, c: firstKey})
			c.emit(instruction{op: opSetStringField, a: table, b: secondKey, c: value})
			c.releaseTemp(table)
			return nil
		}
		if first.field != "" && second.index != nil {
			firstKey := c.addStringConstant(first.field)
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
		key := c.addStringConstant(last.field)
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
	branch := stmt
	if !c.suppressTagChains {
		if ok, err := c.compileStringTagElseIfChain(branch); ok || err != nil {
			return err
		}
	}
	return c.compileIfDefault(branch)
}

func (c *compiler) compileIfSlowPath(branch ifStatement) error {
	previous := c.suppressTagChains
	c.suppressTagChains = true
	defer func() {
		c.suppressTagChains = previous
	}()
	return c.compileIfDefault(branch)
}

func (c *compiler) compileIfDefault(branch ifStatement) error {
	jumpIfFalse, ok, err := c.compileConditionJumpIfFalse(branch.condition)
	if err != nil {
		return err
	}
	if !ok {
		condition, err := c.compileTempExpression(branch.condition)
		if err != nil {
			return err
		}
		jumpIfFalse = c.emitJumpIfFalse(condition)
		c.releaseTemp(condition)
	}

	if err := c.compileStatements(branch.thenStatements); err != nil {
		return err
	}

	jumpEnd := c.emitJump()

	elseStart := c.pc()
	c.patchJump(jumpIfFalse, elseStart)

	if len(branch.elseStatements) > 0 {
		if err := c.compileStatements(branch.elseStatements); err != nil {
			return err
		}
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
	arms     []stringTagElseIfArm
	elseBody []statement
}

func (c *compiler) compileStringTagElseIfChain(branch ifStatement) (bool, error) {
	chain, ok := c.stringTagElseIfChain(branch)
	if !ok {
		return false, nil
	}

	metatableJump := c.emit(instruction{op: opJumpIfTableHasMetatable, a: chain.table})
	tag := c.allocTemp()
	field := c.addStringConstant(chain.field)
	c.emit(instruction{op: opGetStringField, a: tag, b: chain.table, c: field})

	endJumps := make([]int, 0, len(chain.arms)+1)
	for _, arm := range chain.arms {
		value := c.addStringConstant(arm.value)
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
				guardCondition, err := c.compileTempExpression(expression{
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
	}
	endJumps = append(endJumps, c.emitJump())

	slowStart := c.pc()
	c.patchJump(metatableJump, slowStart)
	c.releaseTemp(tag)
	if err := c.compileIfSlowPath(branch); err != nil {
		return true, err
	}
	end := c.pc()
	for _, jump := range endJumps {
		c.patchJump(jump, end)
	}
	return true, nil
}

func (c *compiler) stringTagElseIfChain(branch ifStatement) (stringTagElseIfChain, bool) {
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
		arms: []stringTagElseIfArm{{
			value:  firstValue,
			guards: firstGuards,
			body:   branch.thenStatements,
		}},
	}
	elseBody := branch.elseStatements
	for len(elseBody) == 1 && elseBody[0].ifStmt != nil {
		nextBranch := *elseBody[0].ifStmt
		condition, guards, ok := c.stringTagArmCondition(nextBranch.condition)
		if !ok ||
			condition.table != chain.table ||
			condition.field != chain.field {
			return stringTagElseIfChain{}, false
		}
		conditionValue, ok := condition.value.String()
		if !ok {
			return stringTagElseIfChain{}, false
		}
		chain.arms = append(chain.arms, stringTagElseIfArm{
			value:  conditionValue,
			guards: guards,
			body:   nextBranch.thenStatements,
		})
		elseBody = nextBranch.elseStatements
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
	jumpIfFalse, ok, err := c.compileConditionJumpIfFalse(expr.condition)
	if err != nil {
		return err
	}
	if !ok {
		condition, err := c.compileTempExpression(expr.condition)
		if err != nil {
			return err
		}
		jumpIfFalse = c.emitJumpIfFalse(condition)
		c.releaseTemp(condition)
	}

	if err := c.compileExpressionTo(expr.thenValue, target); err != nil {
		return err
	}

	jumpEnd := c.emitJump()

	c.patchJump(jumpIfFalse, c.pc())
	if err := c.compileExpressionTo(expr.elseValue, target); err != nil {
		return err
	}

	c.patchJump(jumpEnd, c.pc())
	return nil
}

func (c *compiler) compileConditionJumpIfFalse(expr expression) (int, bool, error) {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) {
		return 0, false, nil
	}
	if jump, ok, err := c.compileStringFieldEqualityJumpIfFalse(expr); ok || err != nil {
		return jump, ok, err
	}
	if jump, ok, err := c.compileStringFieldNumericJumpIfFalse(expr); ok || err != nil {
		return jump, ok, err
	}
	if jump, ok, err := c.compileRegisterStringFieldNumericJumpIfFalse(expr); ok || err != nil {
		return jump, ok, err
	}
	if jump, ok, err := c.compileStringFieldTruthyJumpIfFalse(expr); ok || err != nil {
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
		modResult := c.allocTemp()
		c.emit(instruction{op: opModK, a: modResult, b: condition.source.index, c: mod})
		jump := c.emit(instruction{op: opJumpIfNotEqualK, a: modResult, b: value})
		c.releaseTemp(modResult)
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
	rightField string
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
			if plan.field != "" {
				falseJumps = append(falseJumps, c.emitAndChainStringFieldNumericBranch(plan, constant))
			} else {
				falseJumps = append(falseJumps, c.emit(instruction{
					op: plan.op,
					a:  plan.a,
					b:  constant,
				}))
			}
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
	if comparison.right == nil {
		return andChainBranchPlan{}, false
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
	c.emitLocalStringFieldLoad(left, plan.a, plan.field)
	right := c.allocTemp()
	c.emitLocalStringFieldLoad(right, plan.b, plan.rightField)
	jump := c.emit(instruction{op: plan.op, a: left, b: right})
	c.releaseTemp(right)
	c.releaseTemp(left)
	return jump
}

func (c *compiler) emitAndChainStringFieldNumericBranch(plan andChainBranchPlan, constant int) int {
	value := c.allocTemp()
	c.emitLocalStringFieldLoad(value, plan.a, plan.field)
	jump := c.emit(instruction{op: plan.op, a: value, b: constant})
	c.releaseTemp(value)
	return jump
}

func (c *compiler) emitLocalStringFieldLoad(target int, table int, field string) {
	key := c.addStringConstant(field)
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
		op = opJumpIfNotGreaterK
	case comparisonLessEqual:
		op = opJumpIfGreaterK
	default:
		return andChainBranchPlan{}, false
	}
	return andChainBranchPlan{
		op:       op,
		a:        table.index,
		constant: right,
		field:    field,
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
		rightField: rightField,
	}, true
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
	field := c.addStringConstant(condition.field)
	value := c.addConstant(condition.value)
	loaded := c.allocTemp()
	c.emit(instruction{op: opGetStringField, a: loaded, b: condition.table, c: field})
	jump := c.emit(instruction{op: opJumpIfNotEqualK, a: loaded, b: value})
	c.releaseTemp(loaded)
	return jump
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
	return stringFieldEqualityCondition{
		table: table.index,
		field: field,
		value: value,
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
	if term.lit == nil || valueKind(*term.lit) == NilKind {
		return NilValue(), false
	}
	switch valueKind(*term.lit) {
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
	fieldConstant := c.addStringConstant(field)
	valueConstant := c.addConstant(NumberValue(right))
	switch comparison.op {
	case comparisonGreater:
		loaded := c.allocTemp()
		c.emit(instruction{op: opGetStringField, a: loaded, b: table.index, c: fieldConstant})
		jump := c.emit(instruction{op: opJumpIfNotGreaterK, a: loaded, b: valueConstant})
		c.releaseTemp(loaded)
		return jump, true, nil
	case comparisonLessEqual:
		loaded := c.allocTemp()
		c.emit(instruction{op: opGetStringField, a: loaded, b: table.index, c: fieldConstant})
		jump := c.emit(instruction{op: opJumpIfGreaterK, a: loaded, b: valueConstant})
		c.releaseTemp(loaded)
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
	fieldConstant := c.addStringConstant(field)
	loaded := c.allocTemp()
	c.emit(instruction{op: opGetStringField, a: loaded, b: table.index, c: fieldConstant})
	jump := c.emit(instruction{op: opJumpIfNotGreater, a: loaded, b: left})
	c.releaseTemp(loaded)
	releaseLeft()
	return jump, true, nil
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
	value := c.allocTemp()
	fieldConstant := c.addStringConstant(field)
	c.emit(instruction{op: opGetStringField, a: value, b: table.index, c: fieldConstant})
	jump := c.emitJumpIfFalse(value)
	c.releaseTemp(value)
	return jump, true, nil
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
	ref, ok := c.resolveBoundUseNoError(term.id)
	return ref, ok && ref.kind == variableLocal
}

func (c *compiler) expressionLocalRef(expr expression) (variableRef, bool) {
	if c.options.optimizations.enabled(optimizationHIRSimplify) {
		if _, ok := foldConstantExpression(expr); ok {
			return variableRef{}, false
		}
	}
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
	conditionStart := c.pc()

	jumpIfFalse, ok, err := c.compileConditionJumpIfFalse(stmt.condition)
	if err != nil {
		return err
	}
	if !ok {
		condition, err := c.compileTempExpression(stmt.condition)
		if err != nil {
			return err
		}
		jumpIfFalse = c.emitJumpIfFalse(condition)
		c.releaseTemp(condition)
	}

	c.loops = append(c.loops, loopContext{continueTarget: conditionStart})
	if err := c.compileStatements(stmt.statements); err != nil {
		return err
	}
	loop := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]

	c.emit(instruction{op: opJump, b: conditionStart})
	c.patchJump(jumpIfFalse, c.pc())
	for _, jump := range loop.breakJumps {
		c.patchJump(jump, c.pc())
	}
	return nil
}

func (c *compiler) compileFor(stmt forStatement) error {
	loopVar := c.allocReg()
	limit := c.allocReg()
	step := c.allocReg()

	if err := c.compileExpressionTo(stmt.start, loopVar); err != nil {
		return err
	}
	if err := c.compileExpressionTo(stmt.limit, limit); err != nil {
		return err
	}
	if stmt.step != nil {
		if err := c.compileExpressionTo(*stmt.step, step); err != nil {
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

	if err := c.assignDefinition(stmt.nameID, symbolLocal, loopVar); err != nil {
		return err
	}
	c.loops = append(c.loops, loopContext{continueTarget: -1})
	if err := c.compileStatements(stmt.statements); err != nil {
		return err
	}
	loop := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]

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
	if len(stmt.names) == 0 {
		return fmt.Errorf("compile: generic for has no names")
	}

	generator := c.allocReg()
	state := c.allocReg()
	control := c.allocReg()
	targets := []int{generator, state, control}
	if err := c.compileExpressionListTo(stmt.values, targets); err != nil {
		return err
	}
	if len(stmt.values) == 1 {
		c.emit(instruction{op: opPrepareIter, a: generator, b: state, c: control})
	}

	resultStart := control
	c.reserveRegistersThrough(resultStart + 4)
	c.reserveRegistersThrough(resultStart + len(stmt.names))
	c.claimRegisterRange(resultStart, resultStart+len(stmt.names))
	loopStart := c.pc()
	var jumpExit int
	if len(stmt.names) == 2 {
		jumpExit = c.emit(instruction{op: opArrayNextJump2, a: resultStart, b: generator, c: state})
	} else {
		nilReg := c.allocReg()
		c.compileNilTo(nilReg)
		condition := c.allocReg()
		c.emit(instruction{op: opArrayNext, a: resultStart, b: generator, c: state, d: len(stmt.names)})
		c.emit(instruction{op: opMove, a: control, b: resultStart})
		c.emit(instruction{op: opNotEqual, a: condition, b: resultStart, c: nilReg})
		jumpExit = c.emitJumpIfFalse(condition)
	}

	for i := range stmt.names {
		register := resultStart + i
		if err := c.assignDefinition(syntaxNameID(stmt.nameID, i), symbolLocal, register); err != nil {
			return err
		}
	}
	c.loops = append(c.loops, loopContext{continueTarget: loopStart})
	if err := c.compileStatements(stmt.statements); err != nil {
		return err
	}
	loop := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]

	c.emit(instruction{op: opJump, b: loopStart})
	exit := c.pc()
	c.patchJump(jumpExit, exit)
	for _, jump := range loop.breakJumps {
		c.patchJump(jump, exit)
	}
	return nil
}

func (c *compiler) compileRepeat(stmt repeatStatement) error {
	bodyStart := c.pc()

	c.loops = append(c.loops, loopContext{continueTarget: -1})
	if err := c.compileStatements(stmt.statements); err != nil {
		return err
	}
	loop := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]

	conditionStart := c.pc()
	for _, jump := range loop.continueJumps {
		c.patchJump(jump, conditionStart)
	}

	condition, err := c.compileTempExpression(stmt.condition)
	if err != nil {
		return err
	}
	c.emit(instruction{op: opJumpIfFalse, a: condition, b: bodyStart})
	c.releaseTemp(condition)
	exit := c.pc()
	for _, jump := range loop.breakJumps {
		c.patchJump(jump, exit)
	}
	return nil
}

func (c *compiler) compileBlock(stmt blockStatement) error {
	if err := c.compileStatements(stmt.statements); err != nil {
		return err
	}
	return nil
}

func (c *compiler) compileTableTo(table tableExpression, target int) error {
	arrayCapacity, fieldCapacity := tableCapacity(table)
	c.emit(instruction{op: opNewTable, a: target, b: arrayCapacity, c: fieldCapacity})
	for _, field := range table.fields {
		value := c.allocTemp()
		if err := c.compileExpressionTo(field.value, value); err != nil {
			c.releaseTemp(value)
			return err
		}
		switch {
		case field.key != nil:
			key := c.allocTemp()
			if err := c.compileExpressionTo(*field.key, key); err != nil {
				c.releaseTemp(key)
				c.releaseTemp(value)
				return err
			}
			c.emit(instruction{op: opSetIndex, a: target, b: key, c: value})
			c.releaseTemp(key)
		case field.arrayIndex != 0:
			key := c.addConstant(NumberValue(float64(field.arrayIndex)))
			c.emit(instruction{op: opSetField, a: target, b: key, c: value})
		case field.name != "":
			key := c.addStringConstant(field.name)
			c.emit(instruction{op: opSetStringField, a: target, b: key, c: value})
		default:
			c.releaseTemp(value)
			return fmt.Errorf("compile: table field has no key")
		}
		c.releaseTemp(value)
	}
	return nil
}

func tableCapacity(table tableExpression) (int, int) {
	arrayCapacity := 0
	fieldCapacity := 0
	for _, field := range table.fields {
		switch {
		case field.arrayIndex != 0:
			if field.arrayIndex > arrayCapacity {
				arrayCapacity = field.arrayIndex
			}
		case field.name != "" || field.key != nil:
			fieldCapacity++
		}
	}
	return arrayCapacity, fieldCapacity
}

func (c *compiler) compileGlobalNameTo(name string, target int) {
	constant := c.addStringConstant(name)
	c.emit(instruction{op: opLoadGlobal, a: target, b: constant})
}

func (c *compiler) compileNamedTermTo(term term, target int) error {
	ref, bound, err := c.resolveBoundUse(term.id)
	if err != nil {
		return err
	}
	if bound {
		return c.compileVariableRefTo(ref, target)
	}
	c.compileGlobalNameTo(term.name, target)
	return nil
}

func (c *compiler) termLocalRef(term term) (variableRef, bool) {
	if !isNamedTerm(term) {
		return variableRef{}, false
	}
	if ref, ok := c.resolveBoundUseNoError(term.id); ok && ref.kind == variableLocal {
		return ref, true
	}
	return variableRef{}, false
}

func (c *compiler) compileAssignTargetBaseTo(target assignTarget, register int) error {
	ref, bound, err := c.resolveBoundUse(target.id)
	if err != nil {
		return err
	}
	if bound {
		return c.compileVariableRefTo(ref, register)
	}
	c.compileGlobalNameTo(target.name, register)
	return nil
}

func (c *compiler) resolveAssignTarget(target assignTarget) (variableRef, bool) {
	return c.resolveBoundUseNoError(target.id)
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

func (c *compiler) resolveSymbol(symbolID int) (variableRef, bool) {
	if symbolID < 0 || symbolID >= len(c.bind.symbols) {
		return variableRef{}, false
	}
	if register, ok := denseSymbolSlot(c.symbolRegisters, symbolID); ok {
		return variableRef{kind: variableLocal, index: register}, true
	}
	upvalue, ok := c.resolveSymbolUpvalue(symbolID)
	if !ok {
		return variableRef{}, false
	}
	return variableRef{kind: variableUpvalue, index: upvalue}, true
}

func (c *compiler) resolveSymbolUpvalue(symbolID int) (int, bool) {
	if upvalue, ok := denseSymbolSlot(c.upvaluesByID, symbolID); ok {
		return upvalue, true
	}
	if c.parent == nil {
		return 0, false
	}

	if register, ok := denseSymbolSlot(c.parent.symbolRegisters, symbolID); ok {
		return c.addSymbolUpvalue(symbolID, upvalueDesc{local: true, index: register, copy: c.canCopyParentLocalUpvalue(symbolID)}), true
	}
	parentUpvalue, ok := c.parent.resolveSymbolUpvalue(symbolID)
	if !ok {
		return 0, false
	}
	return c.addSymbolUpvalue(symbolID, upvalueDesc{local: false, index: parentUpvalue}), true
}

func (c *compiler) addSymbolUpvalue(symbolID int, desc upvalueDesc) int {
	if len(c.upvaluesByID) < len(c.bind.symbols) {
		c.upvaluesByID = newDenseSymbolSlots(len(c.bind.symbols))
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

func (c *compiler) claimSymbol(node syntaxID, kind symbolKind) (boundSymbol, error) {
	symbol, ok := c.bind.definition(node)
	if !ok {
		return boundSymbol{}, fmt.Errorf("compile: missing binding definition for node %d", node)
	}
	if symbol.kind != kind {
		return boundSymbol{}, fmt.Errorf("compile: binding definition for node %d is %s, want %s", node, symbol.kind, kind)
	}
	return symbol, nil
}

func (c *compiler) assignDefinition(node syntaxID, kind symbolKind, register int) error {
	symbol, err := c.claimSymbol(node, kind)
	if err != nil {
		return err
	}
	return c.assignSymbolRegister(symbol.id, register)
}

func (c *compiler) assignSymbolRegister(symbolID int, register int) error {
	if symbolID < 0 || symbolID >= len(c.symbolRegisters) {
		return fmt.Errorf("compile: invalid binding symbol %d for register %d", symbolID, register)
	}
	c.symbolRegisters[symbolID] = register
	c.localRegisters.add(register)
	return nil
}

// resolveBoundUse is the strict emitter seam for identifier binding. A valid
// global is deliberately distinct from an unvisited node: only the former is
// allowed to fall through to a host/global load.
func (c *compiler) resolveBoundUse(node syntaxID) (variableRef, bool, error) {
	classification := c.bind.useClassification(node)
	switch {
	case classification == boundUseGlobal:
		return variableRef{}, false, nil
	case classification == boundUseUnvisited:
		return variableRef{}, false, fmt.Errorf("compile: missing binding fact for node %d", node)
	case classification < 0:
		return variableRef{}, false, fmt.Errorf("compile: invalid binding classification %d for node %d", classification, node)
	}
	ref, ok := c.resolveSymbol(int(classification))
	if !ok {
		return variableRef{}, false, fmt.Errorf("compile: missing bound symbol %d for node %d", classification, node)
	}
	return ref, true, nil
}

func (c *compiler) resolveBoundUseNoError(node syntaxID) (variableRef, bool) {
	ref, bound, err := c.resolveBoundUse(node)
	if err != nil || !bound {
		return variableRef{}, false
	}
	return ref, true
}

func (c *compiler) compileCallTo(call callExpression, target int) error {
	return c.compileCallToResults(call, target, 1)
}

func (c *compiler) compileCallToResults(call callExpression, target int, resultCount int) error {
	plan := planCall(call)
	return c.compilePlannedCallToResults(plan, call.args, target, resultCount)
}

func (c *compiler) compilePlannedCallToResults(plan callPlan, args []expression, target int, resultCount int) error {
	if c.callNeedsScratch(target, resultCount) {
		scratch := c.nextReg
		if err := c.compilePlannedCallToResultsDirect(plan, args, scratch, resultCount); err != nil {
			return err
		}
		for i := 0; i < resultCount; i++ {
			c.emit(instruction{op: opMove, a: target + i, b: scratch + i})
		}
		c.claimRegisterRange(target, target+resultCount)
		return nil
	}
	return c.compilePlannedCallToResultsDirect(plan, args, target, resultCount)
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
	return c.localRegisters.contains(register)
}

func (c *compiler) compilePlannedCallToResultsDirect(lowered callPlan, args []expression, target int, resultCount int) error {
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
	return c.compilePlannedCallToResultsGeneric(lowered, args, target, resultCount)
}

type methodOneResultCall struct {
	receiver int
	field    string
}

func (c *compiler) methodOneResultCall(lowered callPlan, resultCount int) (methodOneResultCall, bool) {
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
	for i := range lowered.args.len() {
		if lowered.args.item(i).kind != valuePlanSingle {
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
	lowered callPlan,
	args []expression,
	target int,
) error {
	span := len(args) + 2
	c.reserveRegistersThrough(target + span)
	for i := range lowered.args.len() {
		item := lowered.args.item(i)
		if err := c.compileExpressionTo(args[item.source], target+2+i); err != nil {
			return err
		}
	}
	c.claimRegister(target)
	key := c.addStringConstant(method.field)
	c.emit(instruction{op: opCallMethodOne, a: target, b: method.receiver, c: key, d: len(args)})
	return nil
}

func (c *compiler) selectVarargCountCall(lowered callPlan, args []expression, resultCount int) bool {
	if resultCount == 0 || lowered.receiver != nil || !c.variadic {
		return false
	}
	if !c.isUnboundGlobalName(lowered.target, "select") {
		return false
	}
	if len(args) != 2 || lowered.args.len() != 2 {
		return false
	}
	if marker, ok := expressionStringLiteral(args[0]); !ok || marker != "#" {
		return false
	}
	if _, ok := expressionSingleVararg(args[1]); !ok {
		return false
	}
	return lowered.args.item(0).kind == valuePlanSingle &&
		lowered.args.item(1).kind == valuePlanExpanded
}

func (c *compiler) compileSelectVarargCountToResults(target int, resultCount int) error {
	c.reserveRegistersThrough(target + 1)
	c.claimRegister(target)
	c.emit(instruction{op: opFastCall, a: target, b: int(nativeFuncSelect), c: 0, d: resultCount})
	return nil
}

func (c *compiler) rawLenIntrinsicCall(lowered callPlan) bool {
	return c.options.optimizations.enabled(optimizationBytecodePeephole) &&
		lowered.receiver == nil &&
		c.isUnboundGlobalName(lowered.target, "rawlen")
}

func (c *compiler) isUnboundGlobalName(term term, name string) bool {
	if !isNamedTerm(term) || term.name != name {
		return false
	}
	return c.bind.useClassification(term.id) == boundUseGlobal
}

func expressionStringLiteral(expr expression) (string, bool) {
	value, ok := expressionSingleTerm(expr)
	if !ok || value.lit == nil {
		return "", false
	}
	return value.lit.String()
}

func (c *compiler) upvalueOneResultCall(lowered callPlan, resultCount int) (int, bool) {
	if resultCount != 1 || lowered.receiver != nil {
		return 0, false
	}
	target := lowered.target
	if !isNamedTerm(target) || len(target.selectors) != 0 {
		return 0, false
	}
	for i := range lowered.args.len() {
		if lowered.args.item(i).kind != valuePlanSingle {
			return 0, false
		}
	}
	ref, ok := c.resolveBoundUseNoError(target.id)
	return ref.index, ok && ref.kind == variableUpvalue
}

func (c *compiler) selfUpvalueOneResultCall(lowered callPlan, resultCount int) (int, bool) {
	if c.selfFunctionSymbol < 0 || resultCount != 1 || lowered.receiver != nil {
		return 0, false
	}
	target := lowered.target
	if !isNamedTerm(target) || len(target.selectors) != 0 {
		return 0, false
	}
	for i := range lowered.args.len() {
		if lowered.args.item(i).kind != valuePlanSingle {
			return 0, false
		}
	}
	classification := c.bind.useClassification(target.id)
	if classification != boundUseClassification(c.selfFunctionSymbol) {
		return 0, false
	}
	ref, ok := c.resolveSymbol(int(classification))
	return ref.index, ok && ref.kind == variableUpvalue
}

func (c *compiler) localOneResultCall(lowered callPlan, resultCount int) (int, bool) {
	if resultCount != 1 || lowered.receiver != nil {
		return 0, false
	}
	target := lowered.target
	if !isNamedTerm(target) || len(target.selectors) != 0 {
		return 0, false
	}
	for i := range lowered.args.len() {
		if lowered.args.item(i).kind != valuePlanSingle {
			return 0, false
		}
	}
	ref, ok := c.resolveBoundUseNoError(target.id)
	return ref.index, ok && ref.kind == variableLocal
}

func (c *compiler) compileLocalOneResultCallToResults(local int, lowered callPlan, args []expression, target int) error {
	span := len(args)
	if span <= 0 {
		span = 1
	}
	c.reserveRegistersThrough(target + span)
	for i := range lowered.args.len() {
		item := lowered.args.item(i)
		if err := c.compileExpressionTo(args[item.source], target+i); err != nil {
			return err
		}
	}
	c.claimRegister(target)
	c.emit(instruction{op: opCallLocalOne, a: target, b: local, c: target, d: len(args)})
	return nil
}

func (c *compiler) compileUpvalueOneResultCallToResults(upvalue int, lowered callPlan, args []expression, target int) error {
	span := len(args)
	if span <= 0 {
		span = 1
	}
	c.reserveRegistersThrough(target + span)
	for i := range lowered.args.len() {
		item := lowered.args.item(i)
		if err := c.compileExpressionTo(args[item.source], target+i); err != nil {
			return err
		}
	}
	c.claimRegister(target)
	c.emit(instruction{op: opCallUpvalueOne, a: target, b: upvalue, c: target, d: len(args)})
	return nil
}

func (c *compiler) compileSelfUpvalueOneResultCallToResults(upvalue int, lowered callPlan, args []expression, target int) error {
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
	for i := range lowered.args.len() {
		item := lowered.args.item(i)
		if err := c.compileExpressionTo(args[item.source], target+i); err != nil {
			return err
		}
	}
	c.claimRegister(target)
	c.emit(instruction{op: opCallUpvalueOne, a: target, b: upvalue, c: target, d: len(args)})
	return nil
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

func (c *compiler) tableIntrinsicCall(lowered callPlan) (nativeFuncID, bool) {
	return c.baseFieldIntrinsicCall(lowered, "table")
}

func (c *compiler) coroutineIntrinsicCall(lowered callPlan) (nativeFuncID, bool) {
	return c.baseFieldIntrinsicCall(lowered, "coroutine")
}

func (c *compiler) mathIntrinsicCall(lowered callPlan) (nativeFuncID, bool) {
	return c.baseFieldIntrinsicCall(lowered, "math")
}

func (c *compiler) baseFieldIntrinsicCall(lowered callPlan, globalName string) (nativeFuncID, bool) {
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

func (c *compiler) isUnboundBaseField(term term, name string) bool {
	base := term
	base.selectors = nil
	if !isNamedTerm(base) || base.name != name ||
		len(term.selectors) != 1 ||
		term.selectors[0].field == "" ||
		term.selectors[0].index != nil {
		return false
	}
	return c.bind.useClassification(term.id) == boundUseGlobal
}

func (c *compiler) compileBaseIntrinsicCallToResults(
	nativeID nativeFuncID,
	lowered callPlan,
	args []expression,
	target int,
	resultCount int,
) error {
	for i := range lowered.args.len() {
		if lowered.args.item(i).kind != valuePlanSingle {
			return c.compilePlannedCallToResultsGeneric(lowered, args, target, resultCount)
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
	for i := range lowered.args.len() {
		item := lowered.args.item(i)
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

func (c *compiler) compilePlannedCallToResultsGeneric(lowered callPlan, args []expression, target int, resultCount int) error {
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
	for i := range lowered.args.len() {
		item := lowered.args.item(i)
		argRegister := firstArg + item.source
		switch item.kind {
		case valuePlanExpanded:
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
		case valuePlanSingle:
			if err := c.compileExpressionTo(args[item.source], argRegister); err != nil {
				return err
			}
			fixedArgCount++
			argCount = fixedArgCount
		default:
			return fmt.Errorf("compile: unknown value plan kind %d", item.kind)
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
