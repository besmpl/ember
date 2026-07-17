package ember

import (
	"fmt"
	"math"
)

type backendGoScalarFieldKey struct {
	table backendValueID
	name  machineStringID
}

type backendGoScalarField struct {
	key         backendGoScalarFieldKey
	isIndex     bool
	tags        backendTagMask
	child       backendValueID
	methodProto int32
}

type backendGoScalarMetatable struct {
	table     backendValueID
	metatable backendValueID
	fallback  backendValueID
}

type backendGoCapturedTableField struct {
	name machineStringID
	tags backendTagMask
}

type backendGoScalarDynamicExternalGet struct {
	table backendValueID
	tags  backendTagMask
}

type backendGoScalarArray struct {
	table       backendValueID
	tags        backendTagMask
	length      uint32
	capacity    uint32
	appendBound uint32
	mutable     bool
	present     map[uint32]bool
}

type backendGoScalarTablePlan struct {
	roots                  []backendValueID
	fields                 []backendGoScalarField
	index                  map[backendGoScalarFieldKey]int
	arrays                 []backendGoScalarArray
	arrayIndex             map[backendValueID]int
	arrayGetByPC           map[int32]int
	indexFallback          map[backendValueID]backendValueID
	metatableByPC          map[int32]backendGoScalarMetatable
	dynamicExternalGetByPC map[int32]backendGoScalarDynamicExternalGet
	dynamicExternalSetByPC map[int32]backendGoScalarDynamicExternalGet
	iteratorValues         []bool
	iteratorByPC           map[int32]int
	externalRoot           backendValueID
	externalRoots          []backendValueID
	tableUpvalues          []bool
	capturedExternalRoots  map[backendValueID]bool
	partial                bool
}

func analyzeBackendGoScalarTables(ir *backendProtoIR, receiverTable bool) (backendGoScalarTablePlan, error) {
	return analyzeBackendGoScalarTablesExcludingCount(
		ir,
		backendGoNumericReceiverTableCount(receiverTable, 0),
		nil,
	)
}

func analyzeBackendGoScalarTablesExcluding(
	ir *backendProtoIR,
	receiverTable bool,
	excluded map[backendValueID]bool,
) (backendGoScalarTablePlan, error) {
	return analyzeBackendGoScalarTablesExcludingCount(
		ir,
		backendGoNumericReceiverTableCount(receiverTable, 0),
		excluded,
	)
}

func analyzeBackendGoScalarTablesExcludingCount(
	ir *backendProtoIR,
	receiverTables int,
	excluded map[backendValueID]bool,
) (backendGoScalarTablePlan, error) {
	return analyzeBackendGoScalarTablesExcludingCountWithFields(ir, receiverTables, excluded, nil)
}

