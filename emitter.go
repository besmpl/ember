package ember

import (
	"fmt"
	"sort"
	"strings"
)

type compiler struct {
	bytecodeBuilder
	tree               syntaxTree
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

// Keep the largest one-shot backing allocation below roughly 1.5 MiB on the
// current IR representation. Larger functions continue to grow incrementally.
const maxEstimatedIRCapacity = 1 << 14

// estimatedIRCapacity returns a bounded, overflow-safe hint for a function's
// IR backing storage. Syntax nodes are a useful root estimate; child emitters
// can fall back to their statement count when no cheap node count is exposed.
func estimatedIRCapacity(nodeCount, statementCount int) int {
	if nodeCount <= 0 && statementCount <= 0 {
		return 0
	}
	estimate := statementCount
	if nodeCount > 0 {
		nodeEstimate := nodeCount / 4
		if nodeCount%4 != 0 {
			nodeEstimate++
		}
		if nodeEstimate > estimate {
			estimate = nodeEstimate
		}
	}
	if estimate < 0 || estimate > maxEstimatedIRCapacity {
		return maxEstimatedIRCapacity
	}
	return estimate
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
		bytecodeBuilder:    bytecodeBuilder{ir: make([]bytecodeIRInstruction, 0, estimatedIRCapacity(source.tree.nodeCount(), len(source.tree.statements())))},
		tree:               source.tree,
		bind:               source.bind,
		sourceLines:        newSourceLineMap(source.source.Text),
		symbolRegisters:    newDenseSymbolSlots(len(source.bind.symbols)),
		selfFunctionSymbol: -1,
		options:            options,
		sourceName:         sourceName,
		functionName:       "<module>",
	}
	c.sourceText = source.source.Text

	if err := c.compileStatements(source.tree.statements()); err != nil {
		if c.conversionErr != nil {
			return nil, c.conversionErr
		}
		return nil, err
	}
	if !statementsHaveReturn(source.tree.statements()) {
		c.emit(instruction{op: opReturn})
	}
	if c.conversionErr != nil {
		return nil, c.conversionErr
	}

	c.optimizeFunction(options.optimizations)
	draft, err := c.buildFunctionDraft(nil, 0, false)
	if err != nil {
		return nil, err
	}
	return sealFunctionDraft(draft)
}

