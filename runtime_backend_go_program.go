package ember

import "fmt"

type backendGoNumericProgramOptions struct {
	packageName          string
	functionPrefix       string
	preparedFunctionName string
	entryProto           int32
	coroutineDeadString  machineStringID
}

type backendGoNumericProgramFile struct {
	name    string
	protoID int32
	source  []byte
}

// emitBackendGoNumericProgram composes the private per-Proto proof emitter into
// one deterministic package file set. It deliberately remains below Program's
// public surface until the prepared compiler clears its private proof gates.
func emitBackendGoNumericProgram(
	irs []*backendProtoIR,
	options backendGoNumericProgramOptions,
) ([]backendGoNumericProgramFile, error) {
	protoIDs, err := backendGoNumericReachableProtoIDs(irs, options.entryProto)
	if err != nil {
		return nil, err
	}
	targetIDs := make([]int32, 0, len(protoIDs)-1)
	for _, protoID := range protoIDs {
		if protoID != options.entryProto {
			targetIDs = append(targetIDs, protoID)
		}
	}
	targets, err := inferBackendGoNumericTargets(
		irs, targetIDs, options.functionPrefix+"Target",
	)
	if err != nil {
		return nil, err
	}
	upvalueTargets := inferBackendGoNumericProgramUpvalueTargets(irs, protoIDs, targets)

	descriptorOnly := make(map[int32]bool)
	embeddedCoroutine := make(map[int32]bool)
	for _, protoID := range protoIDs {
		if protoID != options.entryProto {
			if _, ok := backendGoNumericClosureFactory(irs[protoID], targets); ok {
				descriptorOnly[protoID] = true
				continue
			}
			if backendGoNumericHasCoroutineYield(irs[protoID]) {
				continue
			}
		}
		plan, err := buildBackendGoNumericPlan(
			irs[protoID],
			backendGoNumericProgramProtoOptions(protoID, targets, upvalueTargets, options),
		)
		if err != nil {
			return nil, fmt.Errorf("emit backend Go numeric program: Proto %d: %w", protoID, err)
		}
		if plan.coroutines.enabled {
			embeddedCoroutine[plan.coroutines.targetProto] = true
		}
	}
	for _, protoID := range targetIDs {
		if backendGoNumericHasCoroutineYield(irs[protoID]) && !embeddedCoroutine[protoID] {
			return nil, fmt.Errorf(
				"emit backend Go numeric program: coroutine target Proto %d has no proved creator",
				protoID,
			)
		}
	}
	if err := verifyBackendGoNumericProgramNames(
		protoIDs, targets, descriptorOnly, embeddedCoroutine, options,
	); err != nil {
		return nil, err
	}

	files := make([]backendGoNumericProgramFile, 0, len(protoIDs))
	for _, protoID := range protoIDs {
		if descriptorOnly[protoID] || embeddedCoroutine[protoID] {
			continue
		}
		source, err := emitBackendGoNumericProof(
			irs[protoID],
			backendGoNumericProgramProtoOptions(protoID, targets, upvalueTargets, options),
		)
		if err != nil {
			return nil, fmt.Errorf("emit backend Go numeric program: Proto %d: %w", protoID, err)
		}
		files = append(files, backendGoNumericProgramFile{
			name:    fmt.Sprintf("prepared_proto_%d.go", protoID),
			protoID: protoID,
			source:  source,
		})
	}
	return files, nil
}

func verifyBackendGoNumericProgramNames(
	protoIDs []int32,
	targets []backendGoNumericTarget,
	descriptorOnly map[int32]bool,
	embeddedCoroutine map[int32]bool,
	options backendGoNumericProgramOptions,
) error {
	names := make(map[string]bool, len(protoIDs)*2+1)
	add := func(name string) error {
		if name == "" {
			return nil
		}
		if names[name] {
			return fmt.Errorf("emit backend Go numeric program: duplicate generated name %q", name)
		}
		names[name] = true
		return nil
	}
	for _, protoID := range protoIDs {
		if descriptorOnly[protoID] {
			continue
		}
		if protoID == options.entryProto {
			if err := add(options.functionPrefix + "Entry"); err != nil {
				return err
			}
			continue
		}
		target := targets[protoID]
		if err := add(target.functionName); err != nil {
			return err
		}
		if target.selfRecursive {
			if err := add(target.functionName + "Body"); err != nil {
				return err
			}
		}
		if embeddedCoroutine[protoID] {
			if err := add(target.functionName + "State"); err != nil {
				return err
			}
		}
	}
	return add(options.preparedFunctionName)
}

