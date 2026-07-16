package ember

type backendGoScalarMethodCall struct {
	target       backendGoNumericTarget
	receiverRoot backendValueID
	callerFields []int
}

type backendGoScalarMethodPlan struct {
	calls          map[int32]backendGoScalarMethodCall
	methodClosures []bool
}

func analyzeBackendGoScalarMethods(
	ir *backendProtoIR,
	tables backendGoScalarTablePlan,
	options backendGoNumericOptions,
) (backendGoScalarMethodPlan, error) {
	plan := backendGoScalarMethodPlan{
		calls:          make(map[int32]backendGoScalarMethodCall),
		methodClosures: make([]bool, len(ir.values)),
	}
	setters := make(map[backendGoScalarFieldKey][]*backendOperationIR)
	for _, field := range tables.fields {
		if field.methodProto < 0 {
			continue
		}
		for pc := range ir.ops {
			operation := &ir.ops[pc]
			if operation.op != opSetStringField {
				continue
			}
			_, candidate, ok := tables.operationField(ir, operation)
			if !ok || candidate.key != field.key || candidate.methodProto != field.methodProto {
				continue
			}
			setters[field.key] = append(setters[field.key], operation)
			source := backendOperationUse(operation, operation.c)
			if ir.validBackendValue(source) {
				plan.methodClosures[source-1] = true
			}
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opCallMethodOne {
			continue
		}
		receiverRoot := tables.root(backendOperationUse(operation, operation.b))
		name, ok := backendGoStringFieldName(ir, operation.access.constant)
		if receiverRoot == invalidBackendValueID || !ok {
			continue
		}
		_, methodField, ok := tables.resolveField(receiverRoot, name)
		if !ok || methodField.methodProto < 0 ||
			int(methodField.methodProto) >= len(options.directTargets) {
			continue
		}
		methodSetters := setters[methodField.key]
		if len(methodSetters) != 1 ||
			!backendGoScalarMethodSetterDominates(ir, methodSetters[0], operation) {
			continue
		}
		target := options.directTargets[methodField.methodProto]
		if target.ir == nil ||
			target.functionName == "" ||
			!target.receiverTable ||
			target.selfRecursive ||
			target.ir.variadic ||
			len(target.ir.upvalues) != 0 ||
			target.ir.params == 0 ||
			operation.callArgCount != int32(target.ir.params) ||
			operation.callArgCount < 1 ||
			operation.callResults != 1 {
			continue
		}
		resultCount, fixedResults := backendGoNumericFixedResultCount(target.ir)
		if !fixedResults || resultCount != 1 {
			continue
		}
		targetOptions := backendGoNumericOptions{
			functionName:     target.functionName,
			directTargets:    options.directTargets,
			fixedVarargCount: target.fixedVarargCount,
			receiverTable:    true,
		}
		targetPlan, err := buildBackendGoNumericPlan(target.ir, targetOptions)
		if err != nil || targetPlan.tables.externalRoot == invalidBackendValueID {
			continue
		}
		callerFields := make([]int, 0)
		valid := true
		for _, targetField := range targetPlan.tables.fields {
			if targetField.key.table != targetPlan.tables.externalRoot ||
				targetField.tags != backendTagNumber ||
				targetField.child != invalidBackendValueID ||
				targetField.methodProto >= 0 {
				valid = false
				break
			}
			callerField, callerValue, ok := tables.resolveField(receiverRoot, targetField.key.name)
			if !ok ||
				callerValue.tags != targetField.tags ||
				callerValue.child != invalidBackendValueID ||
				callerValue.methodProto >= 0 {
				valid = false
				break
			}
			callerFields = append(callerFields, callerField)
		}
		if !valid || len(callerFields) == 0 {
			continue
		}
		plan.calls[operation.pc] = backendGoScalarMethodCall{
			target:       target,
			receiverRoot: receiverRoot,
			callerFields: callerFields,
		}
	}
	return plan, nil
}

func backendGoScalarMethodSetterDominates(
	ir *backendProtoIR,
	setter *backendOperationIR,
	call *backendOperationIR,
) bool {
	if ir == nil || setter == nil || call == nil ||
		setter.block < 0 || int(setter.block) >= len(ir.blocks) ||
		call.block < 0 || int(call.block) >= len(ir.blocks) ||
		!backendBlockDominates(&ir.blocks[call.block], setter.block) {
		return false
	}
	return setter.block != call.block || setter.pc < call.pc
}

func (plan backendGoScalarMethodPlan) call(
	operation *backendOperationIR,
) (backendGoScalarMethodCall, bool) {
	if operation == nil {
		return backendGoScalarMethodCall{}, false
	}
	call, ok := plan.calls[operation.pc]
	return call, ok
}

func (plan backendGoScalarMethodPlan) scalarClosure(id backendValueID) bool {
	return id != invalidBackendValueID &&
		int(id) <= len(plan.methodClosures) &&
		plan.methodClosures[id-1]
}
