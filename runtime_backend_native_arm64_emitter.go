package ember

// This emitter is host-independent: cross-platform builds generate ARM64
// artifacts without executing them.

import (
	"encoding/binary"
	"fmt"
	"math"
)

type backendNativeARM64FunctionCode struct {
	words     []uint32
	entryWord int
	calls     []backendNativeARM64CallFixup
}

type backendNativeARM64CallFixup struct {
	instruction int
	targetProto int32
}

func emitBackendNativeARM64Program(program *backendProgramIR) (backendNativeProgram, error) {
	if err := verifyBackendProgramIR(program); err != nil {
		return backendNativeProgram{}, fmt.Errorf("emit backend native ARM64 program: %w", err)
	}
	result := backendNativeProgram{
		architecture:    backendNativeArchitectureARM64,
		abiVersion:      program.abiVersion,
		semanticVersion: program.semanticVersion,
		programHash:     program.programHash,
		modules:         make([]backendNativeModule, len(program.modules)),
	}
	for moduleIndex := range program.modules {
		module, err := emitBackendNativeARM64Module(program.modules[moduleIndex].protos, moduleIndex)
		if err != nil {
			return backendNativeProgram{}, fmt.Errorf(
				"emit backend native ARM64 program: module %d: %w",
				moduleIndex,
				err,
			)
		}
		result.modules[moduleIndex] = module
	}
	return result, nil
}

func emitBackendNativeARM64Module(
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
	frameSizes := make([]int, len(candidates))
	for protoIndex, candidate := range candidates {
		if candidate == nil {
			continue
		}
		frame, err := planBackendNativeARM64Frame(candidate)
		if err != nil {
			candidates[protoIndex] = nil
			continue
		}
		frameSizes[protoIndex] = frame.size
	}
	pruneBackendNativeStackCandidates(candidates, frameSizes)

	functionCode := make([]backendNativeARM64FunctionCode, len(irs))
	functions := make([]backendNativeFunction, len(irs))
	for protoIndex, candidate := range candidates {
		if candidate == nil {
			continue
		}
		code, err := emitBackendNativeARM64Function(candidate, candidates)
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
			image = appendBackendNativeARM64Word(image, arm64NOP)
		}
		functions[protoIndex].bodyOffset = uint32(len(image))
		functions[protoIndex].offset = functions[protoIndex].bodyOffset +
			uint32(functionCode[protoIndex].entryWord*4)
		for _, word := range functionCode[protoIndex].words {
			image = appendBackendNativeARM64Word(image, word)
		}
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
			instruction := int(functions[protoIndex].bodyOffset) + fixup.instruction*4
			target := int(functions[fixup.targetProto].bodyOffset)
			encoded, err := encodeBackendNativeARM64Delta(target-instruction, 26)
			if err != nil {
				return backendNativeModule{}, fmt.Errorf("Proto %d call: %w", protoIndex, err)
			}
			binary.LittleEndian.PutUint32(image[instruction:], arm64BL|encoded)
		}
	}
	return backendNativeModule{code: image, functions: functions}, nil
}

func appendBackendNativeARM64Word(destination []byte, word uint32) []byte {
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], word)
	return append(destination, encoded[:]...)
}

func encodeBackendNativeARM64Delta(deltaBytes, bits int) (uint32, error) {
	if deltaBytes%4 != 0 {
		return 0, fmt.Errorf("unaligned branch delta %d", deltaBytes)
	}
	delta := int64(deltaBytes / 4)
	minimum := -(int64(1) << (bits - 1))
	maximum := (int64(1) << (bits - 1)) - 1
	if delta < minimum || delta > maximum {
		return 0, fmt.Errorf("branch delta %d exceeds signed %d-bit range", delta, bits)
	}
	return uint32(delta) & uint32((uint64(1)<<bits)-1), nil
}

