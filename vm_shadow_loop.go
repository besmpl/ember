package ember

import (
	"fmt"
	"math"
)

const directCompactLoopPlanBytes = 272

const directCompactDispatchEntryCap = 16

type directCompactLoopKind uint8

const (
	directCompactLoopNumeric directCompactLoopKind = iota + 1
	directCompactLoopArray
	directCompactLoopNestedDispatch
)

// directCompactLoopPlan is a bounded natural-loop functionlet over canonical
// frame registers. Operations retain only physical PCs, operands, constants,
// and cache IDs. On a guard miss, prior completed effects stay canonical and
// dispatch resumes at the exact failed operation.
type directCompactLoopPlan struct {
	startPC        int32
	exitPC         int32
	operationCount uint8
	kind           directCompactLoopKind
	_              uint16
	_              int32
	operations     [directCompactLoopInstructionCap]directCompactLeafOperation
}

// directCompactDispatchEntry is transient loop-invariant data. It exists only
// on the executor stack, so the shadow retains neither semantic roots nor a
// callee variant. Every call still uses the same guarded leaf mechanism.
type directCompactDispatchEntry struct {
	event    Value
	key      Value
	amount   Value
	callee   Value
	closure  *closure
	instance *vmFunctionInstance
	plan     *directCompactLeafFunctionPlan
}

func tileDirectCompactLoops(proto *Proto, shadow *directShadowCode) error {
	decoded, _, err := wordcodeDecodeWords(proto.words)
	if err != nil {
		return err
	}
	for startIndex, entry := range decoded {
		endIndex := -1
		exitPC := -1
		kind := directCompactLoopKind(0)
		switch entry.ins.op {
		case opNumericForCheck:
			endIndex, exitPC = directCompactNumericLoop(decoded, startIndex)
			kind = directCompactLoopNumeric
		case opArrayNextJump2:
			endIndex, exitPC = directCompactArrayLoop(decoded, startIndex)
			kind = directCompactLoopArray
		default:
			continue
		}
		if endIndex < startIndex || exitPC < 0 || endIndex-startIndex+1 > directCompactLoopInstructionCap ||
			directTraceHasInteriorEntry(decoded, startIndex, endIndex) {
			continue
		}
		startWord := shadow.words[entry.wordPC]
		if startWord.handler() != directHandlerID(entry.ins.op) {
			continue
		}
		plan, ok := buildDirectCompactLoopPlan(proto, decoded[startIndex:endIndex+1], kind, exitPC)
		if !ok {
			continue
		}
		planIndex := len(shadow.compactLoops)
		shadow.compactLoops = append(shadow.compactLoops, plan)
		if shadow.retainedBytes() > directShadowStateLimit(len(proto.words)) {
			shadow.compactLoops = append([]directCompactLoopPlan(nil), shadow.compactLoops[:planIndex]...)
			continue
		}
		shadow.words[entry.wordPC] = startWord.withHandler(directHandlerCompactLoop)
	}
	return nil
}

func directCompactNumericLoop(decoded []wordcodeDecoded, startIndex int) (int, int) {
	if startIndex < 0 || startIndex >= len(decoded) {
		return -1, -1
	}
	start := decoded[startIndex]
	exitPC := start.nextWord + start.ins.d
	for index := startIndex + 1; index < len(decoded) && index-startIndex+1 <= directCompactLoopInstructionCap; index++ {
		entry := decoded[index]
		if entry.wordPC >= exitPC {
			break
		}
		if entry.ins.op != opNumericForLoop {
			continue
		}
		if entry.nextWord+entry.ins.d == start.wordPC && entry.nextWord == exitPC &&
			entry.ins.a == start.ins.a && entry.ins.b == start.ins.c {
			return index, exitPC
		}
	}
	return -1, -1
}

func directCompactArrayLoop(decoded []wordcodeDecoded, startIndex int) (int, int) {
	if startIndex < 0 || startIndex >= len(decoded) {
		return -1, -1
	}
	start := decoded[startIndex]
	exitPC := start.nextWord + start.ins.d
	exitIndex := -1
	for index := startIndex + 1; index < len(decoded); index++ {
		if decoded[index].wordPC == exitPC {
			exitIndex = index
			break
		}
		if decoded[index].wordPC > exitPC {
			break
		}
	}
	if exitIndex <= startIndex+1 {
		return -1, -1
	}
	endIndex := exitIndex - 1
	end := decoded[endIndex]
	if end.ins.op != opJump || end.nextWord+end.ins.b != start.wordPC || end.nextWord != exitPC {
		return -1, -1
	}
	return endIndex, exitPC
}

