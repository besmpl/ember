package ember

import (
	"fmt"
	"sort"
)

func buildBackendProtoIR(proto *machineProto) (*backendProtoIR, error) {
	return buildBackendProtoIRWithStrings(proto, nil, nil)
}

func buildBackendProtoIRWithStrings(
	proto *machineProto,
	stringRecords []machineStringRecord,
	stringData []byte,
) (*backendProtoIR, error) {
	if proto == nil {
		return nil, fmt.Errorf("build backend IR: nil Machine prototype")
	}
	if !proto.eligible {
		return nil, fmt.Errorf("build backend IR: ineligible Machine prototype: %s", proto.rejectReason)
	}
	if len(proto.operations) == 0 || len(proto.blocks) == 0 {
		return nil, fmt.Errorf("build backend IR: empty Machine prototype")
	}
	ir := &backendProtoIR{
		registers:                proto.registers,
		params:                   proto.params,
		variadic:                 proto.variadic,
		maxResults:               proto.maxResults,
		detachable:               proto.detachable,
		requiresOwner:            proto.requiresOwner,
		requiresNumericCoercion:  proto.requiresNumericCoercion,
		requiresGeneratedStrings: proto.requiresGeneratedStrings,
		sourceName:               proto.sourceName,
		functionName:             proto.functionName,
		blocks:                   make([]backendBlockIR, len(proto.blocks)),
		ops:                      make([]backendOperationIR, len(proto.operations)),
		pcToBlock:                make([]int32, len(proto.operations)),
		constants:                append([]machineConstant(nil), proto.constants...),
		// String storage belongs to the immutable CodeImage and is shared by
		// every Proto IR in the module. Keep one borrowed read-only view rather
		// than copying the complete module string table once per Proto.
		stringRecords: stringRecords,
		stringData:    stringData,
		upvalues:      append([]machineUpvalue(nil), proto.upvalues...),
	}
	for blockIndex, source := range proto.blocks {
		block := &ir.blocks[blockIndex]
		block.id = int32(blockIndex)
		block.first = source.first
		block.last = source.last
		block.immediateDominator = -1
		block.use = newBackendRegisterSet(proto.registers)
		block.def = newBackendRegisterSet(proto.registers)
		block.liveIn = newBackendRegisterSet(proto.registers)
		block.liveOut = newBackendRegisterSet(proto.registers)
		for pc := int(source.first); pc < int(source.last); pc++ {
			if pc < 0 || pc >= len(ir.ops) {
				return nil, fmt.Errorf("build backend IR: block %d has invalid PC %d", blockIndex, pc)
			}
			ir.pcToBlock[pc] = int32(blockIndex)
		}
	}
	for pc, source := range proto.operations {
		reads, writes := backendOperationRegisters(source, proto.registers)
		effects := backendEffects(source.op)
		exit := backendExitNone
		if effects != 0 {
			exit = backendExitBeforeOperation
		}
		targetPC := int32(-1)
		switch opcodeJumpTarget(source.op) {
		case opcodeJumpTargetB:
			targetPC = source.b
		case opcodeJumpTargetD:
			targetPC = source.d
		}
		blockID := ir.pcToBlock[pc]
		operation := &ir.ops[pc]
		*operation = backendOperationIR{
			op:           source.op,
			pc:           int32(pc),
			wordPC:       source.wordPC,
			line:         source.line,
			block:        blockID,
			targetPC:     targetPC,
			a:            source.a,
			b:            source.b,
			c:            source.c,
			d:            source.d,
			targetProto:  source.targetProto,
			callArgStart: source.callArgStart,
			callArgCount: source.callArgCount,
			callPrefix:   source.callPrefix,
			callResults:  source.callResults,
			returnCount:  source.returnCount,
			tailCall:     source.tailCall,
			globalIndex:  source.globalIndex,
			nativeID:     source.nativeID,
			guardField:   source.guardField,
			guestCharge:  source.guestCharge,
			tailCharge:   source.tailCharge,
			errorClass:   source.errorClass,
			effects:      effects,
			exit:         exit,
			reads:        reads,
			writes:       writes,
			spill:        newBackendRegisterSet(proto.registers),
		}
		block := &ir.blocks[blockID]
		block.guestCharge += uint64(source.guestCharge)
		for register := 0; register < proto.registers; register++ {
			if reads.has(register) && !block.def.has(register) {
				block.use.add(register)
			}
			if writes.has(register) {
				block.def.add(register)
			}
		}
	}
	if err := buildBackendCFG(ir); err != nil {
		return nil, err
	}
	analyzeBackendDominators(ir)
	analyzeBackendLoops(ir)
	analyzeBackendLiveness(ir)
	buildBackendSSA(ir)
	buildBackendEdges(ir)
	analyzeBackendFacts(ir)
	if err := verifyBackendProtoIR(ir); err != nil {
		return nil, err
	}
	return ir, nil
}

