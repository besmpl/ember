package ember

const backendGoMaxFixedResultCount = 32

func backendGoNumericFixedResultCount(ir *backendProtoIR) (int, bool) {
	return backendGoNumericFixedResultCountFor(ir, 0)
}

func backendGoNumericFixedResultCountFor(
	ir *backendProtoIR,
	fixedVarargCount int,
) (int, bool) {
	return backendGoNumericFixedResultCountForOptions(ir, backendGoNumericOptions{
		fixedVarargCount: fixedVarargCount,
	})
}

func backendGoNumericFixedResultCountForOptions(
	ir *backendProtoIR,
	options backendGoNumericOptions,
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
			count, ok := backendGoNumericReturnCountForOptions(ir, options, operation)
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
	return backendGoNumericReturnCountForOptions(ir, backendGoNumericOptions{
		fixedVarargCount: fixedVarargCount,
	}, operation)
}

func backendGoNumericReturnCountForOptions(
	ir *backendProtoIR,
	options backendGoNumericOptions,
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
	if _, ok := backendGoNumericOpenDirectTailCall(ir, options, operation); ok {
		return 1, true
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
		!backendGoNumericFixedVarargSelect(ir, options.fixedVarargCount, producer) {
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
	return backendGoNumericReturnValueForOptions(ir, backendGoNumericOptions{
		fixedVarargCount: fixedVarargCount,
	}, operation, result)
}

func backendGoNumericReturnValueForOptions(
	ir *backendProtoIR,
	options backendGoNumericOptions,
	operation *backendOperationIR,
	result int,
) (backendValueID, bool) {
	count, ok := backendGoNumericReturnCountForOptions(ir, options, operation)
	if !ok || result < 0 || result >= count {
		return invalidBackendValueID, false
	}
	register := operation.a + int32(result)
	if id := backendOperationUse(operation, register); ir.validBackendValue(id) {
		return id, true
	}
	if producer, direct := backendGoNumericOpenDirectTailCall(ir, options, operation); direct {
		for _, definition := range producer.defs {
			if definition.register == register {
				return definition.value, ir.validBackendValue(definition.value)
			}
		}
		return invalidBackendValueID, false
	}
	if operation.op != opReturn || operation.returnCount >= 0 || result != count-1 || operation.pc <= 0 {
		return invalidBackendValueID, false
	}
	producer := &ir.ops[operation.pc-1]
	if !backendGoNumericFixedVarargSelect(ir, options.fixedVarargCount, producer) ||
		producer.d >= 0 ||
		len(producer.defs) != 1 ||
		producer.defs[0].register != register {
		return invalidBackendValueID, false
	}
	return producer.defs[0].value, true
}

// backendGoNumericOpenDirectTailCall recognizes only the bounded form
// `return directTarget(...)`. Luau encodes its CALL and RETURN with open-result
// markers even when the statically bound target has exactly one result.
func backendGoNumericOpenDirectTailCall(
	ir *backendProtoIR,
	options backendGoNumericOptions,
	operation *backendOperationIR,
) (*backendOperationIR, bool) {
	if ir == nil || operation == nil || operation.op != opReturn ||
		operation.returnCount != -1 || operation.pc <= 0 || int(operation.pc) >= len(ir.ops) {
		return nil, false
	}
	producer := &ir.ops[operation.pc-1]
	if producer.op != opCall || producer.pc+1 != operation.pc ||
		producer.callResults != -1 || producer.a != operation.a {
		return nil, false
	}
	target, ok := backendGoNumericDirectTarget(options, producer)
	if !ok {
		return nil, false
	}
	argumentCount, argsOK := backendGoNumericArgumentCount(target.ir, target.fixedVarargCount)
	resultCount, resultsOK := backendGoNumericFixedResultCountFor(target.ir, target.fixedVarargCount)
	if !argsOK || !resultsOK || resultCount != 1 ||
		producer.callArgCount != int32(argumentCount) {
		return nil, false
	}
	definitions := 0
	for _, definition := range producer.defs {
		if definition.register == producer.a && ir.validBackendValue(definition.value) {
			definitions++
		}
	}
	return producer, definitions == 1
}

func backendGoNumericEffectiveCallResultCount(
	ir *backendProtoIR,
	options backendGoNumericOptions,
	operation *backendOperationIR,
) (int, bool) {
	if ir == nil || operation == nil || (operation.op != opCall && operation.op != opCallLocalOne) {
		return 0, false
	}
	if operation.callResults >= 0 {
		return int(operation.callResults), true
	}
	if operation.op != opCall {
		return 0, false
	}
	if operation.pc < 0 || int(operation.pc+1) >= len(ir.ops) {
		return 0, false
	}
	producer, ok := backendGoNumericOpenDirectTailCall(ir, options, &ir.ops[operation.pc+1])
	return 1, ok && producer == operation
}
