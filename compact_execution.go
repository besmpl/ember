package ember

import (
	"fmt"
	"math"
)

// compactProgram is an immutable, compiler-built description of a pure
// numeric call graph. Canonical wordcode remains the executable source of
// truth; this sidecar only resolves direct call targets and their fixed ABI.
type compactProgram struct {
	functions  []compactFunction
	calls      []compactCallSite
	callByWord []uint32
	entry      uint16
}

type compactFunction struct {
	proto           *Proto
	callLookupStart uint32
	wordCount       uint32
}

type compactCallSite struct {
	wordPC        uint32
	target        uint16
	argumentStart uint8
	argumentCount uint8
	result        uint8
	flags         uint8
}

const compactCallBorrowed uint8 = 1 << iota

// compactCallFrame is the complete continuation for one fixed one-result
// script call. It intentionally contains no Go pointers, slices, or interface
// values, so recursive script calls do not create write-barrier traffic.
type compactCallFrame struct {
	callerFunction uint16
	_              uint16
	returnPC       uint32
	callerBase     uint32
	callerTop      uint32
	resultBase     uint32
}

const (
	compactFactUnknown uint32 = iota
	compactFactNumber
	compactFactFunctionBase
)

type compactBuildFunction struct {
	proto          *Proto
	code           []instruction
	boundaries     []int
	parent         int
	parentChild    int
	children       []uint16
	upvalueTargets []int
	calls          []compactCallSite
}

// buildCompactCallProgram admits the deliberately narrow first whole-graph
// tier: pure, fixed-arity numeric functions with fixed one-result calls,
// direct child closures, and direct self recursion. Unsupported graphs simply
// retain the established VM path.
func buildCompactCallProgram(root *Proto) *compactProgram {
	if root == nil || len(root.prototypes) == 0 || root.variadic || len(root.upvalues) != 0 ||
		!compactGraphCaptureEligible(root) {
		return nil
	}

	builder := compactProgramBuilder{}
	if _, ok := builder.collect(root, -1, -1); !ok || len(builder.functions) == 0 {
		return nil
	}
	if !builder.resolveUpvalueTargets() {
		return nil
	}

	for index := range builder.functions {
		if !builder.analyzeFunction(index) {
			return nil
		}
	}
	program, err := builder.buildCompactProgram()
	if err != nil {
		return nil
	}
	return program
}

func (builder *compactProgramBuilder) buildCompactProgram() (*compactProgram, error) {
	if builder == nil {
		return nil, fmt.Errorf("compact call program has nil builder")
	}
	if len(builder.functions) == 0 || len(builder.functions) > math.MaxUint16 {
		return nil, fmt.Errorf("compact call program has invalid function count %d", len(builder.functions))
	}
	totalCalls := uint64(0)
	for _, function := range builder.functions {
		calls := uint64(len(function.calls))
		if calls > uint64(math.MaxUint32)-totalCalls {
			return nil, fmt.Errorf("compact call count overflows uint32")
		}
		totalCalls += calls
	}
	if totalCalls == 0 {
		return nil, fmt.Errorf("compact call program has no call sites")
	}
	if totalCalls > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("compact call count cannot be allocated")
	}
	program := &compactProgram{
		functions: make([]compactFunction, len(builder.functions)),
		calls:     make([]compactCallSite, 0, int(totalCalls)),
	}
	totalWords := uint64(0)
	for index := range builder.functions {
		function := &builder.functions[index]
		if function.proto == nil {
			return nil, fmt.Errorf("compact function %d has nil prototype", index)
		}
		wordCount := uint64(len(function.proto.words))
		if wordCount > uint64(math.MaxUint32) || wordCount > uint64(^uint(0)>>1) || totalWords > uint64(math.MaxUint32)-wordCount {
			return nil, fmt.Errorf("compact physical word count overflows uint32")
		}
		if uint64(len(program.calls)) > uint64(math.MaxUint32)-uint64(len(function.calls)) {
			return nil, fmt.Errorf("compact function %d call range overflows", index)
		}
		program.functions[index] = compactFunction{
			proto:           function.proto,
			callLookupStart: uint32(totalWords),
			wordCount:       uint32(wordCount),
		}
		program.calls = append(program.calls, function.calls...)
		totalWords += wordCount
	}
	if totalWords > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("compact physical word count cannot be allocated")
	}
	program.callByWord = make([]uint32, int(totalWords))
	globalCallIndex := uint64(0)
	for functionIndex, function := range builder.functions {
		compactFunction := program.functions[functionIndex]
		for callIndex, site := range function.calls {
			if uint64(site.wordPC) >= uint64(compactFunction.wordCount) {
				return nil, fmt.Errorf("compact call site %d in function %d has word pc %d outside word count %d", callIndex, functionIndex, site.wordPC, compactFunction.wordCount)
			}
			if globalCallIndex >= uint64(math.MaxUint32) {
				return nil, fmt.Errorf("compact global call index overflows uint32")
			}
			lookupIndex := uint64(compactFunction.callLookupStart) + uint64(site.wordPC)
			if lookupIndex >= uint64(len(program.callByWord)) || program.callByWord[lookupIndex] != 0 {
				return nil, fmt.Errorf("duplicate or invalid compact call word pc %d in function %d", site.wordPC, functionIndex)
			}
			program.callByWord[lookupIndex] = uint32(globalCallIndex) + 1
			globalCallIndex++
		}
	}
	return program, nil
}