func analyzeBackendGoScalarTablesExcludingCountWithFields(
	ir *backendProtoIR,
	receiverTables int,
	excluded map[backendValueID]bool,
	capturedFields map[int32][]backendGoCapturedTableField,
) (backendGoScalarTablePlan, error) {
	if ir == nil {
		return backendGoScalarTablePlan{}, nil
	}
	plan := backendGoScalarTablePlan{
		roots:                  make([]backendValueID, len(ir.values)),
		index:                  make(map[backendGoScalarFieldKey]int),
		arrayIndex:             make(map[backendValueID]int),
		arrayGetByPC:           make(map[int32]int),
		indexFallback:          make(map[backendValueID]backendValueID),
		metatableByPC:          make(map[int32]backendGoScalarMetatable),
		dynamicExternalGetByPC: make(map[int32]backendGoScalarDynamicExternalGet),
		dynamicExternalSetByPC: make(map[int32]backendGoScalarDynamicExternalGet),
		iteratorByPC:           make(map[int32]int),
		externalRoot:           invalidBackendValueID,
		tableUpvalues:          make([]bool, len(ir.upvalues)),
		capturedExternalRoots:  make(map[backendValueID]bool),
		partial:                len(excluded) != 0,
	}
	for valueIndex := range ir.values {
		value := &ir.values[valueIndex]
		if value.object != backendObjectTable || len(value.origins) != 1 {
			continue
		}
		root := value.origins[0]
		if !backendGoNewTableRoot(ir, root) || excluded[root] {
			continue
		}
		plan.roots[valueIndex] = root
	}
	if receiverTables > 0 {
		if receiverTables > ir.params || receiverTables > len(ir.initial) {
			return backendGoScalarTablePlan{}, nil
		}
		plan.externalRoot = ir.initial[0]
		for parameter := 0; parameter < receiverTables; parameter++ {
			root := ir.initial[parameter]
			plan.externalRoots = append(plan.externalRoots, root)
			plan.roots[root-1] = root
		}
	}
	capturedUpvalues := backendGoCapturedRecordUpvalues(ir)
	for upvalue := 0; upvalue < len(ir.upvalues); upvalue++ {
		capturedRoots := capturedUpvalues[int32(upvalue)]
		if len(capturedRoots) == 0 {
			continue
		}
		root := capturedRoots[0]
		plan.tableUpvalues[upvalue] = true
		if plan.externalRoot == invalidBackendValueID {
			plan.externalRoot = root
		}
		plan.externalRoots = append(plan.externalRoots, root)
		plan.capturedExternalRoots[root] = true
		for _, capturedRoot := range capturedRoots {
			plan.roots[capturedRoot-1] = root
		}
		for _, capturedField := range capturedFields[int32(upvalue)] {
			if capturedField.name == invalidMachineStringID ||
				capturedField.tags == 0 ||
				capturedField.tags&^(backendTagNumber|backendTagBool|backendTagString) != 0 {
				return backendGoScalarTablePlan{}, nil
			}
			key := backendGoScalarFieldKey{table: root, name: capturedField.name}
			if fieldIndex, exists := plan.index[key]; exists {
				if plan.fields[fieldIndex].tags != capturedField.tags {
					return backendGoScalarTablePlan{}, nil
				}
				continue
			}
			plan.index[key] = len(plan.fields)
			plan.fields = append(plan.fields, backendGoScalarField{
				key: key, tags: capturedField.tags, methodProto: -1,
			})
		}
	}
	setRoot := func(id, root backendValueID) (bool, bool) {
		if !ir.validBackendValue(id) {
			return false, false
		}
		if root == invalidBackendValueID {
			return false, true
		}
		current := plan.roots[id-1]
		if current == invalidBackendValueID {
			plan.roots[id-1] = root
			return true, true
		}
		return false, current == root
	}
	ensureArray := func(table backendValueID) (int, *backendGoScalarArray) {
		arrayIndex, exists := plan.arrayIndex[table]
		if !exists {
			arrayIndex = len(plan.arrays)
			plan.arrayIndex[table] = arrayIndex
			plan.arrays = append(plan.arrays, backendGoScalarArray{
				table:   table,
				present: make(map[uint32]bool),
			})
		}
		return arrayIndex, &plan.arrays[arrayIndex]
	}

	for iteration := 0; iteration <= len(ir.values)+len(ir.ops); iteration++ {
		changed := false
		for valueIndex := range ir.values {
			value := &ir.values[valueIndex]
			if value.kind != backendValuePhi {
				continue
			}
			block := &ir.blocks[value.block]
			phi := block.phis[value.register]
			root := invalidBackendValueID
			ready := true
			for inputIndex, input := range phi.inputs {
				if !ir.blocks[block.predecessors[inputIndex]].reachable {
					continue
				}
				if input == value.id {
					continue
				}
				candidate := plan.root(input)
				if candidate == invalidBackendValueID {
					ready = false
					break
				}
				if root != invalidBackendValueID && root != candidate {
					ready = false
					break
				}
				root = candidate
			}
			if !ready {
				continue
			}
			if updated, ok := setRoot(value.id, root); !ok {
				return backendGoScalarTablePlan{}, nil
			} else if updated {
				changed = true
			}
		}
		for pc := range ir.ops {
			operation := &ir.ops[pc]
			switch operation.op {
			case opMove:
				root := plan.root(backendOperationUse(operation, operation.b))
				for _, definition := range operation.defs {
					if updated, ok := setRoot(definition.value, root); !ok {
						return backendGoScalarTablePlan{}, nil
					} else if updated {
						changed = true
					}
				}
			case opPrepareIter:
				root := plan.root(backendOperationUse(operation, operation.a))
				if root == invalidBackendValueID {
					continue
				}
				for _, definition := range operation.defs {
					if definition.register != operation.a {
						continue
					}
					if updated, ok := setRoot(definition.value, root); !ok {
						return backendGoScalarTablePlan{}, nil
					} else if updated {
						changed = true
					}
				}
			case opSetField:
				table := plan.root(backendOperationUse(operation, operation.a))
				index, ok := backendGoArrayIndexConstant(ir, operation.access.constant)
				source := backendOperationUse(operation, operation.c)
				if table == invalidBackendValueID || !ok || !ir.validBackendValue(source) {
					continue
				}
				if plan.root(source) != invalidBackendValueID {
					return backendGoScalarTablePlan{}, nil
				}
				tags := ir.values[source-1].tags
				if tags == 0 || tags&^(backendTagNumber|backendTagBool|backendTagString) != 0 {
					return backendGoScalarTablePlan{}, nil
				}
				_, array := ensureArray(table)
				array.tags |= tags
				array.present[index] = true
				if index > array.length {
					array.length = index
				}
			case opSetStringField:
				table := plan.root(backendOperationUse(operation, operation.a))
				if table == invalidBackendValueID {
					continue
				}
				name, ok := backendGoStringFieldName(ir, operation.access.constant)
				if !ok {
					return backendGoScalarTablePlan{}, nil
				}
				isIndex := backendGoStringFieldIsIndex(ir, operation.access.constant)
				source := backendOperationUse(operation, operation.c)
				if !ir.validBackendValue(source) {
					return backendGoScalarTablePlan{}, nil
				}
				key := backendGoScalarFieldKey{table: table, name: name}
				fieldIndex, exists := plan.index[key]
				if !exists {
					fieldIndex = len(plan.fields)
					plan.index[key] = fieldIndex
					plan.fields = append(plan.fields, backendGoScalarField{
						key: key, isIndex: isIndex, methodProto: -1,
					})
				}
				field := &plan.fields[fieldIndex]
				child := plan.root(source)
				if child != invalidBackendValueID {
					if field.tags != 0 || field.child != invalidBackendValueID && field.child != child {
						return backendGoScalarTablePlan{}, nil
					}
					if field.child != child {
						field.child = child
						changed = true
					}
					continue
				}
				if proto, ok := backendGoScalarMethodClosure(ir, source); ok {
					if field.tags != 0 ||
						field.child != invalidBackendValueID ||
						field.methodProto >= 0 && field.methodProto != proto {
						return backendGoScalarTablePlan{}, nil
					}
					field.methodProto = proto
					continue
				}
				tags := ir.values[source-1].tags
				if tags == 0 ||
					tags&^(backendTagNumber|backendTagBool|backendTagString) != 0 ||
					field.child != invalidBackendValueID ||
					field.methodProto >= 0 {
					return backendGoScalarTablePlan{}, nil
				}
				next := field.tags | tags
				if next != field.tags {
					field.tags = next
					changed = true
				}
			case opGetStringField:
				table := plan.root(backendOperationUse(operation, operation.b))
				if table == invalidBackendValueID {
					continue
				}
				name, ok := backendGoStringFieldName(ir, operation.access.constant)
				if !ok {
					return backendGoScalarTablePlan{}, nil
				}
				key := backendGoScalarFieldKey{table: table, name: name}
				fieldIndex, exists := plan.index[key]
				if !exists {
					if receiverTables == 0 || !plan.isExternalRoot(table) || len(operation.defs) != 1 {
						continue
					}
					tags := backendGoScalarRequiredTags(ir, operation.defs[0].value, nil)
					if tags == 0 || tags&^(backendTagNumber|backendTagBool|backendTagString) != 0 {
						return backendGoScalarTablePlan{}, nil
					}
					fieldIndex = len(plan.fields)
					plan.index[key] = fieldIndex
					plan.fields = append(plan.fields, backendGoScalarField{
						key: key, isIndex: backendGoStringFieldIsIndex(ir, operation.access.constant),
						tags: tags, methodProto: -1,
					})
					changed = true
				}
				child := plan.fields[fieldIndex].child
				if child == invalidBackendValueID {
					continue
				}
				for _, definition := range operation.defs {
					if plan.roots[definition.value-1] == invalidBackendValueID {
						plan.roots[definition.value-1] = child
						changed = true
					} else if plan.roots[definition.value-1] != child {
						return backendGoScalarTablePlan{}, nil
					}
				}
			case opCallMethodOne:
				root := plan.root(backendOperationUse(operation, operation.b))
				if root == invalidBackendValueID {
					continue
				}
				for _, definition := range operation.defs {
					if definition.register != operation.callArgStart {
						continue
					}
					if updated, ok := setRoot(definition.value, root); !ok {
						return backendGoScalarTablePlan{}, nil
					} else if updated {
						changed = true
					}
				}
			case opFastCall:
				if nativeFuncID(operation.nativeID) != nativeFuncSetMetatable ||
					operation.c != 2 || operation.d != 1 {
					continue
				}
				table := plan.root(backendOperationUse(operation, operation.a))
				metatable := plan.root(backendOperationUse(operation, operation.a+1))
				if table == invalidBackendValueID || metatable == invalidBackendValueID {
					continue
				}
				fallback := invalidBackendValueID
				fieldCount := 0
				for _, field := range plan.fields {
					if field.key.table != metatable {
						continue
					}
					fieldCount++
					if field.isIndex && field.tags == 0 {
						fallback = field.child
					}
				}
				_, metatableHasArray := plan.arrayIndex[metatable]
				if fieldCount != 1 || fallback == invalidBackendValueID ||
					fallback == table || fallback == metatable ||
					metatableHasArray {
					continue
				}
				if current, exists := plan.indexFallback[table]; exists && current != fallback {
					return backendGoScalarTablePlan{}, nil
				}
				if plan.indexFallback[table] != fallback {
					plan.indexFallback[table] = fallback
					changed = true
				}
				metatableOperation := backendGoScalarMetatable{
					table: table, metatable: metatable, fallback: fallback,
				}
				if current, exists := plan.metatableByPC[operation.pc]; exists && current != metatableOperation {
					return backendGoScalarTablePlan{}, nil
				}
				plan.metatableByPC[operation.pc] = metatableOperation
				for _, definition := range operation.defs {
					if updated, ok := setRoot(definition.value, table); !ok {
						return backendGoScalarTablePlan{}, nil
					} else if updated {
						changed = true
					}
				}
			}
		}
		if !changed {
			break
		}
		if iteration == len(ir.values)+len(ir.ops) {
			return backendGoScalarTablePlan{}, fmt.Errorf("emit backend Go numeric proof: scalar table analysis did not converge")
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opFastCall {
			continue
		}
		switch nativeFuncID(operation.nativeID) {
		case nativeFuncSetMetatable:
			if _, ok := plan.metatableByPC[operation.pc]; !ok {
				table := backendOperationUse(operation, operation.a)
				metatable := backendOperationUse(operation, operation.a+1)
				if backendGoExcludedTableValue(ir, table, excluded) &&
					backendGoExcludedTableValue(ir, metatable, excluded) {
					continue
				}
				return backendGoScalarTablePlan{}, nil
			}
			continue
		case nativeFuncTableInsert:
			table := plan.root(backendOperationUse(operation, operation.a))
			if table == invalidBackendValueID {
				continue
			}
			_, array := ensureArray(table)
			if operation.c != 2 || operation.d != 1 {
				return backendGoScalarTablePlan{}, nil
			}
			source := backendOperationUse(operation, operation.a+1)
			if !ir.validBackendValue(source) ||
				plan.root(source) != invalidBackendValueID {
				return backendGoScalarTablePlan{}, nil
			}
			tags := ir.values[source-1].tags
			if tags == 0 || tags&^(backendTagNumber|backendTagBool|backendTagString) != 0 {
				return backendGoScalarTablePlan{}, nil
			}
			executions, ok := backendGoOperationExecutionBound(ir, operation)
			if !ok || executions == 0 ||
				uint64(array.appendBound)+uint64(executions) > uint64(backendGoMaxScalarArrayCapacity) {
				return backendGoScalarTablePlan{}, nil
			}
			array.tags |= tags
			array.appendBound += executions
			array.mutable = true
		case nativeFuncTableRemove:
			table := plan.root(backendOperationUse(operation, operation.a))
			if table == invalidBackendValueID {
				continue
			}
			_, array := ensureArray(table)
			if operation.c != 2 || operation.d != 1 ||
				!backendGoStaticNumberEquals(ir, backendOperationUse(operation, operation.a+1), 1) {
				return backendGoScalarTablePlan{}, nil
			}
			array.mutable = true
		case nativeFuncRawLen:
			table := plan.root(backendOperationUse(operation, operation.a))
			if table == invalidBackendValueID {
				continue
			}
			ensureArray(table)
			if operation.c != 1 || operation.d != 1 {
				return backendGoScalarTablePlan{}, nil
			}
		default:
			usesScalarTable := false
			for _, use := range operation.uses {
				if plan.root(use.value) != invalidBackendValueID {
					usesScalarTable = true
					break
				}
			}
			if usesScalarTable {
				return backendGoScalarTablePlan{}, nil
			}
		}
	}
	if len(plan.fields) == 0 && len(plan.arrays) == 0 {
		return backendGoScalarTablePlan{}, nil
	}
	for table := range plan.indexFallback {
		seen := make(map[backendValueID]bool)
		for current := table; current != invalidBackendValueID; current = plan.indexFallback[current] {
			if seen[current] {
				return backendGoScalarTablePlan{}, nil
			}
			seen[current] = true
		}
	}
	for arrayIndex := range plan.arrays {
		array := &plan.arrays[arrayIndex]
		if array.tags == 0 || array.tags&(array.tags-1) != 0 {
			return backendGoScalarTablePlan{}, nil
		}
		for index := uint32(1); index <= array.length; index++ {
			if !array.present[index] {
				return backendGoScalarTablePlan{}, nil
			}
		}
		capacity := uint64(array.length) + uint64(array.appendBound)
		if capacity == 0 || capacity > uint64(backendGoMaxScalarArrayCapacity) {
			return backendGoScalarTablePlan{}, nil
		}
		array.capacity = uint32(capacity)
		if array.mutable {
			if array.length != 0 {
				return backendGoScalarTablePlan{}, nil
			}
			for _, field := range plan.fields {
				if field.key.table == array.table {
					return backendGoScalarTablePlan{}, nil
				}
			}
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opGetIndex || len(operation.defs) != 1 {
			continue
		}
		table := plan.root(backendOperationUse(operation, operation.b))
		arrayIndex, ok := plan.arrayIndex[table]
		if !ok {
			continue
		}
		if plan.arrays[arrayIndex].mutable {
			return backendGoScalarTablePlan{}, nil
		}
		plan.arrayGetByPC[operation.pc] = arrayIndex
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opGetIndex || len(operation.defs) != 1 {
			continue
		}
		table := plan.root(backendOperationUse(operation, operation.b))
		if !plan.capturedExternalRoots[table] {
			continue
		}
		var tags backendTagMask
		fieldCount := 0
		valid := true
		for _, field := range plan.fields {
			if field.key.table != table || field.child != invalidBackendValueID || field.methodProto >= 0 {
				continue
			}
			fieldCount++
			if tags == 0 {
				tags = field.tags
			} else if tags != field.tags {
				valid = false
				break
			}
		}
		if valid && fieldCount != 0 {
			plan.dynamicExternalGetByPC[operation.pc] = backendGoScalarDynamicExternalGet{
				table: table, tags: tags,
			}
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opSetIndex {
			continue
		}
		table := plan.root(backendOperationUse(operation, operation.a))
		if !plan.capturedExternalRoots[table] {
			continue
		}
		var tags backendTagMask
		fieldCount := 0
		valid := true
		for _, field := range plan.fields {
			if field.key.table != table || field.child != invalidBackendValueID || field.methodProto >= 0 {
				continue
			}
			fieldCount++
			if tags == 0 {
				tags = field.tags
			} else if tags != field.tags {
				valid = false
				break
			}
		}
		if valid && fieldCount != 0 {
			plan.dynamicExternalSetByPC[operation.pc] = backendGoScalarDynamicExternalGet{
				table: table, tags: tags,
			}
		}
	}
	if !plan.analyzeIterators(ir) {
		return backendGoScalarTablePlan{}, nil
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		for _, use := range operation.uses {
			root := plan.root(use.value)
			if root == invalidBackendValueID {
				continue
			}
			switch operation.op {
			case opMove:
				if use.register != operation.b {
					return backendGoScalarTablePlan{}, nil
				}
			case opSetStringField:
				if use.register == operation.a {
					continue
				}
				if use.register != operation.c ||
					plan.root(backendOperationUse(operation, operation.a)) == invalidBackendValueID {
					return backendGoScalarTablePlan{}, nil
				}
			case opGetStringField:
				if use.register != operation.b {
					return backendGoScalarTablePlan{}, nil
				}
			case opGetIndex:
				if use.register != operation.b {
					return backendGoScalarTablePlan{}, nil
				}
				if _, ok := plan.arrayGetByPC[operation.pc]; !ok {
					if _, dynamic := plan.dynamicExternalGetByPC[operation.pc]; dynamic {
						continue
					}
					return backendGoScalarTablePlan{}, nil
				}
			case opSetIndex:
				if use.register == operation.a {
					if _, dynamic := plan.dynamicExternalSetByPC[operation.pc]; dynamic {
						continue
					}
				}
				return backendGoScalarTablePlan{}, nil
			case opSetField:
				if use.register != operation.a && use.register != operation.c {
					return backendGoScalarTablePlan{}, nil
				}
			case opPrepareIter:
				if use.register != operation.a {
					return backendGoScalarTablePlan{}, nil
				}
			case opArrayNextJump2:
				if use.register != operation.b &&
					!plan.iteratorValue(use.value) {
					return backendGoScalarTablePlan{}, nil
				}
			case opFastCall:
				if metatable, ok := plan.metatableOperation(operation); ok {
					if use.register != operation.a && use.register != operation.a+1 ||
						root != metatable.table && root != metatable.metatable {
						return backendGoScalarTablePlan{}, nil
					}
					continue
				}
				if use.register != operation.a {
					return backendGoScalarTablePlan{}, nil
				}
				if _, _, _, ok := plan.arrayOperation(ir, operation); !ok {
					return backendGoScalarTablePlan{}, nil
				}
			case opCallMethodOne:
				if use.register != operation.b {
					return backendGoScalarTablePlan{}, nil
				}
			default:
				return backendGoScalarTablePlan{}, nil
			}
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opGetStringField {
			continue
		}
		table := plan.root(backendOperationUse(operation, operation.b))
		if table == invalidBackendValueID {
			continue
		}
		name, ok := backendGoStringFieldName(ir, operation.access.constant)
		if !ok {
			return backendGoScalarTablePlan{}, nil
		}
		_, field, ok := plan.resolveField(table, name)
		if !ok || field.child == invalidBackendValueID && field.tags == 0 && field.methodProto < 0 {
			return backendGoScalarTablePlan{}, nil
		}
	}
	return plan, nil
}

func backendGoExcludedTableValue(
	ir *backendProtoIR,
	id backendValueID,
	excluded map[backendValueID]bool,
) bool {
	if !ir.validBackendValue(id) {
		return false
	}
	value := &ir.values[id-1]
	return value.object == backendObjectTable && len(value.origins) == 1 && excluded[value.origins[0]]
}

func (plan backendGoScalarTablePlan) isExternalRoot(root backendValueID) bool {
	for _, external := range plan.externalRoots {
		if root == external {
			return true
		}
	}
	return false
}

func (plan backendGoScalarTablePlan) tableUpvalue(upvalue int32) bool {
	return upvalue >= 0 && int(upvalue) < len(plan.tableUpvalues) && plan.tableUpvalues[upvalue]
}

func backendGoScalarRequiredTags(
	ir *backendProtoIR,
	id backendValueID,
	seen map[backendValueID]bool,
) backendTagMask {
	if !ir.validBackendValue(id) {
		return 0
	}
	if seen == nil {
		seen = make(map[backendValueID]bool)
	}
	if seen[id] {
		return 0
	}
	seen[id] = true
	var tags backendTagMask
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		for _, use := range operation.uses {
			if use.value != id {
				continue
			}
			switch operation.op {
			case opMove:
				for _, definition := range operation.defs {
					tags |= backendGoScalarRequiredTags(ir, definition.value, seen)
				}
			case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow,
				opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
				opNeg, opLess, opLessEqual, opGreater, opGreaterEqual,
				opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater,
				opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK,
				opNumericForCheck, opNumericForLoop:
				tags |= backendTagNumber
			case opJumpIfNotEqualK:
				if operation.b >= 0 && int(operation.b) < len(ir.constants) {
					tags |= backendTagForValueKind(ir.constants[operation.b].kind)
				}
			}
		}
	}
	if tags&(tags-1) != 0 {
		return 0
	}
	return tags
}

func backendGoCapturedRecordRoots(ir *backendProtoIR) []backendValueID {
	roots := make([]backendValueID, 0)
	upvalues := backendGoCapturedRecordUpvalues(ir)
	for upvalue := 0; ir != nil && upvalue < len(ir.upvalues); upvalue++ {
		roots = append(roots, upvalues[int32(upvalue)]...)
	}
	return roots
}

func backendGoCapturedRecordUpvalues(ir *backendProtoIR) map[int32][]backendValueID {
	if ir == nil {
		return nil
	}
	written := make([]bool, len(ir.upvalues))
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op == opSetUpvalue && operation.a >= 0 && int(operation.a) < len(written) {
			written[operation.a] = true
		}
	}
	upvalues := make(map[int32][]backendValueID)
	invalid := make([]bool, len(ir.upvalues))
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opGetUpvalue || operation.b < 0 || int(operation.b) >= len(ir.upvalues) {
			continue
		}
		if written[operation.b] || len(operation.defs) != 1 {
			invalid[operation.b] = true
			continue
		}
		root := operation.defs[0].value
		if !backendGoCapturedTableRoot(ir, root) {
			invalid[operation.b] = true
			continue
		}
		upvalues[operation.b] = append(upvalues[operation.b], root)
	}
	for upvalue, rejected := range invalid {
		if rejected {
			delete(upvalues, int32(upvalue))
		}
	}
	return upvalues
}

func backendGoCapturedTableRoot(ir *backendProtoIR, root backendValueID) bool {
	if !ir.validBackendValue(root) {
		return false
	}
	proved := false
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		for _, use := range operation.uses {
			if use.value != root {
				continue
			}
			switch operation.op {
			case opMove:
				if use.register != operation.b {
					return false
				}
			case opGetStringField, opGetIndex:
				if use.register != operation.b {
					return false
				}
				proved = true
			case opSetStringField, opSetField, opSetIndex:
				if use.register != operation.a {
					return false
				}
				proved = true
			case opPrepareIter:
				if use.register != operation.a {
					return false
				}
				proved = true
			default:
				return false
			}
		}
	}
	return proved
}

