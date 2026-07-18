package ember

import (
	"encoding/binary"
	"fmt"
	"math"
)

type backendNativeX8664FunctionCode struct {
	code        []byte
	entryOffset int
	calls       []backendNativeX8664CallFixup
}

type backendNativeX8664CallFixup struct {
	displacement int
	next         int
	targetProto  int32
}

func emitBackendNativeX8664Program(program *backendProgramIR) (backendNativeProgram, error) {
	if err := verifyBackendProgramIR(program); err != nil {
		return backendNativeProgram{}, fmt.Errorf("emit backend native x86-64 program: %w", err)
	}
	result := backendNativeProgram{
		architecture:    backendNativeArchitectureX8664,
		abiVersion:      program.abiVersion,
		semanticVersion: program.semanticVersion,
		programHash:     program.programHash,
		modules:         make([]backendNativeModule, len(program.modules)),
	}
	for moduleIndex := range program.modules {
		module, err := emitBackendNativeX8664Module(program.modules[moduleIndex].protos, moduleIndex)
		if err != nil {
			return backendNativeProgram{}, fmt.Errorf(
				"emit backend native x86-64 program: module %d: %w",
				moduleIndex,
				err,
			)
		}
		result.modules[moduleIndex] = module
	}
	return result, nil
}

func emitBackendNativeX8664Module(
	irs []*backendProtoIR,
	moduleIndex int,
) (backendNativeModule, error) {
	if len(irs) == 0 {
		return backendNativeModule{}, fmt.Errorf("empty Proto inventory")
	}
	candidates, err := buildBackendNativeModuleCandidates(irs, moduleIndex)
	if err != nil {
		return backendNativeModule{}, err
	}
	for protoIndex, candidate := range candidates {
		if candidate == nil {
			continue
		}
		if _, err := planBackendNativeX8664Frame(candidate); err != nil {
			candidates[protoIndex] = nil
		}
	}
	for changed := true; changed; {
		changed = false
		for protoIndex, candidate := range candidates {
			if candidate == nil {
				continue
			}
			for _, dependency := range candidate.dependencies {
				if dependency < 0 || int(dependency) >= len(candidates) || candidates[dependency] == nil {
					candidates[protoIndex] = nil
					changed = true
					break
				}
			}
		}
	}

	functionCode := make([]backendNativeX8664FunctionCode, len(irs))
	functions := make([]backendNativeFunction, len(irs))
	for protoIndex, candidate := range candidates {
		if candidate == nil {
			continue
		}
		code, err := emitBackendNativeX8664Function(candidate, candidates)
		if err != nil {
			return backendNativeModule{}, fmt.Errorf("Proto %d: %w", protoIndex, err)
		}
		functionCode[protoIndex] = code
		functions[protoIndex] = backendNativeFunction{
			parameterCount: candidate.parameterCount,
			argumentCount:  candidate.argumentCount,
			entry:          candidate.argumentCount == candidate.parameterCount,
			prepared:       true,
		}
	}

	var image []byte
	for protoIndex := range functionCode {
		if !functions[protoIndex].prepared {
			continue
		}
		for len(image)%16 != 0 {
			image = append(image, x8664NOP)
		}
		functions[protoIndex].bodyOffset = uint32(len(image))
		functions[protoIndex].offset = functions[protoIndex].bodyOffset +
			uint32(functionCode[protoIndex].entryOffset)
		image = append(image, functionCode[protoIndex].code...)
	}
	for protoIndex, code := range functionCode {
		if !functions[protoIndex].prepared {
			continue
		}
		for _, fixup := range code.calls {
			if fixup.targetProto < 0 || int(fixup.targetProto) >= len(functions) ||
				!functions[fixup.targetProto].prepared {
				return backendNativeModule{}, fmt.Errorf(
					"Proto %d call target %d is unavailable",
					protoIndex,
					fixup.targetProto,
				)
			}
			functionOffset := int(functions[protoIndex].bodyOffset)
			next := functionOffset + fixup.next
			target := int(functions[fixup.targetProto].bodyOffset)
			delta, err := backendNativeX8664RelativeDelta(target, next)
			if err != nil {
				return backendNativeModule{}, fmt.Errorf("Proto %d call: %w", protoIndex, err)
			}
			binary.LittleEndian.PutUint32(
				image[functionOffset+fixup.displacement:],
				uint32(delta),
			)
		}
	}
	return backendNativeModule{code: image, functions: functions}, nil
}

const (
	x8664NOP = byte(0x90)

	x8664ConditionBelow      = byte(0x82)
	x8664ConditionAboveEqual = byte(0x83)
	x8664ConditionEqual      = byte(0x84)
	x8664ConditionNotEqual   = byte(0x85)
	x8664ConditionBelowEqual = byte(0x86)
	x8664ConditionAbove      = byte(0x87)
	x8664ConditionParity     = byte(0x8a)
)