func (c *compiler) buildFunctionDraft(upvalues []upvalueDesc, params int, variadic bool) (*functionDraft, error) {
	if c == nil {
		return nil, fmt.Errorf("nil compiler")
	}
	if c.conversionErr != nil {
		return nil, c.conversionErr
	}
	c.shrinkCompiledFrameRegisters(params, variadic)
	if c.conversionErr != nil {
		return nil, c.conversionErr
	}
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
	return draft, nil
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
		remapBytecodeIRRegisterOperands(&c.ir[i], remap)
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

func remapBytecodeIRRegisterOperands(instruction *bytecodeIRInstruction, remap []int) {
	for slot := bytecodeIROperandSlotA; slot <= bytecodeIROperandSlotD; slot++ {
		kind := instruction.operandKind(slot)
		value := instruction.operandValue(slot)
		if kind != bytecodeOperandRegister || value < 0 || value >= len(remap) {
			continue
		}
		instruction.setOperandValue(slot, remap[value])
	}
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
	switch c.tree.statementKind(&stmt) {
	case syntaxStatementLocal:
		return c.compileLocal(*c.tree.local(&stmt))
	case syntaxStatementLocalFunction:
		return c.compileLocalFunction(*c.tree.localFunction(&stmt))
	case syntaxStatementFunctionDeclaration:
		return c.compileFunctionDeclaration(*c.tree.functionDeclaration(&stmt))
	case syntaxStatementAssign:
		return c.compileAssignment(*c.tree.assignment(&stmt))
	case syntaxStatementCall:
		return c.compileCallStatement(c.tree.call(&stmt))
	case syntaxStatementIf:
		return c.compileIf(*c.tree.ifStatement(&stmt))
	case syntaxStatementWhile:
		return c.compileWhile(*c.tree.whileStatement(&stmt))
	case syntaxStatementFor:
		return c.compileFor(*c.tree.forStatement(&stmt))
	case syntaxStatementGenericFor:
		return c.compileGenericFor(*c.tree.genericForStatement(&stmt))
	case syntaxStatementRepeat:
		return c.compileRepeat(*c.tree.repeatStatement(&stmt))
	case syntaxStatementBlock:
		return c.compileBlock(*c.tree.blockStatement(&stmt))
	case syntaxStatementTypeAlias:
		return nil
	case syntaxStatementBreak:
		return c.compileBreak()
	case syntaxStatementContinue:
		return c.compileContinue()
	case syntaxStatementReturn:
		return c.compileReturn(*c.tree.returnStatement(&stmt))
	default:
		return fmt.Errorf("compile: empty statement")
	}
}

func (c *compiler) compileLocal(stmt localStatement) error {
	names := c.tree.localNames(&stmt)
	if len(names) == 0 {
		return fmt.Errorf("compile: local statement has no names")
	}

	first := c.allocReg()
	targets := make([]int, len(names))
	for i := range targets {
		targets[i] = first + i
	}
	c.reserveRegistersThrough(first + len(targets))

	values := c.tree.localValues(&stmt)
	plan := fixedValueListPlan(c.tree, values, len(targets))
	if err := c.compileValueListTo(plan, targets); err != nil {
		return err
	}
	for i := range names {
		if err := c.assignDefinition(syntaxNameID(c.tree.localNameID(&stmt), i), symbolLocal, targets[i]); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileReturn(stmt returnStatement) error {
	values := c.tree.returnValues(&stmt)
	if len(values) == 0 {
		c.emit(instruction{op: opReturn})
		return nil
	}

	plan := openValueListPlan(c.tree, values)
	if plan.len() == 1 && plan.item(0).kind == valuePlanSingle {
		if ref, ok := c.expressionLocalRef(values[0]); ok {
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
			if vararg, ok := expressionSingleVararg(c.tree, values[item.source]); ok {
				if err := c.compileVarargToResults(vararg, target, item.resultCount); err != nil {
					return err
				}
			} else if call, ok := expressionSingleCall(c.tree, values[item.source]); ok {
				if err := c.compileCallToResults(call, target, item.resultCount); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("compile: expanded return value is not a call or vararg")
			}
			c.emit(instruction{op: opReturn, a: first, b: -(i + 1)})
			return nil
		case valuePlanSingle:
			if err := c.compileExpressionTo(values[item.source], target); err != nil {
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

func (c *compiler) compileCallStatement(stmt termID) error {
	call, ok := c.tree.termCall(stmt)
	if !ok {
		return fmt.Errorf("compile: call statement has no call")
	}
	result := c.allocReg()
	args, _ := c.tree.callArgs(call)
	return c.compilePlannedCallToResults(planCall(c.tree, call), args, result, 1)
}

func (c *compiler) compileExpressionListTo(values []expressionID, targets []int) error {
	if len(targets) == 0 {
		return nil
	}
	return c.compileValueListTo(fixedValueListPlan(c.tree, values, len(targets)), targets)
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
			if vararg, ok := expressionSingleVararg(c.tree, plan.values[item.source]); ok {
				return c.compileVarargToResults(vararg, target, item.resultCount)
			}
			if call, ok := expressionSingleCall(c.tree, plan.values[item.source]); ok {
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
	closure := planLocalFunction(c.tree, stmt)
	target := c.allocReg()
	selfFunctionSymbol := -1
	symbol, err := c.claimSymbol(c.tree.localFunctionNameID(&stmt), symbolLocalFunction)
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
	closure := planFunctionDeclaration(c.tree, stmt)

	value := c.allocReg()
	if err := c.compileClosureTo(closure, value); err != nil {
		return err
	}
	return c.compileAssignTargetFromRegister(*c.tree.functionDeclarationTarget(&stmt), value)
}

func (c *compiler) compileFunctionDraft(closure closurePlan, selfFunctionSymbol int) (*functionDraft, error) {
	fn := compiler{
		bytecodeBuilder:    bytecodeBuilder{ir: make([]bytecodeIRInstruction, 0, estimatedIRCapacity(0, len(closure.body)))},
		tree:               c.tree,
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
		if fn.conversionErr != nil {
			return nil, fn.conversionErr
		}
		return nil, err
	}
	if !statementsHaveReturn(closure.body) {
		fn.emit(instruction{op: opReturn})
	}
	if fn.conversionErr != nil {
		return nil, fn.conversionErr
	}

	fn.optimizeFunction(c.options.optimizations)
	return fn.buildFunctionDraft(fn.upvalueDescs, closure.paramCount(), closure.variadic)
}

func (c *compiler) compileTempExpression(expr expressionID) (int, error) {
	target := c.allocTemp()
	if err := c.compileExpressionTo(expr, target); err != nil {
		c.releaseTemp(target)
		return 0, err
	}
	return target, nil
}

func (c *compiler) compileExpressionTo(expr expressionID, target int) error {
	c.claimRegister(target)
	source := expressionRange(c.tree, expr)
	return c.withSourceRange(source, func() error {
		if c.options.optimizations.enabled(optimizationHIRSimplify) {
			if value, ok := foldConstantExpression(c.tree, expr); ok {
				c.emitLoadConst(target, value)
				return nil
			}
		}
		terms, ok := c.tree.expressionTerms(expr)
		if !ok || len(terms) == 0 {
			return fmt.Errorf("compile: empty expression")
		}

		if err := c.compileAndExpressionTo(terms[0], target); err != nil {
			return err
		}

		for _, term := range terms[1:] {
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

func (c *compiler) compileAndExpressionTo(expr andExpressionID, target int) error {
	terms, ok := c.tree.andTerms(expr)
	if !ok || len(terms) == 0 {
		return fmt.Errorf("compile: empty expression")
	}

	if err := c.compileComparisonExpressionTo(terms[0], target); err != nil {
		return err
	}

	for _, term := range terms[1:] {
		jumpEnd := c.emitJumpIfFalse(target)
		if err := c.compileComparisonExpressionTo(term, target); err != nil {
			return err
		}
		c.patchJump(jumpEnd, c.pc())
	}

	return nil
}

func (c *compiler) compileComparisonExpressionTo(expr comparisonExpressionID, target int) error {
	if c.tree.comparisonOperator(expr) == "" {
		return c.compileConcatExpressionTo(c.tree.comparisonLeft(expr), target)
	}

	rightExpr := c.tree.comparisonRight(expr)
	if rightExpr == 0 {
		return fmt.Errorf("compile: missing comparison right operand")
	}

	if err := c.compileConcatExpressionTo(c.tree.comparisonLeft(expr), target); err != nil {
		return err
	}

	right := c.allocTemp()
	if err := c.compileConcatExpressionTo(rightExpr, right); err != nil {
		c.releaseTemp(right)
		return err
	}

	switch c.tree.comparisonOperator(expr) {
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
		return fmt.Errorf("compile: unsupported comparison %q", c.tree.comparisonOperator(expr))
	}

	c.releaseTemp(right)
	return nil
}

func (c *compiler) compileConcatExpressionTo(expr concatExpressionID, target int) error {
	rest, _ := c.tree.concatRest(expr)
	operandCount := 1 + len(rest)
	if operandCount >= 3 && target+1 >= c.nextReg {
		return c.compileConcatChainExpressionTo(expr, target, operandCount)
	}

	if err := c.compileAdditiveExpressionTo(c.tree.concatFirst(expr), target); err != nil {
		return err
	}

	for _, part := range rest {
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

func (c *compiler) compileConcatChainExpressionTo(expr concatExpressionID, target int, operandCount int) error {
	rest, _ := c.tree.concatRest(expr)
	end := target + operandCount
	c.reserveRegistersThrough(end)
	c.claimRegisterRange(target, end)

	if err := c.compileAdditiveExpressionTo(c.tree.concatFirst(expr), target); err != nil {
		return err
	}
	for index, part := range rest {
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

func (c *compiler) compileAdditiveExpressionTo(expr additiveExpressionID, target int) error {
	if err := c.compileMultiplicativeExpressionTo(c.tree.additiveFirst(expr), target); err != nil {
		return err
	}

	parts, _ := c.tree.additiveRest(expr)
	for _, part := range parts {
		if right, ok := foldNumberMultiplicative(c.tree, part.value); ok {
			constant := c.addConstant(NumberValue(right))
			switch part.op {
			case arenaAdditiveAdd:
				c.emit(instruction{op: opAddK, a: target, b: target, c: constant})
				continue
			case arenaAdditiveSubtract:
				c.emit(instruction{op: opSubK, a: target, b: target, c: constant})
				continue
			}
		}
		partValue := part.value
		right := c.allocArithmeticOperandRegister(partValue)
		if err := c.compileMultiplicativeExpressionTo(partValue, right); err != nil {
			c.releaseTemp(right)
			return err
		}
		switch part.op {
		case arenaAdditiveAdd:
			c.emit(instruction{op: opAdd, a: target, b: target, c: right})
		case arenaAdditiveSubtract:
			c.emit(instruction{op: opSub, a: target, b: target, c: right})
		default:
			c.releaseTemp(right)
			return fmt.Errorf("compile: unsupported additive operator %q", part.op)
		}
		c.releaseTemp(right)
	}

	return nil
}

func (c *compiler) allocArithmeticOperandRegister(expr multiplicativeExpressionID) int {
	if _, ok := multiplicativeSingleCall(c.tree, expr); ok {
		return c.allocReg()
	}
	return c.allocTemp()
}

func multiplicativeSingleCall(tree syntaxTree, expr multiplicativeExpressionID) (arenaCallID, bool) {
	rest, _ := tree.multiplicativeRest(expr)
	if len(rest) != 0 {
		return 0, false
	}
	value := tree.multiplicativeFirst(expr)
	call, ok := tree.termCall(value)
	selectors, _ := tree.termSelectors(value)
	if !ok || len(selectors) != 0 {
		return 0, false
	}
	return call, true
}

func (c *compiler) compileMultiplicativeExpressionTo(expr multiplicativeExpressionID, target int) error {
	if err := c.compileTermTo(c.tree.multiplicativeFirst(expr), target); err != nil {
		return err
	}

	parts, _ := c.tree.multiplicativeRest(expr)
	for _, part := range parts {
		partValue := part.value
		if right, ok := foldNumberTerm(c.tree, partValue); ok {
			constant := c.addConstant(NumberValue(right))
			switch part.op {
			case arenaMultiplicativeMultiply:
				c.emit(instruction{op: opMulK, a: target, b: target, c: constant})
				continue
			case arenaMultiplicativeDivide:
				c.emit(instruction{op: opDivK, a: target, b: target, c: constant})
				continue
			case arenaMultiplicativeModulo:
				c.emit(instruction{op: opModK, a: target, b: target, c: constant})
				continue
			case arenaMultiplicativeFloorDiv:
				c.emit(instruction{op: opIDivK, a: target, b: target, c: constant})
				continue
			}
		}
		right := c.allocTemp()
		if err := c.compileTermTo(partValue, right); err != nil {
			c.releaseTemp(right)
			return err
		}
		switch part.op {
		case arenaMultiplicativeMultiply:
			c.emit(instruction{op: opMul, a: target, b: target, c: right})
		case arenaMultiplicativeDivide:
			c.emit(instruction{op: opDiv, a: target, b: target, c: right})
		case arenaMultiplicativeModulo:
			c.emit(instruction{op: opMod, a: target, b: target, c: right})
		case arenaMultiplicativeFloorDiv:
			c.emit(instruction{op: opIDiv, a: target, b: target, c: right})
		default:
			c.releaseTemp(right)
			return fmt.Errorf("compile: unsupported multiplicative operator %q", part.op)
		}
		c.releaseTemp(right)
	}

	return nil
}

func (c *compiler) compileTermTo(term termID, target int) error {
	selectors, _ := c.tree.termSelectors(term)
	if len(selectors) > 0 {
		if ref, ok := c.namedTermLocalRef(term); ok {
			return c.compileSelectorsFromBaseTo(ref.index, selectors, target)
		}
		if isNamedTerm(c.tree, term) {
			if err := c.compileNamedTermTo(term, target); err != nil {
				return err
			}
		} else {
			if err := c.compileTermCoreTo(term, target); err != nil {
				return err
			}
		}
		return c.compileSelectorsTo(selectors, target)
	}
	return c.compileTermCoreTo(term, target)
}

func (c *compiler) compileTermCoreTo(term termID, target int) error {
	if power, ok := c.tree.termPower(term); ok {
		return c.compilePowerTo(power, target)
	}
	if number, ok := c.tree.termNumber(term); ok {
		c.emitLoadConst(target, NumberValue(number))
		return nil
	}
	if literal, ok := c.tree.termLiteral(term); ok {
		c.emitLoadConst(target, literal)
		return nil
	}
	if table, ok := c.tree.termTable(term); ok {
		return c.compileTableTo(table, target)
	}
	if function, ok := c.tree.termFunction(term); ok {
		return c.compileClosureTo(planFunctionExpression(c.tree, function), target)
	}
	if ifExpr, ok := c.tree.termIf(term); ok {
		return c.compileIfExpressionTo(ifExpr, target)
	}
	if call, ok := c.tree.termCall(term); ok {
		return c.compileCallTo(call, target)
	}
	if c.tree.termVararg(term) {
		return c.compileVarargToResults(term, target, 1)
	}
	if child, ok := c.tree.termChild(term); ok {
		switch c.tree.termKind(term) {
		case syntaxTermUnaryNot:
			return c.compileNotTo(child, target)
		case syntaxTermUnaryMinus:
			return c.compileUnaryMinusTo(child, target)
		case syntaxTermUnaryLength:
			return c.compileLengthTo(child, target)
		}
	}
	if group, ok := c.tree.termGroup(term); ok {
		return c.compileExpressionTo(group, target)
	}

	return c.compileNamedTermTo(term, target)
}

func (c *compiler) compilePowerTo(power arenaPowerID, target int) error {
	if err := c.compileTermTo(c.tree.powerBase(power), target); err != nil {
		return err
	}
	exponent := c.allocTemp()
	if err := c.compileTermTo(c.tree.powerExponent(power), exponent); err != nil {
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

func (c *compiler) compileSelectorsTo(selectors []arenaSelector, target int) error {
	for len(selectors) > 0 {
		if len(selectors) >= 2 && c.tree.termSelectorField(selectors[0]) != "" && c.tree.termSelectorField(selectors[1]) != "" {
			firstKey := c.addStringConstant(c.tree.termSelectorField(selectors[0]))
			secondKey := c.addStringConstant(c.tree.termSelectorField(selectors[1]))
			c.emit(instruction{op: opGetStringField, a: target, b: target, c: firstKey})
			c.emit(instruction{op: opGetStringField, a: target, b: target, c: secondKey})
			selectors = selectors[2:]
			continue
		}
		if len(selectors) >= 2 && c.tree.termSelectorField(selectors[0]) != "" && c.tree.termSelectorIndex(selectors[1]) != 0 {
			firstKey := c.addStringConstant(c.tree.termSelectorField(selectors[0]))
			key := c.allocReg()
			if err := c.compileExpressionTo(c.tree.termSelectorIndex(selectors[1]), key); err != nil {
				return err
			}
			c.emit(instruction{op: opGetStringFieldIndex, a: target, b: target, c: firstKey, d: key})
			selectors = selectors[2:]
			continue
		}

		selector := selectors[0]
		if c.tree.termSelectorField(selector) != "" {
			key := c.addStringConstant(c.tree.termSelectorField(selector))
			c.emit(instruction{op: opGetStringField, a: target, b: target, c: key})
			selectors = selectors[1:]
			continue
		}

		key := c.allocReg()
		if err := c.compileExpressionTo(c.tree.termSelectorIndex(selector), key); err != nil {
			return err
		}
		c.emit(instruction{op: opGetIndex, a: target, b: target, c: key})
		selectors = selectors[1:]
	}
	return nil
}

func (c *compiler) compileSelectorsFromBaseTo(base int, selectors []arenaSelector, target int) error {
	if len(selectors) == 0 {
		if target != base {
			c.emit(instruction{op: opMove, a: target, b: base})
		}
		return nil
	}
	first := selectors[0]
	if len(selectors) >= 2 && c.tree.termSelectorField(first) != "" && c.tree.termSelectorField(selectors[1]) != "" {
		firstKey := c.addStringConstant(c.tree.termSelectorField(first))
		secondKey := c.addStringConstant(c.tree.termSelectorField(selectors[1]))
		c.emit(instruction{op: opGetStringField, a: target, b: base, c: firstKey})
		c.emit(instruction{op: opGetStringField, a: target, b: target, c: secondKey})
		return c.compileSelectorsTo(selectors[2:], target)
	}
	if len(selectors) >= 2 && c.tree.termSelectorField(first) != "" && c.tree.termSelectorIndex(selectors[1]) != 0 {
		firstKey := c.addStringConstant(c.tree.termSelectorField(first))
		key := c.allocReg()
		if err := c.compileExpressionTo(c.tree.termSelectorIndex(selectors[1]), key); err != nil {
			return err
		}
		c.emit(instruction{op: opGetStringFieldIndex, a: target, b: base, c: firstKey, d: key})
		return c.compileSelectorsTo(selectors[2:], target)
	}
	if c.tree.termSelectorField(first) != "" {
		key := c.addStringConstant(c.tree.termSelectorField(first))
		c.emit(instruction{op: opGetStringField, a: target, b: base, c: key})
		return c.compileSelectorsTo(selectors[1:], target)
	}
	key := c.allocReg()
	if err := c.compileExpressionTo(c.tree.termSelectorIndex(first), key); err != nil {
		return err
	}
	c.emit(instruction{op: opGetIndex, a: target, b: base, c: key})
	return c.compileSelectorsTo(selectors[1:], target)
}

func isNamedTerm(tree syntaxTree, term termID) bool {
	selectors, _ := tree.termSelectors(term)
	return tree.termName(term) != "" && tree.termKind(term) == syntaxTermName && len(selectors) == 0
}

func (c *compiler) compileCallTargetTo(term termID, target int) error {
	if isNamedTerm(c.tree, term) {
		return c.compileNamedTermTo(term, target)
	}
	return c.compileTermTo(term, target)
}

func (c *compiler) compileNotTo(term termID, target int) error {
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

func (c *compiler) compileUnaryMinusTo(term termID, target int) error {
	if err := c.compileTermTo(term, target); err != nil {
		return err
	}
	c.emit(instruction{op: opNeg, a: target, b: target})
	return nil
}

func (c *compiler) compileLengthTo(term termID, target int) error {
	if err := c.compileTermTo(term, target); err != nil {
		return err
	}
	c.emit(instruction{op: opLen, a: target, b: target})
	return nil
}

func (c *compiler) compileAssignment(stmt assignStatement) error {
	targets := c.tree.assignmentTargets(&stmt)
	values := c.tree.assignmentValues(&stmt)
	if len(targets) == 0 {
		return fmt.Errorf("compile: assignment has no targets")
	}
	plan := fixedValueListPlan(c.tree, values, len(targets))

	if c.canCompileSingleLocalAssignmentInPlace(stmt, plan) {
		target := targets[0]
		ref, _ := c.resolveAssignTarget(target)
		return c.compileExpressionTo(values[plan.item(0).source], ref.index)
	}

	if addField, ok := c.addStringFieldAssignment(stmt, plan); ok {
		return c.compileAddStringFieldAssignment(addField)
	}
	if subField, ok := c.subStringFieldAssignment(stmt, plan); ok {
		return c.compileSubStringFieldAssignment(subField)
	}
	first := c.allocReg()
	registers := make([]int, len(targets))
	for i := range registers {
		registers[i] = first + i
	}
	c.reserveRegistersThrough(first + len(registers))
	if err := c.compileValueListTo(plan, registers); err != nil {
		return err
	}

	for i, target := range targets {
		if err := c.compileAssignTargetFromRegister(target, registers[i]); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) canCompileSingleLocalAssignmentInPlace(stmt assignStatement, plan valueListPlan) bool {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) {
		return false
	}
	targets := c.tree.assignmentTargets(&stmt)
	values := c.tree.assignmentValues(&stmt)
	if len(targets) != 1 || plan.len() != 1 {
		return false
	}
	target := targets[0]
	if len(c.tree.assignTargetSelectors(&target)) != 0 {
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
	return expressionCanAssignToNameInPlace(c.tree, values[item.source], c.tree.assignTargetName(&target))
}

func expressionCanAssignToNameInPlace(tree syntaxTree, expr expressionID, name string) bool {
	if !expressionReferencesName(tree, expr, name) {
		return true
	}
	terms, ok := tree.expressionTerms(expr)
	if !ok || len(terms) == 0 {
		return true
	}
	if !andExpressionCanAssignToNameInPlace(tree, terms[0], name) {
		return false
	}
	for _, term := range terms[1:] {
		if andExpressionReferencesName(tree, term, name) {
			return false
		}
	}
	return true
}

func andExpressionCanAssignToNameInPlace(tree syntaxTree, expr andExpressionID, name string) bool {
	if !andExpressionReferencesName(tree, expr, name) {
		return true
	}
	terms, ok := tree.andTerms(expr)
	if !ok || len(terms) == 0 {
		return true
	}
	if !comparisonExpressionCanAssignToNameInPlace(tree, terms[0], name) {
		return false
	}
	for _, term := range terms[1:] {
		if comparisonExpressionReferencesName(tree, term, name) {
			return false
		}
	}
	return true
}

func comparisonExpressionCanAssignToNameInPlace(tree syntaxTree, expr comparisonExpressionID, name string) bool {
	if !comparisonExpressionReferencesName(tree, expr, name) {
		return true
	}
	if !concatExpressionCanAssignToNameInPlace(tree, tree.comparisonLeft(expr), name) {
		return false
	}
	right := tree.comparisonRight(expr)
	return right == 0 || !concatExpressionReferencesName(tree, right, name)
}

func concatExpressionCanAssignToNameInPlace(tree syntaxTree, expr concatExpressionID, name string) bool {
	if !concatExpressionReferencesName(tree, expr, name) {
		return true
	}
	if !additiveExpressionCanAssignToNameInPlace(tree, tree.concatFirst(expr), name) {
		return false
	}
	parts, _ := tree.concatRest(expr)
	for _, part := range parts {
		if additiveExpressionReferencesName(tree, part, name) {
			return false
		}
	}
	return true
}

func additiveExpressionCanAssignToNameInPlace(tree syntaxTree, expr additiveExpressionID, name string) bool {
	if !additiveExpressionReferencesName(tree, expr, name) {
		return true
	}
	if !multiplicativeExpressionCanAssignToNameInPlace(tree, tree.additiveFirst(expr), name) {
		return false
	}
	parts, _ := tree.additiveRest(expr)
	for _, part := range parts {
		if multiplicativeExpressionReferencesName(tree, tree.additivePartValue(part), name) {
			return false
		}
	}
	return true
}

func multiplicativeExpressionCanAssignToNameInPlace(tree syntaxTree, expr multiplicativeExpressionID, name string) bool {
	if !multiplicativeExpressionReferencesName(tree, expr, name) {
		return true
	}
	if !termCanAssignToNameInPlace(tree, tree.multiplicativeFirst(expr), name) {
		return false
	}
	parts, _ := tree.multiplicativeRest(expr)
	for _, part := range parts {
		if termReferencesName(tree, tree.multiplicativePartValue(part), name) {
			return false
		}
	}
	return true
}

func termCanAssignToNameInPlace(tree syntaxTree, term termID, name string) bool {
	if !termReferencesName(tree, term, name) {
		return true
	}
	if tree.termName(term) == name {
		selectors, _ := tree.termSelectors(term)
		for _, selector := range selectors {
			if index := tree.termSelectorIndex(selector); index != 0 && expressionReferencesName(tree, index, name) {
				return false
			}
		}
		return true
	}
	if child, ok := tree.termChild(term); ok {
		return termCanAssignToNameInPlace(tree, child, name)
	}
	if power, ok := tree.termPower(term); ok {
		return termCanAssignToNameInPlace(tree, tree.powerBase(power), name) &&
			!termReferencesName(tree, tree.powerExponent(power), name)
	}
	if group, ok := tree.termGroup(term); ok {
		return expressionCanAssignToNameInPlace(tree, group, name)
	}
	return false
}

func expressionReferencesName(tree syntaxTree, expr expressionID, name string) bool {
	terms, _ := tree.expressionTerms(expr)
	for _, term := range terms {
		if andExpressionReferencesName(tree, term, name) {
			return true
		}
	}
	return false
}

func andExpressionReferencesName(tree syntaxTree, expr andExpressionID, name string) bool {
	terms, _ := tree.andTerms(expr)
	for _, term := range terms {
		if comparisonExpressionReferencesName(tree, term, name) {
			return true
		}
	}
	return false
}

func comparisonExpressionReferencesName(tree syntaxTree, expr comparisonExpressionID, name string) bool {
	if concatExpressionReferencesName(tree, tree.comparisonLeft(expr), name) {
		return true
	}
	right := tree.comparisonRight(expr)
	return right != 0 && concatExpressionReferencesName(tree, right, name)
}

func concatExpressionReferencesName(tree syntaxTree, expr concatExpressionID, name string) bool {
	if additiveExpressionReferencesName(tree, tree.concatFirst(expr), name) {
		return true
	}
	parts, _ := tree.concatRest(expr)
	for _, part := range parts {
		if additiveExpressionReferencesName(tree, part, name) {
			return true
		}
	}
	return false
}

func additiveExpressionReferencesName(tree syntaxTree, expr additiveExpressionID, name string) bool {
	if multiplicativeExpressionReferencesName(tree, tree.additiveFirst(expr), name) {
		return true
	}
	parts, _ := tree.additiveRest(expr)
	for _, part := range parts {
		if multiplicativeExpressionReferencesName(tree, tree.additivePartValue(part), name) {
			return true
		}
	}
	return false
}

func multiplicativeExpressionReferencesName(tree syntaxTree, expr multiplicativeExpressionID, name string) bool {
	if termReferencesName(tree, tree.multiplicativeFirst(expr), name) {
		return true
	}
	parts, _ := tree.multiplicativeRest(expr)
	for _, part := range parts {
		if termReferencesName(tree, tree.multiplicativePartValue(part), name) {
			return true
		}
	}
	return false
}

func termReferencesName(tree syntaxTree, term termID, name string) bool {
	if tree.termName(term) == name {
		return true
	}
	if table, ok := tree.termTable(term); ok && tableExpressionReferencesName(tree, table, name) {
		return true
	}
	if ifExpr, ok := tree.termIf(term); ok &&
		(expressionReferencesName(tree, tree.ifExpressionCondition(ifExpr), name) ||
			expressionReferencesName(tree, tree.ifExpressionThen(ifExpr), name) ||
			expressionReferencesName(tree, tree.ifExpressionElse(ifExpr), name)) {
		return true
	}
	if call, ok := tree.termCall(term); ok && callExpressionReferencesName(tree, call, name) {
		return true
	}
	if child, ok := tree.termChild(term); ok && termReferencesName(tree, child, name) {
		return true
	}
	if power, ok := tree.termPower(term); ok &&
		(termReferencesName(tree, tree.powerBase(power), name) || termReferencesName(tree, tree.powerExponent(power), name)) {
		return true
	}
	if group, ok := tree.termGroup(term); ok && expressionReferencesName(tree, group, name) {
		return true
	}
	selectors, _ := tree.termSelectors(term)
	for _, selector := range selectors {
		if index := tree.termSelectorIndex(selector); index != 0 && expressionReferencesName(tree, index, name) {
			return true
		}
	}
	return false
}

func tableExpressionReferencesName(tree syntaxTree, table arenaTableID, name string) bool {
	fields, _ := tree.tableFields(table)
	for _, field := range fields {
		if key := tree.tableFieldKey(field); key != 0 && expressionReferencesName(tree, key, name) {
			return true
		}
		if value := tree.tableFieldValue(field); value != 0 && expressionReferencesName(tree, value, name) {
			return true
		}
	}
	return false
}

func callExpressionReferencesName(tree syntaxTree, call arenaCallID, name string) bool {
	if termReferencesName(tree, tree.callTarget(call), name) {
		return true
	}
	if receiver := tree.callReceiver(call); receiver != 0 && termReferencesName(tree, receiver, name) {
		return true
	}
	args, _ := tree.callArgs(call)
	for _, arg := range args {
		if expressionReferencesName(tree, arg, name) {
			return true
		}
	}
	return false
}

type addStringFieldAssignment struct {
	table   int
	field   string
	operand multiplicativeExpressionID
}

type subStringFieldAssignment struct {
	table   int
	field   string
	operand multiplicativeExpressionID
}

func (c *compiler) addStringFieldAssignment(stmt assignStatement, plan valueListPlan) (addStringFieldAssignment, bool) {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) {
		return addStringFieldAssignment{}, false
	}
	targets := c.tree.assignmentTargets(&stmt)
	values := c.tree.assignmentValues(&stmt)
	if len(targets) != 1 || plan.len() != 1 {
		return addStringFieldAssignment{}, false
	}
	item := plan.item(0)
	if item.kind != valuePlanSingle {
		return addStringFieldAssignment{}, false
	}
	target := targets[0]
	selectors := c.tree.assignTargetSelectors(&target)
	if len(selectors) != 1 || c.tree.selectorField(&selectors[0]) == "" {
		return addStringFieldAssignment{}, false
	}
	ref, ok := c.resolveAssignTarget(target)
	if !ok || ref.kind != variableLocal {
		return addStringFieldAssignment{}, false
	}
	operand, ok := fieldAddAssignmentOperand(c.tree, values[item.source], target)
	if !ok {
		return addStringFieldAssignment{}, false
	}
	return addStringFieldAssignment{
		table:   ref.index,
		field:   c.tree.selectorField(&selectors[0]),
		operand: operand,
	}, true
}

func (c *compiler) subStringFieldAssignment(stmt assignStatement, plan valueListPlan) (subStringFieldAssignment, bool) {
	if !c.options.optimizations.enabled(optimizationBytecodePeephole) {
		return subStringFieldAssignment{}, false
	}
	targets := c.tree.assignmentTargets(&stmt)
	values := c.tree.assignmentValues(&stmt)
	if len(targets) != 1 || plan.len() != 1 {
		return subStringFieldAssignment{}, false
	}
	item := plan.item(0)
	if item.kind != valuePlanSingle {
		return subStringFieldAssignment{}, false
	}
	target := targets[0]
	selectors := c.tree.assignTargetSelectors(&target)
	if len(selectors) != 1 || c.tree.selectorField(&selectors[0]) == "" {
		return subStringFieldAssignment{}, false
	}
	ref, ok := c.resolveAssignTarget(target)
	if !ok || ref.kind != variableLocal {
		return subStringFieldAssignment{}, false
	}
	operand, ok := fieldSubAssignmentOperand(c.tree, values[item.source], target)
	if !ok {
		return subStringFieldAssignment{}, false
	}
	return subStringFieldAssignment{
		table:   ref.index,
		field:   c.tree.selectorField(&selectors[0]),
		operand: operand,
	}, true
}

func fieldAddAssignmentOperand(tree syntaxTree, expr expressionID, target assignTarget) (multiplicativeExpressionID, bool) {
	return fieldAddSubAssignmentOperand(tree, expr, target, additiveAdd)
}

func fieldSubAssignmentOperand(tree syntaxTree, expr expressionID, target assignTarget) (multiplicativeExpressionID, bool) {
	return fieldAddSubAssignmentOperand(tree, expr, target, additiveSubtract)
}

func fieldAddSubAssignmentOperand(tree syntaxTree, expr expressionID, target assignTarget, op additiveOperator) (multiplicativeExpressionID, bool) {
	terms, ok := tree.expressionTerms(expr)
	if !ok || len(terms) != 1 {
		return 0, false
	}
	comparisons, ok := tree.andTerms(terms[0])
	if !ok || len(comparisons) != 1 {
		return 0, false
	}
	comparison := comparisons[0]
	left := tree.comparisonLeft(comparison)
	if tree.comparisonOperator(comparison) != "" || tree.comparisonRight(comparison) != 0 {
		return 0, false
	}
	concatRest, ok := tree.concatRest(left)
	if !ok || len(concatRest) != 0 {
		return 0, false
	}
	additive := tree.concatFirst(left)
	parts, ok := tree.additiveRest(additive)
	if !ok || len(parts) != 1 || tree.additivePartOperator(parts[0]) != op {
		return 0, false
	}
	if !multiplicativeMatchesAssignTarget(tree, tree.additiveFirst(additive), target) {
		return 0, false
	}
	operand := tree.additivePartValue(parts[0])
	if !multiplicativeIsSideEffectFreeSingleValue(tree, operand) {
		return 0, false
	}
	return operand, true
}

func multiplicativeMatchesAssignTarget(tree syntaxTree, expr multiplicativeExpressionID, target assignTarget) bool {
	rest, _ := tree.multiplicativeRest(expr)
	if len(rest) != 0 {
		return false
	}
	value := termWithoutCastsAndGroups(tree, tree.multiplicativeFirst(expr))
	selectors, _ := tree.termSelectors(value)
	targetSelectors := tree.assignTargetSelectors(&target)
	if tree.termName(value) != tree.assignTargetName(&target) || len(selectors) != len(targetSelectors) {
		return false
	}
	for i, selector := range selectors {
		targetSelector := targetSelectors[i]
		if tree.termSelectorField(selector) != tree.selectorField(&targetSelector) || tree.termSelectorIndex(selector) != 0 || tree.selectorIndex(&targetSelector) != 0 {
			return false
		}
	}
	return true
}

func multiplicativeIsSideEffectFreeSingleValue(tree syntaxTree, expr multiplicativeExpressionID) bool {
	rest, _ := tree.multiplicativeRest(expr)
	if len(rest) != 0 {
		return false
	}
	value := termWithoutCastsAndGroups(tree, tree.multiplicativeFirst(expr))
	selectors, _ := tree.termSelectors(value)
	if tree.termName(value) != "" && len(selectors) == 0 {
		return true
	}
	_, number := tree.termNumber(value)
	_, literal := tree.termLiteral(value)
	return number || literal
}

func (c *compiler) compileAddStringFieldAssignment(addField addStringFieldAssignment) error {
	operand := c.allocTemp()
	if err := c.compileMultiplicativeExpressionTo(addField.operand, operand); err != nil {
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
	if err := c.compileMultiplicativeExpressionTo(subField.operand, operand); err != nil {
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
	selectors := c.tree.assignTargetSelectors(&target)
	if len(selectors) == 0 {
		ref, bound, err := c.resolveBoundUse(c.tree.assignTargetID(&target))
		if err != nil {
			return err
		}
		if !bound {
			name := c.addStringConstant(c.tree.assignTargetName(&target))
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

	if ref, ok := c.resolveAssignTarget(target); ok && ref.kind == variableLocal && len(selectors) == 1 {
		last := selectors[0]
		if c.tree.selectorField(&last) != "" {
			key := c.addStringConstant(c.tree.selectorField(&last))
			c.emit(instruction{op: opSetStringField, a: ref.index, b: key, c: value})
			return nil
		}
		key := c.allocReg()
		if err := c.compileExpressionTo(c.tree.selectorIndex(&last), key); err != nil {
			return err
		}
		c.emit(instruction{op: opSetIndex, a: ref.index, b: key, c: value})
		return nil
	}

	if ref, ok := c.resolveAssignTarget(target); ok && ref.kind == variableLocal && len(selectors) == 2 {
		first := selectors[0]
		second := selectors[1]
		if c.tree.selectorField(&first) != "" && c.tree.selectorField(&second) != "" {
			firstKey := c.addStringConstant(c.tree.selectorField(&first))
			secondKey := c.addStringConstant(c.tree.selectorField(&second))
			table := c.allocTemp()
			c.emit(instruction{op: opGetStringField, a: table, b: ref.index, c: firstKey})
			c.emit(instruction{op: opSetStringField, a: table, b: secondKey, c: value})
			c.releaseTemp(table)
			return nil
		}
		if c.tree.selectorField(&first) != "" && c.tree.selectorIndex(&second) != 0 {
			firstKey := c.addStringConstant(c.tree.selectorField(&first))
			key := c.allocReg()
			if err := c.compileExpressionTo(c.tree.selectorIndex(&second), key); err != nil {
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

	receivers := selectors[:len(selectors)-1]
	if err := c.compileTargetSelectorsTo(receivers, table); err != nil {
		return err
	}

	last := selectors[len(selectors)-1]
	if c.tree.selectorField(&last) != "" {
		key := c.addStringConstant(c.tree.selectorField(&last))
		c.emit(instruction{op: opSetStringField, a: table, b: key, c: value})
		return nil
	}

	key := c.allocReg()
	if err := c.compileExpressionTo(c.tree.selectorIndex(&last), key); err != nil {
		return err
	}
	c.emit(instruction{op: opSetIndex, a: table, b: key, c: value})
	return nil
}

func (c *compiler) compileTargetSelectorsTo(selectors []selector, target int) error {
	for len(selectors) > 0 {
		if len(selectors) >= 2 && c.tree.selectorField(&selectors[0]) != "" && c.tree.selectorField(&selectors[1]) != "" {
			firstKey := c.addStringConstant(c.tree.selectorField(&selectors[0]))
			secondKey := c.addStringConstant(c.tree.selectorField(&selectors[1]))
			c.emit(instruction{op: opGetStringField, a: target, b: target, c: firstKey})
			c.emit(instruction{op: opGetStringField, a: target, b: target, c: secondKey})
			selectors = selectors[2:]
			continue
		}
		if len(selectors) >= 2 && c.tree.selectorField(&selectors[0]) != "" && c.tree.selectorIndex(&selectors[1]) != 0 {
			firstKey := c.addStringConstant(c.tree.selectorField(&selectors[0]))
			key := c.allocReg()
			if err := c.compileExpressionTo(c.tree.selectorIndex(&selectors[1]), key); err != nil {
				return err
			}
			c.emit(instruction{op: opGetStringFieldIndex, a: target, b: target, c: firstKey, d: key})
			selectors = selectors[2:]
			continue
		}
		selector := selectors[0]
		if c.tree.selectorField(&selector) != "" {
			key := c.addStringConstant(c.tree.selectorField(&selector))
			c.emit(instruction{op: opGetStringField, a: target, b: target, c: key})
			selectors = selectors[1:]
			continue
		}
		key := c.allocReg()
		if err := c.compileExpressionTo(c.tree.selectorIndex(&selector), key); err != nil {
			return err
		}
		c.emit(instruction{op: opGetIndex, a: target, b: target, c: key})
		selectors = selectors[1:]
	}
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
	conditionExpr := c.tree.ifCondition(&branch)
	jumpIfFalse, ok, err := c.compileConditionJumpIfFalse(conditionExpr)
	if err != nil {
		return err
	}
	if !ok {
		condition, err := c.compileTempExpression(conditionExpr)
		if err != nil {
			return err
		}
		jumpIfFalse = c.emitJumpIfFalse(condition)
		c.releaseTemp(condition)
	}

	if err := c.compileStatements(c.tree.ifThenStatements(&branch)); err != nil {
		return err
	}

	jumpEnd := c.emitJump()

	elseStart := c.pc()
	c.patchJump(jumpIfFalse, elseStart)

	elseStatements := c.tree.ifElseStatements(&branch)
	if len(elseStatements) > 0 {
		if err := c.compileStatements(elseStatements); err != nil {
			return err
		}
	}

	c.patchJump(jumpEnd, c.pc())
	return nil
}

type stringTagElseIfArm struct {
	value  string
	guards []comparisonExpressionID
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
			guardJumps, err := c.compileComparisonGuardJumps(arm.guards)
			if err != nil {
				c.releaseTemp(tag)
				return true, err
			}
			nextArmJumps = append(nextArmJumps, guardJumps...)
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
	first, firstGuards, ok := c.stringTagArmCondition(c.tree.ifCondition(&branch))
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
			body:   c.tree.ifThenStatements(&branch),
		}},
	}
	elseBody := c.tree.ifElseStatements(&branch)
	for len(elseBody) == 1 && c.tree.statementKind(&elseBody[0]) == syntaxStatementIf {
		nextBranch := *c.tree.ifStatement(&elseBody[0])
		condition, guards, ok := c.stringTagArmCondition(c.tree.ifCondition(&nextBranch))
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
			body:   c.tree.ifThenStatements(&nextBranch),
		})
		elseBody = c.tree.ifElseStatements(&nextBranch)
	}
	if len(chain.arms) < 3 {
		return stringTagElseIfChain{}, false
	}
	chain.elseBody = elseBody
	return chain, true
}

func (c *compiler) stringTagArmCondition(expr expressionID) (stringFieldEqualityCondition, []comparisonExpressionID, bool) {
	terms, ok := c.tree.expressionTerms(expr)
	if !ok || len(terms) != 1 {
		return stringFieldEqualityCondition{}, nil, false
	}
	comparisons, ok := c.tree.andTerms(terms[0])
	if !ok || len(comparisons) == 0 {
		return stringFieldEqualityCondition{}, nil, false
	}
	condition, ok := c.stringFieldEqualityCondition(comparisons[0])
	if !ok {
		return stringFieldEqualityCondition{}, nil, false
	}
	guards := append([]comparisonExpressionID(nil), comparisons[1:]...)
	return condition, guards, true
}

func (c *compiler) singleStringFieldEqualityCondition(expr expressionID) (stringFieldEqualityCondition, bool) {
	terms, ok := c.tree.expressionTerms(expr)
	if !ok || len(terms) != 1 {
		return stringFieldEqualityCondition{}, false
	}
	comparisons, ok := c.tree.andTerms(terms[0])
	if !ok || len(comparisons) != 1 {
		return stringFieldEqualityCondition{}, false
	}
	return c.stringFieldEqualityCondition(comparisons[0])
}

func (c *compiler) compileIfExpressionTo(expr arenaIfExpressionID, target int) error {
	conditionExpr := c.tree.ifExpressionCondition(expr)
	jumpIfFalse, ok, err := c.compileConditionJumpIfFalse(conditionExpr)
	if err != nil {
		return err
	}
	if !ok {
		condition, err := c.compileTempExpression(conditionExpr)
		if err != nil {
			return err
		}
		jumpIfFalse = c.emitJumpIfFalse(condition)
		c.releaseTemp(condition)
	}

	if err := c.compileExpressionTo(c.tree.ifExpressionThen(expr), target); err != nil {
		return err
	}

	jumpEnd := c.emitJump()

	c.patchJump(jumpIfFalse, c.pc())
	if err := c.compileExpressionTo(c.tree.ifExpressionElse(expr), target); err != nil {
		return err
	}

	c.patchJump(jumpEnd, c.pc())
	return nil
}

func (c *compiler) compileConditionJumpIfFalse(expr expressionID) (int, bool, error) {
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
	comparison, ok := singleComparison(c.tree, expr)
	if !ok {
		return 0, false, nil
	}
	rightExpr := c.tree.comparisonRight(comparison)
	if rightExpr == 0 {
		return 0, false, nil
	}
	right, ok := foldNumberConcat(c.tree, rightExpr)
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
	left, releaseLeft, err := c.compileConditionLeftRegister(c.tree.comparisonLeft(comparison))
	if err != nil {
		return 0, false, err
	}
	constant := c.addConstant(NumberValue(right))
	switch c.tree.comparisonOperator(comparison) {
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

func singleComparison(tree syntaxTree, expr expressionID) (comparisonExpressionID, bool) {
	terms, ok := tree.expressionTerms(expr)
	if !ok || len(terms) != 1 {
		return 0, false
	}
	comparisons, ok := tree.andTerms(terms[0])
	if !ok || len(comparisons) != 1 {
		return 0, false
	}
	return comparisons[0], true
}

func (c *compiler) compileRegisterNumericJumpIfFalse(comparison comparisonExpressionID) (int, bool, error) {
	rightExpr := c.tree.comparisonRight(comparison)
	if rightExpr == 0 {
		return 0, false, nil
	}
	var op opcode
	switch c.tree.comparisonOperator(comparison) {
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
	left, releaseLeft, err := c.compileConditionLeftRegister(c.tree.comparisonLeft(comparison))
	if err != nil {
		return 0, false, err
	}
	right, releaseRight, err := c.compileConditionLeftRegister(rightExpr)
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

func (c *compiler) compileAndChainJumpIfFalse(expr expressionID) (int, bool, error) {
	terms, ok := c.tree.expressionTerms(expr)
	if !ok || len(terms) != 1 {
		return 0, false, nil
	}
	comparisons, ok := c.tree.andTerms(terms[0])
	if !ok || len(comparisons) < 2 {
		return 0, false, nil
	}
	plans := make([]andChainBranchPlan, 0, len(comparisons))
	for _, comparison := range comparisons {
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

// compileComparisonGuardJumps emits false branches for each comparison in an
// already-parsed && chain. The returned jumps are all patched by the caller to
// the next source arm. Keeping the comparison IDs avoids constructing a
// temporary expression tree for the arena-backed representation.
func (c *compiler) compileComparisonGuardJumps(comparisons []comparisonExpressionID) ([]int, error) {
	if len(comparisons) == 0 {
		return nil, nil
	}
	plans := make([]andChainBranchPlan, len(comparisons))
	optimized := true
	for i, comparison := range comparisons {
		plan, ok := c.andChainBranchPlan(comparison)
		if !ok {
			optimized = false
			break
		}
		plans[i] = plan
	}
	if !optimized {
		jumps := make([]int, 0, len(comparisons))
		for _, comparison := range comparisons {
			condition := c.allocTemp()
			if err := c.compileComparisonExpressionTo(comparison, condition); err != nil {
				c.releaseTemp(condition)
				return nil, err
			}
			jumps = append(jumps, c.emitJumpIfFalse(condition))
			c.releaseTemp(condition)
		}
		return jumps, nil
	}
	jumps := make([]int, 0, len(plans))
	for _, plan := range plans {
		switch plan.op {
		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			if plan.field != "" {
				jumps = append(jumps, c.emitAndChainFieldPairBranch(plan))
			} else {
				jumps = append(jumps, c.emit(instruction{op: plan.op, a: plan.a, b: plan.b}))
			}
		case opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			constant := c.addConstant(NumberValue(plan.constant))
			if plan.field != "" {
				jumps = append(jumps, c.emitAndChainStringFieldNumericBranch(plan, constant))
			} else {
				jumps = append(jumps, c.emit(instruction{op: plan.op, a: plan.a, b: constant}))
			}
		default:
			return nil, fmt.Errorf("compile: unsupported guard branch %d", plan.op)
		}
	}
	return jumps, nil
}

func (c *compiler) andChainBranchPlan(comparison comparisonExpressionID) (andChainBranchPlan, bool) {
	rightExpr := c.tree.comparisonRight(comparison)
	if rightExpr == 0 {
		return andChainBranchPlan{}, false
	}
	if plan, ok := c.andChainStringFieldNumericPlan(comparison); ok {
		return plan, true
	}
	if plan, ok := c.andChainStringFieldPairNumericPlan(comparison); ok {
		return plan, true
	}
	var op opcode
	switch c.tree.comparisonOperator(comparison) {
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
	left, ok := c.concatLocalRef(c.tree.comparisonLeft(comparison))
	if !ok {
		return andChainBranchPlan{}, false
	}
	if right, ok := c.concatLocalRef(rightExpr); ok {
		return andChainBranchPlan{
			op: op,
			a:  left.index,
			b:  right.index,
		}, true
	}
	right, ok := foldNumberConcat(c.tree, rightExpr)
	if !ok {
		return andChainBranchPlan{}, false
	}
	switch c.tree.comparisonOperator(comparison) {
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

func (c *compiler) andChainStringFieldNumericPlan(comparison comparisonExpressionID) (andChainBranchPlan, bool) {
	rightExpr := c.tree.comparisonRight(comparison)
	if rightExpr == 0 {
		return andChainBranchPlan{}, false
	}
	table, field, ok := c.concatLocalStringFieldRef(c.tree.comparisonLeft(comparison))
	if !ok {
		return andChainBranchPlan{}, false
	}
	right, ok := foldNumberConcat(c.tree, rightExpr)
	if !ok {
		return andChainBranchPlan{}, false
	}
	var op opcode
	switch c.tree.comparisonOperator(comparison) {
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

func (c *compiler) andChainStringFieldPairNumericPlan(comparison comparisonExpressionID) (andChainBranchPlan, bool) {
	rightExpr := c.tree.comparisonRight(comparison)
	if rightExpr == 0 {
		return andChainBranchPlan{}, false
	}
	leftTable, leftField, ok := c.concatLocalStringFieldRef(c.tree.comparisonLeft(comparison))
	if !ok {
		return andChainBranchPlan{}, false
	}
	rightTable, rightField, ok := c.concatLocalStringFieldRef(rightExpr)
	if !ok {
		return andChainBranchPlan{}, false
	}
	var op opcode
	switch c.tree.comparisonOperator(comparison) {
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

func (c *compiler) moduloConstantEqualityCondition(comparison comparisonExpressionID, right float64) (moduloConstantEqualityCondition, bool) {
	left := c.tree.comparisonLeft(comparison)
	if c.tree.comparisonOperator(comparison) != comparisonEqual || c.tree.comparisonRight(comparison) == 0 {
		return moduloConstantEqualityCondition{}, false
	}
	additive := c.tree.concatFirst(left)
	additiveRest, _ := c.tree.additiveRest(additive)
	if len(additiveRest) != 0 {
		return moduloConstantEqualityCondition{}, false
	}
	multiplicative := c.tree.additiveFirst(additive)
	parts, _ := c.tree.multiplicativeRest(multiplicative)
	if len(parts) != 1 {
		return moduloConstantEqualityCondition{}, false
	}
	modPart := parts[0]
	if c.tree.multiplicativePartOperator(modPart) != multiplicativeModulo {
		return moduloConstantEqualityCondition{}, false
	}
	mod, ok := foldNumberTerm(c.tree, c.tree.multiplicativePartValue(modPart))
	if !ok {
		return moduloConstantEqualityCondition{}, false
	}
	source, ok := c.termLocalRef(c.tree.multiplicativeFirst(multiplicative))
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

func (c *compiler) compileStringFieldEqualityJumpIfFalse(expr expressionID) (int, bool, error) {
	terms, ok := c.tree.expressionTerms(expr)
	if !ok {
		return 0, false, nil
	}
	if len(terms) == 0 || len(terms) > 2 {
		return 0, false, nil
	}
	conditions := make([]stringFieldEqualityCondition, 0, len(terms))
	for _, term := range terms {
		comparisons, ok := c.tree.andTerms(term)
		if !ok {
			return 0, false, nil
		}
		if len(comparisons) != 1 {
			return 0, false, nil
		}
		condition, ok := c.stringFieldEqualityCondition(comparisons[0])
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

func (c *compiler) stringFieldEqualityCondition(expr comparisonExpressionID) (stringFieldEqualityCondition, bool) {
	right := c.tree.comparisonRight(expr)
	if c.tree.comparisonOperator(expr) != comparisonEqual || right == 0 {
		return stringFieldEqualityCondition{}, false
	}
	table, field, ok := c.concatLocalStringFieldRef(c.tree.comparisonLeft(expr))
	if !ok {
		return stringFieldEqualityCondition{}, false
	}
	value, ok := concatNonNilLiteral(c.tree, right)
	if !ok {
		return stringFieldEqualityCondition{}, false
	}
	return stringFieldEqualityCondition{
		table: table.index,
		field: field,
		value: value,
	}, true
}

func (c *compiler) concatLocalStringFieldRef(expr concatExpressionID) (variableRef, string, bool) {
	term, ok := concatSingleTerm(c.tree, expr)
	if !ok {
		return variableRef{}, "", false
	}
	value := termWithoutCastsAndGroups(c.tree, term)
	selectors, _ := c.tree.termSelectors(value)
	if len(selectors) != 1 || c.tree.termSelectorField(selectors[0]) == "" || c.tree.termSelectorIndex(selectors[0]) != 0 {
		return variableRef{}, "", false
	}
	field := c.tree.termSelectorField(selectors[0])
	ref, ok := c.namedTermLocalRef(value)
	return ref, field, ok
}

func concatStringLiteral(tree syntaxTree, expr concatExpressionID) (string, bool) {
	value, ok := concatSingleTerm(tree, expr)
	if !ok {
		return "", false
	}
	term := termWithoutCastsAndGroups(tree, value)
	literal, ok := tree.termLiteral(term)
	selectors, _ := tree.termSelectors(term)
	if !isNamedTerm(tree, term) && ok && len(selectors) == 0 {
		text, ok := literal.String()
		return text, ok
	}
	return "", false
}

func concatNonNilLiteral(tree syntaxTree, expr concatExpressionID) (Value, bool) {
	value, ok := concatSingleTerm(tree, expr)
	if !ok {
		return NilValue(), false
	}
	term := termWithoutCastsAndGroups(tree, value)
	selectors, _ := tree.termSelectors(term)
	if isNamedTerm(tree, term) || len(selectors) != 0 {
		return NilValue(), false
	}
	if number, ok := tree.termNumber(term); ok {
		return NumberValue(number), true
	}
	literal, ok := tree.termLiteral(term)
	if !ok || valueKind(literal) == NilKind {
		return NilValue(), false
	}
	switch valueKind(literal) {
	case BoolKind, NumberKind, StringKind:
		return literal, true
	default:
		return NilValue(), false
	}
}

func concatSingleTerm(tree syntaxTree, expr concatExpressionID) (termID, bool) {
	rest, ok := tree.concatRest(expr)
	if !ok || len(rest) != 0 {
		return 0, false
	}
	additive := tree.concatFirst(expr)
	additiveRest, ok := tree.additiveRest(additive)
	if !ok || len(additiveRest) != 0 {
		return 0, false
	}
	multiplicative := tree.additiveFirst(additive)
	multiplicativeRest, ok := tree.multiplicativeRest(multiplicative)
	if !ok || len(multiplicativeRest) != 0 {
		return 0, false
	}
	return tree.multiplicativeFirst(multiplicative), true
}

func (c *compiler) compileStringFieldNumericJumpIfFalse(expr expressionID) (int, bool, error) {
	comparison, ok := singleComparison(c.tree, expr)
	if !ok {
		return 0, false, nil
	}
	rightExpr := c.tree.comparisonRight(comparison)
	if rightExpr == 0 {
		return 0, false, nil
	}
	table, field, ok := c.concatLocalStringFieldRef(c.tree.comparisonLeft(comparison))
	if !ok {
		return 0, false, nil
	}
	right, ok := foldNumberConcat(c.tree, rightExpr)
	if !ok {
		return 0, false, nil
	}
	fieldConstant := c.addStringConstant(field)
	valueConstant := c.addConstant(NumberValue(right))
	switch c.tree.comparisonOperator(comparison) {
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

func (c *compiler) compileRegisterStringFieldNumericJumpIfFalse(expr expressionID) (int, bool, error) {
	comparison, ok := singleComparison(c.tree, expr)
	if !ok {
		return 0, false, nil
	}
	rightExpr := c.tree.comparisonRight(comparison)
	if c.tree.comparisonOperator(comparison) != comparisonLess || rightExpr == 0 {
		return 0, false, nil
	}
	table, field, ok := c.concatLocalStringFieldRef(rightExpr)
	if !ok {
		return 0, false, nil
	}
	left, releaseLeft, err := c.compileConditionLeftRegister(c.tree.comparisonLeft(comparison))
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

func (c *compiler) compileStringFieldTruthyJumpIfFalse(expr expressionID) (int, bool, error) {
	comparison, ok := singleComparison(c.tree, expr)
	if !ok {
		return 0, false, nil
	}
	if c.tree.comparisonOperator(comparison) != "" || c.tree.comparisonRight(comparison) != 0 {
		return 0, false, nil
	}
	table, field, ok := c.concatLocalStringFieldRef(c.tree.comparisonLeft(comparison))
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

func (c *compiler) compileConditionLeftRegister(expr concatExpressionID) (int, func(), error) {
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

func (c *compiler) concatLocalRef(expr concatExpressionID) (variableRef, bool) {
	term, ok := concatSingleTerm(c.tree, expr)
	if !ok {
		return variableRef{}, false
	}
	if !isNamedTerm(c.tree, term) {
		return variableRef{}, false
	}
	ref, ok := c.resolveBoundUseNoError(c.tree.termID(term))
	return ref, ok && ref.kind == variableLocal
}

func (c *compiler) expressionLocalRef(expr expressionID) (variableRef, bool) {
	if c.options.optimizations.enabled(optimizationHIRSimplify) {
		if _, ok := foldConstantExpression(c.tree, expr); ok {
			return variableRef{}, false
		}
	}
	comparison, ok := singleComparison(c.tree, expr)
	if !ok {
		return variableRef{}, false
	}
	if c.tree.comparisonOperator(comparison) != "" || c.tree.comparisonRight(comparison) != 0 {
		return variableRef{}, false
	}
	return c.concatLocalRef(c.tree.comparisonLeft(comparison))
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
	condition := c.tree.whileCondition(&stmt)

	jumpIfFalse, ok, err := c.compileConditionJumpIfFalse(condition)
	if err != nil {
		return err
	}
	if !ok {
		conditionReg, err := c.compileTempExpression(condition)
		if err != nil {
			return err
		}
		jumpIfFalse = c.emitJumpIfFalse(conditionReg)
		c.releaseTemp(conditionReg)
	}

	c.loops = append(c.loops, loopContext{continueTarget: conditionStart})
	if err := c.compileStatements(c.tree.whileStatements(&stmt)); err != nil {
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

	if err := c.compileExpressionTo(c.tree.numericForStart(&stmt), loopVar); err != nil {
		return err
	}
	if err := c.compileExpressionTo(c.tree.numericForLimit(&stmt), limit); err != nil {
		return err
	}
	if stepExpr := c.tree.numericForStep(&stmt); stepExpr != 0 {
		if err := c.compileExpressionTo(stepExpr, step); err != nil {
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

	if err := c.assignDefinition(c.tree.numericForNameID(&stmt), symbolLocal, loopVar); err != nil {
		return err
	}
	c.loops = append(c.loops, loopContext{continueTarget: -1})
	if err := c.compileStatements(c.tree.numericForStatements(&stmt)); err != nil {
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
	names := c.tree.genericForNames(&stmt)
	if len(names) == 0 {
		return fmt.Errorf("compile: generic for has no names")
	}

	generator := c.allocReg()
	state := c.allocReg()
	control := c.allocReg()
	targets := []int{generator, state, control}
	values := c.tree.genericForValues(&stmt)
	if err := c.compileExpressionListTo(values, targets); err != nil {
		return err
	}
	if len(values) == 1 {
		c.emit(instruction{op: opPrepareIter, a: generator, b: state, c: control})
	}

	resultStart := control
	c.reserveRegistersThrough(resultStart + 4)
	c.reserveRegistersThrough(resultStart + len(names))
	c.claimRegisterRange(resultStart, resultStart+len(names))
	loopStart := c.pc()
	var jumpExit int
	if len(names) == 2 {
		jumpExit = c.emit(instruction{op: opArrayNextJump2, a: resultStart, b: generator, c: state})
	} else {
		nilReg := c.allocReg()
		c.compileNilTo(nilReg)
		condition := c.allocReg()
		c.emit(instruction{op: opArrayNext, a: resultStart, b: generator, c: state, d: len(names)})
		c.emit(instruction{op: opMove, a: control, b: resultStart})
		c.emit(instruction{op: opNotEqual, a: condition, b: resultStart, c: nilReg})
		jumpExit = c.emitJumpIfFalse(condition)
	}

	for i := range names {
		register := resultStart + i
		if err := c.assignDefinition(syntaxNameID(c.tree.genericForNameID(&stmt), i), symbolLocal, register); err != nil {
			return err
		}
	}
	c.loops = append(c.loops, loopContext{continueTarget: loopStart})
	if err := c.compileStatements(c.tree.genericForStatements(&stmt)); err != nil {
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
	if err := c.compileStatements(c.tree.repeatStatements(&stmt)); err != nil {
		return err
	}
	loop := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]

	conditionStart := c.pc()
	for _, jump := range loop.continueJumps {
		c.patchJump(jump, conditionStart)
	}

	condition, err := c.compileTempExpression(c.tree.repeatCondition(&stmt))
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
	if err := c.compileStatements(c.tree.blockStatements(&stmt)); err != nil {
		return err
	}
	return nil
}

func (c *compiler) compileTableTo(table arenaTableID, target int) error {
	arrayCapacity, fieldCapacity := tableCapacity(c.tree, table)
	c.emit(instruction{op: opNewTable, a: target, b: arrayCapacity, c: fieldCapacity})
	fields, _ := c.tree.tableFields(table)
	for _, field := range fields {
		value := c.allocTemp()
		if err := c.compileExpressionTo(c.tree.tableFieldValue(field), value); err != nil {
			c.releaseTemp(value)
			return err
		}
		switch {
		case c.tree.tableFieldKey(field) != 0:
			key := c.allocTemp()
			if err := c.compileExpressionTo(c.tree.tableFieldKey(field), key); err != nil {
				c.releaseTemp(key)
				c.releaseTemp(value)
				return err
			}
			c.emit(instruction{op: opSetIndex, a: target, b: key, c: value})
			c.releaseTemp(key)
		case c.tree.tableFieldArrayIndex(field) != 0:
			key := c.addConstant(NumberValue(float64(c.tree.tableFieldArrayIndex(field))))
			c.emit(instruction{op: opSetField, a: target, b: key, c: value})
		case c.tree.tableFieldName(field) != "":
			key := c.addStringConstant(c.tree.tableFieldName(field))
			c.emit(instruction{op: opSetStringField, a: target, b: key, c: value})
		default:
			c.releaseTemp(value)
			return fmt.Errorf("compile: table field has no key")
		}
		c.releaseTemp(value)
	}
	return nil
}

func tableCapacity(tree syntaxTree, table arenaTableID) (int, int) {
	arrayCapacity := 0
	fieldCapacity := 0
	fields, _ := tree.tableFields(table)
	for _, field := range fields {
		switch {
		case tree.tableFieldArrayIndex(field) != 0:
			if tree.tableFieldArrayIndex(field) > arrayCapacity {
				arrayCapacity = tree.tableFieldArrayIndex(field)
			}
		case tree.tableFieldName(field) != "" || tree.tableFieldKey(field) != 0:
			fieldCapacity++
		}
	}
	return arrayCapacity, fieldCapacity
}

func (c *compiler) compileGlobalNameTo(name string, target int) {
	constant := c.addStringConstant(name)
	c.emit(instruction{op: opLoadGlobal, a: target, b: constant})
}

func (c *compiler) compileNamedTermTo(term termID, target int) error {
	ref, bound, err := c.resolveBoundUse(c.tree.termID(term))
	if err != nil {
		return err
	}
	if bound {
		return c.compileVariableRefTo(ref, target)
	}
	c.compileGlobalNameTo(c.tree.termName(term), target)
	return nil
}

func (c *compiler) termLocalRef(term termID) (variableRef, bool) {
	if !isNamedTerm(c.tree, term) {
		return variableRef{}, false
	}
	if ref, ok := c.resolveBoundUseNoError(c.tree.termID(term)); ok && ref.kind == variableLocal {
		return ref, true
	}
	return variableRef{}, false
}

// namedTermLocalRef resolves the binding for a named term that carries one or
// more selectors. Arena terms keep selectors in immutable spans, so callers
// cannot clear them on a local copy as the old pointer tree did.
func (c *compiler) namedTermLocalRef(term termID) (variableRef, bool) {
	if c.tree.termKind(term) != syntaxTermName || c.tree.termName(term) == "" {
		return variableRef{}, false
	}
	if ref, ok := c.resolveBoundUseNoError(c.tree.termID(term)); ok && ref.kind == variableLocal {
		return ref, true
	}
	return variableRef{}, false
}

func (c *compiler) compileAssignTargetBaseTo(target assignTarget, register int) error {
	ref, bound, err := c.resolveBoundUse(c.tree.assignTargetID(&target))
	if err != nil {
		return err
	}
	if bound {
		return c.compileVariableRefTo(ref, register)
	}
	c.compileGlobalNameTo(c.tree.assignTargetName(&target), register)
	return nil
}

func (c *compiler) resolveAssignTarget(target assignTarget) (variableRef, bool) {
	return c.resolveBoundUseNoError(c.tree.assignTargetID(&target))
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

func (c *compiler) compileCallTo(call arenaCallID, target int) error {
	return c.compileCallToResults(call, target, 1)
}

func (c *compiler) compileCallToResults(call arenaCallID, target int, resultCount int) error {
	plan := planCall(c.tree, call)
	args, _ := c.tree.callArgs(call)
	return c.compilePlannedCallToResults(plan, args, target, resultCount)
}

func (c *compiler) compilePlannedCallToResults(plan callPlan, args []expressionID, target int, resultCount int) error {
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

func (c *compiler) compilePlannedCallToResultsDirect(lowered callPlan, args []expressionID, target int, resultCount int) error {
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
	if resultCount != 1 || lowered.receiver == 0 {
		return methodOneResultCall{}, false
	}
	target := lowered.target
	selectors, _ := c.tree.termSelectors(target)
	if len(selectors) != 1 ||
		c.tree.termSelectorField(selectors[0]) == "" ||
		c.tree.termSelectorIndex(selectors[0]) != 0 {
		return methodOneResultCall{}, false
	}
	receiver, ok := c.termLocalRef(lowered.receiver)
	if !ok {
		return methodOneResultCall{}, false
	}
	targetBase, ok := c.namedTermLocalRef(target)
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
		field:    c.tree.termSelectorField(selectors[0]),
	}, true
}

func (c *compiler) compileMethodOneResultCallToResults(
	method methodOneResultCall,
	lowered callPlan,
	args []expressionID,
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

func (c *compiler) selectVarargCountCall(lowered callPlan, args []expressionID, resultCount int) bool {
	if resultCount == 0 || lowered.receiver != 0 || !c.variadic {
		return false
	}
	if !c.isUnboundGlobalName(lowered.target, "select") {
		return false
	}
	if len(args) != 2 || lowered.args.len() != 2 {
		return false
	}
	if marker, ok := expressionStringLiteral(c.tree, args[0]); !ok || marker != "#" {
		return false
	}
	if _, ok := expressionSingleVararg(c.tree, args[1]); !ok {
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
		lowered.receiver == 0 &&
		c.isUnboundGlobalName(lowered.target, "rawlen")
}

func (c *compiler) isUnboundGlobalName(term termID, name string) bool {
	if !isNamedTerm(c.tree, term) || c.tree.termName(term) != name {
		return false
	}
	return c.bind.useClassification(c.tree.termID(term)) == boundUseGlobal
}

func expressionStringLiteral(tree syntaxTree, expr expressionID) (string, bool) {
	value, ok := expressionSingleTerm(tree, expr)
	literal, okLiteral := tree.termLiteral(value)
	if !ok || !okLiteral {
		return "", false
	}
	return literal.String()
}

func (c *compiler) upvalueOneResultCall(lowered callPlan, resultCount int) (int, bool) {
	if resultCount != 1 || lowered.receiver != 0 {
		return 0, false
	}
	target := lowered.target
	selectors, _ := c.tree.termSelectors(target)
	if !isNamedTerm(c.tree, target) || len(selectors) != 0 {
		return 0, false
	}
	for i := range lowered.args.len() {
		if lowered.args.item(i).kind != valuePlanSingle {
			return 0, false
		}
	}
	ref, ok := c.resolveBoundUseNoError(c.tree.termID(target))
	return ref.index, ok && ref.kind == variableUpvalue
}

func (c *compiler) selfUpvalueOneResultCall(lowered callPlan, resultCount int) (int, bool) {
	if c.selfFunctionSymbol < 0 || resultCount != 1 || lowered.receiver != 0 {
		return 0, false
	}
	target := lowered.target
	selectors, _ := c.tree.termSelectors(target)
	if !isNamedTerm(c.tree, target) || len(selectors) != 0 {
		return 0, false
	}
	for i := range lowered.args.len() {
		if lowered.args.item(i).kind != valuePlanSingle {
			return 0, false
		}
	}
	classification := c.bind.useClassification(c.tree.termID(target))
	if classification != boundUseClassification(c.selfFunctionSymbol) {
		return 0, false
	}
	ref, ok := c.resolveSymbol(int(classification))
	return ref.index, ok && ref.kind == variableUpvalue
}

func (c *compiler) localOneResultCall(lowered callPlan, resultCount int) (int, bool) {
	if resultCount != 1 || lowered.receiver != 0 {
		return 0, false
	}
	target := lowered.target
	selectors, _ := c.tree.termSelectors(target)
	if !isNamedTerm(c.tree, target) || len(selectors) != 0 {
		return 0, false
	}
	for i := range lowered.args.len() {
		if lowered.args.item(i).kind != valuePlanSingle {
			return 0, false
		}
	}
	ref, ok := c.resolveBoundUseNoError(c.tree.termID(target))
	return ref.index, ok && ref.kind == variableLocal
}

func (c *compiler) compileLocalOneResultCallToResults(local int, lowered callPlan, args []expressionID, target int) error {
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

func (c *compiler) compileUpvalueOneResultCallToResults(upvalue int, lowered callPlan, args []expressionID, target int) error {
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

func (c *compiler) compileSelfUpvalueOneResultCallToResults(upvalue int, lowered callPlan, args []expressionID, target int) error {
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

func (c *compiler) selfCallSubtractConstantArg(args []expressionID) (int, int, bool) {
	if len(args) != 1 {
		return 0, 0, false
	}
	expr := args[0]
	comparison, ok := singleComparison(c.tree, expr)
	if !ok {
		return 0, 0, false
	}
	left := c.tree.comparisonLeft(comparison)
	if c.tree.comparisonOperator(comparison) != "" || c.tree.comparisonRight(comparison) != 0 {
		return 0, 0, false
	}
	concatRest, _ := c.tree.concatRest(left)
	if len(concatRest) != 0 {
		return 0, 0, false
	}
	additive := c.tree.concatFirst(left)
	parts, _ := c.tree.additiveRest(additive)
	if len(parts) != 1 || c.tree.additivePartOperator(parts[0]) != additiveSubtract {
		return 0, 0, false
	}
	multiplicative := c.tree.additiveFirst(additive)
	multiplicativeRest, _ := c.tree.multiplicativeRest(multiplicative)
	if len(multiplicativeRest) != 0 {
		return 0, 0, false
	}
	ref, ok := c.termLocalRef(termWithoutCastsAndGroups(c.tree, c.tree.multiplicativeFirst(multiplicative)))
	if !ok {
		return 0, 0, false
	}
	number, ok := foldNumberMultiplicative(c.tree, c.tree.additivePartValue(parts[0]))
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
		lowered.receiver != 0 ||
		!c.isUnboundBaseField(lowered.target, globalName) {
		return nativeFuncUnknown, false
	}
	selectors, _ := c.tree.termSelectors(lowered.target)
	field := c.tree.termSelectorField(selectors[0])
	intrinsic, ok := baseFieldIntrinsic(globalName, field)
	if !ok {
		return nativeFuncUnknown, false
	}
	return intrinsic.nativeID, true
}

func (c *compiler) isUnboundBaseField(term termID, name string) bool {
	selectors, _ := c.tree.termSelectors(term)
	if c.tree.termKind(term) != syntaxTermName || c.tree.termName(term) != name ||
		len(selectors) != 1 ||
		c.tree.termSelectorField(selectors[0]) == "" ||
		c.tree.termSelectorIndex(selectors[0]) != 0 {
		return false
	}
	return c.bind.useClassification(c.tree.termID(term)) == boundUseGlobal
}

func (c *compiler) compileBaseIntrinsicCallToResults(
	nativeID nativeFuncID,
	lowered callPlan,
	args []expressionID,
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

func (c *compiler) compilePlannedCallToResultsGeneric(lowered callPlan, args []expressionID, target int, resultCount int) error {
	if err := c.compileCallTargetTo(lowered.target, target); err != nil {
		return err
	}

	firstArg := target + 1
	fixedArgCount := 0
	if lowered.receiver != 0 {
		c.reserveRegistersThrough(target + 2 + len(args))
		if err := c.compileCallTargetTo(lowered.receiver, firstArg); err != nil {
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
			if vararg, ok := expressionSingleVararg(c.tree, args[item.source]); ok {
				if err := c.compileVarargToResults(vararg, openTarget, item.resultCount); err != nil {
					return err
				}
			} else if nestedCall, ok := expressionSingleCall(c.tree, args[item.source]); ok {
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

func (c *compiler) compileVarargToResults(_ termID, target int, resultCount int) error {
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
