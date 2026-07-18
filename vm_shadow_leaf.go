package ember

import "math"

const directCompactLeafOperationBytes = 16

const directCompactLeafFunctionPlanBytes = 416

const directCompactLeafRegisterCap = 16

const directCompactLeafWriteCap = 4

// directCompactLeafOperation is the pointer-free load-time form of one
// operation in a small non-yielding numeric record closure. Constants and
// cache IDs are descriptors into the live Proto/function instance; no closure,
// table, Value, source, or benchmark identity is retained by the plan.
type directCompactLeafOperation struct {
	wordPC   int32
	constant int32
	cacheID  uint16
	op       opcode
	a        uint8
	b        uint8
	c        uint8
	d        uint8
	_        uint8
}

type directCompactLeafFastPathKind uint8

const (
	directCompactLeafFastPathNone directCompactLeafFastPathKind = iota
	directCompactLeafFastPathRecordUpdate
	directCompactLeafFastPathGuardedRecordUpdate
)

// directCompactLeafFastPath describes a frequent semantic superword: update
// one existing numeric record field from a numeric argument and return it,
// optionally when a preceding numeric field branch selects that tail. Field
// names remain ordinary constant descriptors and are guarded by live shapes.
type directCompactLeafFastPath struct {
	guardConstant      int32
	guardFieldConstant int32
	fieldConstant      int32
	guardCache         uint16
	fieldCache         uint16
	kind               directCompactLeafFastPathKind
	guardOp            opcode
	numericOp          opcode
	tableParam         uint8
	valueParam         uint8
	reverse            uint8
	_                  [2]byte
}

type directCompactLeafFunctionPlan struct {
	operationCount uint8
	registers      uint8
	params         uint8
	_              [5]byte
	operations     [directCompactLeafCallInstructionCap]directCompactLeafOperation
	fastPath       directCompactLeafFastPath
}

// tileDirectCompactLeafFunction decodes at most one bounded plan per Proto.
// It accepts semantic opcode shapes only. Runtime arguments, field shapes,
// metatables, and intrinsic bindings are guarded afresh at every call.
func tileDirectCompactLeafFunction(proto *Proto, shadow *directShadowCode) error {
	decoded, _, err := wordcodeDecodeWords(proto.words)
	if err != nil {
		return err
	}
	plan, ok := buildDirectCompactLeafFunctionPlan(proto, decoded)
	if !ok {
		return nil
	}
	shadow.compactLeafFunctions = append(shadow.compactLeafFunctions, plan)
	if shadow.retainedBytes() > directShadowStateLimit(len(proto.words)) {
		shadow.compactLeafFunctions = shadow.compactLeafFunctions[:0]
	}
	return nil
}

func buildDirectCompactLeafFunctionPlan(proto *Proto, decoded []wordcodeDecoded) (directCompactLeafFunctionPlan, bool) {
	if proto == nil || proto.variadic || proto.params < 1 || proto.params > 3 ||
		len(proto.capturedLocals) != 0 || len(proto.upvalues) != 0 || proto.registers < proto.params ||
		proto.registers <= 0 || proto.registers > directCompactLeafRegisterCap || len(decoded) == 0 ||
		len(decoded) > directCompactLeafCallInstructionCap {
		return directCompactLeafFunctionPlan{}, false
	}
	indices := make(map[int]int, len(decoded))
	for index, entry := range decoded {
		indices[entry.wordPC] = index
	}
	plan := directCompactLeafFunctionPlan{
		operationCount: uint8(len(decoded)),
		registers:      uint8(proto.registers),
		params:         uint8(proto.params),
	}
	for index, entry := range decoded {
		operation, ok := buildDirectCompactLeafOperation(proto, entry, indices, index)
		if !ok {
			return directCompactLeafFunctionPlan{}, false
		}
		plan.operations[index] = operation
	}
	writes := 0
	for _, entry := range decoded {
		if entry.ins.op == opSetStringField {
			writes++
		}
	}
	if writes > directCompactLeafWriteCap {
		return directCompactLeafFunctionPlan{}, false
	}
	if !directCompactLeafControlFlowReturns(plan) {
		return directCompactLeafFunctionPlan{}, false
	}
	plan.fastPath = buildDirectCompactLeafFastPath(proto, decoded)
	return plan, true
}