const (
	arm64X0  = uint32(0)
	arm64X1  = uint32(1)
	arm64X2  = uint32(2)
	arm64X9  = uint32(9)
	arm64X30 = uint32(30)
	arm64SP  = uint32(31)

	arm64D0 = uint32(0)
	arm64D1 = uint32(1)
	arm64D2 = uint32(2)

	arm64ConditionEQ = uint32(0)
	arm64ConditionNE = uint32(1)
	arm64ConditionMI = uint32(4)
	arm64ConditionVS = uint32(6)
	arm64ConditionLS = uint32(9)
	arm64ConditionGE = uint32(10)
	arm64ConditionGT = uint32(12)

	arm64B          = uint32(0x14000000)
	arm64BL         = uint32(0x94000000)
	arm64BranchCond = uint32(0x54000000)
	arm64CBZW       = uint32(0x34000000)
	arm64CBZX       = uint32(0xb4000000)
	arm64RET        = uint32(0xd65f03c0)
	arm64NOP        = uint32(0xd503201f)
)

const backendNativeARM64MaximumFrameSize = 4080

type backendNativeARM64Frame struct {
	captureOffset      int
	closureCellsOffset int
	phiScratchOffset   int
	linkOffset         int
	size               int
}

type backendNativeARM64BranchKind uint8

const (
	backendNativeARM64BranchAlways backendNativeARM64BranchKind = iota
	backendNativeARM64BranchCondition
	backendNativeARM64BranchZeroW
	backendNativeARM64BranchZeroX
)

type backendNativeARM64BranchFixup struct {
	instruction int
	label       int
	kind        backendNativeARM64BranchKind
	condition   uint32
	register    uint32
}

type backendNativeARM64LiteralFixup struct {
	instruction int
	label       int
	register    uint32
}

type backendNativeARM64Literal struct {
	bits  uint64
	label int
}

type backendNativeARM64FunctionEmitter struct {
	candidate      *backendNativeCandidate
	candidates     []*backendNativeCandidate
	frame          backendNativeARM64Frame
	words          []uint32
	calls          []backendNativeARM64CallFixup
	branches       []backendNativeARM64BranchFixup
	literalFixups  []backendNativeARM64LiteralFixup
	literals       []backendNativeARM64Literal
	literalLabels  map[uint64]int
	labelPositions []int
	blockLabels    []int
	failureLabel   int
}

func planBackendNativeARM64Frame(candidate *backendNativeCandidate) (backendNativeARM64Frame, error) {
	if candidate == nil || candidate.ir == nil {
		return backendNativeARM64Frame{}, fmt.Errorf("nil candidate")
	}
	maximumPhiCopies := 0
	for edgeIndex := range candidate.ir.edges {
		count := 0
		for _, copy := range candidate.ir.edges[edgeIndex].phiCopies {
			if candidate.ir.validBackendValue(copy.destination) &&
				candidate.plan.used[copy.destination-1] {
				count++
			}
		}
		if count > maximumPhiCopies {
			maximumPhiCopies = count
		}
	}
	frame := backendNativeARM64Frame{}
	frame.captureOffset = len(candidate.ir.values) * 8
	frame.closureCellsOffset = frame.captureOffset + len(candidate.captureUpvalues)*8
	frame.phiScratchOffset = frame.closureCellsOffset + candidate.plan.closures.cellCount*8
	frame.linkOffset = frame.phiScratchOffset + maximumPhiCopies*8
	frame.size = (frame.linkOffset + 8 + 15) &^ 15
	if frame.size == 0 || frame.size > backendNativeARM64MaximumFrameSize {
		return backendNativeARM64Frame{}, fmt.Errorf(
			"stack frame %d exceeds ARM64 immediate-frame limit %d",
			frame.size,
			backendNativeARM64MaximumFrameSize,
		)
	}
	return frame, nil
}

