package ember

import "math"

const directFixedSelfCallTracePlanBytes = 28

const directFixedSelfContinuationBytes = 16

type directFixedSelfContinuation struct {
	base                 uint32
	callerPreviousLength uint32
	returnPC             uint32
	destination          uint8
	flags                uint8
	_                    uint16
}

const directFixedSelfContinuationFlagCallerBorrowed = 1

const directCompactSelfFunctionPlanBytes = 200

type directCompactSelfOperation struct {
	wordPC   int32
	constant int32
	op       opcode
	a        uint8
	b        uint8
	c        uint8
}

// directCompactSelfFunctionPlan is a bounded loader-decoded program for one
// closed unary numeric function. It contains no closure, Proto, Value, or
// source identity; execution revalidates the live self upvalue at entry.
type directCompactSelfFunctionPlan struct {
	startPC        int32
	operationCount uint8
	registers      uint8
	upvalue        uint8
	_              uint8
	operations     [directCompactSelfFunctionInstructionCap]directCompactSelfOperation
}

// directFixedSelfCallTracePlan is the pointer-free load-time form of one
// Move+numeric+fixed-call superword. It retains only physical PCs, operand
// roles, and a numeric constant index; callee identity is always re-read from
// canonical upvalue state at execution time.
type directFixedSelfCallTracePlan struct {
	startPC       int32
	numericPC     int32
	callPC        int32
	returnPC      int32
	constant      int32
	moveSource    uint8
	moveTarget    uint8
	numericOp     opcode
	upvalue       uint8
	destination   uint8
	argumentStart uint8
	argumentCount uint8
	compact       uint8
}

func tileDirectFixedSelfCallTraces(proto *Proto, shadow *directShadowCode) error {
	decoded, _, err := wordcodeDecodeWords(proto.words)
	if err != nil {
		return err
	}
	for index := 0; index+2 < len(decoded); index++ {
		plan, ok := buildDirectFixedSelfCallTracePlan(proto, decoded[index:index+3])
		if !ok || directTraceHasInteriorEntry(decoded, index, index+2) {
			continue
		}
		start := decoded[index]
		call := decoded[index+2]
		startWord := shadow.words[start.wordPC]
		if startWord.handler() != directHandlerID(opMove) {
			continue
		}
		callWord := shadow.words[call.wordPC]
		cacheIndex, ok := callWord.cacheIndex()
		if !ok || cacheIndex < 0 || cacheIndex >= len(shadow.caches) || shadow.caches[cacheIndex].layout() != directCacheCall {
			continue
		}
		planIndex := len(shadow.fixedSelfCallTraces)
		encoded, ok := shadow.caches[cacheIndex].withPlanIndex(planIndex)
		if !ok {
			continue
		}
		shadow.fixedSelfCallTraces = append(shadow.fixedSelfCallTraces, plan)
		if shadow.retainedBytes() > directShadowStateLimit(len(proto.words)) {
			shadow.fixedSelfCallTraces = append([]directFixedSelfCallTracePlan(nil), shadow.fixedSelfCallTraces[:planIndex]...)
			continue
		}
		shadow.caches[cacheIndex] = encoded
		shadow.words[start.wordPC] = newDirectShadowWord(startWord.raw(), directHandlerFixedSelfCallTrace, cacheIndex)
	}
	if directCompactSelfFunctionEligible(proto, decoded, shadow.fixedSelfCallTraces) {
		for index := range shadow.fixedSelfCallTraces {
			shadow.fixedSelfCallTraces[index].compact = 1
		}
		if plan, ok := buildDirectCompactSelfFunctionPlan(proto, decoded, shadow.fixedSelfCallTraces); ok {
			planIndex := len(shadow.compactSelfFunctions)
			shadow.compactSelfFunctions = append(shadow.compactSelfFunctions, plan)
			if shadow.retainedBytes() > directShadowStateLimit(len(proto.words)) {
				shadow.compactSelfFunctions = append([]directCompactSelfFunctionPlan(nil), shadow.compactSelfFunctions[:planIndex]...)
			} else {
				startPC := int(plan.startPC)
				shadow.words[startPC] = shadow.words[startPC].withHandler(directHandlerCompactSelfFunction)
			}
		}
	}
	return nil
}

