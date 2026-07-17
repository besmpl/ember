package ember

import "fmt"

type backendGoRecordIndexFunctionCall struct {
	target          backendGoNumericTarget
	field           backendGoRecordFieldOperation
	key             machineStringID
	numericCaptures []backendValueID
	capturedFields  []backendGoRecordFieldOperation
}

type backendGoRecordIndexFunctionPlan struct {
	calls map[int32]backendGoRecordIndexFunctionCall
}

func analyzeBackendGoRecordIndexFunctions(
	ir *backendProtoIR,
	records backendGoRecordTablePlan,
	options backendGoNumericOptions,
) backendGoRecordIndexFunctionPlan {
	plan := backendGoRecordIndexFunctionPlan{calls: make(map[int32]backendGoRecordIndexFunctionCall)}
	if ir == nil || !records.enabled || len(records.metatableByPC) == 0 {
		return plan
	}

	type fallback struct {
		setter  *backendOperationIR
		closure *backendOperationIR
		target  backendGoNumericTarget
		fields  []backendGoRecordFieldOperation
	}
	fallbackByMetatable := make(map[backendValueID]fallback)
	for setterPC, functionField := range records.functionSetterPC {
		setter := &ir.ops[setterPC]
		if !backendGoStringFieldIsIndex(ir, setter.access.constant) ||
			functionField.record < 0 || functionField.record >= len(records.records) {
			continue
		}
		record := &records.records[functionField.record]
		if len(record.fieldNames) != 1 || len(record.writesByPC) != 1 {
			continue
		}
		closureValue := backendOperationUse(setter, setter.c)
		_, closure, ok := backendGoCapturedClosure(ir, closureValue, nil)
		if !ok || closure.targetProto < 0 || int(closure.targetProto) >= len(options.directTargets) {
			continue
		}
		target := options.directTargets[closure.targetProto]
		if target.ir == nil || target.functionName == "" || !target.receiverTable ||
			target.receiverTables != 0 || target.selfRecursive || target.ir.variadic ||
			target.ir.params != 2 {
			continue
		}
		capturedFields, capturedOK := backendGoCapturedTableFieldsForClosure(ir, target.ir, closure)
		if !capturedOK {
			continue
		}
		if len(target.capturedTableFields) == 0 {
			target.capturedTableFields = capturedFields
		} else if !backendGoCapturedTableFieldsEqual(target.capturedTableFields, capturedFields) {
			continue
		}
		targetPlan, err := buildBackendGoNumericPlan(target.ir, backendGoNumericOptions{
			functionName:        target.functionName,
			directTargets:       options.directTargets,
			receiverTable:       true,
			capturedTableFields: target.capturedTableFields,
		})
		if err != nil {
			continue
		}
		resultCount, resultOK := backendGoNumericFixedResultCount(target.ir)
		if !resultOK || resultCount != 1 {
			continue
		}
		callerFields, fieldsOK := backendGoIndexFunctionCapturedFields(
			ir, records, target.ir, targetPlan.tables, closure,
		)
		if !fieldsOK {
			continue
		}
		fallbackByMetatable[record.root] = fallback{
			setter: setter, closure: closure, target: target, fields: callerFields,
		}
	}

	metatableByRecord := make(map[int]fallback)
	for _, metatable := range records.metatableByPC {
		recordIndex, recordOK := records.recordByRoot[metatable.table]
		candidate, fallbackOK := fallbackByMetatable[metatable.metatable]
		if !recordOK || !fallbackOK {
			continue
		}
		metatableByRecord[recordIndex] = candidate
	}

	for pc, field := range records.fieldsByPC {
		operation := &ir.ops[pc]
		if operation.op != opGetStringField || field.storage != backendGoRecordFieldArray ||
			field.index < 0 || field.index >= len(records.arrays) {
			continue
		}
		array := &records.arrays[field.index]
		if field.field < 0 || field.field >= len(array.fieldPresent) {
			continue
		}
		optional := false
		for _, present := range array.fieldPresent[field.field] {
			if !present {
				optional = true
				break
			}
		}
		if !optional {
			continue
		}
		var candidate fallback
		valid := len(array.records) != 0
		for recordOffset, recordIndex := range array.records {
			current, ok := metatableByRecord[recordIndex]
			if !ok || !backendGoScalarTableOperationDominates(ir, current.setter, operation) {
				valid = false
				break
			}
			if recordOffset == 0 {
				candidate = current
			} else if current.closure != candidate.closure || current.target.functionName != candidate.target.functionName {
				valid = false
				break
			}
		}
		key, keyOK := backendGoStringFieldName(ir, operation.access.constant)
		if !valid || !keyOK {
			continue
		}
		numericCaptures, capturesOK := backendGoIndexFunctionNumericCaptures(
			ir, candidate.target.ir, operation,
		)
		if !capturesOK {
			continue
		}
		plan.calls[operation.pc] = backendGoRecordIndexFunctionCall{
			target:          candidate.target,
			field:           field,
			key:             key,
			numericCaptures: numericCaptures,
			capturedFields:  candidate.fields,
		}
	}
	return plan
}

