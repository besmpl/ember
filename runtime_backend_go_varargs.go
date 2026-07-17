package ember

const backendGoMaxFixedVarargCount = 32

func backendGoNumericArgumentCount(
	ir *backendProtoIR,
	fixedVarargCount int,
) (int, bool) {
	if ir == nil ||
		ir.params < 0 ||
		fixedVarargCount < 0 ||
		fixedVarargCount > backendGoMaxFixedVarargCount {
		return 0, false
	}
	if !ir.variadic {
		return ir.params, fixedVarargCount == 0
	}
	argumentCount := ir.params + fixedVarargCount
	return argumentCount, argumentCount >= ir.params
}

func backendGoNumericFixedVarargSelect(
	ir *backendProtoIR,
	fixedVarargCount int,
	operation *backendOperationIR,
) bool {
	return ir != nil &&
		ir.variadic &&
		fixedVarargCount >= 0 &&
		operation != nil &&
		operation.op == opFastCall &&
		nativeFuncID(operation.nativeID) == nativeFuncSelect &&
		operation.c == 0 &&
		(operation.d == 1 || operation.d < 0) &&
		len(operation.defs) == 1 &&
		operation.defs[0].register == operation.a
}

func backendGoNumericVarargIndex(
	ir *backendProtoIR,
	fixedVarargCount int,
	operation *backendOperationIR,
	register int32,
) (int, bool) {
	if ir == nil ||
		!ir.variadic ||
		fixedVarargCount < 0 ||
		operation == nil ||
		operation.op != opVararg ||
		operation.b < 0 ||
		int(operation.b) > fixedVarargCount {
		return 0, false
	}
	index := register - operation.a
	if index < 0 || index >= operation.b {
		return 0, false
	}
	return ir.params + int(index), true
}