func buildDirectCompactSelfFunctionPlan(
	proto *Proto,
	decoded []wordcodeDecoded,
	callTraces []directFixedSelfCallTracePlan,
) (directCompactSelfFunctionPlan, bool) {
	if proto == nil || len(decoded) == 0 || len(decoded) > directCompactSelfFunctionInstructionCap || proto.registers <= 0 || proto.registers > 256 {
		return directCompactSelfFunctionPlan{}, false
	}
	callUpvalue := -1
	for _, trace := range callTraces {
		if callUpvalue < 0 {
			callUpvalue = int(trace.upvalue)
		}
		if int(trace.upvalue) != callUpvalue {
			return directCompactSelfFunctionPlan{}, false
		}
	}
	if callUpvalue < 0 {
		return directCompactSelfFunctionPlan{}, false
	}
	indices := make(map[int]int, len(decoded))
	for index, entry := range decoded {
		indices[entry.wordPC] = index
	}
	plan := directCompactSelfFunctionPlan{
		startPC:        int32(decoded[0].wordPC),
		operationCount: uint8(len(decoded)),
		registers:      uint8(proto.registers),
		upvalue:        uint8(callUpvalue),
	}
	for index, entry := range decoded {
		ins := entry.ins
		operation := directCompactSelfOperation{
			wordPC: int32(entry.wordPC),
			op:     ins.op,
			a:      uint8(ins.a),
			b:      uint8(ins.b),
			c:      uint8(ins.c),
		}
		switch ins.op {
		case opLoadConst:
			operation.constant = int32(ins.b)
		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			operation.constant = int32(ins.c)
		case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			operation.constant = int32(ins.b)
			target, ok := indices[entry.nextWord+ins.d]
			if !ok || target >= 256 {
				return directCompactSelfFunctionPlan{}, false
			}
			operation.c = uint8(target)
		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			target, ok := indices[entry.nextWord+ins.d]
			if !ok || target >= 256 {
				return directCompactSelfFunctionPlan{}, false
			}
			operation.c = uint8(target)
		case opJump:
			target, ok := indices[entry.nextWord+ins.b]
			if !ok || target >= 256 {
				return directCompactSelfFunctionPlan{}, false
			}
			operation.c = uint8(target)
		}
		plan.operations[index] = operation
	}
	return plan, true
}

func (code *directShadowCode) compactSelfFunctionAt(startPC int) (*directCompactSelfFunctionPlan, bool) {
	for index := range code.compactSelfFunctions {
		if int(code.compactSelfFunctions[index].startPC) == startPC {
			return &code.compactSelfFunctions[index], true
		}
	}
	return nil, false
}

type directNumericRegisterFacts [4]uint64

// directCompactSelfFunctionEligible proves that every reachable operation is
// numeric, effect-free, and closed except for a tiled fixed self-call. The
// intersection dataflow makes unchecked executor reads safe on every CFG path.
func directCompactSelfFunctionEligible(
	proto *Proto,
	decoded []wordcodeDecoded,
	plans []directFixedSelfCallTracePlan,
) bool {
	if proto == nil || proto.variadic || proto.params != 1 || len(proto.capturedLocals) != 0 ||
		proto.registers <= 0 || proto.registers > 256 || len(decoded) == 0 ||
		len(decoded) > directCompactSelfFunctionInstructionCap || len(plans) == 0 {
		return false
	}
	callPlans := make(map[int]directFixedSelfCallTracePlan, len(plans))
	for _, plan := range plans {
		callPlans[int(plan.callPC)] = plan
	}
	indices := make(map[int]int, len(decoded))
	for index, entry := range decoded {
		indices[entry.wordPC] = index
	}
	successors := make([][]int, len(decoded))
	for index, entry := range decoded {
		control := opcodeControlFlow(entry.ins.op)
		if control != opcodeControlReturn && control != opcodeControlJump && index+1 < len(decoded) {
			successors[index] = append(successors[index], index+1)
		}
		if displacement, ok := instructionJumpTarget(entry.ins); ok {
			target, ok := indices[entry.nextWord+displacement]
			if !ok {
				return false
			}
			successors[index] = append(successors[index], target)
		}
		if control != opcodeControlReturn && len(successors[index]) == 0 {
			return false
		}
	}
	reachable := make([]bool, len(decoded))
	work := []int{0}
	for len(work) != 0 {
		index := work[len(work)-1]
		work = work[:len(work)-1]
		if reachable[index] {
			continue
		}
		reachable[index] = true
		work = append(work, successors[index]...)
	}

	top := directAllNumericRegisterFacts(proto.registers)
	states := make([]directNumericRegisterFacts, len(decoded))
	for index := range states {
		if reachable[index] {
			states[index] = top
		}
	}
	states[0] = directNumericRegisterFacts{}
	states[0].add(0)
	for changed := true; changed; {
		changed = false
		for index, entry := range decoded {
			if !reachable[index] {
				continue
			}
			out, ok := directCompactSelfTransfer(proto, entry, states[index], callPlans, false)
			if !ok {
				return false
			}
			for _, successor := range successors[index] {
				merged := states[successor].intersection(out)
				if merged != states[successor] {
					states[successor] = merged
					changed = true
				}
			}
		}
	}
	returns := 0
	for index, entry := range decoded {
		if !reachable[index] {
			continue
		}
		if _, ok := directCompactSelfTransfer(proto, entry, states[index], callPlans, true); !ok {
			return false
		}
		if entry.ins.op == opReturnOne {
			returns++
		}
	}
	return returns != 0
}