func buildDirectCompactLoopPlan(
	proto *Proto,
	decoded []wordcodeDecoded,
	kind directCompactLoopKind,
	exitPC int,
) (directCompactLoopPlan, bool) {
	if proto == nil || proto.registers <= 0 || proto.registers > 256 || len(decoded) < 2 ||
		len(decoded) > directCompactLoopInstructionCap || exitPC < 0 || exitPC > math.MaxInt32 {
		return directCompactLoopPlan{}, false
	}
	indices := make(map[int]int, len(decoded))
	for index, entry := range decoded {
		indices[entry.wordPC] = index
	}
	plan := directCompactLoopPlan{
		startPC:        int32(decoded[0].wordPC),
		exitPC:         int32(exitPC),
		operationCount: uint8(len(decoded)),
		kind:           kind,
	}
	for index, entry := range decoded {
		operation, ok := buildDirectCompactLoopOperation(proto, entry, indices, index, len(decoded), kind)
		if !ok {
			return directCompactLoopPlan{}, false
		}
		plan.operations[index] = operation
	}
	if kind == directCompactLoopNumeric && directCompactLoopMatchesNestedDispatch(&plan) {
		plan.kind = directCompactLoopNestedDispatch
	}
	return plan, true
}

// directCompactLoopMatchesNestedDispatch recognizes a semantic loop fusion,
// not a source shape: a numeric loop contains a clean array traversal whose
// element selects a callable, supplies one numeric field plus an induction
// transform, and reduces the numeric result. Constant descriptors remain
// operands and field/callee identities are deliberately absent.
func directCompactLoopMatchesNestedDispatch(plan *directCompactLoopPlan) bool {
	if plan == nil || plan.operationCount != 15 {
		return false
	}
	ops := plan.operations[:plan.operationCount]
	if ops[0].op != opNumericForCheck || ops[1].op != opMove || ops[2].op != opPrepareIter ||
		ops[3].op != opArrayNextJump2 || ops[4].op != opGetStringField || ops[5].op != opGetIndex ||
		ops[6].op != opMove || ops[7].op != opGetStringField || ops[8].op != opMove ||
		!directCompactNumberConstantOperation(ops[9].op) || !directCompactNumberBinaryOperation(ops[10].op) ||
		ops[11].op != opCallOne || !directCompactNumberBinaryOperation(ops[12].op) ||
		ops[13].op != opJump || ops[14].op != opNumericForLoop {
		return false
	}
	elementRegister := ops[3].a + 1
	if ops[1].a != ops[2].a ||
		ops[3].a != ops[2].c || ops[3].b != ops[2].a || ops[3].c != ops[2].b || ops[3].d != 14 ||
		ops[4].b != elementRegister || ops[5].c != ops[4].a || ops[7].b != elementRegister ||
		ops[8].b != ops[0].a || ops[9].a != ops[8].a || ops[9].b != ops[8].a ||
		ops[10].a != ops[7].a || !directCompactBinaryReads(ops[10], ops[7].a, ops[9].a) ||
		ops[11].a != ops[5].a || ops[11].b != ops[5].a || ops[11].c != 2 || ops[11].d != 1 ||
		ops[6].a != ops[11].b+1 || ops[10].a != ops[11].b+2 ||
		!directCompactReducingBinary(ops[12], ops[11].a) || ops[13].d != 3 ||
		ops[14].a != ops[0].a || ops[14].b != ops[0].c {
		return false
	}
	controllerRegisters := [3]uint8{ops[0].a, ops[0].b, ops[0].c}
	invariantRegisters := [3]uint8{ops[1].b, ops[5].b, ops[6].b}
	writes := []uint8{
		ops[1].a,
		ops[2].a, ops[2].b, ops[2].c,
		ops[3].a, ops[3].a + 1,
		ops[4].a, ops[5].a, ops[6].a, ops[7].a, ops[8].a,
		ops[9].a, ops[10].a, ops[11].a, ops[12].a,
	}
	for _, write := range writes {
		for _, controller := range controllerRegisters {
			if write == controller {
				return false
			}
		}
		for _, invariant := range invariantRegisters {
			if write == invariant {
				return false
			}
		}
	}
	return true
}