func backendGoCapturedTableFieldsEqual(
	left map[int32][]backendGoCapturedTableField,
	right map[int32][]backendGoCapturedTableField,
) bool {
	if len(left) != len(right) {
		return false
	}
	for upvalue, leftFields := range left {
		rightFields := right[upvalue]
		if len(leftFields) != len(rightFields) {
			return false
		}
		for field := range leftFields {
			if leftFields[field] != rightFields[field] {
				return false
			}
		}
	}
	return true
}

func backendGoIndexFunctionCapturedFields(
	caller *backendProtoIR,
	records backendGoRecordTablePlan,
	target *backendProtoIR,
	tables backendGoScalarTablePlan,
	closure *backendOperationIR,
) ([]backendGoRecordFieldOperation, bool) {
	rootUpvalue := make(map[backendValueID]int32)
	for upvalue, roots := range backendGoCapturedRecordUpvalues(target) {
		for _, root := range roots {
			rootUpvalue[root] = upvalue
		}
	}
	fields := make([]backendGoRecordFieldOperation, 0)
	for _, targetField := range tables.fields {
		if !tables.capturedExternalRoots[targetField.key.table] ||
			targetField.child != invalidBackendValueID || targetField.methodProto >= 0 {
			continue
		}
		upvalue, ok := rootUpvalue[targetField.key.table]
		if !ok || upvalue < 0 || int(upvalue) >= len(target.upvalues) {
			return nil, false
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
		fieldIndex, ok := records.records[recordIndex].fieldIndex[targetField.key.name]
		if !ok {
			return nil, false
		}
		fields = append(fields, backendGoRecordFieldOperation{
			storage: backendGoRecordFieldScratch, index: recordIndex, field: fieldIndex,
		})
	}
	return fields, len(fields) != 0
}

func backendGoIndexFunctionNumericCaptures(
	caller *backendProtoIR,
	target *backendProtoIR,
	operation *backendOperationIR,
) ([]backendValueID, bool) {
	tableUpvalues := backendGoCapturedRecordUpvalues(target)
	captures := make([]backendValueID, 0)
	for upvalue, descriptor := range target.upvalues {
		if len(tableUpvalues[int32(upvalue)]) != 0 {
			continue
		}
		if descriptor.local == 0 || descriptor.index >= uint32(caller.registers) {
			return nil, false
		}
		captured := backendValueBeforeOperation(caller, operation, int32(descriptor.index))
		if !caller.validBackendValue(captured) || caller.values[captured-1].tags != backendTagNumber {
			return nil, false
		}
		captures = append(captures, captured)
	}
	return captures, len(captures) != 0
}

func (plan backendGoRecordIndexFunctionPlan) call(
	operation *backendOperationIR,
) (backendGoRecordIndexFunctionCall, bool) {
	if operation == nil {
		return backendGoRecordIndexFunctionCall{}, false
	}
	call, ok := plan.calls[operation.pc]
	return call, ok
}

func (emitter *backendGoNumericEmitter) emitRecordIndexFunctionGet(
	operation *backendOperationIR,
	definition func(int32) (backendValueID, error),
) (bool, error) {
	call, ok := emitter.plan.indexFunctions.call(operation)
	if !ok {
		return false, nil
	}
	if call.field.storage != backendGoRecordFieldArray ||
		call.field.index < 0 || call.field.index >= len(emitter.plan.records.arrays) {
		return true, fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported __index field storage", operation.pc)
	}
	destination, err := definition(operation.a)
	if err != nil {
		return true, err
	}
	array := emitter.plan.records.arrays[call.field.index]
	if array.mutable {
		fmt.Fprintf(&emitter.body, "\tif v%d < 1 || v%d > float64(rn%d) {\n", call.field.ref, call.field.ref, call.field.index)
		fmt.Fprintf(&emitter.body, "\t\t%s\n", emitter.failureReturn())
		emitter.body.WriteString("\t}\n")
	} else if err := emitter.emitRecordRefGuard(call.field.ref, int(array.length)); err != nil {
		return true, err
	}
	index := fmt.Sprintf("int(v%d)-1", call.field.ref)
	fmt.Fprintf(&emitter.body, "\tif rap%d_%d[%s] {\n", call.field.index, call.field.field, index)
	fmt.Fprintf(&emitter.body, "\t\tv%d = ra%d_%d[%s]\n", destination, call.field.index, call.field.field, index)
	emitter.body.WriteString("\t} else {\n")
	fmt.Fprintf(&emitter.body, "\t\tv%d, ok%d = %s(", destination, operation.pc, call.target.functionName)
	wrote := false
	for _, capture := range call.numericCaptures {
		if wrote {
			emitter.body.WriteString(", ")
		}
		fmt.Fprintf(&emitter.body, "&v%d", capture)
		wrote = true
	}
	for _, field := range call.capturedFields {
		if field.storage != backendGoRecordFieldScratch {
			return true, fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported captured __index field", operation.pc)
		}
		if wrote {
			emitter.body.WriteString(", ")
		}
		fmt.Fprintf(&emitter.body, "&r%d_%d", field.index, field.field)
		wrote = true
	}
	if wrote {
		emitter.body.WriteString(", ")
	}
	fmt.Fprintf(&emitter.body, "uint32(%d))\n", call.key)
	fmt.Fprintf(&emitter.body, "\t\tif !ok%d {\n", operation.pc)
	emitter.emitReplayEntry(3)
	emitter.body.WriteString("\t\t}\n")
	emitter.body.WriteString("\t}\n")
	return true, nil
}