// compactGraphCaptureEligible rejects ordinary captures before the builder
// materializes decoded code or data-flow scratch. The only admitted upvalue is
// a child capturing the exact parent register that receives that child's own
// closure, which compact execution replaces with the child's function id.
func compactGraphCaptureEligible(proto *Proto) bool {
	if proto == nil {
		return false
	}
	for childIndex, child := range proto.prototypes {
		if child == nil {
			return false
		}
		closureRegister := compactClosureRegister(proto, childIndex)
		if closureRegister < 0 {
			return false
		}
		for _, desc := range child.upvalues {
			if !desc.local || desc.copy || desc.index != closureRegister {
				return false
			}
		}
		if !compactGraphCaptureEligible(child) {
			return false
		}
	}
	return true
}

func compactClosureRegister(proto *Proto, childIndex int) int {
	if proto == nil || childIndex < 0 {
		return -1
	}
	closureRegister := -1
	for pc := 0; pc < len(proto.words); pc++ {
		raw := proto.words[pc]
		op := opcode(uint8(raw) & uint8(wordcodeOpcodeMask))
		next := pc + 1
		operand := int(uint16(raw >> 16))
		if raw&wordcodeAuxBit != 0 {
			if next >= len(proto.words) {
				return -1
			}
			operand = int(int32(proto.words[next]))
			pc = next
		}
		if op != opClosure || operand != childIndex {
			continue
		}
		register := int(uint8(raw >> 8))
		if closureRegister >= 0 && closureRegister != register {
			return -1
		}
		closureRegister = register
	}
	return closureRegister
}

type compactProgramBuilder struct {
	functions []compactBuildFunction
}

func (builder *compactProgramBuilder) collect(proto *Proto, parent, parentChild int) (uint16, bool) {
	if proto == nil || proto.variadic || proto.registers < 0 || proto.registers > 255 ||
		proto.params < 0 || proto.params > proto.registers || len(builder.functions) >= math.MaxUint16 {
		return 0, false
	}
	code, err := protoDecodedInstructions(proto)
	if err != nil || len(code) == 0 {
		return 0, false
	}
	boundaries, err := wordcodeBoundaries(code)
	if err != nil || len(boundaries) != len(code)+1 || boundaries[len(boundaries)-1] != len(proto.words) {
		return 0, false
	}

	id := uint16(len(builder.functions))
	index := len(builder.functions)
	builder.functions = append(builder.functions, compactBuildFunction{
		proto:       proto,
		code:        code,
		boundaries:  boundaries,
		parent:      parent,
		parentChild: parentChild,
		children:    make([]uint16, len(proto.prototypes)),
	})
	for childIndex, child := range proto.prototypes {
		childID, ok := builder.collect(child, index, childIndex)
		if !ok {
			return 0, false
		}
		builder.functions[index].children[childIndex] = childID
	}
	return id, true
}