func directCompactNumberConstantOperation(op opcode) bool {
	switch op {
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		return true
	default:
		return false
	}
}

func directCompactNumberBinaryOperation(op opcode) bool {
	switch op {
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		return true
	default:
		return false
	}
}

func directCompactBinaryReads(operation directCompactLeafOperation, first uint8, second uint8) bool {
	return operation.b == first && operation.c == second || operation.b == second && operation.c == first
}

func directCompactReducingBinary(operation directCompactLeafOperation, resultRegister uint8) bool {
	return operation.a == operation.b && operation.c == resultRegister ||
		operation.a == operation.c && operation.b == resultRegister
}

func buildDirectCompactLoopOperation(
	proto *Proto,
	entry wordcodeDecoded,
	indices map[int]int,
	index int,
	count int,
	kind directCompactLoopKind,
) (directCompactLeafOperation, bool) {
	ins := entry.ins
	register := func(values ...int) bool {
		for _, value := range values {
			if value < 0 || value >= proto.registers {
				return false
			}
		}
		return true
	}
	operation := directCompactLeafOperation{wordPC: int32(entry.wordPC), op: ins.op, a: uint8(ins.a), b: uint8(ins.b), c: uint8(ins.c), d: uint8(ins.d)}
	if index == 0 {
		switch kind {
		case directCompactLoopNumeric:
			if ins.op != opNumericForCheck || !register(ins.a, ins.b, ins.c) {
				return directCompactLeafOperation{}, false
			}
			return operation, true
		case directCompactLoopArray:
			if ins.op != opArrayNextJump2 || !register(ins.a, ins.a+1, ins.b, ins.c) {
				return directCompactLeafOperation{}, false
			}
			return operation, true
		}
	}
	if index == count-1 {
		switch kind {
		case directCompactLoopNumeric:
			if ins.op != opNumericForLoop || !register(ins.a, ins.b) {
				return directCompactLeafOperation{}, false
			}
			operation.d = 0
			return operation, true
		case directCompactLoopArray:
			if ins.op != opJump {
				return directCompactLeafOperation{}, false
			}
			operation.d = 0
			return operation, true
		}
	}
	switch ins.op {
	case opPrepareIter:
		if !register(ins.a, ins.b, ins.c) {
			return directCompactLeafOperation{}, false
		}
		return operation, true
	case opArrayNextJump2:
		if !register(ins.a, ins.a+1, ins.b, ins.c) {
			return directCompactLeafOperation{}, false
		}
		target, ok := indices[entry.nextWord+ins.d]
		if !ok || target < 0 || target >= count || target > math.MaxUint8 {
			return directCompactLeafOperation{}, false
		}
		operation.d = uint8(target)
		return operation, true
	case opJump:
		target, ok := indices[entry.nextWord+ins.b]
		if !ok || target < 0 || target >= count || target > math.MaxUint8 {
			return directCompactLeafOperation{}, false
		}
		operation.d = uint8(target)
		return operation, true
	case opGetIndex:
		if !register(ins.a, ins.b, ins.c) || proto.cacheIndex == nil {
			return directCompactLeafOperation{}, false
		}
		cacheID, _, ok := proto.cacheIndex.cacheSiteAt(entry.wordPC)
		if !ok || cacheID > math.MaxUint16 {
			return directCompactLeafOperation{}, false
		}
		operation.cacheID = uint16(cacheID)
		return operation, true
	case opCallOne:
		argCount, borrowHint := decodeFixedCallCount(ins.c)
		if !borrowHint || argCount < 1 || argCount > 3 || !register(ins.a, ins.b, ins.b+argCount) {
			return directCompactLeafOperation{}, false
		}
		operation.c = uint8(argCount)
		operation.d = 1
		return operation, true
	case opFastCall:
		nativeID := nativeFuncID(ins.b)
		if nativeID != nativeFuncTableInsert && nativeID != nativeFuncTableRemove {
			break
		}
		if ins.c < 1 || ins.c > math.MaxUint8 || ins.d < 0 || ins.d > math.MaxUint8 ||
			!register(ins.a, ins.a+ins.c-1) || (ins.d > 0 && !register(ins.a+ins.d-1)) {
			return directCompactLeafOperation{}, false
		}
		return operation, true
	}
	operation, ok := buildDirectCompactLeafOperation(proto, entry, indices, index)
	if !ok || operation.op == opReturnOne || operation.op == opFastCall && nativeFuncID(operation.b) != nativeFuncMathMin {
		return directCompactLeafOperation{}, false
	}
	return operation, true
}