func backendOperationRegisters(operation machineOperation, registerCount int) (backendRegisterSet, backendRegisterSet) {
	instruction := instruction{
		op: operation.op,
		a:  int(operation.a),
		b:  int(operation.b),
		c:  int(operation.c),
		d:  int(operation.d),
	}
	reads := newBackendRegisterSet(registerCount)
	writes := newBackendRegisterSet(registerCount)
	foundReads := instructionRegistersBounded(instruction, instructionRegisterRead, registerCount)
	for register, ok := foundReads.next(); ok; register, ok = foundReads.next() {
		reads.add(register)
	}
	foundWrites := instructionRegistersBounded(instruction, instructionRegisterWrite, registerCount)
	for register, ok := foundWrites.next(); ok; register, ok = foundWrites.next() {
		writes.add(register)
	}
	return reads, writes
}

func backendEffects(op opcode) backendEffect {
	effects := opcodeEffect(op)
	var result backendEffect
	if effects.invokesScriptOrHostCode {
		result |= backendEffectCall
	}
	if effects.mayYield {
		result |= backendEffectYield
	}
	if effects.mayError {
		result |= backendEffectError
	}
	if effects.allocatesOrObservesIdentity {
		result |= backendEffectAllocate
	}
	if effects.readsGlobals {
		result |= backendEffectReadGlobal
	}
	if effects.writesGlobals {
		result |= backendEffectWriteGlobal
	}
	if effects.readsUpvalues {
		result |= backendEffectReadUpvalue
	}
	if effects.writesUpvalues {
		result |= backendEffectWriteUpvalue
	}
	if effects.readsTables {
		result |= backendEffectReadTable
	}
	if effects.writesTables {
		result |= backendEffectWriteTable
	}
	if effects.readsUnknownHeap {
		result |= backendEffectReadUnknownHeap
	}
	if effects.writesUnknownHeap {
		result |= backendEffectWriteUnknownHeap
	}
	return result
}

func buildBackendCFG(ir *backendProtoIR) error {
	for blockIndex := range ir.blocks {
		block := &ir.blocks[blockIndex]
		if block.first < 0 || block.last <= block.first || int(block.last) > len(ir.ops) {
			return fmt.Errorf("build backend IR: block %d has invalid range [%d,%d)", blockIndex, block.first, block.last)
		}
		lastPC := int(block.last) - 1
		operation := ir.ops[lastPC]
		addSuccessor := func(pc int32) error {
			if pc < 0 || int(pc) >= len(ir.pcToBlock) {
				return fmt.Errorf("build backend IR: block %d targets invalid PC %d", blockIndex, pc)
			}
			target := ir.pcToBlock[pc]
			if ir.blocks[target].first != pc {
				return fmt.Errorf("build backend IR: block %d targets non-leader PC %d", blockIndex, pc)
			}
			block.successors = append(block.successors, target)
			return nil
		}
		switch opcodeControlFlow(operation.op) {
		case opcodeControlReturn:
		case opcodeControlJump:
			if err := addSuccessor(operation.targetPC); err != nil {
				return err
			}
		case opcodeControlBranch:
			if err := addSuccessor(operation.targetPC); err != nil {
				return err
			}
			if int(block.last) < len(ir.ops) {
				if err := addSuccessor(block.last); err != nil {
					return err
				}
			}
		default:
			if int(block.last) < len(ir.ops) {
				if err := addSuccessor(block.last); err != nil {
					return err
				}
			}
		}
		sort.Slice(block.successors, func(left, right int) bool {
			return block.successors[left] < block.successors[right]
		})
		block.successors = compactBackendIDs(block.successors)
		for _, successor := range block.successors {
			ir.blocks[successor].predecessors = append(ir.blocks[successor].predecessors, int32(blockIndex))
		}
	}
	for blockIndex := range ir.blocks {
		sort.Slice(ir.blocks[blockIndex].predecessors, func(left, right int) bool {
			return ir.blocks[blockIndex].predecessors[left] < ir.blocks[blockIndex].predecessors[right]
		})
		ir.blocks[blockIndex].predecessors = compactBackendIDs(ir.blocks[blockIndex].predecessors)
	}
	if len(ir.blocks) != 0 {
		markBackendReachable(ir, 0)
	}
	return nil
}