// The first call-graph slice accepts no captured runtime values. The one
// exception is a function's own closure cell, which is replaced by its stable
// compact function id. That removes closure/cell work from direct recursion
// without weakening ordinary upvalue semantics.
func (builder *compactProgramBuilder) resolveUpvalueTargets() bool {
	if len(builder.functions) == 0 || len(builder.functions[0].proto.upvalues) != 0 {
		return false
	}
	for index := 1; index < len(builder.functions); index++ {
		function := &builder.functions[index]
		if function.parent < 0 || function.parent >= len(builder.functions) {
			return false
		}
		parent := &builder.functions[function.parent]
		closureRegister := -1
		for _, ins := range parent.code {
			if ins.op != opClosure || ins.b != function.parentChild {
				continue
			}
			if closureRegister >= 0 && closureRegister != ins.a {
				return false
			}
			closureRegister = ins.a
		}
		if closureRegister < 0 {
			return false
		}
		if len(function.proto.upvalues) == 0 {
			continue
		}
		function.upvalueTargets = make([]int, len(function.proto.upvalues))
		for upvalue, desc := range function.proto.upvalues {
			if !desc.local || desc.copy || desc.index != closureRegister {
				return false
			}
			function.upvalueTargets[upvalue] = index
		}
	}
	return true
}

func compactFunctionFact(id uint16) uint32 {
	return compactFactFunctionBase + uint32(id)
}

func compactFactFunction(fact uint32) (uint16, bool) {
	if fact < compactFactFunctionBase {
		return 0, false
	}
	value := fact - compactFactFunctionBase
	if value > math.MaxUint16 {
		return 0, false
	}
	return uint16(value), true
}