func backendGoNumericProgramProtoOptions(
	protoID int32,
	targets []backendGoNumericTarget,
	upvalueTargets []map[int32]backendGoNumericTarget,
	program backendGoNumericProgramOptions,
) backendGoNumericOptions {
	options := backendGoNumericOptions{
		packageName:          program.packageName,
		directTargets:        targets,
		targetUpvalueTargets: upvalueTargets,
		coroutineDeadString:  program.coroutineDeadString,
	}
	if int(protoID) < len(upvalueTargets) {
		options.upvalueTargets = upvalueTargets[protoID]
	}
	if protoID == program.entryProto {
		options.functionName = program.functionPrefix + "Entry"
		options.preparedFunctionName = program.preparedFunctionName
		return options
	}
	target := targets[protoID]
	options.functionName = target.functionName
	options.selfRecursive = target.selfRecursive
	options.fixedVarargCount = target.fixedVarargCount
	options.receiverTable = target.receiverTable
	options.receiverTables = target.receiverTables
	options.capturedTableFields = target.capturedTableFields
	return options
}

func inferBackendGoNumericProgramUpvalueTargets(
	irs []*backendProtoIR,
	protoIDs []int32,
	targets []backendGoNumericTarget,
) []map[int32]backendGoNumericTarget {
	result := make([]map[int32]backendGoNumericTarget, len(irs))
	for _, protoID := range protoIDs {
		ir := irs[protoID]
		for pc := range ir.ops {
			operation := &ir.ops[pc]
			if operation.op != opCallUpvalueOne {
				continue
			}
			targetProto, ok := backendGoNumericStaticUpvalueTargetProto(irs, protoID, operation.b)
			if !ok || targetProto < 0 || int(targetProto) >= len(targets) || targets[targetProto].ir == nil {
				continue
			}
			if result[protoID] == nil {
				result[protoID] = make(map[int32]backendGoNumericTarget)
			}
			result[protoID][operation.b] = targets[targetProto]
		}
	}
	return result
}

func backendGoNumericStaticUpvalueTargetProto(
	irs []*backendProtoIR,
	protoID int32,
	upvalue int32,
) (int32, bool) {
	if protoID < 0 || int(protoID) >= len(irs) || irs[protoID] == nil ||
		upvalue < 0 || int(upvalue) >= len(irs[protoID].upvalues) {
		return -1, false
	}
	descriptor := irs[protoID].upvalues[upvalue]
	if descriptor.local == 0 {
		return -1, false
	}
	targetProto := int32(-1)
	found := false
	for _, caller := range irs {
		if caller == nil || descriptor.index >= uint32(caller.registers) {
			continue
		}
		for pc := range caller.ops {
			closure := &caller.ops[pc]
			if closure.op != opClosure || closure.targetProto != protoID {
				continue
			}
			captured := backendValueBeforeOperation(caller, closure, int32(descriptor.index))
			if !caller.validBackendValue(captured) {
				return -1, false
			}
			value := &caller.values[captured-1]
			if value.tags != backendTagFunction || value.targetUnknown || len(value.targetProtos) != 1 {
				return -1, false
			}
			if found && targetProto != value.targetProtos[0] {
				return -1, false
			}
			targetProto = value.targetProtos[0]
			found = true
		}
	}
	return targetProto, found
}

func backendGoNumericReachableProtoIDs(irs []*backendProtoIR, entryProto int32) ([]int32, error) {
	if entryProto < 0 || int(entryProto) >= len(irs) || irs[entryProto] == nil {
		return nil, fmt.Errorf("emit backend Go numeric program: invalid entry Proto %d", entryProto)
	}
	seen := make([]bool, len(irs))
	seen[entryProto] = true
	queue := []int32{entryProto}
	for len(queue) != 0 {
		protoID := queue[0]
		queue = queue[1:]
		ir := irs[protoID]
		for pc := range ir.ops {
			operation := &ir.ops[pc]
			targetProto := int32(-1)
			switch {
			case operation.op == opClosure:
				targetProto = operation.targetProto
			case operation.op == opCallUpvalueOne:
				var ok bool
				targetProto, ok = backendGoNumericStaticUpvalueTargetProto(irs, protoID, operation.b)
				if !ok {
					continue
				}
			case operation.call.kind == backendCallDirectProto:
				targetProto = operation.call.targetProto
			}
			if targetProto < 0 {
				continue
			}
			if int(targetProto) >= len(irs) || irs[targetProto] == nil {
				return nil, fmt.Errorf(
					"emit backend Go numeric program: Proto %d PC %d targets unavailable Proto %d",
					protoID, operation.pc, targetProto,
				)
			}
			if !seen[targetProto] {
				seen[targetProto] = true
				queue = append(queue, targetProto)
			}
		}
	}
	protoIDs := make([]int32, 0, len(irs))
	for protoID, reachable := range seen {
		if reachable {
			protoIDs = append(protoIDs, int32(protoID))
		}
	}
	return protoIDs, nil
}