func backendGoScalarMethodClosure(ir *backendProtoIR, id backendValueID) (int32, bool) {
	if ir == nil || !ir.validBackendValue(id) {
		return -1, false
	}
	value := &ir.values[id-1]
	if value.object != backendObjectClosure ||
		value.targetUnknown ||
		len(value.targetProtos) != 1 ||
		value.kind != backendValueOperation ||
		value.pc < 0 ||
		int(value.pc) >= len(ir.ops) ||
		ir.ops[value.pc].op != opClosure {
		return -1, false
	}
	return value.targetProtos[0], true
}

func backendGoNewTableRoot(ir *backendProtoIR, id backendValueID) bool {
	if !ir.validBackendValue(id) {
		return false
	}
	value := &ir.values[id-1]
	return value.kind == backendValueOperation &&
		value.pc >= 0 &&
		int(value.pc) < len(ir.ops) &&
		ir.ops[value.pc].op == opNewTable
}

func backendGoStringFieldName(ir *backendProtoIR, constant int32) (machineStringID, bool) {
	if ir == nil || constant < 0 || int(constant) >= len(ir.constants) {
		return invalidMachineStringID, false
	}
	value := ir.constants[constant]
	if value.kind != StringKind || value.bits == 0 || value.bits > uint64(^uint32(0)) {
		return invalidMachineStringID, false
	}
	return machineStringID(value.bits), true
}