func buildDirectCompactLeafFastPath(proto *Proto, decoded []wordcodeDecoded) directCompactLeafFastPath {
	for tailStart := 0; tailStart < len(decoded); tailStart++ {
		fastPath, ok := matchDirectCompactRecordUpdateTail(proto, decoded, tailStart)
		if !ok {
			continue
		}
		if tailStart == 0 {
			fastPath.kind = directCompactLeafFastPathRecordUpdate
			return fastPath
		}
		// A guarded fast path may skip only a leading, read-only field test.
		// Keeping the guard at operations zero and one prevents the superword
		// from bypassing arbitrary work before the selected update tail.
		for branchIndex := 1; branchIndex == 1 && branchIndex < tailStart; branchIndex++ {
			branch := decoded[branchIndex]
			switch branch.ins.op {
			case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			default:
				continue
			}
			if branch.nextWord+branch.ins.d != decoded[tailStart].wordPC || branch.ins.a != decoded[branchIndex-1].ins.a ||
				branch.ins.b < 0 || branch.ins.b >= len(proto.constantNumberOK) || !proto.constantNumberOK[branch.ins.b] {
				continue
			}
			guard := decoded[branchIndex-1]
			if guard.ins.op != opGetStringField || guard.ins.b != int(fastPath.tableParam) || proto.cacheIndex == nil {
				continue
			}
			cacheID, descriptor, cacheOK := proto.cacheIndex.cacheSiteAt(guard.wordPC)
			if !cacheOK || cacheID > math.MaxUint16 || descriptor < 0 || descriptor >= len(proto.constants) ||
				valueKind(proto.constants[descriptor]) != StringKind {
				continue
			}
			fastPath.kind = directCompactLeafFastPathGuardedRecordUpdate
			fastPath.guardOp = branch.ins.op
			fastPath.guardConstant = int32(branch.ins.b)
			fastPath.guardFieldConstant = int32(descriptor)
			fastPath.guardCache = uint16(cacheID)
			return fastPath
		}
	}
	return directCompactLeafFastPath{}
}

func matchDirectCompactRecordUpdateTail(
	proto *Proto,
	decoded []wordcodeDecoded,
	start int,
) (directCompactLeafFastPath, bool) {
	if proto == nil || proto.cacheIndex == nil || start < 0 || start >= len(decoded) {
		return directCompactLeafFastPath{}, false
	}
	index := start
	valueRegister := -1
	valueParam := -1
	if decoded[index].ins.op == opMove {
		move := decoded[index].ins
		if move.b < 0 || move.b >= proto.params {
			return directCompactLeafFastPath{}, false
		}
		valueRegister = move.a
		valueParam = move.b
		index++
	}
	if len(decoded)-index != 5 {
		return directCompactLeafFastPath{}, false
	}
	get, numeric, set, reload, ret := decoded[index], decoded[index+1], decoded[index+2], decoded[index+3], decoded[index+4]
	if get.ins.op != opGetStringField || set.ins.op != opSetStringField || reload.ins.op != opGetStringField || ret.ins.op != opReturnOne ||
		get.ins.b < 0 || get.ins.b >= proto.params || set.ins.a != get.ins.b || reload.ins.b != get.ins.b || ret.ins.a != reload.ins.a {
		return directCompactLeafFastPath{}, false
	}
	switch numeric.ins.op {
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
	default:
		return directCompactLeafFastPath{}, false
	}
	if numeric.ins.a != set.ins.c {
		return directCompactLeafFastPath{}, false
	}
	reverse := uint8(0)
	if numeric.ins.b == get.ins.a {
		if valueRegister < 0 {
			valueRegister = numeric.ins.c
			valueParam = valueRegister
		}
		if numeric.ins.c != valueRegister {
			return directCompactLeafFastPath{}, false
		}
	} else if numeric.ins.c == get.ins.a {
		if valueRegister < 0 {
			valueRegister = numeric.ins.b
			valueParam = valueRegister
		}
		if numeric.ins.b != valueRegister {
			return directCompactLeafFastPath{}, false
		}
		reverse = 1
	} else {
		return directCompactLeafFastPath{}, false
	}
	if valueParam < 0 || valueParam >= proto.params {
		return directCompactLeafFastPath{}, false
	}
	getCache, getDescriptor, getOK := proto.cacheIndex.cacheSiteAt(get.wordPC)
	_, setDescriptor, setOK := proto.cacheIndex.cacheSiteAt(set.wordPC)
	_, reloadDescriptor, reloadOK := proto.cacheIndex.cacheSiteAt(reload.wordPC)
	if !getOK || !setOK || !reloadOK || getCache > math.MaxUint16 ||
		getDescriptor < 0 || getDescriptor >= len(proto.constants) ||
		setDescriptor < 0 || setDescriptor >= len(proto.constants) ||
		reloadDescriptor < 0 || reloadDescriptor >= len(proto.constants) ||
		valueKind(proto.constants[getDescriptor]) != StringKind ||
		valueKind(proto.constants[setDescriptor]) != StringKind ||
		valueKind(proto.constants[reloadDescriptor]) != StringKind ||
		proto.constants[getDescriptor].stringText() != proto.constants[setDescriptor].stringText() ||
		proto.constants[getDescriptor].stringText() != proto.constants[reloadDescriptor].stringText() {
		return directCompactLeafFastPath{}, false
	}
	return directCompactLeafFastPath{
		fieldConstant: int32(getDescriptor),
		fieldCache:    uint16(getCache),
		numericOp:     numeric.ins.op,
		tableParam:    uint8(get.ins.b),
		valueParam:    uint8(valueParam),
		reverse:       reverse,
	}, true
}