func (code *directShadowCode) compactLoopAt(startPC int) (*directCompactLoopPlan, bool) {
	for index := range code.compactLoops {
		if int(code.compactLoops[index].startPC) == startPC {
			return &code.compactLoops[index], true
		}
	}
	return nil, false
}

func (thread *vmThread) runDirectCompactLoop(
	frame *vmFrame,
	instance *vmFunctionInstance,
	plan *directCompactLoopPlan,
) (int, int, error, bool) {
	if plan == nil {
		return 0, 0, nil, false
	}
	if thread == nil || frame == nil || instance == nil || frame.proto == nil ||
		thread.controller != nil || thread.debugHook != nil || thread.coroutine != nil ||
		thread.nonYieldableDepth != 0 || thread.nearestProtectedFrame != noProtectedFrame ||
		frame.hasPendingCall || frame.callDepthCharged || frame.debugLine() != -1 ||
		plan.operationCount == 0 || int(plan.operationCount) > len(plan.operations) {
		return int(plan.startPC), int(plan.startPC), nil, false
	}
	// No accepted operation can replace a global intrinsic, so one entry guard
	// remains valid for the whole bounded loop. Guarding before the first
	// iteration also guarantees that a miss occurs before any loop effect.
	if !directCompactLoopIntrinsicsUnchanged(thread.globals, plan) {
		return int(plan.startPC), int(plan.startPC), nil, false
	}
	registers := frame.registers
	if plan.kind == directCompactLoopNestedDispatch {
		return thread.runDirectCompactNestedDispatchLoop(frame, instance, plan)
	}
	operationIndex := 0
	for {
		if operationIndex < 0 || operationIndex >= int(plan.operationCount) {
			return int(plan.startPC), int(plan.startPC), nil, false
		}
		operation := plan.operations[operationIndex]
		failedPC := int(operation.wordPC)
		nextOperation := operationIndex + 1
		switch operation.op {
		case opNumericForCheck:
			loopValue := registers[operation.a]
			limitValue := registers[operation.b]
			stepValue := registers[operation.c]
			if valueKind(loopValue) != NumberKind || valueKind(limitValue) != NumberKind || valueKind(stepValue) != NumberKind ||
				math.IsNaN(valueNumber(loopValue)) || math.IsNaN(valueNumber(limitValue)) || math.IsNaN(valueNumber(stepValue)) {
				return failedPC, failedPC, nil, false
			}
			loop := valueNumber(loopValue)
			limit := valueNumber(limitValue)
			step := valueNumber(stepValue)
			if step > 0 && loop > limit || step <= 0 && loop < limit {
				return int(plan.exitPC), failedPC, nil, true
			}
		case opNumericForLoop:
			loopValue := registers[operation.a]
			stepValue := registers[operation.b]
			if valueKind(loopValue) != NumberKind || valueKind(stepValue) != NumberKind {
				return failedPC, failedPC, nil, false
			}
			registers[operation.a] = NumberValue(valueNumber(loopValue) + valueNumber(stepValue))
			nextOperation = 0
		case opPrepareIter:
			iterValue := registers[operation.a]
			iterTable := iterValue.tableRef()
			if !tableCanIterateCleanArray(iterTable) {
				return failedPC, failedPC, nil, false
			}
			registers[operation.a] = valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncArrayNext)
			registers[operation.b] = iterValue
			registers[operation.c] = NilValue()
		case opArrayNextJump2:
			callee := registers[operation.b]
			tableValue := registers[operation.c]
			table := tableValue.tableRef()
			if valueNativeID(callee) != nativeFuncArrayNext || table == nil {
				return failedPC, failedPC, nil, false
			}
			controlValue := registers[operation.a]
			index := 0
			if valueKind(controlValue) != NilKind {
				if valueKind(controlValue) != NumberKind {
					return failedPC, failedPC, nil, false
				}
				index = int(valueNumber(controlValue))
				if float64(index) != valueNumber(controlValue) {
					return failedPC, failedPC, nil, false
				}
			}
			frame.clearOpenResultState()
			next := index + 1
			if next < 1 || next > len(table.array) {
				registers[operation.a] = NilValue()
				registers[operation.a+1] = NilValue()
				if plan.kind == directCompactLoopArray && operationIndex == 0 {
					return int(plan.exitPC), failedPC, nil, true
				}
				nextOperation = int(operation.d)
				break
			}
			registers[operation.a] = NumberValue(float64(next))
			registers[operation.a+1] = table.array[next-1]
		case opLoadConst:
			registers[operation.a] = NumberValue(frame.proto.constantNumbers[operation.constant])
		case opMove:
			registers[operation.a] = registers[operation.b]
		case opGetStringField:
			base := registers[operation.b]
			table := base.tableRef()
			if table == nil || table.metatable != nil {
				return failedPC, failedPC, nil, false
			}
			cache := instance.fieldCacheAt(uint32(operation.cacheID))
			if cache == nil {
				return failedPC, failedPC, nil, false
			}
			constant := int(operation.constant)
			value, ok := cache.get(table)
			if !ok {
				value, ok = cache.resolve(table, frame.proto.constantKeys[constant].str, frame.proto.constants[constant].stringBox())
			}
			if !ok {
				value = NilValue()
			}
			registers[operation.a] = value
		case opSetStringField:
			table := registers[operation.a].tableRef()
			value := registers[operation.c]
			if table == nil || table.metatable != nil || value.IsNil() {
				return failedPC, failedPC, nil, false
			}
			cache := instance.fieldCacheAt(uint32(operation.cacheID))
			if cache == nil {
				return failedPC, failedPC, nil, false
			}
			constant := int(operation.constant)
			if !cache.write(table, value) && !cache.resolveExistingWrite(
				table,
				frame.proto.constantKeys[constant].str,
				frame.proto.constants[constant].stringBox(),
				value,
			) {
				return failedPC, failedPC, nil, false
			}
		case opGetIndex:
			table := registers[operation.b].tableRef()
			key := registers[operation.c]
			if table == nil || table.metatable != nil {
				return failedPC, failedPC, nil, false
			}
			var value Value
			var ok bool
			if valueKind(key) == StringKind && key.stringBox() != nil {
				value, ok = table.rawStringFieldBox(key.stringBox())
			}
			if !ok {
				var err error
				value, err = table.rawGet(key)
				if err != nil {
					return failedPC, failedPC, nil, false
				}
			}
			registers[operation.a] = value
		case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
			left := registers[operation.b]
			right := registers[operation.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				return failedPC, failedPC, nil, false
			}
			registers[operation.a] = NumberValue(directCompactSelfBinary(operation.op, valueNumber(left), valueNumber(right)))
		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			left := registers[operation.b]
			if valueKind(left) != NumberKind {
				return failedPC, failedPC, nil, false
			}
			registers[operation.a] = NumberValue(directCompactSelfBinary(operation.op, valueNumber(left), frame.proto.constantNumbers[operation.constant]))
		case opNeg:
			value := registers[operation.b]
			if valueKind(value) != NumberKind {
				return failedPC, failedPC, nil, false
			}
			registers[operation.a] = NumberValue(-valueNumber(value))
		case opFastCall:
			nativeID := nativeFuncID(operation.b)
			start := int(operation.a)
			argCount := int(operation.c)
			resultCount := int(operation.d)
			switch nativeID {
			case nativeFuncTableInsert:
				if argCount != 2 {
					return failedPC, failedPC, nil, false
				}
				table := registers[start].tableRef()
				if table == nil || !table.canAppendFastArray() {
					return failedPC, failedPC, nil, false
				}
				if err := table.fastArrayAppendWithController(nil, registers[start+1]); err != nil {
					return failedPC, failedPC, fmt.Errorf("run: call failed: host function failed: %w", err), true
				}
				directFrameApplyCallIslandResults(frame, registers, start, resultCount, nil)
			case nativeFuncTableRemove:
				position := NilValue()
				if argCount > 1 {
					position = registers[start+1]
				}
				removed, ok, err := baseTableRemoveFastArrayValue(registers[start], position, argCount)
				if err != nil {
					return failedPC, failedPC, fmt.Errorf("run: call failed: host function failed: %w", err), true
				}
				if !ok {
					return failedPC, failedPC, nil, false
				}
				directFrameApplySingleCallIslandResult(frame, registers, start, resultCount, removed)
			case nativeFuncMathMin:
				if resultCount != 1 {
					return failedPC, failedPC, nil, false
				}
				minimum, err := baseMathMinValue(registers[start : start+argCount])
				if err != nil {
					return failedPC, failedPC, fmt.Errorf("run: call failed: host function failed: %w", err), true
				}
				directFrameApplySingleCallIslandResult(frame, registers, start, resultCount, NumberValue(minimum))
			default:
				return failedPC, failedPC, nil, false
			}
		case opCallOne:
			start := int(operation.b) + 1
			end := start + int(operation.c)
			value, ok := thread.runDirectCompactLeafCall(frame, registers[operation.b], registers[start:end])
			if !ok {
				return failedPC, failedPC, nil, false
			}
			directFrameApplySingleCallIslandResult(frame, registers, int(operation.a), 1, value)
			observeDirectCompactLeafCall(instance, failedPC)
		case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			value := registers[operation.a]
			if valueKind(value) != NumberKind {
				return failedPC, failedPC, nil, false
			}
			left := valueNumber(value)
			right := frame.proto.constantNumbers[operation.constant]
			jump := operation.op == opJumpIfNotEqualK && left != right ||
				operation.op == opJumpIfNotLessK && !(left < right) ||
				operation.op == opJumpIfNotGreaterK && !(left > right) ||
				operation.op == opJumpIfLessK && left < right ||
				operation.op == opJumpIfGreaterK && left > right
			if jump {
				nextOperation = int(operation.d)
			}
		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			left := registers[operation.a]
			right := registers[operation.b]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				return failedPC, failedPC, nil, false
			}
			leftNumber := valueNumber(left)
			rightNumber := valueNumber(right)
			jump := operation.op == opJumpIfNotLess && !(leftNumber < rightNumber) ||
				operation.op == opJumpIfNotGreater && !(leftNumber > rightNumber) ||
				operation.op == opJumpIfLess && leftNumber < rightNumber ||
				operation.op == opJumpIfGreater && leftNumber > rightNumber
			if jump {
				nextOperation = int(operation.d)
			}
		case opJump:
			nextOperation = int(operation.d)
		default:
			return failedPC, failedPC, nil, false
		}
		operationIndex = nextOperation
	}
}

