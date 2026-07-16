package ember

import "fmt"

func verifyBackendProtoIR(ir *backendProtoIR) error {
	if ir == nil {
		return fmt.Errorf("verify backend IR: nil IR")
	}
	if ir.registers < 0 || len(ir.ops) == 0 || len(ir.blocks) == 0 || len(ir.pcToBlock) != len(ir.ops) {
		return fmt.Errorf("verify backend IR: invalid inventory")
	}
	nextPC := int32(0)
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
		var charge uint64
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
			for name, set := range map[string]backendRegisterSet{
				"reads":       operation.reads,
				"writes":      operation.writes,
				"live-before": operation.liveBefore,
				"live-after":  operation.liveAfter,
				"spill":       operation.spill,
			} {
				if err := verifyBackendRegisterSet(ir.registers, set); err != nil {
					return fmt.Errorf("verify backend IR: PC %d %s: %w", pc, name, err)
				}
			}
			charge += uint64(operation.guestCharge)
		}
		if charge != block.guestCharge {
			return fmt.Errorf("verify backend IR: block %d charge=%d want %d", blockIndex, block.guestCharge, charge)
		}
		nextPC = block.last
	}
	if int(nextPC) != len(ir.ops) {
		return fmt.Errorf("verify backend IR: blocks do not cover operations")
	}
	return nil
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