func buildDirectCompactLeafOperation(
	proto *Proto,
	entry wordcodeDecoded,
	indices map[int]int,
	operationIndex int,
) (directCompactLeafOperation, bool) {
	ins := entry.ins
	operation := directCompactLeafOperation{
		wordPC: int32(entry.wordPC),
		op:     ins.op,
		a:      uint8(ins.a),
		b:      uint8(ins.b),
		c:      uint8(ins.c),
		d:      uint8(ins.d),
	}
	register := func(values ...int) bool {
		for _, value := range values {
			if value < 0 || value >= proto.registers {
				return false
			}
		}
		return true
	}
	numberConstant := func(index int) bool {
		return index >= 0 && index < len(proto.constantNumberOK) && proto.constantNumberOK[index]
	}
	jumpTarget := func(displacement int) (uint8, bool) {
		target, ok := indices[entry.nextWord+displacement]
		if !ok || target <= operationIndex || target >= len(indices) || target > math.MaxUint8 {
			return 0, false
		}
		return uint8(target), true
	}
	fieldDescriptor := func() (int32, uint16, bool) {
		if proto.cacheIndex == nil {
			return 0, 0, false
		}
		cacheID, descriptor, ok := proto.cacheIndex.cacheSiteAt(entry.wordPC)
		if !ok || cacheID > math.MaxUint16 || descriptor < 0 || descriptor >= len(proto.constants) ||
			valueKind(proto.constants[descriptor]) != StringKind {
			return 0, 0, false
		}
		return int32(descriptor), uint16(cacheID), true
	}

	switch ins.op {
	case opLoadConst:
		if !register(ins.a) || !numberConstant(ins.b) {
			return directCompactLeafOperation{}, false
		}
		operation.constant = int32(ins.b)
	case opMove:
		if !register(ins.a, ins.b) {
			return directCompactLeafOperation{}, false
		}
	case opGetStringField:
		if !register(ins.a, ins.b) {
			return directCompactLeafOperation{}, false
		}
		constant, cacheID, ok := fieldDescriptor()
		if !ok {
			return directCompactLeafOperation{}, false
		}
		operation.constant = constant
		operation.cacheID = cacheID
	case opSetStringField:
		if !register(ins.a, ins.c) {
			return directCompactLeafOperation{}, false
		}
		constant, cacheID, ok := fieldDescriptor()
		if !ok {
			return directCompactLeafOperation{}, false
		}
		operation.constant = constant
		operation.cacheID = cacheID
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		if !register(ins.a, ins.b, ins.c) {
			return directCompactLeafOperation{}, false
		}
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		if !register(ins.a, ins.b) || !numberConstant(ins.c) {
			return directCompactLeafOperation{}, false
		}
		operation.constant = int32(ins.c)
	case opNeg:
		if !register(ins.a, ins.b) {
			return directCompactLeafOperation{}, false
		}
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
		if !register(ins.a) || !numberConstant(ins.b) {
			return directCompactLeafOperation{}, false
		}
		target, ok := jumpTarget(ins.d)
		if !ok {
			return directCompactLeafOperation{}, false
		}
		operation.constant = int32(ins.b)
		operation.d = target
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		if !register(ins.a, ins.b) {
			return directCompactLeafOperation{}, false
		}
		target, ok := jumpTarget(ins.d)
		if !ok {
			return directCompactLeafOperation{}, false
		}
		operation.d = target
	case opJump:
		target, ok := jumpTarget(ins.b)
		if !ok {
			return directCompactLeafOperation{}, false
		}
		operation.d = target
	case opFastCall:
		if nativeFuncID(ins.b) != nativeFuncMathMin || ins.c < 1 || ins.d != 1 ||
			!register(ins.a, ins.a+ins.c-1) {
			return directCompactLeafOperation{}, false
		}
	case opReturnOne:
		if !register(ins.a) {
			return directCompactLeafOperation{}, false
		}
	default:
		return directCompactLeafOperation{}, false
	}
	return operation, true
}