func (thread *vmThread) runDirectCompactNestedDispatchLoop(
	frame *vmFrame,
	instance *vmFunctionInstance,
	plan *directCompactLoopPlan,
) (int, int, error, bool) {
	if plan == nil {
		return 0, 0, nil, false
	}
	if thread == nil || frame == nil || instance == nil || plan.kind != directCompactLoopNestedDispatch || plan.operationCount != 15 {
		return int(plan.startPC), int(plan.startPC), nil, false
	}
	registers := frame.registers
	ops := plan.operations[:plan.operationCount]
	loopValue := registers[ops[0].a]
	limitValue := registers[ops[0].b]
	stepValue := registers[ops[0].c]
	if valueKind(loopValue) != NumberKind || valueKind(limitValue) != NumberKind || valueKind(stepValue) != NumberKind ||
		math.IsNaN(valueNumber(loopValue)) || math.IsNaN(valueNumber(limitValue)) || math.IsNaN(valueNumber(stepValue)) {
		return int(plan.startPC), int(plan.startPC), nil, false
	}
	loop := valueNumber(loopValue)
	limit := valueNumber(limitValue)
	step := valueNumber(stepValue)
	if !directCompactNumericLoopContinues(loop, limit, step) {
		return int(plan.exitPC), int(plan.startPC), nil, true
	}
	if valueKind(registers[ops[12].a]) != NumberKind {
		return int(plan.startPC), int(plan.startPC), nil, false
	}

	var entries [directCompactDispatchEntryCap]directCompactDispatchEntry
	entryCount, ok := thread.resolveDirectCompactDispatchEntries(frame, instance, plan, &entries)
	if !ok {
		return int(plan.startPC), int(plan.startPC), nil, false
	}

	for directCompactNumericLoopContinues(loop, limit, step) {
		iterValue := registers[ops[1].b]
		registers[ops[1].a] = iterValue
		registers[ops[2].a] = valueWithRefAndNativeID(HostFuncKind, nil, nativeFuncArrayNext)
		registers[ops[2].b] = iterValue
		registers[ops[2].c] = NilValue()

		for entryIndex := 0; entryIndex < entryCount; entryIndex++ {
			entry := &entries[entryIndex]
			frame.clearOpenResultState()
			registers[ops[3].a] = NumberValue(float64(entryIndex + 1))
			registers[ops[3].a+1] = entry.event
			registers[ops[4].a] = entry.key
			registers[ops[5].a] = entry.callee
			registers[ops[6].a] = registers[ops[6].b]
			registers[ops[7].a] = entry.amount
			registers[ops[8].a] = registers[ops[8].b]
			if !directCompactApplyNumberOperation(frame.proto, registers, ops[9]) {
				return int(ops[9].wordPC), int(ops[9].wordPC), nil, false
			}
			if !directCompactApplyNumberOperation(frame.proto, registers, ops[10]) {
				return int(ops[10].wordPC), int(ops[10].wordPC), nil, false
			}

			callStart := int(ops[11].b) + 1
			args := registers[callStart : callStart+int(ops[11].c)]
			result, handled, guarded := runDirectCompactLeafFastPath(entry.closure, entry.instance, entry.plan.fastPath, args)
			if !guarded {
				return int(ops[11].wordPC), int(ops[11].wordPC), nil, false
			}
			if !handled {
				result, ok = thread.runDirectCompactLeafCall(frame, entry.callee, args)
				if !ok {
					return int(ops[11].wordPC), int(ops[11].wordPC), nil, false
				}
			}
			directFrameApplySingleCallIslandResult(frame, registers, int(ops[11].a), 1, result)
			observeDirectCompactLeafCall(instance, int(ops[11].wordPC))
			if !directCompactApplyNumberOperation(frame.proto, registers, ops[12]) {
				return int(ops[12].wordPC), int(ops[12].wordPC), nil, false
			}
		}

		frame.clearOpenResultState()
		registers[ops[3].a] = NilValue()
		registers[ops[3].a+1] = NilValue()
		loop += step
		registers[ops[14].a] = NumberValue(loop)
	}
	return int(plan.exitPC), int(plan.startPC), nil, true
}