func directCompactSelfTransfer(
	proto *Proto,
	entry wordcodeDecoded,
	input directNumericRegisterFacts,
	callPlans map[int]directFixedSelfCallTracePlan,
	requireNumeric bool,
) (directNumericRegisterFacts, bool) {
	ins := entry.ins
	out := input
	writes := instructionRegistersBounded(ins, instructionRegisterWrite, proto.registers)
	for register, ok := writes.next(); ok; register, ok = writes.next() {
		out.remove(register)
	}
	require := func(registers ...int) bool {
		if !requireNumeric {
			return true
		}
		for _, register := range registers {
			if !input.contains(register) {
				return false
			}
		}
		return true
	}
	setResult := func(register int, numeric bool) {
		if numeric {
			out.add(register)
		}
	}
	switch ins.op {
	case opLoadConst:
		numeric := ins.b >= 0 && ins.b < len(proto.constantNumberOK) && proto.constantNumberOK[ins.b]
		if !numeric {
			return out, false
		}
		out.add(ins.a)
	case opMove:
		if !require(ins.b) {
			return out, false
		}
		setResult(ins.a, input.contains(ins.b))
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		if !require(ins.b, ins.c) {
			return out, false
		}
		setResult(ins.a, input.contains(ins.b) && input.contains(ins.c))
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		if ins.c < 0 || ins.c >= len(proto.constantNumberOK) || !proto.constantNumberOK[ins.c] || !require(ins.b) {
			return out, false
		}
		setResult(ins.a, input.contains(ins.b))
	case opNeg:
		if !require(ins.b) {
			return out, false
		}
		setResult(ins.a, input.contains(ins.b))
	case opCallUpvalueOne:
		plan, ok := callPlans[entry.wordPC]
		if !ok || int(plan.argumentStart) != ins.c || int(plan.destination) != ins.a || !require(ins.c) {
			return out, false
		}
		setResult(ins.a, input.contains(ins.c))
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
		if ins.b < 0 || ins.b >= len(proto.constantNumberOK) || !proto.constantNumberOK[ins.b] || !require(ins.a) {
			return out, false
		}
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		if !require(ins.a, ins.b) {
			return out, false
		}
	case opJump:
	case opReturnOne:
		if !require(ins.a) {
			return out, false
		}
	default:
		return out, false
	}
	return out, true
}

func directAllNumericRegisterFacts(registers int) directNumericRegisterFacts {
	var facts directNumericRegisterFacts
	for register := 0; register < registers; register++ {
		facts.add(register)
	}
	return facts
}

func (facts *directNumericRegisterFacts) add(register int) {
	facts[register/64] |= uint64(1) << (register % 64)
}

func (facts *directNumericRegisterFacts) remove(register int) {
	facts[register/64] &^= uint64(1) << (register % 64)
}

func (facts directNumericRegisterFacts) contains(register int) bool {
	return register >= 0 && register < 256 && facts[register/64]&(uint64(1)<<(register%64)) != 0
}

func (facts directNumericRegisterFacts) intersection(other directNumericRegisterFacts) directNumericRegisterFacts {
	for index := range facts {
		facts[index] &= other[index]
	}
	return facts
}

func buildDirectFixedSelfCallTracePlan(proto *Proto, trace []wordcodeDecoded) (directFixedSelfCallTracePlan, bool) {
	if proto == nil || len(trace) != directFixedSelfCallTraceInstructionCap || proto.variadic ||
		len(proto.capturedLocals) != 0 || proto.registers < 0 || proto.registers > 1<<8 {
		return directFixedSelfCallTracePlan{}, false
	}
	move, numeric, call := trace[0], trace[1], trace[2]
	if move.ins.op != opMove || move.nextWord != numeric.wordPC || numeric.nextWord != call.wordPC ||
		call.ins.op != opCallUpvalueOne || move.ins.a != numeric.ins.a || numeric.ins.a != numeric.ins.b {
		return directFixedSelfCallTracePlan{}, false
	}
	constant, ok := directFixedSelfCallTraceNumeric(proto, numeric.ins)
	if !ok {
		return directFixedSelfCallTracePlan{}, false
	}
	argumentCount, borrowHint := decodeFixedCallCount(call.ins.d)
	if !borrowHint || argumentCount != 1 || call.ins.c != move.ins.a || call.ins.a != move.ins.a ||
		move.ins.a < 0 || move.ins.a >= proto.registers || move.ins.b < 0 || move.ins.b >= proto.registers ||
		call.ins.b < 0 || call.ins.b >= len(proto.upvalues) || call.nextWord > math.MaxInt32 ||
		move.wordPC > math.MaxInt32 || numeric.wordPC > math.MaxInt32 || call.wordPC > math.MaxInt32 {
		return directFixedSelfCallTracePlan{}, false
	}
	return directFixedSelfCallTracePlan{
		startPC:       int32(move.wordPC),
		numericPC:     int32(numeric.wordPC),
		callPC:        int32(call.wordPC),
		returnPC:      int32(call.nextWord),
		constant:      int32(constant),
		moveSource:    uint8(move.ins.b),
		moveTarget:    uint8(move.ins.a),
		numericOp:     numeric.ins.op,
		upvalue:       uint8(call.ins.b),
		destination:   uint8(call.ins.a),
		argumentStart: uint8(call.ins.c),
		argumentCount: uint8(argumentCount),
	}, true
}

