package ember

type backendGoScalarClosureFactory struct {
	closureProto    int32
	captureArgument int32
}

type backendGoScalarClosureValue struct {
	cell        int
	targetProto int32
}

type backendGoScalarClosureCall struct {
	cell   int
	target backendGoNumericTarget
}

type backendGoScalarClosurePlan struct {
	cellCount      int
	values         []backendGoScalarClosureValue
	factoriesByPC  map[int32]backendGoScalarClosureFactory
	factoryCells   map[int32]int
	closureCalls   map[int32]backendGoScalarClosureCall
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
		scalarClosures: make([]bool, len(ir.values)),
	}
	for valueIndex := range plan.values {
		plan.values[valueIndex].cell = -1
		plan.values[valueIndex].targetProto = -1
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opCallLocalOne ||
			operation.call.kind != backendCallDirectProto {
			continue
		}
		target, ok := backendGoNumericDirectTarget(options, operation)
		if !ok {
			continue
		}
		factory, ok := backendGoNumericClosureFactory(target.ir, options.directTargets)
		if !ok ||
			operation.callArgCount != int32(target.ir.params) ||
			factory.captureArgument < 0 ||
			factory.captureArgument >= operation.callArgCount ||
			len(operation.defs) != 1 {
			continue
		}
		cell := plan.cellCount
		plan.cellCount++
		plan.factoriesByPC[operation.pc] = factory
		plan.factoryCells[operation.pc] = cell
		definition := operation.defs[0].value
		plan.values[definition-1] = backendGoScalarClosureValue{
			cell:        cell,
			targetProto: factory.closureProto,
		}
	}
	for {
		changed := false
		for valueIndex := range ir.values {
			if plan.values[valueIndex].cell >= 0 {
				continue
			}
			value := &ir.values[valueIndex]
			candidate := backendGoScalarClosureValue{cell: -1, targetProto: -1}
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
					if current.cell < 0 {
						continue
					}
					if candidate.cell >= 0 && candidate != current {
						return backendGoScalarClosurePlan{}, nil
					}
					candidate = current
				}
			default:
				continue
			}
			if candidate.cell >= 0 {
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
		if value.kind != backendValuePhi || plan.values[valueIndex].cell < 0 {
			continue
		}
		block := &ir.blocks[value.block]
		phi := block.phis[value.register]
		for inputIndex, input := range phi.inputs {
			if !ir.blocks[block.predecessors[inputIndex]].reachable ||
				!ir.validBackendValue(input) ||
				plan.values[input-1].cell < 0 {
				continue
			}
			if plan.values[input-1] != plan.values[valueIndex] {
				return backendGoScalarClosurePlan{}, nil
			}
		}
	}
	for valueIndex, closure := range plan.values {
		if closure.cell >= 0 {
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
		if closure.cell < 0 ||
			closure.targetProto < 0 ||
			int(closure.targetProto) >= len(options.directTargets) {
			continue
		}
		target := options.directTargets[closure.targetProto]
		if target.ir == nil ||
			target.functionName == "" ||
			target.ir.variadic ||
			len(target.ir.upvalues) != 1 ||
			operation.callArgCount != int32(target.ir.params) ||
			operation.callArgCount < 0 {
			continue
		}
		plan.closureCalls[operation.pc] = backendGoScalarClosureCall{
			cell:   closure.cell,
			target: target,
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
		case opMove:
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
		len(target.ir.upvalues) != 1 {
		return backendGoScalarClosureFactory{}, false
	}
	upvalue := target.ir.upvalues[0]
	if upvalue.local == 0 || upvalue.copy != 0 ||
		upvalue.index >= uint32(ir.registers) {
		return backendGoScalarClosureFactory{}, false
	}
	captured := backendValueBeforeOperation(ir, closure, int32(upvalue.index))
	parameter, ok := backendGoNumericParameterSource(ir, captured, nil)
	if !ok {
		return backendGoScalarClosureFactory{}, false
	}
	return backendGoScalarClosureFactory{
		closureProto:    closure.targetProto,
		captureArgument: int32(parameter),
	}, true
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

func (plan backendGoScalarClosurePlan) scalarValue(id backendValueID) bool {
	return id != invalidBackendValueID &&
		int(id) <= len(plan.scalarClosures) &&
		plan.scalarClosures[id-1]
}
