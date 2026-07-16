package ember

import "fmt"

func verifyBackendProtoIR(ir *backendProtoIR) error {
	if ir == nil {
		return fmt.Errorf("verify backend IR: nil IR")
	}
	if ir.registers < 0 || ir.params < 0 || ir.params > ir.registers ||
		len(ir.ops) == 0 || len(ir.blocks) == 0 || len(ir.pcToBlock) != len(ir.ops) {
		return fmt.Errorf("verify backend IR: invalid inventory")
	}
	if len(ir.initial) != ir.registers {
		return fmt.Errorf("verify backend IR: initial SSA value count=%d want %d", len(ir.initial), ir.registers)
	}
	for register, id := range ir.initial {
		kind := backendValueUndef
		if register < ir.params {
			kind = backendValueParameter
		}
		if !ir.backendValueMatches(id, kind, int32(register), -1, -1) {
			return fmt.Errorf("verify backend IR: register %d has invalid initial SSA value", register)
		}
	}
	for valueIndex := range ir.values {
		value := ir.values[valueIndex]
		if value.id != backendValueID(valueIndex+1) ||
			value.register < 0 || int(value.register) >= ir.registers {
			return fmt.Errorf("verify backend IR: invalid SSA value %d", valueIndex+1)
		}
		switch value.kind {
		case backendValueUndef, backendValueParameter:
			if value.block != -1 || value.pc != -1 {
				return fmt.Errorf("verify backend IR: initial SSA value %d has a producer", value.id)
			}
		case backendValueOperation:
			if value.block < 0 || int(value.block) >= len(ir.blocks) ||
				value.pc < 0 || int(value.pc) >= len(ir.ops) ||
				ir.ops[value.pc].block != value.block {
				return fmt.Errorf("verify backend IR: operation SSA value %d has an invalid producer", value.id)
			}
		case backendValuePhi:
			if value.block <= 0 || int(value.block) >= len(ir.blocks) ||
				value.pc != ir.blocks[value.block].first {
				return fmt.Errorf("verify backend IR: phi SSA value %d has an invalid producer", value.id)
			}
		default:
			return fmt.Errorf("verify backend IR: SSA value %d has an invalid kind", value.id)
		}
	}
	nextPC := int32(0)
	nextEdge := 0
	for blockIndex := range ir.blocks {
		block := &ir.blocks[blockIndex]
		if block.id != int32(blockIndex) || block.first != nextPC || block.last <= block.first || int(block.last) > len(ir.ops) {
			return fmt.Errorf("verify backend IR: block %d has invalid range or identity", blockIndex)
		}
		if blockIndex == 0 && block.immediateDominator != -1 {
			return fmt.Errorf("verify backend IR: entry block has a dominator")
		}
		if block.reachable && blockIndex != 0 {
			if block.immediateDominator < 0 || int(block.immediateDominator) >= len(ir.blocks) {
				return fmt.Errorf("verify backend IR: block %d has invalid immediate dominator", blockIndex)
			}
			if !backendBlockDominates(block, block.immediateDominator) {
				return fmt.Errorf("verify backend IR: block %d is not dominated by its immediate dominator", blockIndex)
			}
		}
		for _, successor := range block.successors {
			if successor < 0 || int(successor) >= len(ir.blocks) || !backendContainsID(ir.blocks[successor].predecessors, int32(blockIndex)) {
				return fmt.Errorf("verify backend IR: block %d has inconsistent successor %d", blockIndex, successor)
			}
			if nextEdge >= len(ir.edges) {
				return fmt.Errorf("verify backend IR: block %d is missing edge %d", blockIndex, successor)
			}
			edge := &ir.edges[nextEdge]
			target := &ir.blocks[successor]
			critical := len(block.successors) > 1 && len(target.predecessors) > 1
			if edge.id != int32(nextEdge) || edge.from != int32(blockIndex) || edge.to != successor || edge.critical != critical {
				return fmt.Errorf("verify backend IR: edge %d has invalid identity or classification", nextEdge)
			}
			if len(edge.phiCopies) != len(target.phis) {
				return fmt.Errorf("verify backend IR: edge %d phi copy count=%d want %d", nextEdge, len(edge.phiCopies), len(target.phis))
			}
			predecessorIndex := backendIDIndex(target.predecessors, int32(blockIndex))
			for copyIndex, phi := range target.phis {
				copy := edge.phiCopies[copyIndex]
				if predecessorIndex < 0 ||
					copy.register != phi.register ||
					copy.source != phi.inputs[predecessorIndex] ||
					copy.destination != phi.value ||
					!ir.validBackendValue(copy.source) ||
					!ir.validBackendValue(copy.destination) {
					return fmt.Errorf("verify backend IR: edge %d phi copy %d is invalid", nextEdge, copyIndex)
				}
			}
			nextEdge++
		}
		for _, predecessor := range block.predecessors {
			if predecessor < 0 || int(predecessor) >= len(ir.blocks) || !backendContainsID(ir.blocks[predecessor].successors, int32(blockIndex)) {
				return fmt.Errorf("verify backend IR: block %d has inconsistent predecessor %d", blockIndex, predecessor)
			}
		}
		if err := verifyBackendRegisterSet(ir.registers, block.use); err != nil {
			return fmt.Errorf("verify backend IR: block %d use: %w", blockIndex, err)
		}
		if err := verifyBackendRegisterSet(ir.registers, block.def); err != nil {
			return fmt.Errorf("verify backend IR: block %d def: %w", blockIndex, err)
		}
		if err := verifyBackendRegisterSet(ir.registers, block.liveIn); err != nil {
			return fmt.Errorf("verify backend IR: block %d live-in: %w", blockIndex, err)
		}
		if err := verifyBackendRegisterSet(ir.registers, block.liveOut); err != nil {
			return fmt.Errorf("verify backend IR: block %d live-out: %w", blockIndex, err)
		}
		if len(block.entryValues) != ir.registers || len(block.exitValues) != ir.registers {
			return fmt.Errorf("verify backend IR: block %d has invalid SSA frame width", blockIndex)
		}
		for register := 0; register < ir.registers; register++ {
			if !ir.validBackendValue(block.entryValues[register]) {
				return fmt.Errorf("verify backend IR: block %d register %d has unresolved SSA entry", blockIndex, register)
			}
			if !ir.validBackendValue(block.exitValues[register]) {
				return fmt.Errorf("verify backend IR: block %d register %d has unresolved SSA exit", blockIndex, register)
			}
		}
		if blockIndex == 0 {
			if !backendValueIDsEqual(block.entryValues, ir.initial) || len(block.phis) != 0 {
				return fmt.Errorf("verify backend IR: entry block has invalid SSA entry")
			}
		} else if len(block.predecessors) > 1 {
			if len(block.phis) != ir.registers {
				return fmt.Errorf("verify backend IR: block %d phi count=%d want %d", blockIndex, len(block.phis), ir.registers)
			}
			for register := range block.phis {
				phi := block.phis[register]
				if phi.register != int32(register) || !ir.backendValueMatches(phi.value, backendValuePhi, int32(register), int32(blockIndex), block.first) ||
					len(phi.inputs) != len(block.predecessors) || block.entryValues[register] != phi.value {
					return fmt.Errorf("verify backend IR: block %d register %d has invalid phi", blockIndex, register)
				}
				for predecessorIndex, predecessor := range block.predecessors {
					if !ir.validBackendValue(phi.inputs[predecessorIndex]) ||
						phi.inputs[predecessorIndex] != ir.blocks[predecessor].exitValues[register] {
						return fmt.Errorf("verify backend IR: block %d register %d phi input %d mismatches predecessor", blockIndex, register, predecessorIndex)
					}
				}
			}
		} else if len(block.phis) != 0 {
			return fmt.Errorf("verify backend IR: block %d has unnecessary phis", blockIndex)
		}
		var charge uint64
		currentValues := append([]backendValueID(nil), block.entryValues...)
		for pc := block.first; pc < block.last; pc++ {
			operation := &ir.ops[pc]
			if operation.pc != pc || operation.block != int32(blockIndex) || ir.pcToBlock[pc] != int32(blockIndex) {
				return fmt.Errorf("verify backend IR: PC %d has inconsistent block mapping", pc)
			}
			if operation.guestCharge == 0 || operation.wordPC < 0 ||
				!opcodeEffect(operation.op).classified ||
				operation.effects != backendEffects(operation.op) {
				return fmt.Errorf("verify backend IR: PC %d has invalid semantic metadata", pc)
			}
			if operation.effects != 0 && operation.exit != backendExitBeforeOperation {
				return fmt.Errorf("verify backend IR: PC %d effect has no pre-operation exit", pc)
			}
			if operation.exit == backendExitBeforeOperation && !operation.spill.equal(operation.liveBefore) {
				return fmt.Errorf("verify backend IR: PC %d spill map is not exact", pc)
			}
			registerSets := [...]struct {
				name string
				set  backendRegisterSet
			}{
				{name: "reads", set: operation.reads},
				{name: "writes", set: operation.writes},
				{name: "live-before", set: operation.liveBefore},
				{name: "live-after", set: operation.liveAfter},
				{name: "spill", set: operation.spill},
			}
			for _, entry := range registerSets {
				if err := verifyBackendRegisterSet(ir.registers, entry.set); err != nil {
					return fmt.Errorf("verify backend IR: PC %d %s: %w", pc, entry.name, err)
				}
			}
			useIndex := 0
			defIndex := 0
			for register := 0; register < ir.registers; register++ {
				if operation.reads.has(register) {
					if useIndex >= len(operation.uses) ||
						operation.uses[useIndex].register != int32(register) ||
						!ir.validBackendValue(operation.uses[useIndex].value) ||
						operation.uses[useIndex].value != currentValues[register] {
						return fmt.Errorf("verify backend IR: PC %d register %d has invalid SSA use", pc, register)
					}
					useIndex++
				}
				if operation.writes.has(register) {
					if defIndex >= len(operation.defs) ||
						operation.defs[defIndex].register != int32(register) ||
						!ir.backendValueMatches(operation.defs[defIndex].value, backendValueOperation, int32(register), int32(blockIndex), pc) {
						return fmt.Errorf("verify backend IR: PC %d register %d has invalid SSA definition", pc, register)
					}
					currentValues[register] = operation.defs[defIndex].value
					defIndex++
				}
			}
			if useIndex != len(operation.uses) || defIndex != len(operation.defs) {
				return fmt.Errorf("verify backend IR: PC %d has extra SSA uses or definitions", pc)
			}
			charge += uint64(operation.guestCharge)
		}
		if !backendValueIDsEqual(currentValues, block.exitValues) {
			return fmt.Errorf("verify backend IR: block %d exit SSA values mismatch", blockIndex)
		}
		if charge != block.guestCharge {
			return fmt.Errorf("verify backend IR: block %d charge=%d want %d", blockIndex, block.guestCharge, charge)
		}
		nextPC = block.last
	}
	if int(nextPC) != len(ir.ops) {
		return fmt.Errorf("verify backend IR: blocks do not cover operations")
	}
	if nextEdge != len(ir.edges) {
		return fmt.Errorf("verify backend IR: edge inventory has %d trailing entries", len(ir.edges)-nextEdge)
	}
	if err := verifyBackendFacts(ir); err != nil {
		return err
	}
	return nil
}