func directFixedSelfCallTraceNumeric(proto *Proto, ins instruction) (int, bool) {
	switch ins.op {
	case opNeg:
		return -1, true
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		if ins.c >= 0 && ins.c < len(proto.constantNumberOK) && proto.constantNumberOK[ins.c] {
			return ins.c, true
		}
	}
	return 0, false
}

func (code *directShadowCode) fixedSelfCallTraceAt(startPC int, cache directAdaptiveCacheCell) (*directFixedSelfCallTracePlan, bool) {
	index, ok := cache.planIndex()
	if !ok || index < 0 || index >= len(code.fixedSelfCallTraces) || int(code.fixedSelfCallTraces[index].startPC) != startPC {
		return nil, false
	}
	return &code.fixedSelfCallTraces[index], true
}

func (thread *vmThread) enterDirectFixedSelfCallTrace(
	frame *vmFrame,
	plan *directFixedSelfCallTracePlan,
	constantNumbers []float64,
) bool {
	if plan != nil && plan.compact != 0 {
		return thread.enterDirectCompactSelfCall(frame, plan, constantNumbers)
	}
	return thread.enterDirectFixedSelfCallTraceRecord(frame, plan, constantNumbers)
}

func (thread *vmThread) enterDirectCompactSelfCall(
	frame *vmFrame,
	plan *directFixedSelfCallTracePlan,
	constantNumbers []float64,
) bool {
	if thread == nil || frame == nil || plan == nil || frame.proto == nil || frame.currentClosure == nil {
		return false
	}
	rootEntry := len(thread.fixedSelfContinuations) == 0
	if rootEntry {
		if frame.currentClosure.proto != frame.proto || frame.proto.variadic || frame.proto.params != 1 ||
			len(frame.proto.capturedLocals) != 0 || frame.varargCount != 0 || len(frame.cells) != 0 ||
			thread.controller != nil || thread.debugHook != nil || thread.coroutine != nil ||
			thread.nonYieldableDepth != 0 || thread.nearestProtectedFrame != noProtectedFrame ||
			frame.hasPendingCall || frame.openResultStart >= 0 || frame.hasOpenRange() || frame.callDepthCharged ||
			frame.debugLine() != -1 {
			return false
		}
		callee, ok := directClosureUpvalue(frame.currentClosure, int(plan.upvalue))
		if !ok {
			return false
		}
		closure, ok := callee.scriptFunction()
		if !ok || closure != frame.currentClosure {
			return false
		}
	}
	sourceRegister := int(plan.moveSource)
	if sourceRegister < 0 || sourceRegister >= len(frame.registers) {
		return false
	}
	source := frame.registers[sourceRegister]
	if valueKind(source) != NumberKind {
		return false
	}
	constant := 0.0
	if plan.constant >= 0 {
		constantIndex := int(plan.constant)
		if constantIndex >= len(constantNumbers) {
			return false
		}
		constant = constantNumbers[constantIndex]
	}
	numericResult, ok := directFixedSelfCallTraceResult(plan.numericOp, valueNumber(source), constant)
	if !ok {
		return false
	}
	owner := frame.window.owner
	if owner == nil {
		owner = frame.owner
	}
	callerBase := frame.registerBase
	childBase := callerBase + int(plan.argumentStart)
	childEnd := childBase + frame.proto.registers
	previousLength := 0
	if owner != nil {
		previousLength = len(owner.values)
	}
	if owner == nil || owner != thread.stackOwner || callerBase < 0 || childBase < callerBase || childEnd < childBase ||
		uint64(callerBase) > math.MaxUint32 || uint64(childBase) > math.MaxUint32 ||
		frame.window.previousStackLength < 0 || uint64(frame.window.previousStackLength) > math.MaxUint32 ||
		uint64(previousLength) > math.MaxUint32 {
		return false
	}
	flags := uint8(0)
	if frame.window.borrowed {
		flags = directFixedSelfContinuationFlagCallerBorrowed
	}
	thread.fixedSelfContinuations = append(thread.fixedSelfContinuations, directFixedSelfContinuation{
		base:                 uint32(callerBase),
		callerPreviousLength: uint32(frame.window.previousStackLength),
		returnPC:             uint32(plan.returnPC),
		destination:          plan.destination,
		flags:                flags,
	})
	if childEnd > previousLength {
		if childEnd <= cap(owner.values) {
			owner.values = owner.values[:childEnd]
			thread.stack = owner.values
		} else {
			thread.growStack(childEnd)
			owner = thread.stackOwner
		}
	}
	registers := owner.values[childBase:childEnd]
	registers[0] = NumberValue(numericResult)
	frame.registerBase = childBase
	frame.registerCount = len(registers)
	frame.owner = owner
	frame.registers = registers
	frame.varargOwner = nil
	frame.varargBase = childEnd
	frame.varargCount = 0
	frame.pc = 0
	frame.window = vmRegisterWindow{
		owner:               owner,
		base:                childBase,
		length:              len(registers),
		previousStackLength: previousLength,
		borrowed:            true,
	}
	return true
}