const backendNativeX8664MaximumFrameSize = 1 << 20

type backendNativeX8664Frame struct {
	captureOffset      int
	closureCellsOffset int
	phiScratchOffset   int
	size               int
}

type backendNativeX8664BranchFixup struct {
	displacement int
	next         int
	label        int
}

type backendNativeX8664LiteralFixup struct {
	displacement int
	next         int
	label        int
}

type backendNativeX8664Literal struct {
	bits  uint64
	label int
}

type backendNativeX8664FunctionEmitter struct {
	candidate      *backendNativeCandidate
	candidates     []*backendNativeCandidate
	frame          backendNativeX8664Frame
	code           []byte
	calls          []backendNativeX8664CallFixup
	branches       []backendNativeX8664BranchFixup
	literalFixups  []backendNativeX8664LiteralFixup
	literals       []backendNativeX8664Literal
	literalLabels  map[uint64]int
	labelPositions []int
	blockLabels    []int
	failureLabel   int
}

func planBackendNativeX8664Frame(candidate *backendNativeCandidate) (backendNativeX8664Frame, error) {
	if candidate == nil || candidate.ir == nil {
		return backendNativeX8664Frame{}, fmt.Errorf("nil candidate")
	}
	maximumPhiCopies := 0
	for edgeIndex := range candidate.ir.edges {
		count := 0
		for _, copy := range candidate.ir.edges[edgeIndex].phiCopies {
			if candidate.ir.validBackendValue(copy.destination) && candidate.plan.used[copy.destination-1] {
				count++
			}
		}
		if count > maximumPhiCopies {
			maximumPhiCopies = count
		}
	}
	frame := backendNativeX8664Frame{}
	frame.captureOffset = len(candidate.ir.values) * 8
	frame.closureCellsOffset = frame.captureOffset + len(candidate.captureUpvalues)*8
	frame.phiScratchOffset = frame.closureCellsOffset + candidate.plan.closures.cellCount*8
	payload := frame.phiScratchOffset + maximumPhiCopies*8
	// x86 CALL enters with RSP mod 16 == 8. A frame congruent to 8 keeps
	// internal call sites aligned to the System V convention.
	frame.size = ((payload + 15) &^ 15) + 8
	if frame.size <= 0 || frame.size > backendNativeX8664MaximumFrameSize {
		return backendNativeX8664Frame{}, fmt.Errorf(
			"stack frame %d exceeds x86-64 limit %d",
			frame.size,
			backendNativeX8664MaximumFrameSize,
		)
	}
	return frame, nil
}

func emitBackendNativeX8664Function(
	candidate *backendNativeCandidate,
	candidates []*backendNativeCandidate,
) (backendNativeX8664FunctionCode, error) {
	frame, err := planBackendNativeX8664Frame(candidate)
	if err != nil {
		return backendNativeX8664FunctionCode{}, err
	}
	emitter := &backendNativeX8664FunctionEmitter{
		candidate:     candidate,
		candidates:    candidates,
		frame:         frame,
		literalLabels: make(map[uint64]int),
		blockLabels:   make([]int, len(candidate.ir.blocks)),
	}
	for blockIndex := range emitter.blockLabels {
		emitter.blockLabels[blockIndex] = emitter.newLabel()
	}
	emitter.failureLabel = emitter.newLabel()
	emitter.emitPrologue()
	for blockIndex := range candidate.ir.blocks {
		block := &candidate.ir.blocks[blockIndex]
		if !block.reachable {
			continue
		}
		emitter.markLabel(emitter.blockLabels[blockIndex])
		terminated := false
		for pc := block.first; pc < block.last; pc++ {
			operation := &candidate.ir.ops[pc]
			if backendGoNumericOperationDead(candidate.plan, operation) {
				continue
			}
			terminated, err = emitter.emitOperation(operation, block)
			if err != nil {
				return backendNativeX8664FunctionCode{}, err
			}
			if terminated {
				break
			}
		}
		if !terminated {
			if len(block.successors) != 1 {
				return backendNativeX8664FunctionCode{}, fmt.Errorf(
					"block %d has no terminator and %d successors",
					blockIndex,
					len(block.successors),
				)
			}
			if err := emitter.emitGoto(int32(blockIndex), block.successors[0]); err != nil {
				return backendNativeX8664FunctionCode{}, err
			}
		}
	}
	emitter.emitJump(emitter.failureLabel)
	emitter.markLabel(emitter.failureLabel)
	emitter.emitMoveStatus(0)
	emitter.emitEpilogue()
	entryOffset := emitter.emitBoundaryAdapter()
	if err := emitter.finish(); err != nil {
		return backendNativeX8664FunctionCode{}, err
	}
	return backendNativeX8664FunctionCode{
		code:        emitter.code,
		entryOffset: entryOffset,
		calls:       emitter.calls,
	}, nil
}