func backendGoStringFieldIsIndex(ir *backendProtoIR, constant int32) bool {
	return ir != nil &&
		constant >= 0 &&
		int(constant) < len(ir.constants) &&
		ir.constants[constant].kind == StringKind &&
		ir.constants[constant].flags&machineConstantFlagIndexName != 0
}

func backendGoStringFieldIsNewIndex(ir *backendProtoIR, constant int32) bool {
	return ir != nil &&
		constant >= 0 &&
		int(constant) < len(ir.constants) &&
		ir.constants[constant].kind == StringKind &&
		ir.constants[constant].flags&machineConstantFlagNewIndexName != 0
}

func backendGoArrayIndexConstant(ir *backendProtoIR, constant int32) (uint32, bool) {
	if ir == nil || constant < 0 || int(constant) >= len(ir.constants) {
		return 0, false
	}
	value := ir.constants[constant]
	if value.kind != NumberKind {
		return 0, false
	}
	number := math.Float64frombits(value.bits)
	if number < 1 || number > float64(^uint32(0)) || number != math.Trunc(number) {
		return 0, false
	}
	return uint32(number), true
}

const backendGoMaxScalarArrayCapacity = 4096

func backendGoOperationExecutionBound(ir *backendProtoIR, operation *backendOperationIR) (uint32, bool) {
	if ir == nil || operation == nil ||
		operation.block < 0 || int(operation.block) >= len(ir.blocks) {
		return 0, false
	}
	bound := uint64(1)
	for headerIndex := range ir.blocks {
		header := &ir.blocks[headerIndex]
		if !header.loopHeader {
			continue
		}
		members := backendGoNaturalLoopMembers(ir, int32(headerIndex))
		if !members[operation.block] {
			continue
		}
		count, ok := backendGoStaticNumericLoopCount(ir, int32(headerIndex), members)
		if !ok {
			count, ok = backendGoBoundedNumericLoopCount(ir, int32(headerIndex), members)
		}
		if !ok || count == 0 ||
			bound*uint64(count) > uint64(backendGoMaxScalarArrayCapacity) {
			return 0, false
		}
		bound *= uint64(count)
	}
	return uint32(bound), true
}