func (thread *vmThread) resumeDirectCompactSelfCall(frame *vmFrame, value Value) bool {
	if thread == nil || frame == nil || frame.proto == nil || len(thread.fixedSelfContinuations) == 0 {
		return false
	}
	last := len(thread.fixedSelfContinuations) - 1
	record := thread.fixedSelfContinuations[last]
	thread.fixedSelfContinuations = thread.fixedSelfContinuations[:last]
	base := int(record.base)
	top := base + frame.proto.registers
	owner := frame.window.owner
	if owner == nil {
		owner = frame.owner
	}
	destination := base + int(record.destination)
	if owner == nil || owner != thread.stackOwner || base < 0 || top < base || top > len(owner.values) || destination < base || destination >= top {
		return false
	}
	owner.values = owner.values[:top]
	thread.stack = owner.values
	frame.registerBase = base
	frame.registerCount = frame.proto.registers
	frame.owner = owner
	frame.registers = owner.values[base:top]
	frame.varargOwner = nil
	frame.varargBase = top
	frame.varargCount = 0
	frame.pc = int(record.returnPC)
	frame.window = vmRegisterWindow{
		owner:               owner,
		base:                base,
		length:              frame.proto.registers,
		previousStackLength: int(record.callerPreviousLength),
		borrowed:            record.flags&directFixedSelfContinuationFlagCallerBorrowed != 0,
	}
	owner.values[destination] = value
	return true
}