func (emitter *backendNativeX8664FunctionEmitter) emitPrologue() {
	emitter.code = append(emitter.code, 0x48, 0x81, 0xec)
	emitter.appendUint32(uint32(emitter.frame.size))
	for parameter := 0; parameter < emitter.candidate.ir.params; parameter++ {
		id := emitter.candidate.ir.initial[parameter]
		emitter.emitStoreValueXMM(byte(parameter), id)
	}
	for capture := range emitter.candidate.captureUpvalues {
		emitter.emitStoreXMMStack(
			byte(emitter.candidate.parameterCount+capture),
			emitter.frame.captureOffset+capture*8,
		)
	}
	if emitter.candidate.options.selfRecursive {
		parameter := emitter.candidate.ir.initial[0]
		emitter.emitNaNGuard(parameter)
		emitter.emitLoadValueXMM(0, parameter)
		emitter.emitLoadLiteralXMM(1, math.Float64bits(backendGoMaxPreparedRecursiveArgument))
		emitter.emitUCOMISD(0, 1)
		emitter.emitConditionalJump(emitter.failureLabel, x8664ConditionAbove)
	}
}

// emitBoundaryAdapter keeps the pointer/count/result contract at the Go edge.
// Native bodies exchange up to eight scalars in XMM0-XMM7 and return their
// scalar in XMM0 with a prepared status in RAX.
func (emitter *backendNativeX8664FunctionEmitter) emitBoundaryAdapter() int {
	const adapterFrameSize = 24
	entryOffset := len(emitter.code)
	failureLabel := emitter.newLabel()
	emitter.code = append(emitter.code, 0x48, 0x81, 0xec)
	emitter.appendUint32(adapterFrameSize)
	emitter.emitStoreRDXStack(0)
	emitter.code = append(emitter.code, 0x48, 0x81, 0xfe)
	emitter.appendUint32(uint32(emitter.candidate.argumentCount))
	emitter.emitConditionalJump(failureLabel, x8664ConditionNotEqual)
	for argument := 0; argument < emitter.candidate.argumentCount; argument++ {
		emitter.emitLoadXMMRDI(byte(argument), argument*8)
	}
	emitter.emitCall(emitter.candidate.protoID)
	emitter.emitTestRAX()
	emitter.emitConditionalJump(failureLabel, x8664ConditionEqual)
	emitter.emitLoadR9Stack(0)
	emitter.emitStoreXMMR9(0)
	emitter.emitBoundaryAdapterEpilogue(adapterFrameSize)
	emitter.markLabel(failureLabel)
	emitter.emitMoveStatus(0)
	emitter.emitBoundaryAdapterEpilogue(adapterFrameSize)
	return entryOffset
}

func (emitter *backendNativeX8664FunctionEmitter) emitBoundaryAdapterEpilogue(frameSize uint32) {
	emitter.code = append(emitter.code, 0x48, 0x81, 0xc4)
	emitter.appendUint32(frameSize)
	emitter.code = append(emitter.code, 0xc3)
}