func backendGoBoundedNumericLoopCount(
	ir *backendProtoIR,
	header int32,
	members map[int32]bool,
) (uint32, bool) {
	if ir == nil || header < 0 || int(header) >= len(ir.blocks) {
		return 0, false
	}
	var check *backendOperationIR
	block := &ir.blocks[header]
	for pc := block.first; pc < block.last; pc++ {
		operation := &ir.ops[pc]
		if operation.op != opNumericForCheck || check != nil {
			if operation.op == opNumericForCheck {
				return 0, false
			}
			continue
		}
		check = operation
	}
	if check == nil {
		return 0, false
	}
	start, startOK := backendGoLoopInitialNumber(ir, header, members, backendOperationUse(check, check.a))
	limitLow, limitHigh, limitOK := backendGoLoopInitialNumberBounds(
		ir, header, members, backendOperationUse(check, check.b),
	)
	step, stepOK := backendGoLoopInitialNumber(ir, header, members, backendOperationUse(check, check.c))
	if !startOK || !limitOK || !stepOK ||
		math.IsNaN(start) || math.IsNaN(limitLow) || math.IsNaN(limitHigh) || math.IsNaN(step) ||
		math.IsInf(start, 0) || math.IsInf(limitLow, 0) || math.IsInf(limitHigh, 0) || math.IsInf(step, 0) ||
		step == 0 {
		return 0, false
	}
	limit := limitHigh
	if step < 0 {
		limit = limitLow
	}
	current := start
	for count := uint32(0); ; count++ {
		if (step > 0 && current > limit) || (step < 0 && current < limit) {
			return count, true
		}
		if count == backendGoMaxScalarArrayCapacity {
			return 0, false
		}
		next := current + step
		if next == current || math.IsInf(next, 0) {
			return 0, false
		}
		current = next
	}
}