// runDirectCompactSelfFunction checks every fallible runtime guard before the
// first mutation, then executes only the loader-proved numeric program against
// canonical owner-backed Values. One physical frame is rebound as recursion
// advances; scalar continuations preserve caller state without semantic roots.
func (thread *vmThread) runDirectCompactSelfFunction(
	frame *vmFrame,
	plan *directCompactSelfFunctionPlan,
	constantNumbers []float64,
) (Value, int, bool) {
	if thread == nil || frame == nil || plan == nil || frame.proto == nil || frame.currentClosure == nil ||
		frame.currentClosure.proto != frame.proto || frame.proto.variadic || frame.proto.params != 1 ||
		len(frame.proto.capturedLocals) != 0 || frame.proto.registers != int(plan.registers) ||
		plan.operationCount == 0 || int(plan.operationCount) > len(plan.operations) ||
		thread.controller != nil || thread.debugHook != nil || thread.coroutine != nil ||
		thread.nonYieldableDepth != 0 || thread.nearestProtectedFrame != noProtectedFrame ||
		frame.hasPendingCall || frame.openResultStart >= 0 || frame.hasOpenRange() || frame.callDepthCharged ||
		frame.debugLine() != -1 || len(frame.cells) != 0 || len(thread.fixedSelfContinuations) != 0 || len(frame.registers) == 0 ||
		valueKind(frame.registers[0]) != NumberKind {
		return NilValue(), 0, false
	}
	callee, ok := directClosureUpvalue(frame.currentClosure, int(plan.upvalue))
	if !ok {
		return NilValue(), 0, false
	}
	closure, ok := callee.scriptFunction()
	if !ok || closure != frame.currentClosure {
		return NilValue(), 0, false
	}
	owner := frame.window.owner
	if owner == nil {
		owner = frame.owner
	}
	rootBase := frame.registerBase
	rootTop := rootBase + frame.proto.registers
	if owner == nil || owner != thread.stackOwner || rootBase < 0 || rootTop < rootBase || rootTop > len(owner.values) {
		return NilValue(), 0, false
	}
	rootPreviousLength := frame.window.previousStackLength
	rootBorrowed := frame.window.borrowed
	rootContinuationDepth := len(thread.fixedSelfContinuations)
	highWater := len(owner.values)
	base := rootBase
	operationIndex := 0
	operationCount := int(plan.operationCount)
	for {
		operation := plan.operations[operationIndex]
		frame.pc = int(operation.wordPC)
		registers := owner.values[base : base+frame.proto.registers]
		nextOperation := operationIndex + 1
		switch operation.op {
		case opLoadConst:
			registers[operation.a] = NumberValue(constantNumbers[operation.constant])
		case opMove:
			registers[operation.a] = registers[operation.b]
		case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
			left := valueNumber(registers[operation.b])
			right := valueNumber(registers[operation.c])
			registers[operation.a] = NumberValue(directCompactSelfBinary(operation.op, left, right))
		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			left := valueNumber(registers[operation.b])
			right := constantNumbers[operation.constant]
			registers[operation.a] = NumberValue(directCompactSelfBinary(operation.op, left, right))
		case opNeg:
			registers[operation.a] = NumberValue(-valueNumber(registers[operation.b]))
		case opJumpIfNotEqualK:
			if valueNumber(registers[operation.a]) != constantNumbers[operation.constant] {
				nextOperation = int(operation.c)
			}
		case opJumpIfNotLessK:
			if !(valueNumber(registers[operation.a]) < constantNumbers[operation.constant]) {
				nextOperation = int(operation.c)
			}
		case opJumpIfNotGreaterK:
			if !(valueNumber(registers[operation.a]) > constantNumbers[operation.constant]) {
				nextOperation = int(operation.c)
			}
		case opJumpIfLessK:
			if valueNumber(registers[operation.a]) < constantNumbers[operation.constant] {
				nextOperation = int(operation.c)
			}
		case opJumpIfGreaterK:
			if valueNumber(registers[operation.a]) > constantNumbers[operation.constant] {
				nextOperation = int(operation.c)
			}
		case opJumpIfNotLess:
			if !(valueNumber(registers[operation.a]) < valueNumber(registers[operation.b])) {
				nextOperation = int(operation.c)
			}
		case opJumpIfNotGreater:
			if !(valueNumber(registers[operation.a]) > valueNumber(registers[operation.b])) {
				nextOperation = int(operation.c)
			}
		case opJumpIfLess:
			if valueNumber(registers[operation.a]) < valueNumber(registers[operation.b]) {
				nextOperation = int(operation.c)
			}
		case opJumpIfGreater:
			if valueNumber(registers[operation.a]) > valueNumber(registers[operation.b]) {
				nextOperation = int(operation.c)
			}
		case opJump:
			nextOperation = int(operation.c)
		case opCallUpvalueOne:
			previousLength := len(owner.values)
			childBase := base + int(operation.c)
			childEnd := childBase + frame.proto.registers
			if childBase < base || childEnd < childBase || frame.window.previousStackLength < 0 ||
				uint64(frame.window.previousStackLength) > math.MaxUint32 || uint64(base) > math.MaxUint32 ||
				uint64(previousLength) > math.MaxUint32 || uint64(nextOperation) > math.MaxUint32 {
				panic("ember: compact self-function stack exceeded its bounded continuation representation")
			}
			flags := uint8(directFixedSelfContinuationFlagCallerBorrowed)
			if base == rootBase && !rootBorrowed {
				flags = 0
			}
			thread.fixedSelfContinuations = append(thread.fixedSelfContinuations, directFixedSelfContinuation{
				base:                 uint32(base),
				callerPreviousLength: uint32(frame.window.previousStackLength),
				returnPC:             uint32(nextOperation),
				destination:          operation.a,
				flags:                flags,
			})
			argument := registers[operation.c]
			if childEnd > previousLength {
				if childEnd <= cap(owner.values) {
					owner.values = owner.values[:childEnd]
					thread.stack = owner.values
				} else {
					thread.growStack(childEnd)
					owner = thread.stackOwner
				}
			}
			if len(owner.values) > highWater {
				highWater = len(owner.values)
			}
			base = childBase
			registers = owner.values[base : base+frame.proto.registers]
			registers[0] = argument
			frame.registerBase = base
			frame.registerCount = frame.proto.registers
			frame.owner = owner
			frame.registers = registers
			frame.varargOwner = nil
			frame.varargBase = childEnd
			frame.varargCount = 0
			frame.pc = int(plan.operations[0].wordPC)
			frame.window = vmRegisterWindow{owner: owner, base: base, length: frame.proto.registers, previousStackLength: previousLength, borrowed: true}
			nextOperation = 0
		case opReturnOne:
			value := registers[operation.a]
			returnPC := int(operation.wordPC)
			if len(thread.fixedSelfContinuations) == rootContinuationDepth {
				values := owner.values[:highWater]
				clear(values[rootTop:])
				owner.values = values[:rootTop]
				thread.stack = owner.values
				frame.registerBase = rootBase
				frame.registerCount = frame.proto.registers
				frame.owner = owner
				frame.registers = owner.values[rootBase:rootTop]
				frame.varargOwner = nil
				frame.varargBase = rootTop
				frame.varargCount = 0
				frame.pc = returnPC
				frame.window = vmRegisterWindow{owner: owner, base: rootBase, length: frame.proto.registers, previousStackLength: rootPreviousLength, borrowed: rootBorrowed}
				return value, returnPC, true
			}
			last := len(thread.fixedSelfContinuations) - 1
			record := thread.fixedSelfContinuations[last]
			thread.fixedSelfContinuations = thread.fixedSelfContinuations[:last]
			base = int(record.base)
			top := base + frame.proto.registers
			owner.values = owner.values[:top]
			thread.stack = owner.values
			registers = owner.values[base:top]
			registers[int(record.destination)] = value
			frame.registerBase = base
			frame.registerCount = frame.proto.registers
			frame.owner = owner
			frame.registers = registers
			frame.varargOwner = nil
			frame.varargBase = top
			frame.varargCount = 0
			frame.pc = int(plan.operations[record.returnPC].wordPC)
			frame.window = vmRegisterWindow{
				owner:               owner,
				base:                base,
				length:              frame.proto.registers,
				previousStackLength: int(record.callerPreviousLength),
				borrowed:            record.flags&directFixedSelfContinuationFlagCallerBorrowed != 0,
			}
			nextOperation = int(record.returnPC)
		default:
			panic("ember: invalid compact self-function operation")
		}
		if nextOperation < 0 || nextOperation >= operationCount {
			panic("ember: compact self-function escaped its bounded plan")
		}
		operationIndex = nextOperation
	}
}