func (emitter *backendNativeX8664FunctionEmitter) emitOperation(
	operation *backendOperationIR,
	block *backendBlockIR,
) (bool, error) {
	definition := func(register int32) (backendValueID, error) {
		id := backendOperationDefinition(operation, register)
		if !emitter.candidate.ir.validBackendValue(id) {
			return invalidBackendValueID, fmt.Errorf(
				"PC %d has no definition for register %d",
				operation.pc,
				register,
			)
		}
		return id, nil
	}
	use := func(register int32) (backendValueID, error) {
		id := backendOperationUse(operation, register)
		if !emitter.candidate.ir.validBackendValue(id) {
			return invalidBackendValueID, fmt.Errorf(
				"PC %d has no use for register %d",
				operation.pc,
				register,
			)
		}
		return id, nil
	}
	switch operation.op {
	case opLoadConst:
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		constant := emitter.candidate.ir.constants[operation.b]
		switch constant.kind {
		case NumberKind:
			emitter.emitLoadLiteralXMM(0, constant.bits)
			emitter.emitStoreValueXMM(0, destination)
		case BoolKind:
			emitter.emitMoveR9(uint32(constant.bits))
			emitter.emitStoreValueR9(destination)
		default:
			return false, fmt.Errorf(
				"PC %d has unsupported constant kind %s",
				operation.pc,
				constant.kind,
			)
		}
	case opMove:
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		source, err := use(operation.b)
		if err != nil {
			return false, err
		}
		emitter.emitLoadValueR9(source)
		emitter.emitStoreValueR9(destination)
	case opClosure:
		local, ok := emitter.candidate.plan.closures.local(operation)
		if !ok {
			return false, fmt.Errorf("PC %d has no scalar closure", operation.pc)
		}
		for upvalue, capture := range local.captures {
			if capture == invalidBackendValueID {
				continue
			}
			emitter.emitLoadValueXMM(0, capture)
			emitter.emitStoreXMMStack(
				0,
				emitter.frame.closureCellsOffset+(local.cellStart+upvalue)*8,
			)
		}
	case opGetUpvalue:
		capture, ok := emitter.candidate.captureIndex(operation.b)
		if !ok {
			return false, fmt.Errorf("PC %d has no native capture for upvalue %d", operation.pc, operation.b)
		}
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		emitter.emitLoadXMMStack(0, emitter.frame.captureOffset+capture*8)
		emitter.emitStoreValueXMM(0, destination)
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv:
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		left, err := use(operation.b)
		if err != nil {
			return false, err
		}
		right, err := use(operation.c)
		if err != nil {
			return false, err
		}
		emitter.emitLoadValueXMM(0, left)
		emitter.emitLoadValueXMM(1, right)
		emitter.emitNumericBinary(operation.op)
		emitter.emitStoreValueXMM(0, destination)
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		left, err := use(operation.b)
		if err != nil {
			return false, err
		}
		emitter.emitLoadValueXMM(0, left)
		emitter.emitLoadLiteralXMM(1, emitter.candidate.ir.constants[operation.c].bits)
		emitter.emitNumericBinary(operation.op)
		emitter.emitStoreValueXMM(0, destination)
	case opNeg:
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		source, err := use(operation.b)
		if err != nil {
			return false, err
		}
		emitter.emitLoadValueXMM(0, source)
		emitter.emitLoadLiteralXMM(1, uint64(1)<<63)
		emitter.emitXORPD(0, 1)
		emitter.emitStoreValueXMM(0, destination)
	case opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		left, err := use(operation.b)
		if err != nil {
			return false, err
		}
		right, err := use(operation.c)
		if err != nil {
			return false, err
		}
		if operation.op != opEqual && operation.op != opNotEqual {
			emitter.emitNaNGuard(left, right)
		}
		emitter.emitLoadValueXMM(0, left)
		emitter.emitLoadValueXMM(1, right)
		emitter.emitUCOMISD(0, 1)
		conditions, ok := backendNativeX8664ComparisonConditions(operation.op)
		if !ok {
			return false, fmt.Errorf("PC %d has unsupported comparison %s", operation.pc, opcodeName(operation.op))
		}
		emitter.emitComparisonResult(operation.op, conditions)
		emitter.emitStoreValueR9(destination)
	case opNumericForCheck:
		loop, err := use(operation.a)
		if err != nil {
			return false, err
		}
		limit, err := use(operation.b)
		if err != nil {
			return false, err
		}
		step, err := use(operation.c)
		if err != nil {
			return false, err
		}
		emitter.emitNaNGuard(loop, limit, step)
		exit := emitter.candidate.ir.pcToBlock[operation.targetPC]
		next := emitter.nextBlock(block)
		positive := emitter.newLabel()
		emitter.emitLoadValueXMM(0, step)
		emitter.emitLoadLiteralXMM(1, math.Float64bits(0))
		emitter.emitUCOMISD(0, 1)
		emitter.emitConditionalJump(positive, x8664ConditionAbove)
		emitter.emitLoadValueXMM(0, loop)
		emitter.emitLoadValueXMM(1, limit)
		emitter.emitUCOMISD(0, 1)
		if err := emitter.emitConditionalEdges(
			int32(block.id),
			exit,
			next,
			[]byte{x8664ConditionBelow},
		); err != nil {
			return false, err
		}
		emitter.markLabel(positive)
		emitter.emitLoadValueXMM(0, loop)
		emitter.emitLoadValueXMM(1, limit)
		emitter.emitUCOMISD(0, 1)
		if err := emitter.emitConditionalEdges(
			int32(block.id),
			exit,
			next,
			[]byte{x8664ConditionAbove},
		); err != nil {
			return false, err
		}
		return true, nil
	case opNumericForLoop:
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		loop, err := use(operation.a)
		if err != nil {
			return false, err
		}
		step, err := use(operation.b)
		if err != nil {
			return false, err
		}
		emitter.emitLoadValueXMM(0, loop)
		emitter.emitLoadValueXMM(1, step)
		emitter.emitSSEBinary(0x58, 0, 1)
		emitter.emitStoreValueXMM(0, destination)
		if err := emitter.emitGoto(int32(block.id), emitter.candidate.ir.pcToBlock[operation.targetPC]); err != nil {
			return false, err
		}
		return true, nil
	case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
		left, err := use(operation.a)
		if err != nil {
			return false, err
		}
		if operation.op != opJumpIfNotEqualK {
			emitter.emitNaNGuard(left)
		}
		emitter.emitLoadValueXMM(0, left)
		emitter.emitLoadLiteralXMM(1, emitter.candidate.ir.constants[operation.b].bits)
		emitter.emitUCOMISD(0, 1)
		conditions, ok := backendNativeX8664ComparisonConditions(operation.op)
		if !ok {
			return false, fmt.Errorf("PC %d has unsupported branch %s", operation.pc, opcodeName(operation.op))
		}
		if err := emitter.emitConditionalEdges(
			int32(block.id),
			emitter.candidate.ir.pcToBlock[operation.targetPC],
			emitter.nextBlock(block),
			conditions,
		); err != nil {
			return false, err
		}
		return true, nil
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		left, err := use(operation.a)
		if err != nil {
			return false, err
		}
		right, err := use(operation.b)
		if err != nil {
			return false, err
		}
		emitter.emitNaNGuard(left, right)
		emitter.emitLoadValueXMM(0, left)
		emitter.emitLoadValueXMM(1, right)
		emitter.emitUCOMISD(0, 1)
		conditions, ok := backendNativeX8664ComparisonConditions(operation.op)
		if !ok {
			return false, fmt.Errorf("PC %d has unsupported branch %s", operation.pc, opcodeName(operation.op))
		}
		if err := emitter.emitConditionalEdges(
			int32(block.id),
			emitter.candidate.ir.pcToBlock[operation.targetPC],
			emitter.nextBlock(block),
			conditions,
		); err != nil {
			return false, err
		}
		return true, nil
	case opJumpIfFalse:
		condition, err := use(operation.a)
		if err != nil {
			return false, err
		}
		if emitter.candidate.plan.tags[condition-1] != backendTagBool {
			if err := emitter.emitGoto(int32(block.id), emitter.nextBlock(block)); err != nil {
				return false, err
			}
			return true, nil
		}
		emitter.emitLoadValueR9(condition)
		emitter.emitTestR9()
		if err := emitter.emitConditionalEdges(
			int32(block.id),
			emitter.candidate.ir.pcToBlock[operation.targetPC],
			emitter.nextBlock(block),
			[]byte{x8664ConditionEqual},
		); err != nil {
			return false, err
		}
		return true, nil
	case opJump:
		if err := emitter.emitGoto(int32(block.id), emitter.candidate.ir.pcToBlock[operation.targetPC]); err != nil {
			return false, err
		}
		return true, nil
	case opCall, opCallLocalOne, opCallUpvalueOne:
		target, ok := emitter.candidate.directTarget(operation)
		if !ok {
			return false, fmt.Errorf("PC %d lost its static target", operation.pc)
		}
		targetProto := backendNativeTargetProtoID(emitter.candidate.options.directTargets, target.ir)
		if targetProto < 0 || int(targetProto) >= len(emitter.candidates) || emitter.candidates[targetProto] == nil {
			return false, fmt.Errorf("PC %d has no prepared target", operation.pc)
		}
		for argument := int32(0); argument < operation.callArgCount; argument++ {
			value, err := use(operation.callArgStart + argument)
			if err != nil {
				return false, err
			}
			emitter.emitLoadValueXMM(byte(argument), value)
		}
		capturePlan, ok := emitter.candidate.callCapturePlan(operation, target)
		if !ok {
			return false, fmt.Errorf("PC %d lost its native capture plan", operation.pc)
		}
		if capturePlan.forward {
			for capture := range emitter.candidate.captureUpvalues {
				emitter.emitLoadXMMStack(
					byte(int(operation.callArgCount)+capture),
					emitter.frame.captureOffset+capture*8,
				)
			}
		} else {
			for capture, cell := range capturePlan.cells {
				emitter.emitLoadXMMStack(
					byte(int(operation.callArgCount)+capture),
					emitter.frame.closureCellsOffset+cell*8,
				)
			}
		}
		emitter.emitCall(targetProto)
		emitter.emitTestRAX()
		emitter.emitConditionalJump(emitter.failureLabel, x8664ConditionEqual)
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		emitter.emitStoreValueXMM(0, destination)
	case opReturnOne, opReturn:
		value, ok := backendGoNumericReturnValueForOptions(
			emitter.candidate.ir,
			emitter.candidate.options,
			operation,
			0,
		)
		if !ok {
			return false, fmt.Errorf("PC %d has no numeric return value", operation.pc)
		}
		emitter.emitLoadValueXMM(0, value)
		emitter.emitMoveStatus(1)
		emitter.emitEpilogue()
		return true, nil
	default:
		return false, fmt.Errorf("PC %d uses unsupported opcode %s", operation.pc, opcodeName(operation.op))
	}
	return false, nil
}

