package ember

import "math"

const backendGoMaxPreparedRecursiveArgument = 24

func backendGoNumericSelfRecursiveTarget(ir *backendProtoIR) bool {
	if ir == nil ||
		ir.variadic ||
		ir.params != 1 ||
		len(ir.upvalues) != 1 {
		return false
	}
	upvalue := ir.upvalues[0]
	if upvalue.local == 0 || upvalue.copy != 0 {
		return false
	}
	var baseGuard *backendOperationIR
	recursivePCs := make([]int32, 0, 2)
	recursiveCalls := 0
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		switch operation.op {
		case opMove, opSubK, opAdd, opReturnOne:
		case opJumpIfNotLessK:
			if operation.a != 0 ||
				operation.b < 0 ||
				int(operation.b) >= len(ir.constants) ||
				ir.constants[operation.b].kind != NumberKind {
				continue
			}
			base := math.Float64frombits(ir.constants[operation.b].bits)
			if !math.IsNaN(base) &&
				!math.IsInf(base, 0) &&
				base >= 0 &&
				base <= backendGoMaxPreparedRecursiveArgument {
				if baseGuard != nil {
					return false
				}
				baseGuard = operation
			}
		case opCallUpvalueOne:
			if operation.b != 0 ||
				operation.callArgCount != 1 ||
				!backendGoNumericDecreasingRecursiveArgument(ir, operation) {
				return false
			}
			recursiveCalls++
			recursivePCs = append(recursivePCs, operation.pc)
		case opGetUpvalue, opSetUpvalue:
			return false
		default:
			return false
		}
	}
	if baseGuard == nil ||
		baseGuard.pc != 0 ||
		baseGuard.targetPC <= baseGuard.pc+1 ||
		int(baseGuard.pc+1) >= len(ir.ops) {
		return false
	}
	baseReturn := &ir.ops[baseGuard.pc+1]
	if baseReturn.op != opReturnOne ||
		backendOperationUse(baseReturn, baseReturn.a) != ir.initial[0] {
		return false
	}
	for _, pc := range recursivePCs {
		if pc < baseGuard.targetPC {
			return false
		}
	}
	return recursiveCalls != 0
}

func backendGoNumericDecreasingRecursiveArgument(
	ir *backendProtoIR,
	operation *backendOperationIR,
) bool {
	if ir == nil || operation == nil || operation.callArgStart < 0 {
		return false
	}
	argument := backendOperationUse(operation, operation.callArgStart)
	if !ir.validBackendValue(argument) {
		return false
	}
	value := &ir.values[argument-1]
	if value.kind != backendValueOperation ||
		value.pc < 0 ||
		int(value.pc) >= len(ir.ops) {
		return false
	}
	subtract := &ir.ops[value.pc]
	if subtract.op != opSubK ||
		subtract.c < 0 ||
		int(subtract.c) >= len(ir.constants) ||
		ir.constants[subtract.c].kind != NumberKind {
		return false
	}
	decrement := math.Float64frombits(ir.constants[subtract.c].bits)
	if math.IsNaN(decrement) ||
		math.IsInf(decrement, 0) ||
		decrement < 1 {
		return false
	}
	return backendGoNumericParameterValue(ir, backendOperationUse(subtract, subtract.b), nil)
}

func backendGoNumericParameterValue(
	ir *backendProtoIR,
	id backendValueID,
	seen map[backendValueID]bool,
) bool {
	if !ir.validBackendValue(id) {
		return false
	}
	if seen == nil {
		seen = make(map[backendValueID]bool)
	}
	if seen[id] {
		return false
	}
	seen[id] = true
	value := &ir.values[id-1]
	switch value.kind {
	case backendValueParameter:
		return value.register == 0
	case backendValueOperation:
		return value.pc >= 0 &&
			int(value.pc) < len(ir.ops) &&
			ir.ops[value.pc].op == opMove &&
			backendGoNumericParameterValue(
				ir,
				backendOperationUse(&ir.ops[value.pc], ir.ops[value.pc].b),
				seen,
			)
	default:
		return false
	}
}

func backendGoNumericSelfClosure(
	ir *backendProtoIR,
	id backendValueID,
	target *backendProtoIR,
) bool {
	if ir == nil ||
		target == nil ||
		len(target.upvalues) != 1 ||
		!ir.validBackendValue(id) {
		return false
	}
	value := &ir.values[id-1]
	if len(value.origins) != 1 ||
		len(value.targetProtos) != 1 ||
		!ir.validBackendValue(value.origins[0]) {
		return false
	}
	origin := &ir.values[value.origins[0]-1]
	if origin.kind != backendValueOperation ||
		origin.pc < 0 ||
		int(origin.pc) >= len(ir.ops) {
		return false
	}
	closure := &ir.ops[origin.pc]
	upvalue := target.upvalues[0]
	return closure.op == opClosure &&
		closure.targetProto >= 0 &&
		value.targetProtos[0] == closure.targetProto &&
		upvalue.local != 0 &&
		upvalue.copy == 0 &&
		upvalue.index == uint32(closure.a)
}