func (builder *compactProgramBuilder) analyzeFunction(index int) bool {
	function := &builder.functions[index]
	proto := function.proto
	code := function.code
	registers := proto.registers
	if len(code) == 0 || registers == 0 || len(code) > math.MaxInt/registers {
		return false
	}

	facts := make([]uint32, len(code)*registers)
	reachable := make([]bool, len(code))
	queued := make([]bool, len(code))
	work := make([]int, 0, len(code))
	scratch := make([]uint32, registers)
	sites := make([]compactCallSite, len(code))
	siteOK := make([]bool, len(code))
	fallsThrough := false
	returns := 0

	entry := facts[:registers]
	for register := 0; register < proto.params; register++ {
		entry[register] = compactFactNumber
	}
	reachable[0] = true
	queued[0] = true
	work = append(work, 0)

	merge := func(pc int, incoming []uint32) bool {
		if pc == len(code) {
			fallsThrough = true
			return true
		}
		if pc < 0 || pc >= len(code) {
			return false
		}
		row := facts[pc*registers : (pc+1)*registers]
		changed := false
		if !reachable[pc] {
			copy(row, incoming)
			reachable[pc] = true
			changed = true
		} else {
			for register, value := range incoming {
				if row[register] != value && row[register] != compactFactUnknown {
					row[register] = compactFactUnknown
					changed = true
				}
			}
		}
		if changed && !queued[pc] {
			queued[pc] = true
			work = append(work, pc)
		}
		return true
	}

	for head := 0; head < len(work); head++ {
		pc := work[head]
		queued[pc] = false
		copy(scratch, facts[pc*registers:(pc+1)*registers])
		ins := code[pc]

		registerOK := func(register int) bool {
			return register >= 0 && register < registers
		}
		numberFact := func(register int) bool {
			return registerOK(register) && scratch[register] == compactFactNumber
		}
		numberConstant := func(constant int) bool {
			return constant >= 0 && constant < len(proto.constants) && valueKind(proto.constants[constant]) == NumberKind
		}
		setNumber := func(register int) bool {
			if !registerOK(register) {
				return false
			}
			scratch[register] = compactFactNumber
			return true
		}

		switch ins.op {
		case opClosure:
			if !registerOK(ins.a) || ins.b < 0 || ins.b >= len(function.children) {
				return false
			}
			scratch[ins.a] = compactFunctionFact(function.children[ins.b])
		case opLoadConst:
			if !numberConstant(ins.b) || !setNumber(ins.a) {
				return false
			}
		case opMove:
			if !registerOK(ins.a) || !registerOK(ins.b) {
				return false
			}
			scratch[ins.a] = scratch[ins.b]
		case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
			if !numberFact(ins.b) || !numberFact(ins.c) || !setNumber(ins.a) {
				return false
			}
		case opNeg:
			if !numberFact(ins.b) || !setNumber(ins.a) {
				return false
			}
		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			if !numberFact(ins.b) || !numberConstant(ins.c) || !setNumber(ins.a) {
				return false
			}
		case opNumericForCheck:
			if !numberFact(ins.a) || !numberFact(ins.b) || !numberFact(ins.c) {
				return false
			}
		case opNumericForLoop:
			if !numberFact(ins.a) || !numberFact(ins.b) || !setNumber(ins.a) {
				return false
			}
		case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			if !numberFact(ins.a) || !numberConstant(ins.b) {
				return false
			}
		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			if !numberFact(ins.a) || !numberFact(ins.b) {
				return false
			}
		case opJump:
		case opCall, opCallOne, opCallLocalOne, opCallUpvalueOne:
			site, ok := builder.callSite(index, pc, scratch)
			if !ok {
				return false
			}
			callee := builder.functions[site.target].proto
			if int(site.argumentCount) != callee.params {
				return false
			}
			argumentStart := int(site.argumentStart)
			argumentCount := int(site.argumentCount)
			if argumentStart < 0 || argumentStart+argumentCount > registers {
				return false
			}
			for argument := 0; argument < argumentCount; argument++ {
				if !numberFact(argumentStart + argument) {
					return false
				}
			}
			if !setNumber(int(site.result)) {
				return false
			}
			if siteOK[pc] && sites[pc] != site {
				return false
			}
			sites[pc] = site
			siteOK[pc] = true
		case opReturnOne:
			if !numberFact(ins.a) {
				return false
			}
			returns++
		case opReturn:
			if !compactOneResultReturn(code, pc, ins) || !numberFact(ins.a) {
				return false
			}
			returns++
		default:
			return false
		}

		switch opcodeControlFlow(ins.op) {
		case opcodeControlReturn:
			continue
		case opcodeControlJump:
			target, ok := instructionJumpTarget(ins)
			if !ok || !merge(target, scratch) {
				return false
			}
		case opcodeControlBranch:
			target, ok := instructionJumpTarget(ins)
			if !ok || !merge(target, scratch) || !merge(pc+1, scratch) {
				return false
			}
		default:
			if !merge(pc+1, scratch) {
				return false
			}
		}
	}

	if fallsThrough || returns == 0 {
		return false
	}
	function.calls = function.calls[:0]
	for pc, ok := range siteOK {
		if ok {
			function.calls = append(function.calls, sites[pc])
		}
	}
	return true
}

func compactOneResultReturn(code []instruction, pc int, ins instruction) bool {
	if ins.op != opReturn {
		return false
	}
	if ins.b == 1 {
		return true
	}
	if ins.b >= 0 {
		return false
	}
	prefixCount := -ins.b - 1
	return prefixCount == 0 && compactOpenResultProducer(code, pc-1, ins.a)
}