func (emitter *backendNativeX8664FunctionEmitter) emitNumericBinary(operation opcode) {
	switch operation {
	case opAdd, opAddK:
		emitter.emitSSEBinary(0x58, 0, 1)
	case opSub, opSubK:
		emitter.emitSSEBinary(0x5c, 0, 1)
	case opMul, opMulK:
		emitter.emitSSEBinary(0x59, 0, 1)
	case opDiv, opDivK:
		emitter.emitSSEBinary(0x5e, 0, 1)
	case opIDiv, opIDivK:
		emitter.emitSSEBinary(0x5e, 0, 1)
		emitter.emitROUNDSD(0, 0, 1)
	case opMod, opModK:
		emitter.emitMoveXMM(2, 0)
		emitter.emitSSEBinary(0x5e, 2, 1)
		emitter.emitROUNDSD(2, 2, 1)
		emitter.emitSSEBinary(0x59, 2, 1)
		emitter.emitSSEBinary(0x5c, 0, 2)
	}
}

func backendNativeX8664ComparisonConditions(operation opcode) ([]byte, bool) {
	switch operation {
	case opEqual:
		return []byte{x8664ConditionEqual}, true
	case opNotEqual, opJumpIfNotEqualK:
		return []byte{x8664ConditionParity, x8664ConditionNotEqual}, true
	case opLess, opJumpIfLess, opJumpIfLessK:
		return []byte{x8664ConditionBelow}, true
	case opLessEqual:
		return []byte{x8664ConditionBelowEqual}, true
	case opGreater, opJumpIfGreater, opJumpIfGreaterK:
		return []byte{x8664ConditionAbove}, true
	case opGreaterEqual, opJumpIfNotLess, opJumpIfNotLessK:
		return []byte{x8664ConditionAboveEqual}, true
	case opJumpIfNotGreater, opJumpIfNotGreaterK:
		return []byte{x8664ConditionBelowEqual}, true
	default:
		return nil, false
	}
}

