package ember

const backendGoMaxFixedResultCount = 32

func backendGoNumericFixedResultCount(ir *backendProtoIR) (int, bool) {
	return backendGoNumericFixedResultCountFor(ir, 0)
}

func backendGoNumericFixedResultCountFor(
	ir *backendProtoIR,
	fixedVarargCount int,
) (int, bool) {
	if ir == nil {
		return 0, false
	}
	resultCount := -1
	for blockIndex := range ir.blocks {
		block := &ir.blocks[blockIndex]
		if !block.reachable {
			continue
		}
		for pc := block.first; pc < block.last; pc++ {
			operation := &ir.ops[pc]
			if operation.op != opReturnOne && operation.op != opReturn {
				continue
			}
			count, ok := backendGoNumericReturnCount(ir, fixedVarargCount, operation)
			if !ok {
				return 0, false
			}
			if count < 0 || count > backendGoMaxFixedResultCount {
				return 0, false
			}
			if resultCount >= 0 && resultCount != count {
				return 0, false
			}
			resultCount = count
		}
	}
	return resultCount, resultCount >= 0
}

func backendGoNumericReturnCount(
	ir *backendProtoIR,
	fixedVarargCount int,
	operation *backendOperationIR,
) (int, bool) {
	if ir == nil || operation == nil {
		return 0, false
	}
	switch operation.op {
	case opReturnOne:
		return 1, true
	case opReturn:
		if operation.returnCount > 0 {
			return int(operation.returnCount), true
		}
		if operation.returnCount >= 0 {
			return 0, true
		}
	default:
		return 0, false
	}

	// Luau represents `return prefix, select("#", ...)` with an open tail.
	// The fixed-vararg select intrinsic still has exactly one SSA definition, so
	// this exact producer has a bounded result count even though RETURN retains
	// the bytecode's open-result encoding. No other open producer is normalized.
	count := -int(operation.returnCount)
	if count <= 0 || count > backendGoMaxFixedResultCount {
		return 0, false
	}
	tailRegister := operation.a + int32(count-1)
	tailID := backendOperationUse(operation, tailRegister)
	var producer *backendOperationIR
	if ir.validBackendValue(tailID) {
		tail := &ir.values[tailID-1]
		if tail.kind != backendValueOperation || tail.register != tailRegister {
			return 0, false
		}
		producer = &ir.ops[tail.pc]
	} else {
		// The bytecode open-tail RETURN does not name the FAST_CALL definition
		// as an ordinary SSA use. Require it to be the immediately preceding
		// operation and to define the exact tail register instead.
		if operation.pc <= 0 || int(operation.pc) >= len(ir.ops) {
			return 0, false
		}
		producer = &ir.ops[operation.pc-1]
		if len(producer.defs) != 1 || producer.defs[0].register != tailRegister {
			return 0, false
		}
	}
	if producer.d >= 0 ||
		!backendGoNumericFixedVarargSelect(ir, fixedVarargCount, producer) {
		return 0, false
	}
	return count, true
}

func backendGoNumericReturnValue(
	ir *backendProtoIR,
	fixedVarargCount int,
	operation *backendOperationIR,
	result int,
) (backendValueID, bool) {
	count, ok := backendGoNumericReturnCount(ir, fixedVarargCount, operation)
	if !ok || result < 0 || result >= count {
		return invalidBackendValueID, false
	}
	register := operation.a + int32(result)
	if id := backendOperationUse(operation, register); ir.validBackendValue(id) {
		return id, true
	}
	if operation.op != opReturn || operation.returnCount >= 0 || result != count-1 || operation.pc <= 0 {
		return invalidBackendValueID, false
	}
	producer := &ir.ops[operation.pc-1]
	if !backendGoNumericFixedVarargSelect(ir, fixedVarargCount, producer) ||
		producer.d >= 0 ||
		len(producer.defs) != 1 ||
		producer.defs[0].register != register {
		return invalidBackendValueID, false
	}
	return producer.defs[0].value, true
}