func compactOpenResultProducer(code []instruction, pc, result int) bool {
	if pc < 0 || pc >= len(code) {
		return false
	}
	ins := code[pc]
	return ins.op == opCall && ins.a == result && ins.d < 0
}

func (builder *compactProgramBuilder) callSite(functionIndex, pc int, facts []uint32) (compactCallSite, bool) {
	function := &builder.functions[functionIndex]
	if pc < 0 || pc >= len(function.code) || pc >= len(function.boundaries)-1 {
		return compactCallSite{}, false
	}
	ins := function.code[pc]
	var target uint16
	var ok bool
	argumentStart := 0
	argumentCount := 0
	borrowed := false

	switch ins.op {
	case opCall:
		if ins.b < 0 || ins.b >= len(facts) {
			return compactCallSite{}, false
		}
		target, ok = compactFactFunction(facts[ins.b])
		if !ok {
			return compactCallSite{}, false
		}
		argumentStart = ins.b + 1
		if ins.c >= 0 {
			argumentCount = ins.c
		} else {
			prefixCount, marked := decodeOpenArgumentCallMarker(ins.c)
			if !marked {
				prefixCount = -ins.c - 1
			}
			if prefixCount < 0 {
				return compactCallSite{}, false
			}
			openStart := argumentStart + prefixCount
			if !compactOpenResultProducer(function.code, pc-1, openStart) {
				return compactCallSite{}, false
			}
			argumentCount = prefixCount + 1
			borrowed = marked
		}
		if ins.d != 1 {
			if ins.d >= 0 {
				return compactCallSite{}, false
			}
			if _, fixed := decodeFixedMultiResultCount(ins.d, function.proto.registers); fixed {
				return compactCallSite{}, false
			}
			borrowed = borrowed || decodeOpenResultCallMarker(ins.d)
		}
	case opCallOne:
		if ins.b < 0 || ins.b >= len(facts) {
			return compactCallSite{}, false
		}
		target, ok = compactFactFunction(facts[ins.b])
		if !ok {
			return compactCallSite{}, false
		}
		argumentStart = ins.b + 1
		argumentCount, borrowed = decodeFixedCallCount(ins.c)
	case opCallLocalOne:
		if ins.b < 0 || ins.b >= len(facts) {
			return compactCallSite{}, false
		}
		target, ok = compactFactFunction(facts[ins.b])
		if !ok {
			return compactCallSite{}, false
		}
		argumentStart = ins.c
		argumentCount, borrowed = decodeFixedCallCount(ins.d)
	case opCallUpvalueOne:
		if ins.b < 0 || ins.b >= len(function.upvalueTargets) || function.upvalueTargets[ins.b] < 0 {
			return compactCallSite{}, false
		}
		target = uint16(function.upvalueTargets[ins.b])
		argumentStart = ins.c
		argumentCount, borrowed = decodeFixedCallCount(ins.d)
	default:
		return compactCallSite{}, false
	}

	if int(target) >= len(builder.functions) || ins.a < 0 || ins.a > math.MaxUint8 ||
		argumentStart < 0 || argumentStart > math.MaxUint8 ||
		argumentCount < 0 || argumentCount > math.MaxUint8 ||
		function.boundaries[pc] < 0 || uint64(function.boundaries[pc]) > uint64(^uint32(0)) {
		return compactCallSite{}, false
	}
	flags := uint8(0)
	if borrowed {
		flags |= compactCallBorrowed
	}
	return compactCallSite{
		wordPC:        uint32(function.boundaries[pc]),
		target:        target,
		argumentStart: uint8(argumentStart),
		argumentCount: uint8(argumentCount),
		result:        uint8(ins.a),
		flags:         flags,
	}, true
}

func (program *compactProgram) callSite(functionID uint16, pc int) (compactCallSite, bool) {
	if program == nil || int(functionID) >= len(program.functions) || pc < 0 {
		return compactCallSite{}, false
	}
	function := program.functions[functionID]
	if pc >= int(function.wordCount) {
		return compactCallSite{}, false
	}
	lookupIndex := int(function.callLookupStart) + pc
	if lookupIndex >= len(program.callByWord) {
		return compactCallSite{}, false
	}
	callIndex := program.callByWord[lookupIndex]
	if callIndex == 0 {
		return compactCallSite{}, false
	}
	return program.calls[callIndex-1], true
}