func (emitter *backendNativeX8664FunctionEmitter) emitComparisonResult(
	operation opcode,
	conditions []byte,
) {
	trueLabel := emitter.newLabel()
	doneLabel := emitter.newLabel()
	emitter.emitMoveR9(0)
	if operation == opEqual {
		// UCOMISD sets ZF for both equality and unordered. Preserve Luau's
		// NaN equality by skipping the equality branch when PF is set.
		emitter.emitConditionalJump(doneLabel, x8664ConditionParity)
	}
	for _, condition := range conditions {
		emitter.emitConditionalJump(trueLabel, condition)
	}
	emitter.emitJump(doneLabel)
	emitter.markLabel(trueLabel)
	emitter.emitMoveR9(1)
	emitter.markLabel(doneLabel)
}

func (emitter *backendNativeX8664FunctionEmitter) emitNaNGuard(values ...backendValueID) {
	for _, value := range values {
		emitter.emitLoadValueXMM(0, value)
		emitter.emitUCOMISD(0, 0)
		emitter.emitConditionalJump(emitter.failureLabel, x8664ConditionParity)
	}
}

func (emitter *backendNativeX8664FunctionEmitter) nextBlock(block *backendBlockIR) int32 {
	if block != nil && int(block.last) < len(emitter.candidate.ir.ops) {
		return emitter.candidate.ir.pcToBlock[block.last]
	}
	return -1
}

func (emitter *backendNativeX8664FunctionEmitter) emitConditionalEdges(
	from, taken, notTaken int32,
	conditions []byte,
) error {
	takenEdge := emitter.newLabel()
	for _, condition := range conditions {
		emitter.emitConditionalJump(takenEdge, condition)
	}
	if err := emitter.emitGoto(from, notTaken); err != nil {
		return err
	}
	emitter.markLabel(takenEdge)
	return emitter.emitGoto(from, taken)
}

func (emitter *backendNativeX8664FunctionEmitter) emitGoto(from, to int32) error {
	if to < 0 || int(to) >= len(emitter.blockLabels) {
		return fmt.Errorf("CFG edge %d -> %d is unavailable", from, to)
	}
	for edgeIndex := range emitter.candidate.ir.edges {
		edge := &emitter.candidate.ir.edges[edgeIndex]
		if edge.from != from || edge.to != to {
			continue
		}
		copyCount := 0
		for _, copy := range edge.phiCopies {
			if !emitter.candidate.ir.validBackendValue(copy.destination) ||
				!emitter.candidate.plan.used[copy.destination-1] {
				continue
			}
			if !emitter.candidate.ir.validBackendValue(copy.source) {
				return fmt.Errorf("CFG edge %d -> %d has invalid phi source", from, to)
			}
			emitter.emitLoadValueR9(copy.source)
			emitter.emitStoreR9Stack(emitter.frame.phiScratchOffset + copyCount*8)
			copyCount++
		}
		copyCount = 0
		for _, copy := range edge.phiCopies {
			if !emitter.candidate.ir.validBackendValue(copy.destination) ||
				!emitter.candidate.plan.used[copy.destination-1] {
				continue
			}
			emitter.emitLoadR9Stack(emitter.frame.phiScratchOffset + copyCount*8)
			emitter.emitStoreValueR9(copy.destination)
			copyCount++
		}
		emitter.emitJump(emitter.blockLabels[to])
		return nil
	}
	return fmt.Errorf("CFG edge %d -> %d is unavailable", from, to)
}

