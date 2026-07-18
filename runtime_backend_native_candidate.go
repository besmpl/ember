package ember

import "strconv"

const (
	backendNativeMaximumParameters      = 8
	backendNativeMaximumStackPathBytes  = 64 << 10
	backendNativeMaximumRecursiveFrames = backendGoMaxPreparedRecursiveArgument + 2
)

// backendNativeCandidate is the target-independent numeric subset accepted by
// reload-time native backends. It owns semantic selection and static-call
// dependencies; ISA emitters own only instruction selection and relocation.
type backendNativeCandidate struct {
	protoID         int32
	ir              *backendProtoIR
	plan            backendGoNumericPlan
	options         backendGoNumericOptions
	parameterCount  int
	argumentCount   int
	captureUpvalues []int32
	dependencies    []int32
}

type backendNativeCallCapturePlan struct {
	forward bool
	cells   []int
}

func buildBackendNativeModuleCandidates(
	irs []*backendProtoIR,
	moduleIndex int,
) ([]*backendNativeCandidate, error) {
	protoIDs := make([]int32, 0, len(irs)-1)
	for protoID := 1; protoID < len(irs); protoID++ {
		protoIDs = append(protoIDs, int32(protoID))
	}
	prefix := "backendNativeM" + strconv.Itoa(moduleIndex)
	targets, err := inferBackendGoNumericTargets(irs, protoIDs, prefix)
	if err != nil {
		return nil, err
	}
	upvalueTargets := inferBackendGoNumericProgramUpvalueTargets(irs, protoIDs, targets)
	common := backendGoNumericOptions{
		packageName:          "ember",
		directTargets:        targets,
		targetUpvalueTargets: upvalueTargets,
	}

	candidates := make([]*backendNativeCandidate, len(irs))
	for protoIndex := range irs {
		options := common
		if protoIndex == 0 {
			options.functionName = prefix + "Root"
		} else {
			options = backendGoNumericTargetOptions(common, targets[protoIndex])
		}
		if protoIndex < len(upvalueTargets) {
			options.upvalueTargets = upvalueTargets[protoIndex]
		}
		candidate, ok := buildBackendNativeCandidate(
			irs,
			int32(protoIndex),
			options,
			targets,
		)
		if ok {
			candidates[protoIndex] = candidate
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
	return candidates, nil
}

func buildBackendNativeCandidate(
	irs []*backendProtoIR,
	protoID int32,
	options backendGoNumericOptions,
	targets []backendGoNumericTarget,
) (*backendNativeCandidate, bool) {
	if protoID < 0 || int(protoID) >= len(irs) {
		return nil, false
	}
	ir := irs[protoID]
	if ir == nil || ir.variadic || ir.params > backendNativeMaximumParameters ||
		backendGoNumericReceiverTableCount(options.receiverTable, options.receiverTables) != 0 {
		return nil, false
	}
	captureUpvalues, ok := backendNativeCaptureUpvalues(ir, options)
	if !ok || ir.params+len(captureUpvalues) > backendNativeMaximumParameters {
		return nil, false
	}
	parameterTags, ok := backendGoNumericParameterTags(ir, 0)
	if !ok || len(parameterTags) != ir.params {
		return nil, false
	}
	for _, tags := range parameterTags {
		if tags != backendTagNumber {
			return nil, false
		}
	}
	plan, err := buildBackendGoNumericPlan(ir, options)
	if err != nil {
		return nil, false
	}
	resultTypes, err := backendGoNumericResultTypes(ir, plan, options)
	if err != nil || len(resultTypes) != 1 || resultTypes[0] != "float64" {
		return nil, false
	}
	candidate := &backendNativeCandidate{
		protoID:         protoID,
		ir:              ir,
		plan:            plan,
		options:         options,
		parameterCount:  ir.params,
		argumentCount:   ir.params + len(captureUpvalues),
		captureUpvalues: captureUpvalues,
	}
	dependencies, ok := candidate.validateOperations(targets)
	if !ok {
		return nil, false
	}
	candidate.dependencies = dependencies
	return candidate, true
}

func (candidate *backendNativeCandidate) validateOperations(
	targets []backendGoNumericTarget,
) ([]int32, bool) {
	dependencies := make([]int32, 0)
	for blockIndex := range candidate.ir.blocks {
		block := &candidate.ir.blocks[blockIndex]
		if !block.reachable {
			continue
		}
		for pc := block.first; pc < block.last; pc++ {
			operation := &candidate.ir.ops[pc]
			if backendGoNumericOperationDead(candidate.plan, operation) {
				continue
			}
			switch operation.op {
			case opLoadConst:
				if operation.b < 0 || int(operation.b) >= len(candidate.ir.constants) {
					return nil, false
				}
				kind := candidate.ir.constants[operation.b].kind
				if kind != NumberKind && kind != BoolKind {
					return nil, false
				}
			case opMove, opAdd, opSub, opMul, opDiv, opMod, opIDiv,
				opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
				opNeg, opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual,
				opNumericForCheck, opNumericForLoop, opJumpIfNotEqualK,
				opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK,
				opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater,
				opJumpIfFalse, opJump, opReturnOne, opReturn:
			case opClosure:
				local, ok := candidate.plan.closures.local(operation)
				if !ok {
					return nil, false
				}
				for _, capture := range local.captures {
					if capture != invalidBackendValueID && candidate.plan.tags[capture-1] != backendTagNumber {
						return nil, false
					}
				}
			case opGetUpvalue:
				if _, ok := candidate.captureIndex(operation.b); !ok || len(operation.defs) == 0 {
					return nil, false
				}
				for _, definition := range operation.defs {
					if candidate.plan.tags[definition.value-1] != backendTagNumber {
						return nil, false
					}
				}
			case opCall, opCallLocalOne, opCallUpvalueOne:
				target, ok := candidate.directTarget(operation)
				if !ok || operation.callArgCount < 0 {
					return nil, false
				}
				capturePlan, ok := candidate.callCapturePlan(operation, target)
				if !ok || int(operation.callArgCount)+len(capturePlan.cells) > backendNativeMaximumParameters {
					return nil, false
				}
				if capturePlan.forward && int(operation.callArgCount)+len(candidate.captureUpvalues) > backendNativeMaximumParameters {
					return nil, false
				}
				targetProto := backendNativeTargetProtoID(targets, target.ir)
				if targetProto < 0 || operation.callArgCount != int32(target.ir.params) {
					return nil, false
				}
				dependencies = appendBackendNativeDependency(dependencies, targetProto)
			default:
				return nil, false
			}
		}
	}
	return dependencies, true
}

func (candidate *backendNativeCandidate) directTarget(
	operation *backendOperationIR,
) (backendGoNumericTarget, bool) {
	switch operation.op {
	case opCallUpvalueOne:
		if backendGoNumericSelfRecursiveCall(candidate.ir, candidate.options, operation) {
			for _, target := range candidate.options.directTargets {
				if target.ir == candidate.ir {
					return target, true
				}
			}
			return backendGoNumericTarget{}, false
		}
		return backendGoNumericUpvalueTarget(candidate.options, operation)
	case opCall, opCallLocalOne:
		target, ok := backendGoNumericDirectTarget(candidate.ir, candidate.options, operation)
		return target, ok && backendGoNumericScalarReplacedCall(candidate.ir, candidate.options, operation)
	default:
		return backendGoNumericTarget{}, false
	}
}

func backendNativeCaptureUpvalues(ir *backendProtoIR, options backendGoNumericOptions) ([]int32, bool) {
	if ir == nil || len(ir.upvalues) == 0 {
		return nil, ir != nil
	}
	if !options.selfRecursive {
		return nil, backendGoNumericPreparedStaticUpvalues(ir, options)
	}
	self, ok := backendGoNumericSelfRecursiveUpvalue(ir)
	if !ok {
		return nil, false
	}
	captures := make([]int32, 0, len(ir.upvalues)-1)
	for upvalue := range ir.upvalues {
		if int32(upvalue) != self {
			captures = append(captures, int32(upvalue))
		}
	}
	return captures, true
}

func (candidate *backendNativeCandidate) captureIndex(upvalue int32) (int, bool) {
	for index, candidateUpvalue := range candidate.captureUpvalues {
		if candidateUpvalue == upvalue {
			return index, true
		}
	}
	return 0, false
}

func (candidate *backendNativeCandidate) callCapturePlan(
	operation *backendOperationIR,
	target backendGoNumericTarget,
) (backendNativeCallCapturePlan, bool) {
	captures, ok := backendNativeCaptureUpvalues(
		target.ir,
		backendGoNumericTargetOptions(candidate.options, target),
	)
	if !ok {
		return backendNativeCallCapturePlan{}, false
	}
	if len(captures) == 0 {
		return backendNativeCallCapturePlan{}, true
	}
	if backendGoNumericSelfRecursiveCall(candidate.ir, candidate.options, operation) && target.ir == candidate.ir {
		return backendNativeCallCapturePlan{forward: true}, true
	}
	callee := backendOperationUse(operation, operation.b)
	closure, ok := candidate.plan.closures.value(callee)
	targetProto := backendNativeTargetProtoID(candidate.options.directTargets, target.ir)
	if !ok || targetProto < 0 || closure.targetProto != targetProto || closure.cellCount != len(target.ir.upvalues) {
		return backendNativeCallCapturePlan{}, false
	}
	cells := make([]int, len(captures))
	for index, upvalue := range captures {
		cells[index] = closure.cellStart + int(upvalue)
	}
	return backendNativeCallCapturePlan{cells: cells}, true
}

func backendNativeTargetProtoID(targets []backendGoNumericTarget, ir *backendProtoIR) int32 {
	for protoIndex := range targets {
		if targets[protoIndex].ir == ir {
			return int32(protoIndex)
		}
	}
	return -1
}

func appendBackendNativeDependency(dependencies []int32, dependency int32) []int32 {
	for _, existing := range dependencies {
		if existing == dependency {
			return dependencies
		}
	}
	return append(dependencies, dependency)
}

// pruneBackendNativeStackCandidates removes functions whose generated body
// frames cannot fit in the conservative system-stack budget. Each emitter
// supplies its planned frame sizes; semantic fallback remains target neutral.
func pruneBackendNativeStackCandidates(candidates []*backendNativeCandidate, frameSizes []int) {
	count := len(candidates)
	admitted := make([]bool, count)
	ownDepths := make([]int, count)
	pendingDependencies := make([]int, count)
	callers := make([][]int, count)

	for index, candidate := range candidates {
		if candidate == nil || index >= len(frameSizes) || frameSizes[index] <= 0 {
			continue
		}
		ownDepth := frameSizes[index]
		if candidate.options.selfRecursive {
			if ownDepth > backendNativeMaximumStackPathBytes/backendNativeMaximumRecursiveFrames {
				continue
			}
			ownDepth *= backendNativeMaximumRecursiveFrames
		}
		if ownDepth <= backendNativeMaximumStackPathBytes {
			admitted[index] = true
			ownDepths[index] = ownDepth
		}
	}

	for caller, candidate := range candidates {
		if candidate == nil {
			continue
		}
		for _, dependency := range candidate.dependencies {
			dependencyIndex := int(dependency)
			if dependencyIndex == caller {
				if !candidate.options.selfRecursive {
					admitted[caller] = false
				}
				continue
			}
			if dependencyIndex < 0 || dependencyIndex >= count || candidates[dependencyIndex] == nil {
				admitted[caller] = false
				continue
			}
			callers[dependencyIndex] = append(callers[dependencyIndex], caller)
			pendingDependencies[caller]++
			if !admitted[dependencyIndex] {
				admitted[caller] = false
			}
		}
	}

	depths := make([]int, count)
	maximumDependencyDepths := make([]int, count)
	processed := make([]bool, count)
	queued := make([]bool, count)
	queue := make([]int, 0, count)
	enqueue := func(index int) {
		if !queued[index] {
			queued[index] = true
			queue = append(queue, index)
		}
	}
	for index, candidate := range candidates {
		if candidate != nil && (!admitted[index] || pendingDependencies[index] == 0) {
			enqueue(index)
		}
	}

	for head := 0; head < len(queue); head++ {
		index := queue[head]
		processed[index] = true
		if admitted[index] {
			dependencyDepth := maximumDependencyDepths[index]
			if dependencyDepth > backendNativeMaximumStackPathBytes-ownDepths[index] {
				admitted[index] = false
			} else {
				depths[index] = ownDepths[index] + dependencyDepth
			}
		}

		for _, caller := range callers[index] {
			if processed[caller] {
				continue
			}
			if !admitted[index] {
				admitted[caller] = false
				enqueue(caller)
				continue
			}
			pendingDependencies[caller]--
			if depths[index] > maximumDependencyDepths[caller] {
				maximumDependencyDepths[caller] = depths[index]
			}
			if admitted[caller] && pendingDependencies[caller] == 0 {
				enqueue(caller)
			}
		}
	}

	for index, candidate := range candidates {
		if candidate != nil && (!processed[index] || !admitted[index]) {
			candidates[index] = nil
		}
	}
}