func backendGoLoopInitialNumberBounds(
	ir *backendProtoIR,
	header int32,
	members map[int32]bool,
	id backendValueID,
) (float64, float64, bool) {
	if !ir.validBackendValue(id) {
		return 0, 0, false
	}
	value := &ir.values[id-1]
	if value.kind != backendValuePhi || value.block != header {
		return backendGoNumberBounds(ir, id, nil)
	}
	block := &ir.blocks[header]
	phi := block.phis[value.register]
	var low, high float64
	found := false
	for inputIndex, input := range phi.inputs {
		if members[block.predecessors[inputIndex]] {
			continue
		}
		candidateLow, candidateHigh, ok := backendGoNumberBounds(ir, input, nil)
		if !ok {
			return 0, 0, false
		}
		if !found || candidateLow < low {
			low = candidateLow
		}
		if !found || candidateHigh > high {
			high = candidateHigh
		}
		found = true
	}
	return low, high, found
}

func backendGoNumberBounds(
	ir *backendProtoIR,
	id backendValueID,
	seen map[backendValueID]bool,
) (float64, float64, bool) {
	if !ir.validBackendValue(id) {
		return 0, 0, false
	}
	if seen == nil {
		seen = make(map[backendValueID]bool)
	}
	if seen[id] {
		return 0, 0, false
	}
	seen[id] = true
	defer delete(seen, id)
	value := &ir.values[id-1]
	if value.kind != backendValueOperation || value.pc < 0 || int(value.pc) >= len(ir.ops) {
		return 0, 0, false
	}
	operation := &ir.ops[value.pc]
	constant := func(index int32) (float64, bool) {
		if index < 0 || int(index) >= len(ir.constants) || ir.constants[index].kind != NumberKind {
			return 0, false
		}
		number := math.Float64frombits(ir.constants[index].bits)
		return number, !math.IsNaN(number) && !math.IsInf(number, 0)
	}
	switch operation.op {
	case opLoadConst:
		number, ok := constant(operation.b)
		return number, number, ok
	case opMove:
		return backendGoNumberBounds(ir, backendOperationUse(operation, operation.b), seen)
	case opModK:
		divisor, ok := constant(operation.c)
		if !ok || divisor <= 0 {
			return 0, 0, false
		}
		return 0, divisor, true
	case opAddK, opSubK:
		leftLow, leftHigh, ok := backendGoNumberBounds(ir, backendOperationUse(operation, operation.b), seen)
		right, rightOK := constant(operation.c)
		if !ok || !rightOK {
			return 0, 0, false
		}
		if operation.op == opAddK {
			return leftLow + right, leftHigh + right, true
		}
		return leftLow - right, leftHigh - right, true
	case opAdd, opSub:
		leftLow, leftHigh, leftOK := backendGoNumberBounds(ir, backendOperationUse(operation, operation.b), seen)
		rightLow, rightHigh, rightOK := backendGoNumberBounds(ir, backendOperationUse(operation, operation.c), seen)
		if !leftOK || !rightOK {
			return 0, 0, false
		}
		if operation.op == opAdd {
			return leftLow + rightLow, leftHigh + rightHigh, true
		}
		return leftLow - rightHigh, leftHigh - rightLow, true
	default:
		return 0, 0, false
	}
}

func backendGoNaturalLoopMembers(ir *backendProtoIR, header int32) map[int32]bool {
	members := map[int32]bool{header: true}
	if ir == nil || header < 0 || int(header) >= len(ir.blocks) {
		return members
	}
	stack := make([]int32, 0)
	for _, predecessor := range ir.blocks[header].predecessors {
		if backendBlockDominates(&ir.blocks[predecessor], header) {
			members[predecessor] = true
			stack = append(stack, predecessor)
		}
	}
	for len(stack) != 0 {
		block := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, predecessor := range ir.blocks[block].predecessors {
			if members[predecessor] {
				continue
			}
			members[predecessor] = true
			if predecessor != header {
				stack = append(stack, predecessor)
			}
		}
	}
	return members
}

func backendGoStaticNumericLoopCount(
	ir *backendProtoIR,
	header int32,
	members map[int32]bool,
) (uint32, bool) {
	if ir == nil || header < 0 || int(header) >= len(ir.blocks) {
		return 0, false
	}
	var check *backendOperationIR
	block := &ir.blocks[header]
	for pc := block.first; pc < block.last; pc++ {
		operation := &ir.ops[pc]
		if operation.op != opNumericForCheck {
			continue
		}
		if check != nil {
			return 0, false
		}
		check = operation
	}
	if check == nil {
		return 0, false
	}
	start, startOK := backendGoLoopInitialNumber(ir, header, members, backendOperationUse(check, check.a))
	limit, limitOK := backendGoLoopInitialNumber(ir, header, members, backendOperationUse(check, check.b))
	step, stepOK := backendGoLoopInitialNumber(ir, header, members, backendOperationUse(check, check.c))
	if !startOK || !limitOK || !stepOK ||
		math.IsNaN(start) || math.IsNaN(limit) || math.IsNaN(step) ||
		math.IsInf(start, 0) || math.IsInf(limit, 0) || math.IsInf(step, 0) ||
		step == 0 {
		return 0, false
	}
	current := start
	for count := uint32(0); ; count++ {
		if (step > 0 && current > limit) || (step < 0 && current < limit) {
			return count, true
		}
		if count == backendGoMaxScalarArrayCapacity {
			return 0, false
		}
		next := current + step
		if next == current || math.IsInf(next, 0) {
			return 0, false
		}
		current = next
	}
}