func (emitter *backendNativeX8664FunctionEmitter) valueOffset(id backendValueID) int {
	return (int(id) - 1) * 8
}

func (emitter *backendNativeX8664FunctionEmitter) emitLoadValueXMM(register byte, id backendValueID) {
	emitter.emitLoadXMMStack(register, emitter.valueOffset(id))
}

func (emitter *backendNativeX8664FunctionEmitter) emitStoreValueXMM(register byte, id backendValueID) {
	emitter.emitStoreXMMStack(register, emitter.valueOffset(id))
}

func (emitter *backendNativeX8664FunctionEmitter) emitLoadValueR9(id backendValueID) {
	emitter.emitLoadR9Stack(emitter.valueOffset(id))
}

func (emitter *backendNativeX8664FunctionEmitter) emitStoreValueR9(id backendValueID) {
	emitter.emitStoreR9Stack(emitter.valueOffset(id))
}

func (emitter *backendNativeX8664FunctionEmitter) emitEpilogue() {
	emitter.code = append(emitter.code, 0x48, 0x81, 0xc4)
	emitter.appendUint32(uint32(emitter.frame.size))
	emitter.code = append(emitter.code, 0xc3)
}

func (emitter *backendNativeX8664FunctionEmitter) emitMoveStatus(value uint32) {
	emitter.code = append(emitter.code, 0xb8)
	emitter.appendUint32(value)
}

func (emitter *backendNativeX8664FunctionEmitter) emitMoveR9(value uint32) {
	emitter.code = append(emitter.code, 0x41, 0xb9)
	emitter.appendUint32(value)
}

func (emitter *backendNativeX8664FunctionEmitter) emitLoadXMMStack(register byte, offset int) {
	emitter.code = append(emitter.code, 0xf2, 0x0f, 0x10, 0x84|register<<3, 0x24)
	emitter.appendUint32(uint32(offset))
}

func (emitter *backendNativeX8664FunctionEmitter) emitStoreXMMStack(register byte, offset int) {
	emitter.code = append(emitter.code, 0xf2, 0x0f, 0x11, 0x84|register<<3, 0x24)
	emitter.appendUint32(uint32(offset))
}

func (emitter *backendNativeX8664FunctionEmitter) emitLoadXMMRDI(register byte, offset int) {
	emitter.code = append(emitter.code, 0xf2, 0x0f, 0x10, 0x87|register<<3)
	emitter.appendUint32(uint32(offset))
}

func (emitter *backendNativeX8664FunctionEmitter) emitStoreXMMR9(register byte) {
	emitter.code = append(emitter.code, 0xf2, 0x41, 0x0f, 0x11, 0x01|register<<3)
}

func (emitter *backendNativeX8664FunctionEmitter) emitLoadR9Stack(offset int) {
	emitter.code = append(emitter.code, 0x4c, 0x8b, 0x8c, 0x24)
	emitter.appendUint32(uint32(offset))
}

func (emitter *backendNativeX8664FunctionEmitter) emitStoreR9Stack(offset int) {
	emitter.code = append(emitter.code, 0x4c, 0x89, 0x8c, 0x24)
	emitter.appendUint32(uint32(offset))
}

func (emitter *backendNativeX8664FunctionEmitter) emitStoreRDXStack(offset int) {
	emitter.code = append(emitter.code, 0x48, 0x89, 0x94, 0x24)
	emitter.appendUint32(uint32(offset))
}

func (emitter *backendNativeX8664FunctionEmitter) emitTestRAX() {
	emitter.code = append(emitter.code, 0x48, 0x85, 0xc0)
}

func (emitter *backendNativeX8664FunctionEmitter) emitTestR9() {
	emitter.code = append(emitter.code, 0x4d, 0x85, 0xc9)
}

func (emitter *backendNativeX8664FunctionEmitter) emitSSEBinary(
	opcode, destination, source byte,
) {
	emitter.code = append(emitter.code, 0xf2, 0x0f, opcode, 0xc0|destination<<3|source)
}

func (emitter *backendNativeX8664FunctionEmitter) emitMoveXMM(destination, source byte) {
	emitter.code = append(emitter.code, 0xf2, 0x0f, 0x10, 0xc0|destination<<3|source)
}

func (emitter *backendNativeX8664FunctionEmitter) emitROUNDSD(
	destination, source, mode byte,
) {
	emitter.code = append(
		emitter.code,
		0x66, 0x0f, 0x3a, 0x0b,
		0xc0|destination<<3|source,
		mode,
	)
}

