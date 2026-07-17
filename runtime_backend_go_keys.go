package ember

type backendPreparedStringKey struct {
	first  int32
	second int32
}

type backendGoStructuralKey struct {
	first     backendValueID
	second    backendValueID
	domain    int32
	separator machineStringID
}

type backendGoStructuralKeyPlan struct {
	components     map[backendValueID]backendValueID
	keys           map[backendValueID]backendGoStructuralKey
	concatByPC     map[int32]backendGoStructuralKey
	tostringByPC   map[int32]backendValueID
	scalarReplaced []bool
}

func analyzeBackendGoStructuralKeys(
	ir *backendProtoIR,
	options backendGoNumericOptions,
) backendGoStructuralKeyPlan {
	plan := backendGoStructuralKeyPlan{
		components:     make(map[backendValueID]backendValueID),
		keys:           make(map[backendValueID]backendGoStructuralKey),
		concatByPC:     make(map[int32]backendGoStructuralKey),
		tostringByPC:   make(map[int32]backendValueID),
		scalarReplaced: make([]bool, len(ir.values)),
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opFastCall ||
			nativeFuncID(operation.nativeID) != nativeFuncToString ||
			operation.c != 1 ||
			operation.d != 1 ||
			len(operation.defs) != 1 {
			continue
		}
		source := backendOperationUse(operation, operation.a)
		if !ir.validBackendValue(source) {
			continue
		}
		result := operation.defs[0].value
		plan.components[result] = source
		plan.tostringByPC[operation.pc] = source
		plan.scalarReplaced[result-1] = true
	}
	for iteration := 0; iteration <= len(ir.values); iteration++ {
		changed := false
		for pc := range ir.ops {
			operation := &ir.ops[pc]
			if operation.op != opMove {
				continue
			}
			source, ok := plan.components[backendOperationUse(operation, operation.b)]
			if !ok {
				continue
			}
			for _, definition := range operation.defs {
				if _, exists := plan.components[definition.value]; exists {
					continue
				}
				plan.components[definition.value] = source
				plan.scalarReplaced[definition.value-1] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opConcatChain || operation.c != 3 || len(operation.defs) != 1 {
			continue
		}
		firstString := backendOperationUse(operation, operation.b)
		separator := backendOperationUse(operation, operation.b+1)
		secondString := backendOperationUse(operation, operation.b+2)
		first, firstOK := plan.components[firstString]
		second, secondOK := plan.components[secondString]
		separatorID, ok := backendGoStructuralKeySeparator(ir, separator)
		if !firstOK || !secondOK || !ok {
			continue
		}
		key := backendGoStructuralKey{
			first:     first,
			second:    second,
			domain:    -(operation.pc + 1),
			separator: separatorID,
		}
		result := operation.defs[0].value
		plan.keys[result] = key
		plan.concatByPC[operation.pc] = key
		if ir.validBackendValue(separator) {
			plan.scalarReplaced[separator-1] = true
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opCallLocalOne || len(operation.defs) == 0 {
			continue
		}
		target, ok := backendGoNumericDirectTarget(ir, options, operation)
		targetKey, ok := backendGoStructuralKeyTargetKey(target.ir)
		if !ok || !backendGoStructuralKeyTarget(target.ir) {
			continue
		}
		for _, definition := range operation.defs {
			if definition.register != operation.a {
				continue
			}
			plan.keys[definition.value] = backendGoStructuralKey{
				domain:    operation.call.targetProto + 1,
				separator: targetKey.separator,
			}
		}
	}
	for iteration := 0; iteration <= len(ir.values); iteration++ {
		changed := false
		for pc := range ir.ops {
			operation := &ir.ops[pc]
			if operation.op != opMove {
				continue
			}
			key, ok := plan.keys[backendOperationUse(operation, operation.b)]
			if !ok {
				continue
			}
			for _, definition := range operation.defs {
				if _, exists := plan.keys[definition.value]; !exists {
					plan.keys[definition.value] = key
					changed = true
				}
			}
		}
		for blockIndex := range ir.blocks {
			block := &ir.blocks[blockIndex]
			for phiIndex := range block.phis {
				phi := &block.phis[phiIndex]
				var key backendGoStructuralKey
				ready := true
				found := false
				for inputIndex, input := range phi.inputs {
					if !ir.blocks[block.predecessors[inputIndex]].reachable {
						continue
					}
					candidate, ok := plan.keys[input]
					if !ok {
						ready = false
						break
					}
					if found && candidate.domain != key.domain {
						ready = false
						break
					}
					key = candidate
					found = true
				}
				if ready && found {
					if _, exists := plan.keys[phi.value]; !exists {
						plan.keys[phi.value] = key
						changed = true
					}
				}
			}
		}
		if !changed {
			break
		}
	}
	return plan
}

func backendGoStructuralKeyTarget(ir *backendProtoIR) bool {
	if ir == nil {
		return false
	}
	plan := analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{})
	resultCount, ok := backendGoNumericFixedResultCount(ir)
	if !ok || resultCount != 1 {
		return false
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opReturnOne && operation.op != opReturn {
			continue
		}
		result := backendOperationUse(operation, operation.a)
		if _, ok := plan.keys[result]; !ok {
			return false
		}
	}
	return len(plan.concatByPC) == 1
}

func backendGoStructuralKeySeparator(ir *backendProtoIR, id backendValueID) (machineStringID, bool) {
	if !ir.validBackendValue(id) {
		return invalidMachineStringID, false
	}
	value := &ir.values[id-1]
	if value.kind != backendValueOperation ||
		value.pc < 0 ||
		int(value.pc) >= len(ir.ops) {
		return invalidMachineStringID, false
	}
	operation := &ir.ops[value.pc]
	if operation.op != opLoadConst ||
		operation.b < 0 ||
		int(operation.b) >= len(ir.constants) ||
		ir.constants[operation.b].kind != StringKind {
		return invalidMachineStringID, false
	}
	constant := ir.constants[operation.b]
	stringID := machineStringID(constant.bits)
	if stringID == invalidMachineStringID || uint64(stringID-1) >= uint64(len(ir.stringRecords)) {
		return invalidMachineStringID, false
	}
	record := ir.stringRecords[stringID-1]
	end := uint64(record.offset) + uint64(record.length)
	if record.length == 0 || end > uint64(len(ir.stringData)) {
		return invalidMachineStringID, false
	}
	for _, character := range ir.stringData[record.offset:end] {
		if character != '-' && (character < '0' || character > '9') {
			return stringID, true
		}
	}
	return invalidMachineStringID, false
}

func backendGoStructuralKeyTargetKey(ir *backendProtoIR) (backendGoStructuralKey, bool) {
	if ir == nil {
		return backendGoStructuralKey{}, false
	}
	plan := analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{})
	for _, key := range plan.concatByPC {
		return key, true
	}
	return backendGoStructuralKey{}, false
}

func (plan backendGoStructuralKeyPlan) key(id backendValueID) (backendGoStructuralKey, bool) {
	key, ok := plan.keys[id]
	return key, ok
}

func (plan backendGoStructuralKeyPlan) concat(operation *backendOperationIR) (backendGoStructuralKey, bool) {
	if operation == nil {
		return backendGoStructuralKey{}, false
	}
	key, ok := plan.concatByPC[operation.pc]
	return key, ok
}

func (plan backendGoStructuralKeyPlan) tostring(operation *backendOperationIR) (backendValueID, bool) {
	if operation == nil {
		return invalidBackendValueID, false
	}
	source, ok := plan.tostringByPC[operation.pc]
	return source, ok
}