func backendGoLoopInitialNumber(
	ir *backendProtoIR,
	header int32,
	members map[int32]bool,
	id backendValueID,
) (float64, bool) {
	if !ir.validBackendValue(id) {
		return 0, false
	}
	value := &ir.values[id-1]
	if value.kind != backendValuePhi || value.block != header {
		return backendGoStaticNumber(ir, id, nil)
	}
	block := &ir.blocks[header]
	phi := block.phis[value.register]
	var number float64
	found := false
	for inputIndex, input := range phi.inputs {
		if members[block.predecessors[inputIndex]] {
			continue
		}
		candidate, ok := backendGoStaticNumber(ir, input, nil)
		if !ok || found && candidate != number {
			return 0, false
		}
		number = candidate
		found = true
	}
	return number, found
}

func backendGoStaticNumberEquals(ir *backendProtoIR, id backendValueID, want float64) bool {
	number, ok := backendGoStaticNumber(ir, id, nil)
	return ok && number == want
}

func backendGoStaticArrayIndex(ir *backendProtoIR, id backendValueID) (uint32, bool) {
	number, ok := backendGoStaticNumber(ir, id, nil)
	if !ok || number < 1 || number > float64(^uint32(0)) || number != math.Trunc(number) {
		return 0, false
	}
	return uint32(number), true
}

func backendGoStaticNumber(
	ir *backendProtoIR,
	id backendValueID,
	seen map[backendValueID]bool,
) (float64, bool) {
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
	if value.kind != backendValueOperation ||
		value.pc < 0 || int(value.pc) >= len(ir.ops) {
		return 0, false
	}
	operation := &ir.ops[value.pc]
	switch operation.op {
	case opLoadConst:
		if operation.b < 0 || int(operation.b) >= len(ir.constants) ||
			ir.constants[operation.b].kind != NumberKind {
			return 0, false
		}
		return math.Float64frombits(ir.constants[operation.b].bits), true
	case opMove:
		return backendGoStaticNumber(ir, backendOperationUse(operation, operation.b), seen)
	default:
		return 0, false
	}
}