func emitBackendNativeARM64Function(
	candidate *backendNativeCandidate,
	candidates []*backendNativeCandidate,
) (backendNativeARM64FunctionCode, error) {
	frame, err := planBackendNativeARM64Frame(candidate)
	if err != nil {
		return backendNativeARM64FunctionCode{}, err
	}
	emitter := &backendNativeARM64FunctionEmitter{
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
	if err := emitter.emitPrologue(); err != nil {
		return backendNativeARM64FunctionCode{}, err
	}
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
				return backendNativeARM64FunctionCode{}, err
			}
			if terminated {
				break
			}
		}
		if !terminated {
			if len(block.successors) != 1 {
				return backendNativeARM64FunctionCode{}, fmt.Errorf(
					"block %d has no terminator and %d successors",
					blockIndex,
					len(block.successors),
				)
			}
			if err := emitter.emitGoto(int32(blockIndex), block.successors[0]); err != nil {
				return backendNativeARM64FunctionCode{}, err
			}
		}
	}
	// Verified IR should make this unreachable; keeping it as a replay exit
	// prevents accidental fallthrough into the literal pool if that changes.
	emitter.emitBranch(emitter.failureLabel)
	emitter.markLabel(emitter.failureLabel)
	emitter.emitMoveImmediate(arm64X0, 0)
	emitter.emitEpilogue()
	entryWord := emitter.emitBoundaryAdapter()
	if err := emitter.finish(); err != nil {
		return backendNativeARM64FunctionCode{}, err
	}
	return backendNativeARM64FunctionCode{
		words:     emitter.words,
		entryWord: entryWord,
		calls:     emitter.calls,
	}, nil
}

func (emitter *backendNativeARM64FunctionEmitter) emitPrologue() error {
	emitter.words = append(emitter.words, arm64SubImmediate(arm64SP, arm64SP, emitter.frame.size))
	emitter.emitStoreX(arm64X30, arm64SP, emitter.frame.linkOffset)
	for parameter := 0; parameter < emitter.candidate.ir.params; parameter++ {
		id := emitter.candidate.ir.initial[parameter]
		if !emitter.candidate.ir.validBackendValue(id) {
			return fmt.Errorf("parameter %d has invalid SSA value", parameter)
		}
		emitter.emitStoreValueD(uint32(parameter), id)
	}
	for capture := range emitter.candidate.captureUpvalues {
		emitter.emitStoreD(
			uint32(emitter.candidate.parameterCount+capture),
			arm64SP,
			emitter.frame.captureOffset+capture*8,
		)
	}
	if emitter.candidate.options.selfRecursive {
		parameter := emitter.candidate.ir.initial[0]
		emitter.emitNaNGuard(parameter)
		emitter.emitLoadValueD(arm64D0, parameter)
		emitter.emitLoadLiteralD(arm64D1, math.Float64bits(backendGoMaxPreparedRecursiveArgument))
		emitter.words = append(emitter.words, arm64FCMP(arm64D0, arm64D1))
		emitter.emitConditionalBranch(emitter.failureLabel, arm64ConditionGT)
	}
	return nil
}

// emitBoundaryAdapter keeps the audited Go/native pointer ABI out of direct
// native calls. Bodies accept scalar arguments in D0-D7 and return their
// scalar in D0 with a prepared status in X0.
func (emitter *backendNativeARM64FunctionEmitter) emitBoundaryAdapter() int {
	const (
		adapterFrameSize       = 16
		adapterLinkOffset      = 0
		adapterResultPtrOffset = 8
	)
	entryWord := len(emitter.words)
	failureLabel := emitter.newLabel()
	emitter.words = append(emitter.words, arm64SubImmediate(arm64SP, arm64SP, adapterFrameSize))
	emitter.emitStoreX(arm64X30, arm64SP, adapterLinkOffset)
	emitter.emitStoreX(arm64X2, arm64SP, adapterResultPtrOffset)
	emitter.words = append(emitter.words, arm64CompareImmediate(arm64X1, emitter.candidate.argumentCount))
	emitter.emitConditionalBranch(failureLabel, arm64ConditionNE)
	for argument := 0; argument < emitter.candidate.argumentCount; argument++ {
		emitter.emitLoadD(uint32(argument), arm64X0, argument*8)
	}
	emitter.calls = append(emitter.calls, backendNativeARM64CallFixup{
		instruction: len(emitter.words),
		targetProto: emitter.candidate.protoID,
	})
	emitter.words = append(emitter.words, arm64BL)
	emitter.emitZeroBranch(failureLabel, arm64X0, true)
	emitter.emitLoadX(arm64X9, arm64SP, adapterResultPtrOffset)
	emitter.emitStoreD(arm64D0, arm64X9, 0)
	emitter.emitBoundaryAdapterEpilogue(adapterFrameSize, adapterLinkOffset)
	emitter.markLabel(failureLabel)
	emitter.emitMoveImmediate(arm64X0, 0)
	emitter.emitBoundaryAdapterEpilogue(adapterFrameSize, adapterLinkOffset)
	return entryWord
}

