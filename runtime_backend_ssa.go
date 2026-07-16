package ember

func buildBackendSSA(ir *backendProtoIR) {
	if ir == nil {
		return
	}
	ir.initial = make([]backendValueID, ir.registers)
	for register := 0; register < ir.registers; register++ {
		kind := backendValueUndef
		if register < ir.params {
			kind = backendValueParameter
		}
		ir.initial[register] = ir.addBackendValue(backendValueIR{
			kind:     kind,
			register: int32(register),
			block:    -1,
			pc:       -1,
		})
	}
	for blockIndex := range ir.blocks {
		block := &ir.blocks[blockIndex]
		block.entryValues = make([]backendValueID, ir.registers)
		block.exitValues = make([]backendValueID, ir.registers)
		if blockIndex != 0 && len(block.predecessors) > 1 {
			block.phis = make([]backendPhiIR, ir.registers)
			for register := 0; register < ir.registers; register++ {
				value := ir.addBackendValue(backendValueIR{
					kind:     backendValuePhi,
					register: int32(register),
					block:    int32(blockIndex),
					pc:       block.first,
				})
				block.phis[register] = backendPhiIR{
					register: int32(register),
					value:    value,
					inputs:   make([]backendValueID, len(block.predecessors)),
				}
			}
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		for register := 0; register < ir.registers; register++ {
			if !operation.writes.has(register) {
				continue
			}
			value := ir.addBackendValue(backendValueIR{
				kind:     backendValueOperation,
				register: int32(register),
				block:    operation.block,
				pc:       int32(pc),
			})
			operation.defs = append(operation.defs, backendValueRef{
				register: int32(register),
				value:    value,
			})
		}
	}

	for iteration := 0; iteration <= len(ir.blocks); iteration++ {
		changed := false
		for blockIndex := range ir.blocks {
			block := &ir.blocks[blockIndex]
			entry := ir.backendBlockEntryValues(blockIndex)
			if !backendValueIDsEqual(entry, block.entryValues) {
				copy(block.entryValues, entry)
				changed = true
			}
			current := append([]backendValueID(nil), block.entryValues...)
			for pc := int(block.first); pc < int(block.last); pc++ {
				operation := &ir.ops[pc]
				defIndex := 0
				for register := 0; register < ir.registers; register++ {
					if operation.writes.has(register) {
						current[register] = operation.defs[defIndex].value
						defIndex++
					}
				}
			}
			if !backendValueIDsEqual(current, block.exitValues) {
				copy(block.exitValues, current)
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	for blockIndex := range ir.blocks {
		block := &ir.blocks[blockIndex]
		for phiIndex := range block.phis {
			phi := &block.phis[phiIndex]
			for predecessorIndex, predecessor := range block.predecessors {
				phi.inputs[predecessorIndex] = ir.blocks[predecessor].exitValues[phi.register]
			}
		}
		current := append([]backendValueID(nil), block.entryValues...)
		for pc := int(block.first); pc < int(block.last); pc++ {
			operation := &ir.ops[pc]
			operation.uses = operation.uses[:0]
			for register := 0; register < ir.registers; register++ {
				if operation.reads.has(register) {
					operation.uses = append(operation.uses, backendValueRef{
						register: int32(register),
						value:    current[register],
					})
				}
			}
			for _, definition := range operation.defs {
				current[definition.register] = definition.value
			}
		}
	}
}

func (ir *backendProtoIR) addBackendValue(value backendValueIR) backendValueID {
	value.id = backendValueID(len(ir.values) + 1)
	ir.values = append(ir.values, value)
	return value.id
}

func (ir *backendProtoIR) backendBlockEntryValues(blockIndex int) []backendValueID {
	block := &ir.blocks[blockIndex]
	switch {
	case blockIndex == 0 || !block.reachable || len(block.predecessors) == 0:
		return ir.initial
	case len(block.predecessors) == 1:
		return ir.blocks[block.predecessors[0]].exitValues
	default:
		values := make([]backendValueID, ir.registers)
		for register := range block.phis {
			values[register] = block.phis[register].value
		}
		return values
	}
}

func backendValueIDsEqual(left, right []backendValueID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func buildBackendEdges(ir *backendProtoIR) {
	if ir == nil {
		return
	}
	ir.edges = ir.edges[:0]
	for blockIndex := range ir.blocks {
		block := &ir.blocks[blockIndex]
		for _, successor := range block.successors {
			target := &ir.blocks[successor]
			edge := backendEdgeIR{
				id:       int32(len(ir.edges)),
				from:     int32(blockIndex),
				to:       successor,
				critical: len(block.successors) > 1 && len(target.predecessors) > 1,
			}
			predecessorIndex := backendIDIndex(target.predecessors, int32(blockIndex))
			if predecessorIndex >= 0 {
				for _, phi := range target.phis {
					edge.phiCopies = append(edge.phiCopies, backendPhiCopyIR{
						register:    phi.register,
						source:      phi.inputs[predecessorIndex],
						destination: phi.value,
					})
				}
			}
			ir.edges = append(ir.edges, edge)
		}
	}
}

func backendIDIndex(values []int32, want int32) int {
	for index, value := range values {
		if value == want {
			return index
		}
	}
	return -1
}