func (thread *vmThread) resolveDirectCompactDispatchEntries(
	frame *vmFrame,
	instance *vmFunctionInstance,
	plan *directCompactLoopPlan,
	entries *[directCompactDispatchEntryCap]directCompactDispatchEntry,
) (int, bool) {
	if thread == nil || frame == nil || frame.proto == nil || instance == nil || plan == nil || entries == nil ||
		plan.operationCount != 15 {
		return 0, false
	}
	registers := frame.registers
	ops := plan.operations[:plan.operationCount]
	events := registers[ops[1].b].tableRef()
	dispatch := registers[ops[5].b].tableRef()
	state := registers[ops[6].b].tableRef()
	if !tableCanIterateCleanArray(events) || dispatch == nil || dispatch.metatable != nil ||
		state == nil || state.metatable != nil || events == dispatch || events == state || dispatch == state ||
		len(events.array) > len(entries) {
		return 0, false
	}
	for index, eventValue := range events.array {
		event := eventValue.tableRef()
		if event == nil || event.metatable != nil || event == events || event == dispatch || event == state {
			return 0, false
		}
		key, ok := directCompactLoopStringField(frame.proto, instance, event, ops[4])
		if !ok {
			return 0, false
		}
		amount, ok := directCompactLoopStringField(frame.proto, instance, event, ops[7])
		if !ok || valueKind(amount) != NumberKind {
			return 0, false
		}
		callee, ok := directCompactLoopRawIndex(dispatch, key)
		if !ok {
			return 0, false
		}
		closure, leafInstance, leafPlan, ok := thread.resolveDirectCompactDispatchLeaf(callee)
		if !ok {
			return 0, false
		}
		entries[index] = directCompactDispatchEntry{
			event:    eventValue,
			key:      key,
			amount:   amount,
			callee:   callee,
			closure:  closure,
			instance: leafInstance,
			plan:     leafPlan,
		}
	}
	return len(events.array), true
}