func runCompactCallProgram(proto *Proto, args []Value, state *slotExecutionState) ([]Value, bool, error) {
	return runCompactCallProgramWithController(proto, args, state, nil)
}

func runCompactCallProgramWithController(proto *Proto, args []Value, state *slotExecutionState, controller *executionController) ([]Value, bool, error) {
	if controller != nil {
		if err := controller.enterCall(); err != nil {
			return nil, true, newRuntimeErrorWithController(err, numericRuntimeFrames(proto, 0), controller)
		}
		entryDepth := controller.callDepth - 1
		defer func() {
			for controller.callDepth > entryDepth {
				controller.leaveCall()
			}
		}()
	}
	if proto == nil || proto.compact == nil || state == nil || int(proto.compact.entry) >= len(proto.compact.functions) {
		return nil, false, nil
	}
	entry := proto.compact.functions[proto.compact.entry].proto
	if entry == nil || len(args) < entry.params {
		return nil, false, nil
	}
	stack := state.prepareNumericRegisters(entry.registers)
	initialRemaining := int64(0)
	if controller != nil {
		initialRemaining = controller.remaining
	}
	for index := 0; index < entry.params; index++ {
		if valueKind(args[index]) != NumberKind {
			return nil, false, nil
		}
		stack[index] = valueNumber(args[index])
	}
	state.compactFrames = state.compactFrames[:0]
	result, ok, windowErr := runCompactCallProgramWordsWithController(proto.compact, state, controller)
	if windowErr != nil {
		return nil, true, windowErr
	}
	if !ok || slotExecutionNumberNeedsBox(result) {
		if controller != nil && ok {
			controller.restoreInstructionRemaining(initialRemaining)
		}
		return nil, false, nil
	}
	return []Value{NumberValue(result)}, true, nil
}

func runCompactCallProgramWords(program *compactProgram, state *slotExecutionState) (float64, bool) {
	result, ok, _ := runCompactCallProgramWordsWithController(program, state, nil)
	return result, ok
}

