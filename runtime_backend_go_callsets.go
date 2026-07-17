package ember

type backendGoFiniteCallVariant struct {
	key              machineStringID
	target           backendGoNumericTarget
	targetFieldNames []machineStringID
	callerFields     []backendGoRecordFieldOperation
}

type backendGoFiniteCall struct {
	selectorPC   int32
	key          backendValueID
	receiverRoot backendValueID
	variants     []backendGoFiniteCallVariant
}

type backendGoFiniteCallSetPlan struct {
	calls         map[int32]backendGoFiniteCall
	selectorPCs   map[int32]bool
	setterPCs     map[int32]bool
	excludedRoots map[backendValueID]bool
	scalarValues  []bool
}

func discoverBackendGoFiniteCallSets(
	ir *backendProtoIR,
	records backendGoRecordTablePlan,
	options backendGoNumericOptions,
) backendGoFiniteCallSetPlan {
	plan := backendGoFiniteCallSetPlan{
		calls:         make(map[int32]backendGoFiniteCall),
		selectorPCs:   make(map[int32]bool),
		setterPCs:     make(map[int32]bool),
		excludedRoots: make(map[backendValueID]bool),
	}
	if ir == nil {
		return plan
	}
	plan.scalarValues = make([]bool, len(ir.values))
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opCallOne || operation.callArgCount < 2 || operation.callResults != 1 {
			continue
		}
		function := backendOperationUse(operation, operation.b)
		if !ir.validBackendValue(function) {
			continue
		}
		functionValue := &ir.values[function-1]
		if functionValue.kind != backendValueOperation || functionValue.pc < 0 || int(functionValue.pc) >= len(ir.ops) {
			continue
		}
		selector := &ir.ops[functionValue.pc]
		if selector.op != opGetIndex || len(selector.defs) != 1 || selector.defs[0].value != function {
			continue
		}
		dynamic, ok := records.dynamicGetByPC[selector.pc]
		if !ok || dynamic.record < 0 || dynamic.record >= len(records.records) {
			continue
		}
		handler := &records.records[dynamic.record]
		if handler.storedAtPC >= 0 || !records.recordInitializedBefore(ir, dynamic.record, operation) ||
			len(handler.fieldNames) == 0 || len(handler.fieldNames) != len(handler.setByPC) ||
			len(handler.fieldNames) != len(handler.writesByPC) {
			continue
		}
		receiver := backendOperationUse(operation, operation.callArgStart)
		receiverRoot := records.root(receiver)
		receiverRecord, ok := records.recordByRoot[receiverRoot]
		if !ok || receiverRecord < 0 || receiverRecord >= len(records.records) ||
			records.records[receiverRecord].storedAtPC >= 0 {
			continue
		}

		variants := make([]backendGoFiniteCallVariant, 0, len(handler.fieldNames))
		closureValues := make([]backendValueID, 0, len(handler.fieldNames))
		setterPCs := make([]int32, 0, len(handler.fieldNames))
		valid := true
		for field, name := range handler.fieldNames {
			closure := handler.fieldValues[field]
			protoID, closureOK := backendGoScalarMethodClosure(ir, closure)
			if !closureOK || protoID < 0 || int(protoID) >= len(options.directTargets) {
				valid = false
				break
			}
			target := options.directTargets[protoID]
			if target.ir == nil || target.functionName == "" || !target.receiverTable ||
				target.selfRecursive || target.ir.variadic || len(target.ir.upvalues) != 0 ||
				target.ir.params != int(operation.callArgCount) {
				valid = false
				break
			}
			resultCount, resultOK := backendGoNumericFixedResultCountFor(target.ir, target.fixedVarargCount)
			if !resultOK || resultCount != 1 {
				valid = false
				break
			}
			targetOptions := backendGoNumericOptions{
				functionName:     target.functionName,
				directTargets:    options.directTargets,
				fixedVarargCount: target.fixedVarargCount,
				receiverTable:    true,
			}
			targetPlan, targetErr := buildBackendGoNumericPlan(target.ir, targetOptions)
			if targetErr != nil || targetPlan.tables.externalRoot == invalidBackendValueID {
				valid = false
				break
			}
			resultTypes, resultErr := backendGoNumericResultTypes(target.ir, targetPlan, targetOptions)
			if resultErr != nil || len(resultTypes) != 1 || resultTypes[0] != "float64" {
				valid = false
				break
			}
			fieldNames := make([]machineStringID, 0, len(targetPlan.tables.fields))
			for _, targetField := range targetPlan.tables.fields {
				if targetField.key.table != targetPlan.tables.externalRoot ||
					targetField.tags != backendTagNumber ||
					targetField.child != invalidBackendValueID || targetField.methodProto >= 0 {
					valid = false
					break
				}
				if _, exists := records.records[receiverRecord].fieldIndex[targetField.key.name]; !exists {
					valid = false
					break
				}
				fieldNames = append(fieldNames, targetField.key.name)
			}
			if !valid || len(fieldNames) == 0 {
				break
			}
			variants = append(variants, backendGoFiniteCallVariant{
				key:              name,
				target:           target,
				targetFieldNames: fieldNames,
			})
			closureValues = append(closureValues, closure)
			for setterPC, setterField := range handler.setByPC {
				if setterField == field {
					setterPCs = append(setterPCs, setterPC)
				}
			}
		}
		if !valid || len(variants) != len(handler.fieldNames) {
			continue
		}
		if !backendGoFiniteCallSetUsesAreExact(
			ir,
			records,
			handler.root,
			selector.pc,
			operation.pc,
			closureValues,
			setterPCs,
			function,
		) {
			continue
		}
		plan.calls[operation.pc] = backendGoFiniteCall{
			selectorPC:   selector.pc,
			key:          dynamic.key,
			receiverRoot: receiverRoot,
			variants:     variants,
		}
		plan.selectorPCs[selector.pc] = true
		plan.excludedRoots[handler.root] = true
		plan.scalarValues[function-1] = true
		for _, closure := range closureValues {
			plan.scalarValues[closure-1] = true
		}
		for _, setterPC := range setterPCs {
			plan.setterPCs[setterPC] = true
		}
		for valueIndex := range records.roots {
			if records.root(backendValueID(valueIndex+1)) == handler.root {
				plan.scalarValues[valueIndex] = true
			}
		}
	}
	return plan
}