func (thread *vmThread) resolveDirectCompactDispatchLeaf(
	callee Value,
) (*closure, *vmFunctionInstance, *directCompactLeafFunctionPlan, bool) {
	closure, ok := callee.scriptFunction()
	if !ok || thread == nil || closure == nil || closure.proto == nil || closure.proto.variadic || closure.proto.params != 2 ||
		len(closure.proto.capturedLocals) != 0 || len(closure.proto.upvalues) != 0 ||
		len(closure.upvalues) != 0 || len(closure.upvalueValues) != 0 {
		return nil, nil, nil, false
	}
	instance, err := thread.executionShadowFunctionInstance(closure.proto)
	if err != nil || instance == nil {
		return nil, nil, nil, false
	}
	plan, ok := instance.shadow.compactLeafFunction()
	if !ok || plan.params != 2 || plan.operationCount == 0 || int(plan.operationCount) > len(plan.operations) ||
		plan.fastPath.kind == directCompactLeafFastPathNone || plan.fastPath.tableParam != 0 || plan.fastPath.valueParam != 1 {
		return nil, nil, nil, false
	}
	return closure, instance, plan, true
}

func directCompactLoopStringField(
	proto *Proto,
	instance *vmFunctionInstance,
	table *Table,
	operation directCompactLeafOperation,
) (Value, bool) {
	constant := int(operation.constant)
	if proto == nil || instance == nil || table == nil || table.metatable != nil ||
		constant < 0 || constant >= len(proto.constants) || constant >= len(proto.constantKeys) ||
		valueKind(proto.constants[constant]) != StringKind {
		return NilValue(), false
	}
	cache := instance.fieldCacheAt(uint32(operation.cacheID))
	if cache == nil {
		return NilValue(), false
	}
	value, ok := cache.get(table)
	if !ok {
		value, ok = cache.resolve(table, proto.constantKeys[constant].str, proto.constants[constant].stringBox())
	}
	if !ok {
		return NilValue(), true
	}
	return value, true
}

