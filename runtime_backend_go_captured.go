package ember

func backendGoCapturedTableFieldsForClosure(
	caller *backendProtoIR,
	target *backendProtoIR,
	closure *backendOperationIR,
) (map[int32][]backendGoCapturedTableField, bool) {
	if caller == nil || target == nil || closure == nil || closure.op != opClosure {
		return nil, false
	}
	capturedUpvalues := backendGoCapturedRecordUpvalues(target)
	if len(capturedUpvalues) == 0 {
		return nil, true
	}
	records := analyzeBackendGoRecordTables(caller, analyzeBackendGoStructuralKeys(caller, backendGoNumericOptions{}))
	fields := make(map[int32][]backendGoCapturedTableField)
	for upvalue := 0; upvalue < len(target.upvalues); upvalue++ {
		if len(capturedUpvalues[int32(upvalue)]) == 0 {
			continue
		}
		descriptor := target.upvalues[upvalue]
		if descriptor.local == 0 || descriptor.index >= uint32(caller.registers) {
			return nil, false
		}
		captured := backendValueBeforeOperation(caller, closure, int32(descriptor.index))
		recordIndex, ok := records.recordByRoot[records.root(captured)]
		if !ok || recordIndex < 0 || recordIndex >= len(records.records) {
			return nil, false
		}
		record := &records.records[recordIndex]
		for field, name := range record.fieldNames {
			if field >= len(record.fieldValues) ||
				field >= len(record.fieldPresent) || !record.fieldPresent[field] {
				return nil, false
			}
			value := record.fieldValues[field]
			if !caller.validBackendValue(value) {
				return nil, false
			}
			tags := caller.values[value-1].tags
			if tags == 0 || tags&(tags-1) != 0 ||
				tags&^(backendTagNumber|backendTagBool|backendTagString) != 0 {
				return nil, false
			}
			fields[int32(upvalue)] = append(fields[int32(upvalue)], backendGoCapturedTableField{
				name: name, tags: tags,
			})
		}
		if len(fields[int32(upvalue)]) == 0 {
			return nil, false
		}
	}
	return fields, true
}

type backendGoCapturedRecordCall struct {
	target       backendGoNumericTarget
	callerFields []backendGoRecordFieldOperation
}

type backendGoCapturedRecordPlan struct {
	calls         map[int32]backendGoCapturedRecordCall
	closureValues []bool
}

func analyzeBackendGoCapturedRecordCalls(
	ir *backendProtoIR,
	records backendGoRecordTablePlan,
	options backendGoNumericOptions,
) (backendGoCapturedRecordPlan, error) {
	plan := backendGoCapturedRecordPlan{
		calls:         make(map[int32]backendGoCapturedRecordCall),
		closureValues: make([]bool, len(ir.values)),
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		target, closure, captured, ok := backendGoCapturedRecordCallShape(ir, options, operation)
		if !ok {
			continue
		}
		record, ok := records.recordByRoot[records.root(captured)]
		if !ok || record < 0 || record >= len(records.records) || records.records[record].storedAtPC >= 0 {
			continue
		}
		targetPlan, err := buildBackendGoNumericPlan(target.ir, backendGoNumericOptions{
			functionName:     target.functionName,
			directTargets:    options.directTargets,
			fixedVarargCount: target.fixedVarargCount,
		})
		if err != nil || targetPlan.tables.externalRoot == invalidBackendValueID {
			continue
		}
		callerFields := make([]backendGoRecordFieldOperation, 0)
		valid := true
		for _, targetField := range targetPlan.tables.fields {
			if targetField.key.table != targetPlan.tables.externalRoot ||
				targetField.tags != backendTagNumber ||
				targetField.child != invalidBackendValueID ||
				targetField.methodProto >= 0 {
				valid = false
				break
			}
			callerField, exists := records.records[record].fieldIndex[targetField.key.name]
			if !exists {
				valid = false
				break
			}
			callerFields = append(callerFields, backendGoRecordFieldOperation{
				storage: backendGoRecordFieldScratch,
				index:   record,
				field:   callerField,
			})
		}
		if !valid || len(callerFields) == 0 {
			continue
		}
		plan.calls[operation.pc] = backendGoCapturedRecordCall{
			target:       target,
			callerFields: callerFields,
		}
		plan.closureValues[closure-1] = true
	}
	return plan, nil
}