func directCompactLeafControlFlowReturns(plan directCompactLeafFunctionPlan) bool {
	count := int(plan.operationCount)
	if count == 0 || count > len(plan.operations) {
		return false
	}
	reachable := [directCompactLeafCallInstructionCap]bool{}
	work := [directCompactLeafCallInstructionCap]int{}
	workCount := 1
	work[0] = 0
	returns := 0
	for workCount > 0 {
		workCount--
		index := work[workCount]
		if index < 0 || index >= count || reachable[index] {
			continue
		}
		reachable[index] = true
		operation := plan.operations[index]
		if operation.op == opReturnOne {
			returns++
			continue
		}
		if operation.op != opJump {
			if index+1 >= count {
				return false
			}
			work[workCount] = index + 1
			workCount++
		}
		switch operation.op {
		case opJump,
			opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK,
			opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			target := int(operation.d)
			if target <= index || target >= count || workCount >= len(work) {
				return false
			}
			work[workCount] = target
			workCount++
		}
	}
	return returns != 0
}

func (code *directShadowCode) compactLeafFunction() (*directCompactLeafFunctionPlan, bool) {
	if code == nil || len(code.compactLeafFunctions) != 1 {
		return nil, false
	}
	return &code.compactLeafFunctions[0], true
}

type directCompactLeafWrite struct {
	table  *Table
	value  Value
	offset uint16
	_      [6]byte
}