func (emitter *backendNativeX8664FunctionEmitter) emitXORPD(destination, source byte) {
	emitter.code = append(emitter.code, 0x66, 0x0f, 0x57, 0xc0|destination<<3|source)
}

func (emitter *backendNativeX8664FunctionEmitter) emitUCOMISD(left, right byte) {
	emitter.code = append(emitter.code, 0x66, 0x0f, 0x2e, 0xc0|left<<3|right)
}

func (emitter *backendNativeX8664FunctionEmitter) emitCall(targetProto int32) {
	emitter.code = append(emitter.code, 0xe8)
	displacement := len(emitter.code)
	emitter.appendUint32(0)
	emitter.calls = append(emitter.calls, backendNativeX8664CallFixup{
		displacement: displacement,
		next:         len(emitter.code),
		targetProto:  targetProto,
	})
}

func (emitter *backendNativeX8664FunctionEmitter) newLabel() int {
	label := len(emitter.labelPositions)
	emitter.labelPositions = append(emitter.labelPositions, -1)
	return label
}

func (emitter *backendNativeX8664FunctionEmitter) markLabel(label int) {
	if label >= 0 && label < len(emitter.labelPositions) {
		emitter.labelPositions[label] = len(emitter.code)
	}
}

func (emitter *backendNativeX8664FunctionEmitter) emitJump(label int) {
	emitter.code = append(emitter.code, 0xe9)
	displacement := len(emitter.code)
	emitter.appendUint32(0)
	emitter.branches = append(emitter.branches, backendNativeX8664BranchFixup{
		displacement: displacement,
		next:         len(emitter.code),
		label:        label,
	})
}

func (emitter *backendNativeX8664FunctionEmitter) emitConditionalJump(
	label int,
	condition byte,
) {
	emitter.code = append(emitter.code, 0x0f, condition)
	displacement := len(emitter.code)
	emitter.appendUint32(0)
	emitter.branches = append(emitter.branches, backendNativeX8664BranchFixup{
		displacement: displacement,
		next:         len(emitter.code),
		label:        label,
	})
}

func (emitter *backendNativeX8664FunctionEmitter) emitLoadLiteralXMM(register byte, bits uint64) {
	label, ok := emitter.literalLabels[bits]
	if !ok {
		label = emitter.newLabel()
		emitter.literalLabels[bits] = label
		emitter.literals = append(emitter.literals, backendNativeX8664Literal{bits: bits, label: label})
	}
	emitter.code = append(emitter.code, 0xf2, 0x0f, 0x10, 0x05|register<<3)
	displacement := len(emitter.code)
	emitter.appendUint32(0)
	emitter.literalFixups = append(emitter.literalFixups, backendNativeX8664LiteralFixup{
		displacement: displacement,
		next:         len(emitter.code),
		label:        label,
	})
}

func (emitter *backendNativeX8664FunctionEmitter) finish() error {
	for len(emitter.code)%8 != 0 {
		emitter.code = append(emitter.code, x8664NOP)
	}
	for _, literal := range emitter.literals {
		emitter.markLabel(literal.label)
		var encoded [8]byte
		binary.LittleEndian.PutUint64(encoded[:], literal.bits)
		emitter.code = append(emitter.code, encoded[:]...)
	}
	for _, fixup := range emitter.branches {
		if fixup.label < 0 || fixup.label >= len(emitter.labelPositions) ||
			emitter.labelPositions[fixup.label] < 0 {
			return fmt.Errorf("unresolved x86-64 branch label %d", fixup.label)
		}
		delta, err := backendNativeX8664RelativeDelta(
			emitter.labelPositions[fixup.label],
			fixup.next,
		)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(emitter.code[fixup.displacement:], uint32(delta))
	}
	for _, fixup := range emitter.literalFixups {
		if fixup.label < 0 || fixup.label >= len(emitter.labelPositions) ||
			emitter.labelPositions[fixup.label] < 0 {
			return fmt.Errorf("unresolved x86-64 literal label %d", fixup.label)
		}
		delta, err := backendNativeX8664RelativeDelta(
			emitter.labelPositions[fixup.label],
			fixup.next,
		)
		if err != nil {
			return err
		}
		binary.LittleEndian.PutUint32(emitter.code[fixup.displacement:], uint32(delta))
	}
	return nil
}

func (emitter *backendNativeX8664FunctionEmitter) appendUint32(value uint32) {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], value)
	emitter.code = append(emitter.code, encoded[:]...)
}

func backendNativeX8664RelativeDelta(target, next int) (int32, error) {
	delta := int64(target) - int64(next)
	if delta < math.MinInt32 || delta > math.MaxInt32 {
		return 0, fmt.Errorf("relative delta %d exceeds signed 32-bit range", delta)
	}
	return int32(delta), nil
}