func backendGoCapturedRecordCallShape(
	ir *backendProtoIR,
	options backendGoNumericOptions,
	operation *backendOperationIR,
) (backendGoNumericTarget, backendValueID, backendValueID, bool) {
	if ir == nil || operation == nil || operation.op != opCall {
		return backendGoNumericTarget{}, invalidBackendValueID, invalidBackendValueID, false
	}
	target, ok := backendGoNumericDirectTarget(options, operation)
	if !ok || len(target.ir.upvalues) != 1 || len(backendGoCapturedRecordRoots(target.ir)) == 0 {
		return backendGoNumericTarget{}, invalidBackendValueID, invalidBackendValueID, false
	}
	closureUse := backendOperationUse(operation, operation.b)
	closure, closureOperation, ok := backendGoCapturedClosure(ir, closureUse, nil)
	if !ok || closureOperation.targetProto != operation.call.targetProto {
		return backendGoNumericTarget{}, invalidBackendValueID, invalidBackendValueID, false
	}
	upvalue := target.ir.upvalues[0]
	if upvalue.local == 0 || upvalue.copy == 0 || upvalue.index >= uint32(ir.registers) {
		return backendGoNumericTarget{}, invalidBackendValueID, invalidBackendValueID, false
	}
	captured := backendValueBeforeOperation(ir, closureOperation, int32(upvalue.index))
	if !ir.validBackendValue(captured) || ir.values[captured-1].object != backendObjectTable {
		return backendGoNumericTarget{}, invalidBackendValueID, invalidBackendValueID, false
	}
	return target, closure, captured, true
}

func backendGoCapturedClosure(
	ir *backendProtoIR,
	id backendValueID,
	seen map[backendValueID]bool,
) (backendValueID, *backendOperationIR, bool) {
	if !ir.validBackendValue(id) {
		return invalidBackendValueID, nil, false
	}
	if seen == nil {
		seen = make(map[backendValueID]bool)
	}
	if seen[id] {
		return invalidBackendValueID, nil, false
	}
	seen[id] = true
	value := &ir.values[id-1]
	if len(value.origins) == 1 && value.origins[0] != id {
		return backendGoCapturedClosure(ir, value.origins[0], seen)
	}
	if value.kind == backendValuePhi && len(value.origins) != 0 {
		var closure backendValueID
		var closureOperation *backendOperationIR
		for _, origin := range value.origins {
			candidate, operation, ok := backendGoCapturedClosure(ir, origin, seen)
			if !ok || closure != invalidBackendValueID && closure != candidate {
				return invalidBackendValueID, nil, false
			}
			closure = candidate
			closureOperation = operation
		}
		return closure, closureOperation, closure != invalidBackendValueID
	}
	if value.kind != backendValueOperation || value.pc < 0 || int(value.pc) >= len(ir.ops) {
		return invalidBackendValueID, nil, false
	}
	operation := &ir.ops[value.pc]
	switch operation.op {
	case opClosure:
		if len(operation.defs) != 1 || operation.defs[0].value != id {
			return invalidBackendValueID, nil, false
		}
		return id, operation, true
	case opMove:
		return backendGoCapturedClosure(ir, backendOperationUse(operation, operation.b), seen)
	default:
		return invalidBackendValueID, nil, false
	}
}

func (plan backendGoCapturedRecordPlan) call(
	operation *backendOperationIR,
) (backendGoCapturedRecordCall, bool) {
	if operation == nil {
		return backendGoCapturedRecordCall{}, false
	}
	call, ok := plan.calls[operation.pc]
	return call, ok
}