// runDirectCompactLeafCall performs a read-only proof execution first. Only
// after the active path has reached a numeric return are its existing-field
// writes committed. A miss has no guest effects and lets the caller execute
// the canonical CALL_ONE at the same physical PC.
func (thread *vmThread) runDirectCompactLeafCall(frame *vmFrame, callee Value, args []Value) (Value, bool) {
	closure, ok := callee.scriptFunction()
	if !ok || thread == nil || frame == nil || closure == nil || closure.proto == nil ||
		thread.controller != nil || thread.debugHook != nil || thread.coroutine != nil ||
		thread.nonYieldableDepth != 0 || thread.nearestProtectedFrame != noProtectedFrame ||
		frame.hasPendingCall || frame.callDepthCharged || frame.debugLine() != -1 ||
		closure.proto.variadic || len(closure.proto.capturedLocals) != 0 || len(closure.proto.upvalues) != 0 ||
		len(closure.upvalues) != 0 || len(closure.upvalueValues) != 0 || len(args) != closure.proto.params {
		return NilValue(), false
	}
	instance, err := thread.executionShadowFunctionInstance(closure.proto)
	if err != nil || instance == nil {
		return NilValue(), false
	}
	plan, ok := instance.shadow.compactLeafFunction()
	if !ok || int(plan.params) != len(args) || int(plan.registers) > directCompactLeafRegisterCap ||
		plan.operationCount == 0 || int(plan.operationCount) > len(plan.operations) {
		return NilValue(), false
	}
	if result, handled, guarded := runDirectCompactLeafFastPath(closure, instance, plan.fastPath, args); !guarded {
		return NilValue(), false
	} else if handled {
		return result, true
	}

	var registers [directCompactLeafRegisterCap]Value
	copy(registers[:], args)
	var writes [directCompactLeafWriteCap]directCompactLeafWrite
	writeCount := 0
	operationIndex := 0
	for steps := 0; steps < int(plan.operationCount); steps++ {
		if operationIndex < 0 || operationIndex >= int(plan.operationCount) {
			return NilValue(), false
		}
		operation := plan.operations[operationIndex]
		nextOperation := operationIndex + 1
		switch operation.op {
		case opLoadConst:
			index := int(operation.constant)
			if index < 0 || index >= len(closure.proto.constantNumbers) {
				return NilValue(), false
			}
			registers[operation.a] = NumberValue(closure.proto.constantNumbers[index])
		case opMove:
			registers[operation.a] = registers[operation.b]
		case opGetStringField:
			table := registers[operation.b].tableRef()
			if table == nil || table.metatable != nil {
				return NilValue(), false
			}
			fieldCache := instance.fieldCacheAt(uint32(operation.cacheID))
			if fieldCache == nil {
				return NilValue(), false
			}
			constant := int(operation.constant)
			value, found := fieldCache.get(table)
			if !found {
				value, found = fieldCache.resolve(
					table,
					closure.proto.constantKeys[constant].str,
					closure.proto.constants[constant].stringBox(),
				)
			}
			if !found {
				return NilValue(), false
			}
			for index := writeCount - 1; index >= 0; index-- {
				if writes[index].table == table && writes[index].offset == fieldCache.offset {
					value = writes[index].value
					break
				}
			}
			registers[operation.a] = value
		case opSetStringField:
			table := registers[operation.a].tableRef()
			value := registers[operation.c]
			if table == nil || table.metatable != nil || valueKind(value) != NumberKind || writeCount >= len(writes) {
				return NilValue(), false
			}
			fieldCache := instance.fieldCacheAt(uint32(operation.cacheID))
			if fieldCache == nil {
				return NilValue(), false
			}
			constant := int(operation.constant)
			if _, found := fieldCache.get(table); !found {
				_, found = fieldCache.resolve(
					table,
					closure.proto.constantKeys[constant].str,
					closure.proto.constants[constant].stringBox(),
				)
				if !found {
					return NilValue(), false
				}
			}
			writes[writeCount] = directCompactLeafWrite{table: table, value: value, offset: fieldCache.offset}
			writeCount++
		case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
			left := registers[operation.b]
			right := registers[operation.c]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				return NilValue(), false
			}
			registers[operation.a] = NumberValue(directCompactSelfBinary(operation.op, valueNumber(left), valueNumber(right)))
		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			left := registers[operation.b]
			constant := int(operation.constant)
			if valueKind(left) != NumberKind || constant < 0 || constant >= len(closure.proto.constantNumbers) {
				return NilValue(), false
			}
			registers[operation.a] = NumberValue(directCompactSelfBinary(operation.op, valueNumber(left), closure.proto.constantNumbers[constant]))
		case opNeg:
			value := registers[operation.b]
			if valueKind(value) != NumberKind {
				return NilValue(), false
			}
			registers[operation.a] = NumberValue(-valueNumber(value))
		case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			value := registers[operation.a]
			constant := int(operation.constant)
			if valueKind(value) != NumberKind || constant < 0 || constant >= len(closure.proto.constantNumbers) {
				return NilValue(), false
			}
			left := valueNumber(value)
			right := closure.proto.constantNumbers[constant]
			jump := false
			switch operation.op {
			case opJumpIfNotEqualK:
				jump = left != right
			case opJumpIfNotLessK:
				jump = !(left < right)
			case opJumpIfNotGreaterK:
				jump = !(left > right)
			case opJumpIfLessK:
				jump = left < right
			case opJumpIfGreaterK:
				jump = left > right
			}
			if jump {
				nextOperation = int(operation.d)
			}
		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			left := registers[operation.a]
			right := registers[operation.b]
			if valueKind(left) != NumberKind || valueKind(right) != NumberKind {
				return NilValue(), false
			}
			leftNumber := valueNumber(left)
			rightNumber := valueNumber(right)
			jump := false
			switch operation.op {
			case opJumpIfNotLess:
				jump = !(leftNumber < rightNumber)
			case opJumpIfNotGreater:
				jump = !(leftNumber > rightNumber)
			case opJumpIfLess:
				jump = leftNumber < rightNumber
			case opJumpIfGreater:
				jump = leftNumber > rightNumber
			}
			if jump {
				nextOperation = int(operation.d)
			}
		case opJump:
			nextOperation = int(operation.d)
		case opFastCall:
			if nativeFuncID(operation.b) != nativeFuncMathMin || operation.c == 0 || operation.d != 1 ||
				!fastCallNativeUnchanged(thread.globals, nativeFuncMathMin) {
				return NilValue(), false
			}
			start := int(operation.a)
			end := start + int(operation.c)
			minimum, err := baseMathMinValue(registers[start:end])
			if err != nil {
				return NilValue(), false
			}
			registers[start] = NumberValue(minimum)
		case opReturnOne:
			result := registers[operation.a]
			if valueKind(result) != NumberKind {
				return NilValue(), false
			}
			for index := 0; index < writeCount; index++ {
				write := writes[index]
				if !write.table.setExistingShapedStringField(write.offset, write.value) {
					panic("ember: compact leaf existing-field proof invalidated during commit")
				}
			}
			return result, true
		default:
			return NilValue(), false
		}
		operationIndex = nextOperation
	}
	return NilValue(), false
}

