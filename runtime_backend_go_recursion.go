package ember

import "math"

const backendGoMaxPreparedRecursiveArgument = 24

func backendGoNumericSelfRecursiveCall(
	ir *backendProtoIR,
	options backendGoNumericOptions,
	operation *backendOperationIR,
) bool {
	if !options.selfRecursive || operation == nil || operation.op != opCallUpvalueOne {
		return false
	}
	self, ok := backendGoNumericSelfRecursiveUpvalue(ir)
	return ok && operation.b == self
}

func backendGoNumericSelfRecursiveTarget(ir *backendProtoIR) bool {
	_, ok := backendGoNumericSelfRecursiveUpvalue(ir)
	return ok
}

func backendGoNumericSelfRecursiveUpvalue(ir *backendProtoIR) (int32, bool) {
	if ir == nil ||
		ir.variadic ||
		ir.params != 1 ||
		len(ir.upvalues) == 0 {
		return -1, false
	}
	self := int32(-1)
	for upvalue, descriptor := range ir.upvalues {
		if descriptor.local == 0 {
			return -1, false
		}
		if descriptor.copy == 0 {
			if self >= 0 {
				return -1, false
			}
			self = int32(upvalue)
		}
	}
	if self < 0 {
		return -1, false
	}
	var baseGuard *backendOperationIR
	recursivePCs := make([]int32, 0, 2)
	recursiveCalls := 0
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		switch operation.op {
		case opLoadConst, opMove, opSubK, opAddK, opAdd, opModK, opReturnOne:
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
					return -1, false
				}
				baseGuard = operation
			}
		case opJumpIfNotLess:
			base := backendOperationUse(operation, operation.b)
			lower, upper, bounded := backendGoNumericRecursiveBaseInterval(ir, base, nil)
			if operation.a != 0 || !bounded || lower < 0 || upper > backendGoMaxPreparedRecursiveArgument {
				continue
			}
			if baseGuard != nil {
				return -1, false
			}
			baseGuard = operation
		case opCallUpvalueOne:
			if operation.b != self ||
				operation.callArgCount != 1 ||
				!backendGoNumericDecreasingRecursiveArgument(ir, operation) {
				return -1, false
			}
			recursiveCalls++
			recursivePCs = append(recursivePCs, operation.pc)
		case opGetUpvalue:
			if operation.b == self {
				return -1, false
			}
		case opSetUpvalue:
			return -1, false
		default:
			return -1, false
		}
	}
	if baseGuard == nil ||
		baseGuard.targetPC <= baseGuard.pc+1 ||
		int(baseGuard.pc+1) >= len(ir.ops) {
		return -1, false
	}
	baseReturn := &ir.ops[baseGuard.pc+1]
	if baseReturn.op != opReturnOne ||
		backendOperationUse(baseReturn, baseReturn.a) != ir.initial[0] {
		return -1, false
	}
	for _, pc := range recursivePCs {
		if pc < baseGuard.targetPC {
			return -1, false
		}
	}
	return self, recursiveCalls != 0
}

func backendGoNumericRecursiveBaseInterval(
	ir *backendProtoIR,
	id backendValueID,
	seen map[backendValueID]bool,
) (float64, float64, bool) {
	if ir == nil || !ir.validBackendValue(id) {
		return 0, 0, false
	}
	if seen == nil {
		seen = make(map[backendValueID]bool)
	}
	if seen[id] {
		return 0, 0, false
	}
	seen[id] = true
	value := &ir.values[id-1]
	if value.kind != backendValueOperation || value.pc < 0 || int(value.pc) >= len(ir.ops) {
		return 0, 0, false
	}
	operation := &ir.ops[value.pc]
	switch operation.op {
	case opLoadConst:
		if operation.b < 0 || int(operation.b) >= len(ir.constants) || ir.constants[operation.b].kind != NumberKind {
			return 0, 0, false
		}
		constant := math.Float64frombits(ir.constants[operation.b].bits)
		return constant, constant, !math.IsNaN(constant) && !math.IsInf(constant, 0)
	case opMove:
		return backendGoNumericRecursiveBaseInterval(ir, backendOperationUse(operation, operation.b), seen)
	case opAdd:
		leftLower, leftUpper, leftOK := backendGoNumericRecursiveBaseInterval(ir, backendOperationUse(operation, operation.b), seen)
		rightLower, rightUpper, rightOK := backendGoNumericRecursiveBaseInterval(ir, backendOperationUse(operation, operation.c), seen)
		lower, upper := leftLower+rightLower, leftUpper+rightUpper
		return lower, upper, leftOK && rightOK && backendGoNumericFiniteInterval(lower, upper)
	case opAddK, opSubK:
		lower, upper, ok := backendGoNumericRecursiveBaseInterval(ir, backendOperationUse(operation, operation.b), seen)
		if !ok || operation.c < 0 || int(operation.c) >= len(ir.constants) || ir.constants[operation.c].kind != NumberKind {
			return 0, 0, false
		}
		constant := math.Float64frombits(ir.constants[operation.c].bits)
		if math.IsNaN(constant) || math.IsInf(constant, 0) {
			return 0, 0, false
		}
		if operation.op == opSubK {
			constant = -constant
		}
		lower += constant
		upper += constant
		return lower, upper, backendGoNumericFiniteInterval(lower, upper)
	case opModK:
		if !backendGoNumericRecursiveFiniteSource(ir, backendOperationUse(operation, operation.b), nil) ||
			operation.c < 0 || int(operation.c) >= len(ir.constants) || ir.constants[operation.c].kind != NumberKind {
			return 0, 0, false
		}
		divisor := math.Float64frombits(ir.constants[operation.c].bits)
		if math.IsNaN(divisor) || math.IsInf(divisor, 0) || divisor <= 0 {
			return 0, 0, false
		}
		return 0, divisor, true
	default:
		return 0, 0, false
	}
}

func backendGoNumericFiniteInterval(lower, upper float64) bool {
	return !math.IsNaN(lower) && !math.IsInf(lower, 0) &&
		!math.IsNaN(upper) && !math.IsInf(upper, 0) && lower <= upper
}

func backendGoNumericRecursiveFiniteSource(
	ir *backendProtoIR,
	id backendValueID,
	seen map[backendValueID]bool,
) bool {
	if ir == nil || !ir.validBackendValue(id) {
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
	if value.kind != backendValueOperation || value.pc < 0 || int(value.pc) >= len(ir.ops) {
		return false
	}
	operation := &ir.ops[value.pc]
	if operation.op == opMove {
		return backendGoNumericRecursiveFiniteSource(ir, backendOperationUse(operation, operation.b), seen)
	}
	return operation.op == opGetUpvalue
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
	self, ok := backendGoNumericSelfRecursiveUpvalue(target)
	if !ok {
		return false
	}
	upvalue := target.upvalues[self]
	return closure.op == opClosure &&
		closure.targetProto >= 0 &&
		value.targetProtos[0] == closure.targetProto &&
		upvalue.local != 0 &&
		upvalue.copy == 0 &&
		upvalue.index == uint32(closure.a)
}