func backendGoFiniteCallSetUsesAreExact(
	ir *backendProtoIR,
	records backendGoRecordTablePlan,
	handlerRoot backendValueID,
	selectorPC int32,
	callPC int32,
	closures []backendValueID,
	setterPCs []int32,
	selectorValue backendValueID,
) bool {
	if ir == nil || len(closures) == 0 || len(closures) != len(setterPCs) {
		return false
	}
	setters := make(map[int32]bool, len(setterPCs))
	closureSetters := make(map[backendValueID]int32, len(closures))
	for index, pc := range setterPCs {
		setters[pc] = true
		closureSetters[closures[index]] = pc
	}
	selectorUses := 0
	closureUses := make(map[backendValueID]int, len(closures))
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		for _, use := range operation.uses {
			if records.root(use.value) == handlerRoot {
				switch {
				case operation.op == opMove && use.register == operation.b:
				case operation.op == opSetStringField && setters[operation.pc] && use.register == operation.a:
				case operation.op == opGetIndex && operation.pc == selectorPC && use.register == operation.b:
				default:
					return false
				}
			}
			if setterPC, closure := closureSetters[use.value]; closure {
				if operation.op != opSetStringField || operation.pc != setterPC || use.register != operation.c {
					return false
				}
				closureUses[use.value]++
			}
			if use.value == selectorValue {
				if operation.op != opCallOne || operation.pc != callPC || use.register != operation.b {
					return false
				}
				selectorUses++
			}
		}
	}
	if selectorUses != 1 {
		return false
	}
	for _, closure := range closures {
		if closureUses[closure] != 1 {
			return false
		}
	}
	return true
}

func finalizeBackendGoFiniteCallSets(
	plan backendGoFiniteCallSetPlan,
	records backendGoRecordTablePlan,
) (backendGoFiniteCallSetPlan, bool) {
	for pc, call := range plan.calls {
		record, ok := records.recordByRoot[call.receiverRoot]
		if !ok || record < 0 || record >= len(records.records) {
			return backendGoFiniteCallSetPlan{}, false
		}
		for variantIndex := range call.variants {
			variant := &call.variants[variantIndex]
			variant.callerFields = make([]backendGoRecordFieldOperation, 0, len(variant.targetFieldNames))
			for _, name := range variant.targetFieldNames {
				field, exists := records.records[record].fieldIndex[name]
				if !exists {
					return backendGoFiniteCallSetPlan{}, false
				}
				variant.callerFields = append(variant.callerFields, backendGoRecordFieldOperation{
					storage: backendGoRecordFieldScratch,
					index:   record,
					field:   field,
				})
			}
		}
		plan.calls[pc] = call
	}
	return plan, true
}

func (plan backendGoFiniteCallSetPlan) call(operation *backendOperationIR) (backendGoFiniteCall, bool) {
	if operation == nil {
		return backendGoFiniteCall{}, false
	}
	call, ok := plan.calls[operation.pc]
	return call, ok
}

func (plan backendGoFiniteCallSetPlan) hasCall(operation *backendOperationIR) bool {
	_, ok := plan.call(operation)
	return ok
}

func (plan backendGoFiniteCallSetPlan) selector(operation *backendOperationIR) bool {
	return operation != nil && plan.selectorPCs[operation.pc]
}

func (plan backendGoFiniteCallSetPlan) setter(operation *backendOperationIR) bool {
	return operation != nil && plan.setterPCs[operation.pc]
}