func compactBackendIDs(values []int32) []int32 {
	if len(values) < 2 {
		return values
	}
	output := values[:1]
	for _, value := range values[1:] {
		if value != output[len(output)-1] {
			output = append(output, value)
		}
	}
	return output
}

func markBackendReachable(ir *backendProtoIR, blockID int32) {
	block := &ir.blocks[blockID]
	if block.reachable {
		return
	}
	block.reachable = true
	for _, successor := range block.successors {
		markBackendReachable(ir, successor)
	}
}

func analyzeBackendDominators(ir *backendProtoIR) {
	wordCount := (len(ir.blocks) + 63) / 64
	for blockIndex := range ir.blocks {
		block := &ir.blocks[blockIndex]
		block.dominators = make([]uint64, wordCount)
		if !block.reachable {
			continue
		}
		if blockIndex == 0 {
			block.dominators[0] = 1
			continue
		}
		for index := range block.dominators {
			block.dominators[index] = ^uint64(0)
		}
		if remainder := len(ir.blocks) % 64; remainder != 0 {
			block.dominators[len(block.dominators)-1] &= uint64(1)<<uint(remainder) - 1
		}
	}
	changed := true
	for changed {
		changed = false
		for blockIndex := 1; blockIndex < len(ir.blocks); blockIndex++ {
			block := &ir.blocks[blockIndex]
			if !block.reachable {
				continue
			}
			next := make([]uint64, wordCount)
			first := true
			for _, predecessor := range block.predecessors {
				if !ir.blocks[predecessor].reachable {
					continue
				}
				if first {
					copy(next, ir.blocks[predecessor].dominators)
					first = false
					continue
				}
				for word := range next {
					next[word] &= ir.blocks[predecessor].dominators[word]
				}
			}
			next[blockIndex/64] |= uint64(1) << uint(blockIndex%64)
			if !backendWordsEqual(next, block.dominators) {
				block.dominators = next
				changed = true
			}
		}
	}
	for blockIndex := 1; blockIndex < len(ir.blocks); blockIndex++ {
		block := &ir.blocks[blockIndex]
		if !block.reachable {
			continue
		}
		for candidate := range ir.blocks {
			if candidate == blockIndex || !backendBlockDominates(block, int32(candidate)) {
				continue
			}
			immediate := true
			for other := range ir.blocks {
				if other == blockIndex || other == candidate || !backendBlockDominates(block, int32(other)) {
					continue
				}
				if backendBlockDominates(&ir.blocks[other], int32(candidate)) {
					immediate = false
					break
				}
			}
			if immediate {
				block.immediateDominator = int32(candidate)
				break
			}
		}
	}
}

func analyzeBackendLoops(ir *backendProtoIR) {
	for blockIndex := range ir.blocks {
		block := &ir.blocks[blockIndex]
		for _, successor := range block.successors {
			if backendBlockDominates(block, successor) {
				ir.blocks[successor].loopHeader = true
			}
		}
	}
}

func analyzeBackendLiveness(ir *backendProtoIR) {
	changed := true
	for changed {
		changed = false
		for blockIndex := len(ir.blocks) - 1; blockIndex >= 0; blockIndex-- {
			block := &ir.blocks[blockIndex]
			nextOut := newBackendRegisterSet(ir.registers)
			for _, successor := range block.successors {
				nextOut.union(ir.blocks[successor].liveIn)
			}
			nextIn := nextOut.clone()
			nextIn.subtract(block.def)
			nextIn.union(block.use)
			if !nextOut.equal(block.liveOut) || !nextIn.equal(block.liveIn) {
				block.liveOut = nextOut
				block.liveIn = nextIn
				changed = true
			}
		}
	}
	for blockIndex := range ir.blocks {
		block := &ir.blocks[blockIndex]
		live := block.liveOut.clone()
		for pc := int(block.last) - 1; pc >= int(block.first); pc-- {
			operation := &ir.ops[pc]
			operation.liveAfter = live.clone()
			live.subtract(operation.writes)
			live.union(operation.reads)
			operation.liveBefore = live.clone()
			if operation.exit == backendExitBeforeOperation {
				operation.spill = operation.liveBefore.clone()
			}
		}
	}
}

func backendBlockDominates(block *backendBlockIR, candidate int32) bool {
	return block != nil && candidate >= 0 &&
		int(candidate)/64 < len(block.dominators) &&
		block.dominators[candidate/64]&(uint64(1)<<uint(candidate%64)) != 0
}

func backendWordsEqual(left, right []uint64) bool {
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