func runCompactCallProgramWordsWithController(program *compactProgram, state *slotExecutionState, controller *executionController) (result float64, ok bool, windowErr error) {
	if program == nil || state == nil || int(program.entry) >= len(program.functions) {
		return 0, false, nil
	}
	functionID := program.entry
	function := program.functions[functionID]
	if function.proto == nil {
		return 0, false, nil
	}
	stack := state.numericRegisters
	frames := state.compactFrames[:0]
	base := 0
	top := function.proto.registers
	pc := 0
	words := function.proto.words
	constants := function.proto.constantNumbers
	window := newExecutionWindow(controller)
	defer func() {
		if !ok && windowErr == nil {
			controller.recordSpeculativeRemaining(window.remaining)
		}
		if windowErr != nil {
			windowErr = newRuntimeErrorWithController(windowErr, compactRuntimeFrames(program, functionID, pc, frames), controller)
		}
		if ok || windowErr != nil {
			window.commit()
		}
	}()

	for {
		if uint(pc) >= uint(len(words)) {
			state.numericRegisters = stack
			state.compactFrames = frames
			return 0, false, nil
		}
		if err := window.stepInstruction(); err != nil {
			return 0, false, err
		}
		raw := words[pc]
		op := opcode(uint8(raw) & uint8(wordcodeOpcodeMask))
		a := int(uint8(raw >> 8))
		b := int(uint8(raw >> 16))
		c := int(uint8(raw >> 24))
		next := pc + 1

		switch op {
		case opClosure:
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return 0, false, nil
				}
				next++
			}

		case opLoadConst:
			b = int(uint16(raw >> 16))
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return 0, false, nil
				}
				b = int(int32(words[next]))
				next++
			}
			stack[base+a] = constants[b]

		case opMove:
			stack[base+a] = stack[base+b]

		case opAdd:
			stack[base+a] = stack[base+b] + stack[base+c]
		case opSub:
			stack[base+a] = stack[base+b] - stack[base+c]
		case opMul:
			stack[base+a] = stack[base+b] * stack[base+c]
		case opDiv:
			stack[base+a] = stack[base+b] / stack[base+c]
		case opMod:
			left, right := stack[base+b], stack[base+c]
			stack[base+a] = left - math.Floor(left/right)*right
		case opIDiv:
			stack[base+a] = math.Floor(stack[base+b] / stack[base+c])
		case opPow:
			stack[base+a] = math.Pow(stack[base+b], stack[base+c])
		case opNeg:
			stack[base+a] = -stack[base+b]

		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return 0, false, nil
				}
				c = int(int32(words[next]))
				next++
			}
			left, right := stack[base+b], constants[c]
			switch op {
			case opAddK:
				stack[base+a] = left + right
			case opSubK:
				stack[base+a] = left - right
			case opMulK:
				stack[base+a] = left * right
			case opDivK:
				stack[base+a] = left / right
			case opModK:
				stack[base+a] = left - math.Floor(left/right)*right
			case opIDivK:
				stack[base+a] = math.Floor(left / right)
			}

		case opNumericForCheck:
			if next >= len(words) {
				return 0, false, nil
			}
			target := int(int32(words[next]))
			next++
			target += next
			loopValue, limitValue, stepValue := stack[base+a], stack[base+b], stack[base+c]
			if math.IsNaN(loopValue) || math.IsNaN(limitValue) || math.IsNaN(stepValue) {
				return 0, false, nil
			}
			if (stepValue > 0 && loopValue > limitValue) || (stepValue <= 0 && loopValue < limitValue) {
				pc = target
				continue
			}

		case opNumericForLoop:
			if next >= len(words) {
				return 0, false, nil
			}
			target := int(int32(words[next]))
			next++
			target += next
			stack[base+a] += stack[base+b]
			pc = target
			continue

		case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			if next >= len(words) {
				return 0, false, nil
			}
			b = int(uint16(raw >> 16))
			target := int(int32(words[next]))
			next++
			target += next
			left, right := stack[base+a], constants[b]
			jump := false
			switch op {
			case opJumpIfNotEqualK:
				jump = left != right
			default:
				if math.IsNaN(left) || math.IsNaN(right) {
					return 0, false, nil
				}
				switch op {
				case opJumpIfNotLessK:
					jump = !(left < right)
				case opJumpIfNotGreaterK:
					jump = !(left > right)
				case opJumpIfLessK:
					jump = left < right
				case opJumpIfGreaterK:
					jump = left > right
				}
			}
			if jump {
				pc = target
				continue
			}

		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			if next >= len(words) {
				return 0, false, nil
			}
			target := int(int32(words[next]))
			next++
			target += next
			left, right := stack[base+a], stack[base+b]
			if math.IsNaN(left) || math.IsNaN(right) {
				return 0, false, nil
			}
			jump := false
			switch op {
			case opJumpIfNotLess:
				jump = !(left < right)
			case opJumpIfNotGreater:
				jump = !(left > right)
			case opJumpIfLess:
				jump = left < right
			case opJumpIfGreater:
				jump = left > right
			}
			if jump {
				pc = target
				continue
			}

		case opJump:
			pc = int(int32(raw)>>8) + next
			continue

		case opCall, opCallOne, opCallLocalOne, opCallUpvalueOne:
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return 0, false, nil
				}
				next++
			}
			site, ok := program.callSite(functionID, pc)
			if !ok || int(site.target) >= len(program.functions) {
				return 0, false, nil
			}
			callee := program.functions[site.target]
			if callee.proto == nil || int(site.argumentCount) != callee.proto.params {
				return 0, false, nil
			}
			if controller != nil {
				if err := controller.enterCall(); err != nil {
					return 0, false, err
				}
			}
			callerTop := top
			calleeBase := top
			if site.flags&compactCallBorrowed != 0 {
				calleeBase = base + int(site.argumentStart)
			}
			need := calleeBase + callee.proto.registers
			if calleeBase < 0 || need < calleeBase || uint64(need) > math.MaxUint32 {
				return 0, false, nil
			}
			stack = growCompactNumericStack(state, stack, need)
			if stack == nil {
				return 0, false, nil
			}
			if site.flags&compactCallBorrowed == 0 {
				argumentStart := base + int(site.argumentStart)
				argumentEnd := argumentStart + int(site.argumentCount)
				if argumentStart < 0 || argumentEnd > len(stack) {
					return 0, false, nil
				}
				copy(stack[calleeBase:calleeBase+int(site.argumentCount)], stack[argumentStart:argumentEnd])
			}
			frames = append(frames, compactCallFrame{
				callerFunction: functionID,
				returnPC:       uint32(next),
				callerBase:     uint32(base),
				callerTop:      uint32(callerTop),
				resultBase:     uint32(base + int(site.result)),
			})
			state.compactFrames = frames
			functionID = site.target
			function = callee
			base = calleeBase
			if need > top {
				top = need
			}
			pc = 0
			words = function.proto.words
			constants = function.proto.constantNumbers
			continue

		case opReturnOne, opReturn:
			resultIndex := base + a
			if resultIndex < 0 || resultIndex >= len(stack) {
				return 0, false, nil
			}
			result := stack[resultIndex]
			if len(frames) == 0 {
				state.numericRegisters = stack
				state.compactFrames = frames
				return result, true, nil
			}
			last := len(frames) - 1
			if controller != nil {
				controller.leaveCall()
			}
			record := frames[last]
			frames[last] = compactCallFrame{}
			frames = frames[:last]
			if int(record.callerFunction) >= len(program.functions) || int(record.resultBase) >= len(stack) {
				return 0, false, nil
			}
			functionID = record.callerFunction
			function = program.functions[functionID]
			base = int(record.callerBase)
			top = int(record.callerTop)
			pc = int(record.returnPC)
			stack[int(record.resultBase)] = result
			words = function.proto.words
			constants = function.proto.constantNumbers
			continue

		default:
			return 0, false, nil
		}
		pc = next
	}
}