func verifyBackendFacts(ir *backendProtoIR) error {
	for valueIndex := range ir.values {
		value := &ir.values[valueIndex]
		if value.tags == 0 || value.tags&^backendTagAny != 0 {
			return fmt.Errorf("verify backend IR: SSA value %d has invalid tag facts", value.id)
		}
		if value.representation != backendRepresentationForTags(value.tags) {
			return fmt.Errorf("verify backend IR: SSA value %d has invalid representation", value.id)
		}
		for originIndex, origin := range value.origins {
			if !ir.validBackendValue(origin) || originIndex > 0 && value.origins[originIndex-1] >= origin {
				return fmt.Errorf("verify backend IR: SSA value %d has invalid escape origin", value.id)
			}
		}
		for targetIndex, target := range value.targetProtos {
			if target < 0 || targetIndex > 0 && value.targetProtos[targetIndex-1] >= target {
				return fmt.Errorf("verify backend IR: SSA value %d has invalid target Proto fact", value.id)
			}
		}
		expected := ir.backendExpectedValueFacts(value)
		if value.tags != expected.tags ||
			value.object != expected.object ||
			value.fromVararg != expected.fromVararg ||
			!backendValueIDsEqual(value.origins, expected.origins) ||
			!backendInt32sEqual(value.targetProtos, expected.targetProtos) ||
			value.targetUnknown != expected.targetUnknown {
			return fmt.Errorf("verify backend IR: SSA value %d has inconsistent derived facts", value.id)
		}
	}
	escaping := backendEscapingValues(ir)
	for valueIndex := range ir.values {
		expected := escaping[valueIndex]
		for _, origin := range ir.values[valueIndex].origins {
			if ir.validBackendValue(origin) && escaping[origin-1] {
				expected = true
				break
			}
		}
		if ir.values[valueIndex].escapes != expected {
			return fmt.Errorf("verify backend IR: SSA value %d has inconsistent escape fact", ir.values[valueIndex].id)
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.call != ir.backendCallClassification(operation) {
			return fmt.Errorf("verify backend IR: PC %d has inconsistent call classification", pc)
		}
		if operation.access != backendAccessClassification(operation) {
			return fmt.Errorf("verify backend IR: PC %d has inconsistent access classification", pc)
		}
	}
	return nil
}

func (ir *backendProtoIR) backendValueMatches(id backendValueID, kind backendValueKind, register, block, pc int32) bool {
	if !ir.validBackendValue(id) {
		return false
	}
	value := ir.values[id-1]
	return value.kind == kind && value.register == register && value.block == block && value.pc == pc
}

func (ir *backendProtoIR) validBackendValue(id backendValueID) bool {
	return ir != nil && id != invalidBackendValueID && int(id) <= len(ir.values)
}

func verifyBackendRegisterSet(registers int, set backendRegisterSet) error {
	wantWords := 0
	if registers > 0 {
		wantWords = (registers + 63) / 64
	}
	if len(set) != wantWords {
		return fmt.Errorf("word count=%d want %d", len(set), wantWords)
	}
	if registers == 0 || len(set) == 0 || registers%64 == 0 {
		return nil
	}
	valid := uint64(1)<<uint(registers%64) - 1
	if set[len(set)-1]&^valid != 0 {
		return fmt.Errorf("contains registers outside 0..%d", registers-1)
	}
	return nil
}

func backendContainsID(values []int32, want int32) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