func directCompactSelfBinary(op opcode, left float64, right float64) float64 {
	switch op {
	case opAdd, opAddK:
		return left + right
	case opSub, opSubK:
		return left - right
	case opMul, opMulK:
		return left * right
	case opDiv, opDivK:
		return left / right
	case opMod, opModK:
		return left - math.Floor(left/right)*right
	case opIDiv, opIDivK:
		return math.Floor(left / right)
	case opPow:
		return math.Pow(left, right)
	default:
		panic("ember: invalid compact self-function numeric operation")
	}
}

func (thread *vmThread) enterDirectFixedSelfCallTraceRecord(
	frame *vmFrame,
	plan *directFixedSelfCallTracePlan,
	constantNumbers []float64,
) bool {
	if thread == nil || frame == nil || plan == nil || frame.currentClosure == nil ||
		frame.currentClosure.proto != frame.proto || frame.proto == nil || frame.proto.variadic ||
		len(frame.proto.capturedLocals) != 0 || frame.varargCount != 0 || len(frame.cells) != 0 ||
		thread.controller != nil || thread.debugHook != nil || thread.coroutine != nil ||
		thread.nonYieldableDepth != 0 || thread.nearestProtectedFrame != noProtectedFrame ||
		frame.hasPendingCall || frame.openResultStart >= 0 || frame.hasOpenRange() || frame.callDepthCharged ||
		frame.debugLine() != -1 || frame.registerBase < 0 || frame.depth < 0 || frame.depth > math.MaxUint16 {
		return false
	}
	sourceRegister := int(plan.moveSource)
	targetRegister := int(plan.moveTarget)
	if sourceRegister >= len(frame.registers) || targetRegister >= len(frame.registers) {
		return false
	}
	source := frame.registers[sourceRegister]
	if valueKind(source) != NumberKind {
		return false
	}
	constant := 0.0
	if plan.constant >= 0 {
		constantIndex := int(plan.constant)
		if constantIndex >= len(constantNumbers) {
			return false
		}
		constant = constantNumbers[constantIndex]
	}
	sourceNumber := valueNumber(source)
	numericResult, ok := directFixedSelfCallTraceResult(plan.numericOp, sourceNumber, constant)
	if !ok {
		return false
	}
	callee, ok := directClosureUpvalue(frame.currentClosure, int(plan.upvalue))
	if !ok {
		return false
	}
	closure, ok := callee.scriptFunction()
	if !ok || closure != frame.currentClosure {
		return false
	}
	owner := frame.window.owner
	if owner == nil {
		owner = frame.owner
	}
	callerBase := frame.registerBase
	callerTop := callerBase + frame.registerCount
	childBase := callerBase + int(plan.argumentStart)
	childEnd := childBase + frame.proto.registers
	destination := callerBase + int(plan.destination)
	if owner == nil {
		return false
	}
	previousLength := len(owner.values)
	if owner != thread.stackOwner || callerTop < callerBase || childBase < callerBase ||
		childEnd < childBase || destination < callerBase || destination >= callerTop ||
		uint64(callerBase) > math.MaxUint32 || uint64(callerTop) > math.MaxUint32 ||
		uint64(childBase) > math.MaxUint32 || uint64(destination) > math.MaxUint32 ||
		uint64(previousLength) > math.MaxUint32 {
		return false
	}
	if childEnd > previousLength {
		thread.growStack(childEnd)
		owner = thread.stackOwner
	}

	flags := vmFrameRecordFlagRecordOnly | vmFrameRecordFlagFixedSelfCall | vmFrameRecordFlagFixedSelfCallTrace
	if frame.window.borrowed {
		flags |= vmFrameRecordFlagCallerBorrowed
	}
	recordBaseDepth := frame.recordBaseDepth
	if recordBaseDepth < 0 {
		recordBaseDepth = len(thread.frameRecords)
	}
	record := vmFrameRecord{
		closure:           closure,
		returnPC:          uint32(plan.returnPC),
		base:              uint32(callerBase),
		top:               uint32(callerTop),
		resultDestination: uint32(destination),
		resultCount:       1,
		argumentBase:      uint32(childBase),
		varargBase:        uint32(previousLength),
		frameDepth:        uint16(frame.depth),
		argumentCount:     uint16(plan.argumentCount),
		flags:             flags,
	}
	thread.frameRecords = append(thread.frameRecords, record)
	if len(thread.frameRecords) > thread.maxFrameRecords {
		thread.maxFrameRecords = len(thread.frameRecords)
	}

	registers := owner.values[childBase:childEnd]
	for _, register := range frame.proto.entryNilRegisters {
		registers[register] = NilValue()
	}
	for register := int(plan.argumentCount); register < frame.proto.params && register < len(registers); register++ {
		registers[register] = NilValue()
	}
	if frame.proto.params > 0 {
		registers[0] = NumberValue(numericResult)
	}
	frame.registerBase = childBase
	frame.registerCount = len(registers)
	frame.owner = owner
	frame.registers = registers
	frame.varargOwner = nil
	frame.varargBase = childEnd
	frame.varargCount = 0
	frame.pc = 0
	frame.recordBaseDepth = recordBaseDepth
	frame.window = vmRegisterWindow{
		owner:               owner,
		base:                childBase,
		length:              len(registers),
		previousStackLength: previousLength,
		borrowed:            true,
	}
	return true
}

