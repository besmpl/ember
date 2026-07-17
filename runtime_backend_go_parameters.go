package ember

func backendGoNumericParameterTags(
	ir *backendProtoIR,
	fixedVarargCount int,
) ([]backendTagMask, bool) {
	argumentCount, ok := backendGoNumericArgumentCount(ir, fixedVarargCount)
	if !ok {
		return nil, false
	}
	tags := make([]backendTagMask, argumentCount)
	for argument := range tags {
		tags[argument] = backendTagNumber
	}
	if ir.params == 0 {
		return tags, true
	}

	origins := make([]int, len(ir.values))
	for index := range origins {
		origins[index] = -1
	}
	for parameter := 0; parameter < ir.params; parameter++ {
		id := ir.initial[parameter]
		if !ir.validBackendValue(id) {
			return nil, false
		}
		origins[id-1] = parameter
	}
	for iteration := 0; iteration <= len(ir.values); iteration++ {
		changed := false
		for valueIndex := range ir.values {
			value := &ir.values[valueIndex]
			origin := -1
			switch value.kind {
			case backendValueOperation:
				operation := &ir.ops[value.pc]
				if operation.op == opMove {
					source := backendOperationUse(operation, operation.b)
					if ir.validBackendValue(source) {
						origin = origins[source-1]
					}
				}
			case backendValuePhi:
				block := &ir.blocks[value.block]
				phi := block.phis[value.register]
				for inputIndex, input := range phi.inputs {
					if !ir.blocks[block.predecessors[inputIndex]].reachable || !ir.validBackendValue(input) {
						continue
					}
					candidate := origins[input-1]
					if candidate < 0 {
						origin = -1
						break
					}
					if origin >= 0 && origin != candidate {
						origin = -1
						break
					}
					origin = candidate
				}
			}
			if origin >= 0 && origins[valueIndex] != origin {
				origins[valueIndex] = origin
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opEqual && operation.op != opNotEqual {
			continue
		}
		left := backendOperationUse(operation, operation.b)
		right := backendOperationUse(operation, operation.c)
		if !ir.validBackendValue(left) || !ir.validBackendValue(right) {
			continue
		}
		if !backendGoNumericConstrainParameterTag(ir, origins, tags, left, right) ||
			!backendGoNumericConstrainParameterTag(ir, origins, tags, right, left) {
			return nil, false
		}
	}
	capturedRoots := make(map[backendValueID]bool)
	for _, roots := range backendGoCapturedRecordUpvalues(ir) {
		for _, root := range roots {
			capturedRoots[root] = true
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		var table, key backendValueID
		switch operation.op {
		case opGetIndex:
			table = backendOperationUse(operation, operation.b)
			key = backendOperationUse(operation, operation.c)
		case opSetIndex:
			table = backendOperationUse(operation, operation.a)
			key = backendOperationUse(operation, operation.b)
		default:
			continue
		}
		if capturedRoots[table] &&
			!backendGoNumericRequireParameterTag(ir, origins, tags, key, backendTagString) {
			return nil, false
		}
	}
	return tags, true
}

func backendGoNumericRequireParameterTag(
	ir *backendProtoIR,
	origins []int,
	tags []backendTagMask,
	value backendValueID,
	want backendTagMask,
) bool {
	if !ir.validBackendValue(value) {
		return false
	}
	parameter := origins[value-1]
	if parameter < 0 {
		return true
	}
	if parameter >= len(tags) ||
		tags[parameter] != backendTagNumber && tags[parameter] != want {
		return false
	}
	tags[parameter] = want
	return true
}

func backendGoNumericConstrainParameterTag(
	ir *backendProtoIR,
	origins []int,
	tags []backendTagMask,
	parameterValue backendValueID,
	constantValue backendValueID,
) bool {
	parameter := origins[parameterValue-1]
	if parameter < 0 {
		return true
	}
	constant := &ir.values[constantValue-1]
	if constant.kind != backendValueOperation {
		return true
	}
	operation := &ir.ops[constant.pc]
	if operation.op != opLoadConst || operation.b < 0 || int(operation.b) >= len(ir.constants) {
		return true
	}
	want := backendTagForValueKind(ir.constants[operation.b].kind)
	if want != backendTagNumber && want != backendTagBool && want != backendTagString {
		return false
	}
	if tags[parameter] != backendTagNumber && tags[parameter] != want {
		return false
	}
	tags[parameter] = want
	return true
}