func directCompactLoopRawIndex(table *Table, key Value) (Value, bool) {
	if table == nil || table.metatable != nil {
		return NilValue(), false
	}
	if valueKind(key) == StringKind && key.stringBox() != nil {
		if value, ok := table.rawStringFieldBox(key.stringBox()); ok {
			return value, true
		}
	}
	value, err := table.rawGet(key)
	return value, err == nil
}

func directCompactApplyNumberOperation(proto *Proto, registers []Value, operation directCompactLeafOperation) bool {
	switch operation.op {
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		left := registers[operation.b]
		right := registers[operation.c]
		if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
			return false
		}
		registers[operation.a] = NumberValue(directCompactSelfBinary(operation.op, valueNumber(left), valueNumber(right)))
		return true
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		constant := int(operation.constant)
		left := registers[operation.b]
		if proto == nil || valueKind(left) != NumberKind || constant < 0 || constant >= len(proto.constantNumbers) {
			return false
		}
		registers[operation.a] = NumberValue(directCompactSelfBinary(operation.op, valueNumber(left), proto.constantNumbers[constant]))
		return true
	default:
		return false
	}
}

func directCompactNumericLoopContinues(loop float64, limit float64, step float64) bool {
	return step > 0 && loop <= limit || step <= 0 && loop >= limit
}

func directCompactLoopIntrinsicsUnchanged(globals *globalEnv, plan *directCompactLoopPlan) bool {
	if plan == nil || plan.operationCount == 0 || int(plan.operationCount) > len(plan.operations) {
		return false
	}
	for index := 0; index < int(plan.operationCount); index++ {
		operation := plan.operations[index]
		if operation.op == opFastCall && !fastCallNativeUnchanged(globals, nativeFuncID(operation.b)) {
			return false
		}
	}
	return true
}