func (plan *backendGoScalarTablePlan) analyzeIterators(ir *backendProtoIR) bool {
	if plan == nil || ir == nil {
		return false
	}
	plan.iteratorValues = make([]bool, len(ir.values))
	iteratorArray := make([]int, len(ir.values))
	for valueIndex := range iteratorArray {
		iteratorArray[valueIndex] = -1
	}
	prepareByArray := make(map[int]int32)
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opPrepareIter {
			continue
		}
		table := plan.root(backendOperationUse(operation, operation.a))
		arrayIndex, ok := plan.arrayIndex[table]
		if !ok {
			continue
		}
		if plan.arrays[arrayIndex].mutable {
			return false
		}
		if _, exists := plan.iteratorByPC[operation.pc]; exists {
			return false
		}
		for _, existing := range plan.iteratorByPC {
			if existing == arrayIndex {
				return false
			}
		}
		if _, exists := prepareByArray[arrayIndex]; exists {
			return false
		}
		for _, field := range plan.fields {
			if field.key.table == table {
				return false
			}
		}
		for candidatePC := range ir.ops {
			candidate := &ir.ops[candidatePC]
			if candidate.op != opSetField ||
				plan.root(backendOperationUse(candidate, candidate.a)) != table {
				continue
			}
			if candidate.pc >= operation.pc {
				return false
			}
		}
		plan.iteratorByPC[operation.pc] = arrayIndex
		prepareByArray[arrayIndex] = operation.pc
		for _, definition := range operation.defs {
			if definition.register == operation.a ||
				definition.register == operation.b ||
				definition.register == operation.c {
				plan.iteratorValues[definition.value-1] = true
				iteratorArray[definition.value-1] = arrayIndex
			}
		}
	}
	adjacent := make([][]backendValueID, len(ir.values))
	for valueIndex := range ir.values {
		value := &ir.values[valueIndex]
		if value.kind != backendValuePhi {
			continue
		}
		block := &ir.blocks[value.block]
		phi := block.phis[value.register]
		for inputIndex, input := range phi.inputs {
			if !ir.blocks[block.predecessors[inputIndex]].reachable ||
				!ir.validBackendValue(input) ||
				ir.values[input-1].register != value.register {
				continue
			}
			adjacent[valueIndex] = append(adjacent[valueIndex], input)
			adjacent[input-1] = append(adjacent[input-1], value.id)
		}
	}
	queue := make([]backendValueID, 0, len(ir.values))
	for valueIndex, token := range plan.iteratorValues {
		if token {
			queue = append(queue, backendValueID(valueIndex+1))
		}
	}
	for len(queue) != 0 {
		id := queue[0]
		queue = queue[1:]
		for _, next := range adjacent[id-1] {
			if plan.iteratorValues[next-1] {
				if iteratorArray[next-1] != iteratorArray[id-1] {
					return false
				}
				continue
			}
			plan.iteratorValues[next-1] = true
			iteratorArray[next-1] = iteratorArray[id-1]
			queue = append(queue, next)
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opArrayNextJump2 {
			continue
		}
		table := backendOperationUse(operation, operation.b)
		control := backendOperationUse(operation, operation.c)
		if !plan.iteratorValue(table) ||
			!plan.iteratorValue(control) ||
			iteratorArray[table-1] < 0 ||
			iteratorArray[table-1] != iteratorArray[control-1] {
			continue
		}
		arrayIndex := iteratorArray[control-1]
		preparePC, ok := prepareByArray[arrayIndex]
		if !ok ||
			!backendGoScalarTableOperationDominates(ir, &ir.ops[preparePC], operation) {
			continue
		}
		plan.iteratorByPC[operation.pc] = arrayIndex
	}
	for valueIndex, token := range plan.iteratorValues {
		if !token || iteratorArray[valueIndex] < 0 {
			continue
		}
		value := &ir.values[valueIndex]
		if value.kind != backendValueOperation {
			continue
		}
		arrayIndex := iteratorArray[valueIndex]
		preparePC, ok := prepareByArray[arrayIndex]
		if !ok {
			return false
		}
		definition := &ir.ops[value.pc]
		if definition.pc == preparePC ||
			definition.op == opArrayNextJump2 &&
				plan.iteratorByPC[definition.pc] == arrayIndex {
			continue
		}
		if backendGoScalarTableOperationDominates(ir, &ir.ops[preparePC], definition) {
			return false
		}
	}
	for valueIndex, token := range plan.iteratorValues {
		if !token {
			continue
		}
		id := backendValueID(valueIndex + 1)
		for pc := range ir.ops {
			operation := &ir.ops[pc]
			for _, use := range operation.uses {
				if use.value == id && operation.op != opArrayNextJump2 {
					return false
				}
			}
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opArrayNextJump2 {
			continue
		}
		if _, ok := plan.iteratorByPC[operation.pc]; !ok && !plan.partial {
			return false
		}
	}
	return true
}

func backendGoScalarTableOperationDominates(
	ir *backendProtoIR,
	dominator *backendOperationIR,
	operation *backendOperationIR,
) bool {
	if ir == nil || dominator == nil || operation == nil ||
		dominator.block < 0 || int(dominator.block) >= len(ir.blocks) ||
		operation.block < 0 || int(operation.block) >= len(ir.blocks) ||
		!backendBlockDominates(&ir.blocks[operation.block], dominator.block) {
		return false
	}
	return dominator.block != operation.block || dominator.pc < operation.pc
}

func (plan backendGoScalarTablePlan) root(id backendValueID) backendValueID {
	if id == invalidBackendValueID || int(id) > len(plan.roots) {
		return invalidBackendValueID
	}
	return plan.roots[id-1]
}

func (plan backendGoScalarTablePlan) iteratorValue(id backendValueID) bool {
	return id != invalidBackendValueID &&
		int(id) <= len(plan.iteratorValues) &&
		plan.iteratorValues[id-1]
}

func (plan backendGoScalarTablePlan) arrayOperation(
	ir *backendProtoIR,
	operation *backendOperationIR,
) (int, backendGoScalarArray, uint32, bool) {
	if ir == nil || operation == nil {
		return 0, backendGoScalarArray{}, 0, false
	}
	switch operation.op {
	case opSetField:
		table := plan.root(backendOperationUse(operation, operation.a))
		arrayIndex, ok := plan.arrayIndex[table]
		element, elementOK := backendGoArrayIndexConstant(ir, operation.access.constant)
		if !ok || !elementOK {
			return 0, backendGoScalarArray{}, 0, false
		}
		return arrayIndex, plan.arrays[arrayIndex], element, true
	case opGetIndex:
		arrayIndex, ok := plan.arrayGetByPC[operation.pc]
		if !ok || arrayIndex < 0 || arrayIndex >= len(plan.arrays) {
			return 0, backendGoScalarArray{}, 0, false
		}
		return arrayIndex, plan.arrays[arrayIndex], 0, true
	case opPrepareIter, opArrayNextJump2:
		arrayIndex, ok := plan.iteratorByPC[operation.pc]
		if !ok || arrayIndex < 0 || arrayIndex >= len(plan.arrays) {
			return 0, backendGoScalarArray{}, 0, false
		}
		return arrayIndex, plan.arrays[arrayIndex], 0, true
	case opFastCall:
		table := plan.root(backendOperationUse(operation, operation.a))
		arrayIndex, ok := plan.arrayIndex[table]
		if !ok || arrayIndex < 0 || arrayIndex >= len(plan.arrays) {
			return 0, backendGoScalarArray{}, 0, false
		}
		switch nativeFuncID(operation.nativeID) {
		case nativeFuncTableInsert, nativeFuncTableRemove, nativeFuncRawLen:
			return arrayIndex, plan.arrays[arrayIndex], 0, true
		default:
			return 0, backendGoScalarArray{}, 0, false
		}
	default:
		return 0, backendGoScalarArray{}, 0, false
	}
}

func (plan backendGoScalarTablePlan) field(key backendGoScalarFieldKey) (backendGoScalarField, bool) {
	index, ok := plan.index[key]
	if !ok || index < 0 || index >= len(plan.fields) {
		return backendGoScalarField{}, false
	}
	return plan.fields[index], true
}

func (plan backendGoScalarTablePlan) resolveField(
	table backendValueID,
	name machineStringID,
) (int, backendGoScalarField, bool) {
	seen := make(map[backendValueID]bool)
	for table != invalidBackendValueID && !seen[table] {
		seen[table] = true
		key := backendGoScalarFieldKey{table: table, name: name}
		if index, ok := plan.index[key]; ok && index >= 0 && index < len(plan.fields) {
			return index, plan.fields[index], true
		}
		table = plan.indexFallback[table]
	}
	return 0, backendGoScalarField{}, false
}

func (plan backendGoScalarTablePlan) metatableOperation(
	operation *backendOperationIR,
) (backendGoScalarMetatable, bool) {
	if operation == nil || operation.op != opFastCall ||
		nativeFuncID(operation.nativeID) != nativeFuncSetMetatable {
		return backendGoScalarMetatable{}, false
	}
	metatable, ok := plan.metatableByPC[operation.pc]
	return metatable, ok
}

func (plan backendGoScalarTablePlan) operationField(
	ir *backendProtoIR,
	operation *backendOperationIR,
) (int, backendGoScalarField, bool) {
	if ir == nil || operation == nil {
		return 0, backendGoScalarField{}, false
	}
	var tableRegister int32
	switch operation.op {
	case opSetStringField:
		tableRegister = operation.a
	case opGetStringField:
		tableRegister = operation.b
	default:
		return 0, backendGoScalarField{}, false
	}
	table := plan.root(backendOperationUse(operation, tableRegister))
	name, ok := backendGoStringFieldName(ir, operation.access.constant)
	if table == invalidBackendValueID || !ok {
		return 0, backendGoScalarField{}, false
	}
	return plan.resolveField(table, name)
}
