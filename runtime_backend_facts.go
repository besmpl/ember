package ember

import "sort"

type backendValueFacts struct {
	tags          backendTagMask
	object        backendObjectKind
	fromVararg    bool
	origins       []backendValueID
	targetProtos  []int32
	targetUnknown bool
}

func analyzeBackendFacts(ir *backendProtoIR) {
	if ir == nil {
		return
	}
	for valueIndex := range ir.values {
		value := &ir.values[valueIndex]
		value.tags = 0
		value.representation = backendRepresentationGeneric
		value.object = backendObjectNone
		value.fromVararg = false
		value.escapes = false
		value.origins = nil
		value.targetProtos = nil
		value.targetUnknown = false
	}
	for {
		changed := false
		for valueIndex := range ir.values {
			value := &ir.values[valueIndex]
			facts := ir.backendExpectedValueFacts(value)
			if mergeBackendValueFacts(value, facts) {
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	unresolved := false
	for valueIndex := range ir.values {
		value := &ir.values[valueIndex]
		if value.tags != 0 {
			continue
		}
		value.tags = backendTagAny
		value.targetUnknown = true
		unresolved = true
	}
	if unresolved {
		for {
			changed := false
			for valueIndex := range ir.values {
				if mergeBackendValueFacts(&ir.values[valueIndex], ir.backendExpectedValueFacts(&ir.values[valueIndex])) {
					changed = true
				}
			}
			if !changed {
				break
			}
		}
	}
	for valueIndex := range ir.values {
		ir.values[valueIndex].representation = backendRepresentationForTags(ir.values[valueIndex].tags)
	}
	escaping := backendEscapingValues(ir)
	for valueIndex := range ir.values {
		value := &ir.values[valueIndex]
		if escaping[valueIndex] {
			value.escapes = true
			continue
		}
		for _, origin := range value.origins {
			if origin != invalidBackendValueID && int(origin) <= len(escaping) && escaping[origin-1] {
				value.escapes = true
				break
			}
		}
	}
	for pc := range ir.ops {
		ir.ops[pc].call = ir.backendCallClassification(&ir.ops[pc])
		ir.ops[pc].access = backendAccessClassification(&ir.ops[pc])
	}
}

func (ir *backendProtoIR) backendExpectedValueFacts(value *backendValueIR) backendValueFacts {
	if ir == nil || value == nil {
		return backendValueFacts{}
	}
	switch value.kind {
	case backendValueUndef:
		return backendValueFacts{tags: backendTagNil}
	case backendValueParameter:
		return backendValueFacts{tags: backendTagAny, targetUnknown: true}
	case backendValuePhi:
		block := &ir.blocks[value.block]
		if int(value.register) >= len(block.phis) {
			return backendValueFacts{}
		}
		var facts backendValueFacts
		for _, input := range block.phis[value.register].inputs {
			facts = mergeBackendFacts(facts, ir.backendFactsForID(input))
		}
		return facts
	case backendValueOperation:
		return ir.backendOperationValueFacts(value)
	default:
		return backendValueFacts{}
	}
}

func (ir *backendProtoIR) backendOperationValueFacts(value *backendValueIR) backendValueFacts {
	if value.pc < 0 || int(value.pc) >= len(ir.ops) {
		return backendValueFacts{}
	}
	operation := &ir.ops[value.pc]
	switch operation.op {
	case opLoadConst:
		if operation.b < 0 || int(operation.b) >= len(ir.constants) {
			return backendValueFacts{}
		}
		return backendValueFacts{tags: backendTagForValueKind(ir.constants[operation.b].kind)}
	case opMove:
		return ir.backendFactsForID(backendOperationUse(operation, operation.b))
	case opNewTable:
		return backendValueFacts{
			tags:    backendTagTable,
			object:  backendObjectTable,
			origins: []backendValueID{value.id},
		}
	case opClosure:
		return backendValueFacts{
			tags:         backendTagFunction,
			object:       backendObjectClosure,
			origins:      []backendValueID{value.id},
			targetProtos: []int32{operation.targetProto},
		}
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opNeg,
		opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
		opNumericForLoop:
		return backendValueFacts{tags: backendTagNumber}
	case opConcat, opConcatChain:
		return backendValueFacts{
			tags:    backendTagString,
			object:  backendObjectString,
			origins: []backendValueID{value.id},
		}
	case opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		return backendValueFacts{tags: backendTagBool}
	case opVararg:
		return backendValueFacts{
			tags:          backendTagAny,
			fromVararg:    true,
			targetUnknown: true,
		}
	case opFastCall:
		switch nativeFuncID(operation.nativeID) {
		case nativeFuncMathMin, nativeFuncRawLen:
			return backendValueFacts{tags: backendTagNumber}
		case nativeFuncToString:
			return backendValueFacts{tags: backendTagString, object: backendObjectString}
		default:
			return backendValueFacts{tags: backendTagAny, targetUnknown: true}
		}
	default:
		return backendValueFacts{tags: backendTagAny, targetUnknown: true}
	}
}

func (ir *backendProtoIR) backendFactsForID(id backendValueID) backendValueFacts {
	if !ir.validBackendValue(id) {
		return backendValueFacts{}
	}
	value := &ir.values[id-1]
	return backendValueFacts{
		tags:          value.tags,
		object:        value.object,
		fromVararg:    value.fromVararg,
		origins:       value.origins,
		targetProtos:  value.targetProtos,
		targetUnknown: value.targetUnknown,
	}
}

func mergeBackendValueFacts(value *backendValueIR, facts backendValueFacts) bool {
	changed := false
	tags := value.tags | facts.tags
	if tags != value.tags {
		value.tags = tags
		changed = true
	}
	object := mergeBackendObjectKind(value.object, facts.object)
	if object != value.object {
		value.object = object
		changed = true
	}
	if facts.fromVararg && !value.fromVararg {
		value.fromVararg = true
		changed = true
	}
	origins := mergeBackendValueIDs(value.origins, facts.origins)
	if !backendValueIDsEqual(origins, value.origins) {
		value.origins = origins
		changed = true
	}
	targets := mergeBackendInt32s(value.targetProtos, facts.targetProtos)
	if !backendInt32sEqual(targets, value.targetProtos) {
		value.targetProtos = targets
		changed = true
	}
	if facts.targetUnknown && !value.targetUnknown {
		value.targetUnknown = true
		changed = true
	}
	return changed
}

func mergeBackendFacts(left, right backendValueFacts) backendValueFacts {
	return backendValueFacts{
		tags:          left.tags | right.tags,
		object:        mergeBackendObjectKind(left.object, right.object),
		fromVararg:    left.fromVararg || right.fromVararg,
		origins:       mergeBackendValueIDs(left.origins, right.origins),
		targetProtos:  mergeBackendInt32s(left.targetProtos, right.targetProtos),
		targetUnknown: left.targetUnknown || right.targetUnknown,
	}
}

func mergeBackendObjectKind(left, right backendObjectKind) backendObjectKind {
	switch {
	case left == right:
		return left
	case left == backendObjectNone:
		return right
	case right == backendObjectNone:
		return left
	default:
		return backendObjectMixed
	}
}

func mergeBackendValueIDs(left, right []backendValueID) []backendValueID {
	result := append([]backendValueID(nil), left...)
	result = append(result, right...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	if len(result) < 2 {
		return result
	}
	output := result[:1]
	for _, value := range result[1:] {
		if value != output[len(output)-1] {
			output = append(output, value)
		}
	}
	return output
}

func mergeBackendInt32s(left, right []int32) []int32 {
	result := append([]int32(nil), left...)
	result = append(result, right...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return compactBackendIDs(result)
}

func backendInt32sEqual(left, right []int32) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func backendTagForValueKind(kind ValueKind) backendTagMask {
	switch kind {
	case NilKind:
		return backendTagNil
	case BoolKind:
		return backendTagBool
	case NumberKind:
		return backendTagNumber
	case StringKind:
		return backendTagString
	case TableKind:
		return backendTagTable
	case UserDataKind:
		return backendTagUserData
	case FunctionKind:
		return backendTagFunction
	case HostFuncKind:
		return backendTagHostFunction
	default:
		return backendTagAny
	}
}

func backendRepresentationForTags(tags backendTagMask) backendRepresentation {
	switch tags {
	case backendTagNil:
		return backendRepresentationNil
	case backendTagBool:
		return backendRepresentationBool
	case backendTagNumber:
		return backendRepresentationNumber
	case backendTagString:
		return backendRepresentationString
	case backendTagTable:
		return backendRepresentationTable
	case backendTagFunction:
		return backendRepresentationFunction
	default:
		return backendRepresentationGeneric
	}
}

func backendOperationUse(operation *backendOperationIR, register int32) backendValueID {
	if operation == nil {
		return invalidBackendValueID
	}
	for _, use := range operation.uses {
		if use.register == register {
			return use.value
		}
	}
	return invalidBackendValueID
}

func backendOperationDefinition(operation *backendOperationIR, register int32) backendValueID {
	if operation == nil {
		return invalidBackendValueID
	}
	for _, definition := range operation.defs {
		if definition.register == register {
			return definition.value
		}
	}
	return invalidBackendValueID
}

func (ir *backendProtoIR) backendCallClassification(operation *backendOperationIR) backendCallIR {
	result := backendCallIR{targetProto: -1, nativeID: -1}
	switch operation.op {
	case opFastCall:
		result.kind = backendCallDirectNative
		result.nativeID = operation.nativeID
		return result
	case opCallUpvalueOne, opCallMethodOne:
		result.kind = backendCallGuarded
		return result
	case opCall, opCallOne, opCallLocalOne:
		target := ir.backendFactsForID(backendOperationUse(operation, operation.b))
		if target.tags == backendTagFunction && !target.targetUnknown && len(target.targetProtos) == 1 {
			result.kind = backendCallDirectProto
			result.targetProto = target.targetProtos[0]
			return result
		}
		if target.tags != backendTagAny && target.tags&(backendTagFunction|backendTagHostFunction|backendTagTable) != 0 {
			result.kind = backendCallGuarded
			return result
		}
		result.kind = backendCallDynamic
		return result
	default:
		return result
	}
}

func backendAccessClassification(operation *backendOperationIR) backendAccessIR {
	result := backendAccessIR{constant: -1, globalIndex: -1}
	switch operation.op {
	case opLoadGlobal, opSetGlobal:
		result.kind = backendAccessGlobal
		result.globalIndex = operation.globalIndex
	case opSetField, opSetStringField, opSetStringFieldIndex:
		result.kind = backendAccessStaticProperty
		result.constant = operation.b
	case opGetStringField, opGetStringFieldIndex:
		result.kind = backendAccessStaticProperty
		result.constant = operation.c
	case opCallMethodOne:
		result.kind = backendAccessStaticProperty
		result.constant = operation.c
	case opGetIndex, opSetIndex:
		result.kind = backendAccessDynamicIndex
	case opPrepareIter, opArrayNext, opArrayNextJump2:
		result.kind = backendAccessArrayIteration
	case opJumpIfTableHasMetatable:
		result.kind = backendAccessMetatableGuard
	}
	return result
}

func backendEscapingValues(ir *backendProtoIR) []bool {
	escaping := make([]bool, len(ir.values))
	mark := func(id backendValueID) {
		if !ir.validBackendValue(id) {
			return
		}
		value := &ir.values[id-1]
		if value.object == backendObjectNone && !value.fromVararg && len(value.origins) == 0 {
			return
		}
		escaping[id-1] = true
		for _, origin := range value.origins {
			if ir.validBackendValue(origin) {
				escaping[origin-1] = true
			}
		}
	}
	markRegister := func(operation *backendOperationIR, register int32) {
		mark(backendOperationUse(operation, register))
	}
	markRange := func(operation *backendOperationIR, start, count int32) {
		if start < 0 {
			return
		}
		end := int32(ir.registers)
		if count >= 0 && start+count < end {
			end = start + count
		}
		for register := start; register < end; register++ {
			markRegister(operation, register)
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		switch operation.op {
		case opSetGlobal:
			markRegister(operation, operation.b)
		case opSetUpvalue:
			markRegister(operation, operation.b)
		case opSetField, opSetStringField:
			markRegister(operation, operation.c)
		case opSetStringFieldIndex:
			markRegister(operation, operation.d)
		case opSetIndex:
			markRegister(operation, operation.b)
			markRegister(operation, operation.c)
		case opFastCall:
			markRange(operation, operation.a, operation.c)
		case opCall, opCallOne, opCallLocalOne, opCallUpvalueOne:
			markRange(operation, operation.callArgStart, operation.callArgCount)
		case opCallMethodOne:
			markRegister(operation, operation.b)
			markRange(operation, operation.callArgStart, operation.callArgCount)
		case opReturnOne, opReturn:
			for _, use := range operation.uses {
				mark(use.value)
			}
		case opClosure:
			for register := 0; register < ir.registers; register++ {
				if operation.liveBefore.has(register) {
					mark(backendValueBeforeOperation(ir, operation, int32(register)))
				}
			}
		}
	}
	return escaping
}

func backendValueBeforeOperation(ir *backendProtoIR, operation *backendOperationIR, register int32) backendValueID {
	if ir == nil || operation == nil || register < 0 || int(register) >= ir.registers {
		return invalidBackendValueID
	}
	block := &ir.blocks[operation.block]
	current := block.entryValues[register]
	for pc := block.first; pc < operation.pc; pc++ {
		for _, definition := range ir.ops[pc].defs {
			if definition.register == register {
				current = definition.value
			}
		}
	}
	return current
}
