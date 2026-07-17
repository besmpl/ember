package ember

import "math"

type backendGoScalarClosureFactory struct {
	closureProto int32
	captures     []backendGoScalarClosureCapture
}

type backendGoScalarClosureCapture struct {
	argument int32
	constant float64
}

type backendGoScalarClosureValue struct {
	cellStart   int
	cellCount   int
	targetProto int32
}

type backendGoScalarClosureCall struct {
	cellStart int
	cellCount int
	target    backendGoNumericTarget
}

type backendGoScalarLocalClosure struct {
	cellStart   int
	targetProto int32
	captures    []backendValueID
}

type backendGoScalarClosurePlan struct {
	cellCount      int
	values         []backendGoScalarClosureValue
	factoriesByPC  map[int32]backendGoScalarClosureFactory
	factoryCells   map[int32]int
	closureCalls   map[int32]backendGoScalarClosureCall
	localsByPC     map[int32]backendGoScalarLocalClosure
	scalarClosures []bool
}

func analyzeBackendGoScalarClosures(
	ir *backendProtoIR,
	options backendGoNumericOptions,
) (backendGoScalarClosurePlan, error) {
	plan := backendGoScalarClosurePlan{
		values:         make([]backendGoScalarClosureValue, len(ir.values)),
		factoriesByPC:  make(map[int32]backendGoScalarClosureFactory),
		factoryCells:   make(map[int32]int),
		closureCalls:   make(map[int32]backendGoScalarClosureCall),
		localsByPC:     make(map[int32]backendGoScalarLocalClosure),
		scalarClosures: make([]bool, len(ir.values)),
	}
	for valueIndex := range plan.values {
		plan.values[valueIndex].cellStart = -1
		plan.values[valueIndex].targetProto = -1
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opClosure || len(operation.defs) != 1 ||
			operation.targetProto < 0 || int(operation.targetProto) >= len(options.directTargets) {
			continue
		}
		target := options.directTargets[operation.targetProto]
		self, recursive := backendGoNumericSelfRecursiveUpvalue(target.ir)
		if target.ir == nil || target.functionName == "" || !target.selfRecursive || !recursive ||
			len(target.ir.upvalues) == 1 || target.ir.upvalues[self].index != uint32(operation.a) {
			continue
		}
		captures := make([]backendValueID, len(target.ir.upvalues))
		valid := true
		for upvalue, descriptor := range target.ir.upvalues {
			if int32(upvalue) == self {
				continue
			}
			if descriptor.local == 0 || descriptor.copy == 0 || descriptor.index >= uint32(ir.registers) {
				valid = false
				break
			}
			capture := backendValueBeforeOperation(ir, operation, int32(descriptor.index))
			if !ir.validBackendValue(capture) {
				valid = false
				break
			}
			captures[upvalue] = capture
		}
		if !valid {
			continue
		}
		cell := plan.cellCount
		plan.cellCount += len(target.ir.upvalues)
		plan.localsByPC[operation.pc] = backendGoScalarLocalClosure{
			cellStart: cell, targetProto: operation.targetProto, captures: captures,
		}
		plan.values[operation.defs[0].value-1] = backendGoScalarClosureValue{
			cellStart: cell, cellCount: len(target.ir.upvalues), targetProto: operation.targetProto,
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opCallLocalOne ||
			operation.call.kind != backendCallDirectProto {
			continue
		}
		target, ok := backendGoNumericDirectTarget(ir, options, operation)
		if !ok {
			continue
		}
		factory, ok := backendGoNumericClosureFactory(target.ir, options.directTargets)
		if !ok ||
			operation.callArgCount != int32(target.ir.params) ||
			len(factory.captures) == 0 ||
			len(operation.defs) != 1 {
			continue
		}
		capturesValid := true
		for _, capture := range factory.captures {
			if capture.argument >= operation.callArgCount {
				capturesValid = false
				break
			}
		}
		if !capturesValid {
			continue
		}
		cell := plan.cellCount
		plan.cellCount += len(factory.captures)
		plan.factoriesByPC[operation.pc] = factory
		plan.factoryCells[operation.pc] = cell
		definition := operation.defs[0].value
		plan.values[definition-1] = backendGoScalarClosureValue{
			cellStart:   cell,
			cellCount:   len(factory.captures),
			targetProto: factory.closureProto,
		}
	}
	for {
		changed := false
		for valueIndex := range ir.values {
			if plan.values[valueIndex].cellStart >= 0 {
				continue
			}
			value := &ir.values[valueIndex]
			candidate := backendGoScalarClosureValue{cellStart: -1, targetProto: -1}
			switch value.kind {
			case backendValueOperation:
				operation := &ir.ops[value.pc]
				if operation.op != opMove {
					continue
				}
				source := backendOperationUse(operation, operation.b)
				if !ir.validBackendValue(source) {
					continue
				}
				candidate = plan.values[source-1]
			case backendValuePhi:
				block := &ir.blocks[value.block]
				phi := block.phis[value.register]
				for inputIndex, input := range phi.inputs {
					if !ir.blocks[block.predecessors[inputIndex]].reachable ||
						!ir.validBackendValue(input) {
						continue
					}
					current := plan.values[input-1]
					if current.cellStart < 0 {
						continue
					}
					if candidate.cellStart >= 0 && candidate != current {
						return backendGoScalarClosurePlan{}, nil
					}
					candidate = current
				}
			default:
				continue
			}
			if candidate.cellStart >= 0 {
				plan.values[valueIndex] = candidate
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	for valueIndex := range ir.values {
		value := &ir.values[valueIndex]
		if value.kind != backendValuePhi || plan.values[valueIndex].cellStart < 0 {
			continue
		}
		block := &ir.blocks[value.block]
		phi := block.phis[value.register]
		for inputIndex, input := range phi.inputs {
			if !ir.blocks[block.predecessors[inputIndex]].reachable ||
				!ir.validBackendValue(input) ||
				plan.values[input-1].cellStart < 0 {
				continue
			}
			if plan.values[input-1] != plan.values[valueIndex] {
				return backendGoScalarClosurePlan{}, nil
			}
		}
	}
	for valueIndex, closure := range plan.values {
		if closure.cellStart >= 0 {
			plan.scalarClosures[valueIndex] = true
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opCallLocalOne {
			continue
		}
		callee := backendOperationUse(operation, operation.b)
		if !ir.validBackendValue(callee) {
			continue
		}
		closure := plan.values[callee-1]
		if closure.cellStart < 0 ||
			closure.targetProto < 0 ||
			int(closure.targetProto) >= len(options.directTargets) {
			continue
		}
		target := options.directTargets[closure.targetProto]
		if target.ir == nil ||
			target.functionName == "" ||
			target.ir.variadic ||
			len(target.ir.upvalues) != closure.cellCount ||
			operation.callArgCount != int32(target.ir.params) ||
			operation.callArgCount < 0 {
			continue
		}
		plan.closureCalls[operation.pc] = backendGoScalarClosureCall{
			cellStart: closure.cellStart,
			cellCount: closure.cellCount,
			target:    target,
		}
	}
	return plan, nil
}

func backendGoNumericClosureFactory(
	ir *backendProtoIR,
	targets []backendGoNumericTarget,
) (backendGoScalarClosureFactory, bool) {
	if ir == nil || ir.variadic || len(ir.upvalues) != 0 {
		return backendGoScalarClosureFactory{}, false
	}
	var closure *backendOperationIR
	var returned backendValueID
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		switch operation.op {
		case opMove, opLoadConst:
		case opClosure:
			if closure != nil || len(operation.defs) != 1 {
				return backendGoScalarClosureFactory{}, false
			}
			closure = operation
		case opReturnOne, opReturn:
			if returned != invalidBackendValueID ||
				operation.returnCount != 1 {
				return backendGoScalarClosureFactory{}, false
			}
			returned = backendOperationUse(operation, operation.a)
		default:
			return backendGoScalarClosureFactory{}, false
		}
	}
	if closure == nil ||
		returned != closure.defs[0].value ||
		closure.targetProto < 0 ||
		int(closure.targetProto) >= len(targets) {
		return backendGoScalarClosureFactory{}, false
	}
	target := targets[closure.targetProto]
	if target.ir == nil ||
		target.functionName == "" ||
		target.ir.variadic ||
		len(target.ir.upvalues) == 0 {
		return backendGoScalarClosureFactory{}, false
	}
	captures := make([]backendGoScalarClosureCapture, 0, len(target.ir.upvalues))
	for _, upvalue := range target.ir.upvalues {
		if upvalue.local == 0 || upvalue.index >= uint32(ir.registers) {
			return backendGoScalarClosureFactory{}, false
		}
		captured := backendValueBeforeOperation(ir, closure, int32(upvalue.index))
		if parameter, ok := backendGoNumericParameterSource(ir, captured, nil); ok {
			captures = append(captures, backendGoScalarClosureCapture{argument: int32(parameter)})
			continue
		}
		constant, ok := backendGoNumericStaticValue(ir, captured)
		if !ok {
			return backendGoScalarClosureFactory{}, false
		}
		captures = append(captures, backendGoScalarClosureCapture{argument: -1, constant: constant})
	}
	return backendGoScalarClosureFactory{
		closureProto: closure.targetProto,
		captures:     captures,
	}, true
}

func backendGoNumericStaticValue(ir *backendProtoIR, id backendValueID) (float64, bool) {
	if !ir.validBackendValue(id) {
		return 0, false
	}
	value := &ir.values[id-1]
	if value.kind != backendValueOperation || value.pc < 0 || int(value.pc) >= len(ir.ops) {
		return 0, false
	}
	operation := &ir.ops[value.pc]
	if operation.op == opMove {
		return backendGoNumericStaticValue(ir, backendOperationUse(operation, operation.b))
	}
	if operation.op != opLoadConst || operation.b < 0 || int(operation.b) >= len(ir.constants) {
		return 0, false
	}
	constant := ir.constants[operation.b]
	if constant.kind != NumberKind {
		return 0, false
	}
	return math.Float64frombits(constant.bits), true
}

func backendGoNumericParameterSource(
	ir *backendProtoIR,
	id backendValueID,
	seen map[backendValueID]bool,
) (int, bool) {
	if !ir.validBackendValue(id) {
		return 0, false
	}
	if seen == nil {
		seen = make(map[backendValueID]bool)
	}
	if seen[id] {
		return 0, false
	}
	seen[id] = true
	value := &ir.values[id-1]
	switch value.kind {
	case backendValueParameter:
		if value.register < 0 || int(value.register) >= ir.params {
			return 0, false
		}
		return int(value.register), true
	case backendValueOperation:
		if value.pc < 0 || int(value.pc) >= len(ir.ops) ||
			ir.ops[value.pc].op != opMove {
			return 0, false
		}
		return backendGoNumericParameterSource(
			ir,
			backendOperationUse(&ir.ops[value.pc], ir.ops[value.pc].b),
			seen,
		)
	default:
		return 0, false
	}
}

func (plan backendGoScalarClosurePlan) factory(
	operation *backendOperationIR,
) (backendGoScalarClosureFactory, int, bool) {
	if operation == nil {
		return backendGoScalarClosureFactory{}, 0, false
	}
	factory, ok := plan.factoriesByPC[operation.pc]
	if !ok {
		return backendGoScalarClosureFactory{}, 0, false
	}
	cell, ok := plan.factoryCells[operation.pc]
	return factory, cell, ok
}

func (plan backendGoScalarClosurePlan) call(
	operation *backendOperationIR,
) (backendGoScalarClosureCall, bool) {
	if operation == nil {
		return backendGoScalarClosureCall{}, false
	}
	call, ok := plan.closureCalls[operation.pc]
	return call, ok
}

func (plan backendGoScalarClosurePlan) local(
	operation *backendOperationIR,
) (backendGoScalarLocalClosure, bool) {
	if operation == nil {
		return backendGoScalarLocalClosure{}, false
	}
	local, ok := plan.localsByPC[operation.pc]
	return local, ok
}

func (plan backendGoScalarClosurePlan) scalarValue(id backendValueID) bool {
	return id != invalidBackendValueID &&
		int(id) <= len(plan.scalarClosures) &&
		plan.scalarClosures[id-1]
}

func (plan backendGoScalarClosurePlan) value(id backendValueID) (backendGoScalarClosureValue, bool) {
	if id == invalidBackendValueID || int(id) > len(plan.values) {
		return backendGoScalarClosureValue{}, false
	}
	value := plan.values[id-1]
	return value, value.cellStart >= 0
}