// runDirectCompactLeafFastPath executes a shape-guarded record-update
// superword. The third result distinguishes a selected fast path from a guard
// miss: a non-selected branch continues through the bounded leaf interpreter,
// while a guard miss resumes the canonical caller before any mutation.
func runDirectCompactLeafFastPath(
	closure *closure,
	instance *vmFunctionInstance,
	fastPath directCompactLeafFastPath,
	args []Value,
) (Value, bool, bool) {
	if fastPath.kind == directCompactLeafFastPathNone {
		return NilValue(), false, true
	}
	if closure == nil || closure.proto == nil || instance == nil ||
		int(fastPath.tableParam) >= len(args) || int(fastPath.valueParam) >= len(args) {
		return NilValue(), false, false
	}
	table := args[fastPath.tableParam].tableRef()
	argument := args[fastPath.valueParam]
	if table == nil || table.metatable != nil || valueKind(argument) != NumberKind {
		return NilValue(), false, false
	}
	if fastPath.kind == directCompactLeafFastPathGuardedRecordUpdate {
		guardValue, ok := directCompactLeafCachedField(
			closure.proto,
			instance,
			table,
			fastPath.guardCache,
			fastPath.guardFieldConstant,
		)
		constant := int(fastPath.guardConstant)
		if !ok || valueKind(guardValue) != NumberKind || constant < 0 || constant >= len(closure.proto.constantNumbers) {
			return NilValue(), false, false
		}
		if !directCompactLeafNumericJump(
			fastPath.guardOp,
			valueNumber(guardValue),
			closure.proto.constantNumbers[constant],
		) {
			return NilValue(), false, true
		}
	}
	fieldValue, ok := directCompactLeafCachedField(
		closure.proto,
		instance,
		table,
		fastPath.fieldCache,
		fastPath.fieldConstant,
	)
	if !ok || valueKind(fieldValue) != NumberKind {
		return NilValue(), false, false
	}
	left := valueNumber(fieldValue)
	right := valueNumber(argument)
	if fastPath.reverse != 0 {
		left, right = right, left
	}
	result := NumberValue(directCompactSelfBinary(fastPath.numericOp, left, right))
	cache := instance.fieldCacheAt(uint32(fastPath.fieldCache))
	if cache == nil || !cache.write(table, result) {
		return NilValue(), false, false
	}
	return result, true, true
}

func directCompactLeafCachedField(
	proto *Proto,
	instance *vmFunctionInstance,
	table *Table,
	cacheID uint16,
	constantDescriptor int32,
) (Value, bool) {
	constant := int(constantDescriptor)
	if proto == nil || instance == nil || table == nil || constant < 0 || constant >= len(proto.constants) ||
		constant >= len(proto.constantKeys) || valueKind(proto.constants[constant]) != StringKind {
		return NilValue(), false
	}
	cache := instance.fieldCacheAt(uint32(cacheID))
	if cache == nil {
		return NilValue(), false
	}
	value, ok := cache.get(table)
	if ok {
		return value, true
	}
	return cache.resolve(table, proto.constantKeys[constant].str, proto.constants[constant].stringBox())
}

func directCompactLeafNumericJump(op opcode, left float64, right float64) bool {
	switch op {
	case opJumpIfNotEqualK:
		return left != right
	case opJumpIfNotLessK:
		return !(left < right)
	case opJumpIfNotGreaterK:
		return !(left > right)
	case opJumpIfLessK:
		return left < right
	case opJumpIfGreaterK:
		return left > right
	default:
		return false
	}
}

func observeDirectCompactLeafCall(instance *vmFunctionInstance, pc int) {
	if instance == nil || pc < 0 || pc >= len(instance.shadow.words) {
		return
	}
	word := instance.shadow.words[pc]
	if word.handler() != directHandlerID(opCallOne) {
		return
	}
	word = word.incrementCounter()
	if word.counter() >= directAdaptiveStableHitThreshold {
		word = word.withHandler(directHandlerCompactLeafCall)
	}
	instance.shadow.words[pc] = word
}