func directClosureUpvalue(closure *closure, index int) (Value, bool) {
	if closure == nil || index < 0 {
		return NilValue(), false
	}
	if index < len(closure.upvalueValueOK) && closure.upvalueValueOK[index] {
		return closure.upvalueValues[index], true
	}
	if index >= len(closure.upvalues) || closure.upvalues[index] == nil {
		return NilValue(), false
	}
	return closure.upvalues[index].get(), true
}

func (thread *vmThread) resumeDirectFixedSelfCallTrace(frame *vmFrame, record vmFrameRecord, value Value) bool {
	if thread == nil || frame == nil || record.closure == nil || frame.currentClosure != record.closure ||
		frame.proto != record.closure.proto || frame.proto == nil || frame.proto.variadic ||
		len(frame.proto.capturedLocals) != 0 || frame.varargCount != 0 || len(frame.cells) != 0 ||
		frame.hasPendingCall || frame.openResultStart >= 0 || frame.hasOpenRange() ||
		len(thread.frameRecords) == 0 {
		return false
	}
	base := int(record.base)
	top := int(record.top)
	returnPC := int(record.returnPC)
	destination := int(record.resultDestination)
	previousLength := int(record.varargBase)
	owner := frame.window.owner
	if owner == nil {
		owner = frame.owner
	}
	childStart := frame.registerBase
	childEnd := childStart + frame.registerCount
	if owner == nil || owner != thread.stackOwner || base < 0 || top < base || top > len(owner.values) ||
		destination < base || destination >= top || childStart < 0 || childEnd < childStart || childEnd > len(owner.values) {
		return false
	}

	clear(owner.values[childStart:childEnd])
	owner.values = owner.values[:top]
	thread.stack = owner.values
	last := len(thread.frameRecords) - 1
	thread.frameRecords[last] = vmFrameRecord{}
	thread.frameRecords = thread.frameRecords[:last]

	frame.registerBase = base
	frame.registerCount = top - base
	frame.owner = owner
	frame.registers = owner.values[base:top]
	frame.varargOwner = nil
	frame.varargBase = top
	frame.varargCount = 0
	frame.pc = returnPC
	frame.window = vmRegisterWindow{
		owner:               owner,
		base:                base,
		length:              top - base,
		previousStackLength: previousLength,
		borrowed:            record.flags&vmFrameRecordFlagCallerBorrowed != 0,
	}
	if frame.recordBaseDepth >= 0 && len(thread.frameRecords) == frame.recordBaseDepth {
		frame.recordBaseDepth = -1
	}
	owner.values[destination] = value
	return true
}

func directFixedSelfCallTraceResult(op opcode, source float64, constant float64) (float64, bool) {
	switch op {
	case opAddK:
		return source + constant, true
	case opSubK:
		return source - constant, true
	case opMulK:
		return source * constant, true
	case opDivK:
		return source / constant, true
	case opModK:
		return source - math.Floor(source/constant)*constant, true
	case opIDivK:
		return math.Floor(source / constant), true
	case opNeg:
		return -source, true
	}
	return 0, false
}