func (emitter *backendNativeARM64FunctionEmitter) emitBoundaryAdapterEpilogue(
	frameSize, linkOffset int,
) {
	emitter.emitLoadX(arm64X30, arm64SP, linkOffset)
	emitter.words = append(emitter.words, arm64AddImmediate(arm64SP, arm64SP, frameSize), arm64RET)
}

func (emitter *backendNativeARM64FunctionEmitter) emitOperation(
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
			emitter.emitLoadLiteralD(arm64D0, constant.bits)
			emitter.emitStoreValueD(arm64D0, destination)
		case BoolKind:
			emitter.emitMoveImmediate(arm64X9, uint16(constant.bits))
			emitter.emitStoreValueX(arm64X9, destination)
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
		emitter.emitLoadValueX(arm64X9, source)
		emitter.emitStoreValueX(arm64X9, destination)
	case opClosure:
		local, ok := emitter.candidate.plan.closures.local(operation)
		if !ok {
			return false, fmt.Errorf("PC %d has no scalar closure", operation.pc)
		}
		for upvalue, capture := range local.captures {
			if capture == invalidBackendValueID {
				continue
			}
			emitter.emitLoadValueD(arm64D0, capture)
			emitter.emitStoreD(
				arm64D0,
				arm64SP,
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
		emitter.emitLoadD(arm64D0, arm64SP, emitter.frame.captureOffset+capture*8)
		emitter.emitStoreValueD(arm64D0, destination)
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
		emitter.emitLoadValueD(arm64D0, left)
		emitter.emitLoadValueD(arm64D1, right)
		emitter.emitNumericBinary(operation.op)
		emitter.emitStoreValueD(arm64D0, destination)
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		left, err := use(operation.b)
		if err != nil {
			return false, err
		}
		emitter.emitLoadValueD(arm64D0, left)
		emitter.emitLoadLiteralD(arm64D1, emitter.candidate.ir.constants[operation.c].bits)
		emitter.emitNumericBinary(operation.op)
		emitter.emitStoreValueD(arm64D0, destination)
	case opNeg:
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		source, err := use(operation.b)
		if err != nil {
			return false, err
		}
		emitter.emitLoadValueD(arm64D0, source)
		emitter.words = append(emitter.words, arm64FNEG(arm64D0, arm64D0))
		emitter.emitStoreValueD(arm64D0, destination)
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
		emitter.emitLoadValueD(arm64D0, left)
		emitter.emitLoadValueD(arm64D1, right)
		emitter.words = append(emitter.words, arm64FCMP(arm64D0, arm64D1))
		condition, ok := backendNativeARM64ComparisonCondition(operation.op)
		if !ok {
			return false, fmt.Errorf("PC %d has unsupported comparison %s", operation.pc, opcodeName(operation.op))
		}
		emitter.words = append(emitter.words, arm64CSETX(arm64X9, condition))
		emitter.emitStoreValueX(arm64X9, destination)
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
		emitter.emitLoadValueD(arm64D0, step)
		emitter.emitLoadLiteralD(arm64D1, math.Float64bits(0))
		emitter.words = append(emitter.words, arm64FCMP(arm64D0, arm64D1))
		emitter.emitConditionalBranch(positive, arm64ConditionGT)
		emitter.emitLoadValueD(arm64D0, loop)
		emitter.emitLoadValueD(arm64D1, limit)
		emitter.words = append(emitter.words, arm64FCMP(arm64D0, arm64D1))
		if err := emitter.emitConditionalEdges(int32(block.id), exit, next, arm64ConditionMI); err != nil {
			return false, err
		}
		emitter.markLabel(positive)
		emitter.emitLoadValueD(arm64D0, loop)
		emitter.emitLoadValueD(arm64D1, limit)
		emitter.words = append(emitter.words, arm64FCMP(arm64D0, arm64D1))
		if err := emitter.emitConditionalEdges(int32(block.id), exit, next, arm64ConditionGT); err != nil {
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
		emitter.emitLoadValueD(arm64D0, loop)
		emitter.emitLoadValueD(arm64D1, step)
		emitter.words = append(emitter.words, arm64FADD(arm64D0, arm64D0, arm64D1))
		emitter.emitStoreValueD(arm64D0, destination)
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
		emitter.emitLoadValueD(arm64D0, left)
		emitter.emitLoadLiteralD(arm64D1, emitter.candidate.ir.constants[operation.b].bits)
		emitter.words = append(emitter.words, arm64FCMP(arm64D0, arm64D1))
		condition, ok := backendNativeARM64ComparisonCondition(operation.op)
		if !ok {
			return false, fmt.Errorf("PC %d has unsupported branch %s", operation.pc, opcodeName(operation.op))
		}
		if err := emitter.emitConditionalEdges(
			int32(block.id),
			emitter.candidate.ir.pcToBlock[operation.targetPC],
			emitter.nextBlock(block),
			condition,
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
		emitter.emitLoadValueD(arm64D0, left)
		emitter.emitLoadValueD(arm64D1, right)
		emitter.words = append(emitter.words, arm64FCMP(arm64D0, arm64D1))
		condition, ok := backendNativeARM64ComparisonCondition(operation.op)
		if !ok {
			return false, fmt.Errorf("PC %d has unsupported branch %s", operation.pc, opcodeName(operation.op))
		}
		if err := emitter.emitConditionalEdges(
			int32(block.id),
			emitter.candidate.ir.pcToBlock[operation.targetPC],
			emitter.nextBlock(block),
			condition,
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
		emitter.emitLoadValueX(arm64X9, condition)
		if err := emitter.emitZeroEdges(
			int32(block.id),
			emitter.candidate.ir.pcToBlock[operation.targetPC],
			emitter.nextBlock(block),
			arm64X9,
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
			emitter.emitLoadValueD(uint32(argument), value)
		}
		capturePlan, ok := emitter.candidate.callCapturePlan(operation, target)
		if !ok {
			return false, fmt.Errorf("PC %d lost its native capture plan", operation.pc)
		}
		if capturePlan.forward {
			for capture := range emitter.candidate.captureUpvalues {
				emitter.emitLoadD(
					uint32(int(operation.callArgCount)+capture),
					arm64SP,
					emitter.frame.captureOffset+capture*8,
				)
			}
		} else {
			for capture, cell := range capturePlan.cells {
				emitter.emitLoadD(
					uint32(int(operation.callArgCount)+capture),
					arm64SP,
					emitter.frame.closureCellsOffset+cell*8,
				)
			}
		}
		emitter.calls = append(emitter.calls, backendNativeARM64CallFixup{
			instruction: len(emitter.words),
			targetProto: targetProto,
		})
		emitter.words = append(emitter.words, arm64BL)
		emitter.emitZeroBranch(emitter.failureLabel, arm64X0, true)
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		emitter.emitStoreValueD(arm64D0, destination)
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
		emitter.emitLoadValueD(arm64D0, value)
		emitter.emitMoveImmediate(arm64X0, 1)
		emitter.emitEpilogue()
		return true, nil
	default:
		return false, fmt.Errorf("PC %d uses unsupported opcode %s", operation.pc, opcodeName(operation.op))
	}
	return false, nil
}

func (emitter *backendNativeARM64FunctionEmitter) emitNumericBinary(operation opcode) {
	switch operation {
	case opAdd, opAddK:
		emitter.words = append(emitter.words, arm64FADD(arm64D0, arm64D0, arm64D1))
	case opSub, opSubK:
		emitter.words = append(emitter.words, arm64FSUB(arm64D0, arm64D0, arm64D1))
	case opMul, opMulK:
		emitter.words = append(emitter.words, arm64FMUL(arm64D0, arm64D0, arm64D1))
	case opDiv, opDivK:
		emitter.words = append(emitter.words, arm64FDIV(arm64D0, arm64D0, arm64D1))
	case opIDiv, opIDivK:
		emitter.words = append(
			emitter.words,
			arm64FDIV(arm64D0, arm64D0, arm64D1),
			arm64FRINTM(arm64D0, arm64D0),
		)
	case opMod, opModK:
		emitter.words = append(
			emitter.words,
			arm64FDIV(arm64D2, arm64D0, arm64D1),
			arm64FRINTM(arm64D2, arm64D2),
			arm64FMUL(arm64D2, arm64D2, arm64D1),
			arm64FSUB(arm64D0, arm64D0, arm64D2),
		)
	}
}

func backendNativeARM64ComparisonCondition(operation opcode) (uint32, bool) {
	switch operation {
	case opEqual:
		return arm64ConditionEQ, true
	case opNotEqual, opJumpIfNotEqualK:
		return arm64ConditionNE, true
	case opLess, opJumpIfLess, opJumpIfLessK:
		return arm64ConditionMI, true
	case opLessEqual:
		return arm64ConditionLS, true
	case opGreater, opJumpIfGreater, opJumpIfGreaterK:
		return arm64ConditionGT, true
	case opGreaterEqual:
		return arm64ConditionGE, true
	case opJumpIfNotLess, opJumpIfNotLessK:
		return arm64ConditionGE, true
	case opJumpIfNotGreater, opJumpIfNotGreaterK:
		return arm64ConditionLS, true
	default:
		return 0, false
	}
}

func (emitter *backendNativeARM64FunctionEmitter) emitNaNGuard(values ...backendValueID) {
	for _, value := range values {
		emitter.emitLoadValueD(arm64D0, value)
		emitter.words = append(emitter.words, arm64FCMP(arm64D0, arm64D0))
		emitter.emitConditionalBranch(emitter.failureLabel, arm64ConditionVS)
	}
}

func (emitter *backendNativeARM64FunctionEmitter) nextBlock(block *backendBlockIR) int32 {
	if block != nil && int(block.last) < len(emitter.candidate.ir.ops) {
		return emitter.candidate.ir.pcToBlock[block.last]
	}
	return -1
}

func (emitter *backendNativeARM64FunctionEmitter) emitConditionalEdges(
	from, taken, notTaken int32,
	condition uint32,
) error {
	takenEdge := emitter.newLabel()
	emitter.emitConditionalBranch(takenEdge, condition)
	if err := emitter.emitGoto(from, notTaken); err != nil {
		return err
	}
	emitter.markLabel(takenEdge)
	return emitter.emitGoto(from, taken)
}

func (emitter *backendNativeARM64FunctionEmitter) emitZeroEdges(
	from, taken, notTaken int32,
	register uint32,
) error {
	takenEdge := emitter.newLabel()
	emitter.emitZeroBranch(takenEdge, register, true)
	if err := emitter.emitGoto(from, notTaken); err != nil {
		return err
	}
	emitter.markLabel(takenEdge)
	return emitter.emitGoto(from, taken)
}

func (emitter *backendNativeARM64FunctionEmitter) emitGoto(from, to int32) error {
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
			emitter.emitLoadValueX(arm64X9, copy.source)
			emitter.emitStoreX(
				arm64X9,
				arm64SP,
				emitter.frame.phiScratchOffset+copyCount*8,
			)
			copyCount++
		}
		copyCount = 0
		for _, copy := range edge.phiCopies {
			if !emitter.candidate.ir.validBackendValue(copy.destination) ||
				!emitter.candidate.plan.used[copy.destination-1] {
				continue
			}
			emitter.emitLoadX(
				arm64X9,
				arm64SP,
				emitter.frame.phiScratchOffset+copyCount*8,
			)
			emitter.emitStoreValueX(arm64X9, copy.destination)
			copyCount++
		}
		emitter.emitBranch(emitter.blockLabels[to])
		return nil
	}
	return fmt.Errorf("CFG edge %d -> %d is unavailable", from, to)
}

func (emitter *backendNativeARM64FunctionEmitter) emitEpilogue() {
	emitter.emitLoadX(arm64X30, arm64SP, emitter.frame.linkOffset)
	emitter.words = append(emitter.words, arm64AddImmediate(arm64SP, arm64SP, emitter.frame.size), arm64RET)
}

func (emitter *backendNativeARM64FunctionEmitter) valueOffset(id backendValueID) int {
	return (int(id) - 1) * 8
}

func (emitter *backendNativeARM64FunctionEmitter) emitLoadValueD(register uint32, id backendValueID) {
	emitter.emitLoadD(register, arm64SP, emitter.valueOffset(id))
}

func (emitter *backendNativeARM64FunctionEmitter) emitStoreValueD(register uint32, id backendValueID) {
	emitter.emitStoreD(register, arm64SP, emitter.valueOffset(id))
}

func (emitter *backendNativeARM64FunctionEmitter) emitLoadValueX(register uint32, id backendValueID) {
	emitter.emitLoadX(register, arm64SP, emitter.valueOffset(id))
}

func (emitter *backendNativeARM64FunctionEmitter) emitStoreValueX(register uint32, id backendValueID) {
	emitter.emitStoreX(register, arm64SP, emitter.valueOffset(id))
}

func (emitter *backendNativeARM64FunctionEmitter) emitLoadD(register, base uint32, offset int) {
	emitter.words = append(emitter.words, arm64LoadD(register, base, offset))
}

func (emitter *backendNativeARM64FunctionEmitter) emitStoreD(register, base uint32, offset int) {
	emitter.words = append(emitter.words, arm64StoreD(register, base, offset))
}

func (emitter *backendNativeARM64FunctionEmitter) emitLoadX(register, base uint32, offset int) {
	emitter.words = append(emitter.words, arm64LoadX(register, base, offset))
}

func (emitter *backendNativeARM64FunctionEmitter) emitStoreX(register, base uint32, offset int) {
	emitter.words = append(emitter.words, arm64StoreX(register, base, offset))
}

func (emitter *backendNativeARM64FunctionEmitter) emitMoveImmediate(register uint32, value uint16) {
	emitter.words = append(emitter.words, arm64MoveImmediate(register, value))
}

func (emitter *backendNativeARM64FunctionEmitter) newLabel() int {
	label := len(emitter.labelPositions)
	emitter.labelPositions = append(emitter.labelPositions, -1)
	return label
}

func (emitter *backendNativeARM64FunctionEmitter) markLabel(label int) {
	if label >= 0 && label < len(emitter.labelPositions) {
		emitter.labelPositions[label] = len(emitter.words)
	}
}

func (emitter *backendNativeARM64FunctionEmitter) emitBranch(label int) {
	emitter.branches = append(emitter.branches, backendNativeARM64BranchFixup{
		instruction: len(emitter.words),
		label:       label,
		kind:        backendNativeARM64BranchAlways,
	})
	emitter.words = append(emitter.words, arm64B)
}

func (emitter *backendNativeARM64FunctionEmitter) emitConditionalBranch(label int, condition uint32) {
	emitter.branches = append(emitter.branches, backendNativeARM64BranchFixup{
		instruction: len(emitter.words),
		label:       label,
		kind:        backendNativeARM64BranchCondition,
		condition:   condition,
	})
	emitter.words = append(emitter.words, arm64BranchCond|condition)
}

func (emitter *backendNativeARM64FunctionEmitter) emitZeroBranch(
	label int,
	register uint32,
	wide bool,
) {
	kind := backendNativeARM64BranchZeroW
	word := arm64CBZW
	if wide {
		kind = backendNativeARM64BranchZeroX
		word = arm64CBZX
	}
	emitter.branches = append(emitter.branches, backendNativeARM64BranchFixup{
		instruction: len(emitter.words),
		label:       label,
		kind:        kind,
		register:    register,
	})
	emitter.words = append(emitter.words, word|register)
}

func (emitter *backendNativeARM64FunctionEmitter) emitLoadLiteralD(register uint32, bits uint64) {
	label, ok := emitter.literalLabels[bits]
	if !ok {
		label = emitter.newLabel()
		emitter.literalLabels[bits] = label
		emitter.literals = append(emitter.literals, backendNativeARM64Literal{bits: bits, label: label})
	}
	emitter.literalFixups = append(emitter.literalFixups, backendNativeARM64LiteralFixup{
		instruction: len(emitter.words),
		label:       label,
		register:    register,
	})
	emitter.words = append(emitter.words, 0x5c000000|register)
}

func (emitter *backendNativeARM64FunctionEmitter) finish() error {
	if len(emitter.words)%2 != 0 {
		emitter.words = append(emitter.words, arm64NOP)
	}
	for _, literal := range emitter.literals {
		emitter.markLabel(literal.label)
		emitter.words = append(
			emitter.words,
			uint32(literal.bits),
			uint32(literal.bits>>32),
		)
	}
	for _, fixup := range emitter.branches {
		if fixup.label < 0 || fixup.label >= len(emitter.labelPositions) ||
			emitter.labelPositions[fixup.label] < 0 {
			return fmt.Errorf("unresolved ARM64 branch label %d", fixup.label)
		}
		bits := 26
		if fixup.kind != backendNativeARM64BranchAlways {
			bits = 19
		}
		encoded, err := encodeBackendNativeARM64Delta(
			(emitter.labelPositions[fixup.label]-fixup.instruction)*4,
			bits,
		)
		if err != nil {
			return err
		}
		switch fixup.kind {
		case backendNativeARM64BranchAlways:
			emitter.words[fixup.instruction] = arm64B | encoded
		case backendNativeARM64BranchCondition:
			emitter.words[fixup.instruction] = arm64BranchCond | encoded<<5 | fixup.condition
		case backendNativeARM64BranchZeroW:
			emitter.words[fixup.instruction] = arm64CBZW | encoded<<5 | fixup.register
		case backendNativeARM64BranchZeroX:
			emitter.words[fixup.instruction] = arm64CBZX | encoded<<5 | fixup.register
		default:
			return fmt.Errorf("unknown ARM64 branch fixup kind %d", fixup.kind)
		}
	}
	for _, fixup := range emitter.literalFixups {
		if fixup.label < 0 || fixup.label >= len(emitter.labelPositions) ||
			emitter.labelPositions[fixup.label] < 0 {
			return fmt.Errorf("unresolved ARM64 literal label %d", fixup.label)
		}
		encoded, err := encodeBackendNativeARM64Delta(
			(emitter.labelPositions[fixup.label]-fixup.instruction)*4,
			19,
		)
		if err != nil {
			return err
		}
		emitter.words[fixup.instruction] = 0x5c000000 | encoded<<5 | fixup.register
	}
	return nil
}

func arm64SubImmediate(destination, source uint32, immediate int) uint32 {
	return 0xd1000000 | uint32(immediate)<<10 | source<<5 | destination
}

func arm64AddImmediate(destination, source uint32, immediate int) uint32 {
	return 0x91000000 | uint32(immediate)<<10 | source<<5 | destination
}

func arm64CompareImmediate(source uint32, immediate int) uint32 {
	return 0xf100001f | uint32(immediate)<<10 | source<<5
}

func arm64MoveImmediate(destination uint32, immediate uint16) uint32 {
	return 0xd2800000 | uint32(immediate)<<5 | destination
}

func arm64LoadX(destination, source uint32, offset int) uint32 {
	return 0xf9400000 | uint32(offset/8)<<10 | source<<5 | destination
}

func arm64StoreX(source, destination uint32, offset int) uint32 {
	return 0xf9000000 | uint32(offset/8)<<10 | destination<<5 | source
}

func arm64LoadD(destination, source uint32, offset int) uint32 {
	return 0xfd400000 | uint32(offset/8)<<10 | source<<5 | destination
}

func arm64StoreD(source, destination uint32, offset int) uint32 {
	return 0xfd000000 | uint32(offset/8)<<10 | destination<<5 | source
}

func arm64FADD(destination, left, right uint32) uint32 {
	return 0x1e602800 | right<<16 | left<<5 | destination
}

func arm64FSUB(destination, left, right uint32) uint32 {
	return 0x1e603800 | right<<16 | left<<5 | destination
}

func arm64FMUL(destination, left, right uint32) uint32 {
	return 0x1e600800 | right<<16 | left<<5 | destination
}

func arm64FDIV(destination, left, right uint32) uint32 {
	return 0x1e601800 | right<<16 | left<<5 | destination
}

func arm64FRINTM(destination, source uint32) uint32 {
	return 0x1e654000 | source<<5 | destination
}

func arm64FNEG(destination, source uint32) uint32 {
	return 0x1e614000 | source<<5 | destination
}

func arm64FCMP(left, right uint32) uint32 {
	return 0x1e602000 | right<<16 | left<<5
}

func arm64CSETX(destination, condition uint32) uint32 {
	return 0x9a9f07e0 | (condition^1)<<12 | destination
}