func compactRuntimeFrames(program *compactProgram, functionID uint16, pc int, callers []compactCallFrame) []ScriptFrame {
	if program == nil || int(functionID) >= len(program.functions) {
		return nil
	}
	frames := make([]ScriptFrame, 0, len(callers)+1)
	appendFunction := func(id uint16, wordPC int) {
		if int(id) >= len(program.functions) {
			return
		}
		proto := program.functions[id].proto
		if frame, ok := runtimeScriptFrame(proto, wordPC); ok {
			frames = append(frames, frame)
		}
	}
	appendFunction(functionID, pc)
	for index := len(callers) - 1; index >= 0; index-- {
		record := callers[index]
		if int(record.callerFunction) >= len(program.functions) {
			continue
		}
		appendFunction(record.callerFunction, previousWordcodeInstruction(program.functions[record.callerFunction].proto, int(record.returnPC)))
	}
	return frames
}

func growCompactNumericStack(state *slotExecutionState, stack []float64, count int) []float64 {
	if state == nil || count < 0 {
		return nil
	}
	if count <= len(stack) {
		return stack
	}
	if count <= cap(stack) {
		stack = stack[:count]
		state.numericRegisters = stack
		return stack
	}
	capacity := cap(stack) * 2
	if capacity < 16 {
		capacity = 16
	}
	if capacity < count {
		capacity = count
	}
	grown := make([]float64, count, capacity)
	copy(grown, stack)
	state.numericRegisters = grown
	return grown
}
