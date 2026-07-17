package ember

import (
	"fmt"
	"sort"
	"strings"
)

type backendGoFiniteClosureBucket struct {
	key       machineStringID
	instances []backendGoScalarClosureValue
}

type backendGoFiniteClosureReceiverField struct {
	field backendGoRecordFieldOperation
	tags  backendTagMask
}

type backendGoFiniteClosureSet struct {
	selectorPC     int32
	preparePC      int32
	nextPC         int32
	callPC         int32
	key            backendValueID
	bucket         backendValueID
	handler        backendValueID
	target         backendGoNumericTarget
	buckets        []backendGoFiniteClosureBucket
	receiverFields []backendGoFiniteClosureReceiverField
}

type backendGoFiniteClosureSetPlan struct {
	sets          map[int32]backendGoFiniteClosureSet
	selectorByPC  map[int32]int32
	prepareByPC   map[int32]int32
	nextByPC      map[int32]int32
	setterPCs     map[int32]bool
	excludedRoots map[backendValueID]bool
	scalarValues  []bool
}

func discoverBackendGoFiniteClosureSets(
	ir *backendProtoIR,
	roots backendGoRecordTablePlan,
	closures backendGoScalarClosurePlan,
	options backendGoNumericOptions,
) backendGoFiniteClosureSetPlan {
	plan := backendGoFiniteClosureSetPlan{
		sets:          make(map[int32]backendGoFiniteClosureSet),
		selectorByPC:  make(map[int32]int32),
		prepareByPC:   make(map[int32]int32),
		nextByPC:      make(map[int32]int32),
		setterPCs:     make(map[int32]bool),
		excludedRoots: make(map[backendValueID]bool),
	}
	if ir == nil {
		return plan
	}
	plan.scalarValues = make([]bool, len(ir.values))

	type bucketBuilder struct {
		instances map[uint32]backendGoScalarClosureValue
		setters   []int32
	}
	buckets := make(map[backendValueID]*bucketBuilder)
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opSetField {
			continue
		}
		root := roots.root(backendOperationUse(operation, operation.a))
		index, indexOK := backendGoArrayIndexConstant(ir, operation.access.constant)
		instance, instanceOK := closures.value(backendOperationUse(operation, operation.c))
		if root == invalidBackendValueID || !indexOK || !instanceOK || index == 0 {
			continue
		}
		builder := buckets[root]
		if builder == nil {
			builder = &bucketBuilder{instances: make(map[uint32]backendGoScalarClosureValue)}
			buckets[root] = builder
		}
		if _, duplicate := builder.instances[index]; duplicate {
			delete(buckets, root)
			continue
		}
		builder.instances[index] = instance
		builder.setters = append(builder.setters, operation.pc)
	}

	type handlerBuilder struct {
		fields  map[machineStringID]backendValueID
		setters []int32
	}
	handlers := make(map[backendValueID]*handlerBuilder)
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opSetStringField {
			continue
		}
		handlerRoot := roots.root(backendOperationUse(operation, operation.a))
		bucketRoot := roots.root(backendOperationUse(operation, operation.c))
		name, nameOK := backendGoStringFieldName(ir, operation.access.constant)
		if handlerRoot == invalidBackendValueID || buckets[bucketRoot] == nil || !nameOK {
			continue
		}
		builder := handlers[handlerRoot]
		if builder == nil {
			builder = &handlerBuilder{fields: make(map[machineStringID]backendValueID)}
			handlers[handlerRoot] = builder
		}
		if _, duplicate := builder.fields[name]; duplicate {
			delete(handlers, handlerRoot)
			continue
		}
		builder.fields[name] = bucketRoot
		builder.setters = append(builder.setters, operation.pc)
	}

	for pc := range ir.ops {
		selector := &ir.ops[pc]
		if selector.op != opGetIndex || len(selector.defs) != 1 {
			continue
		}
		handlerRoot := roots.root(backendOperationUse(selector, selector.b))
		handler := handlers[handlerRoot]
		key := backendOperationUse(selector, selector.c)
		if handler == nil || !ir.validBackendValue(key) {
			continue
		}
		selectorValue := selector.defs[0].value
		prepare := backendGoFindDerivedOperation(ir, selectorValue, opPrepareIter, -1)
		if prepare == nil || len(prepare.defs) == 0 {
			continue
		}
		bucketValue := backendGoDefinitionForRegister(prepare, prepare.a)
		if bucketValue == invalidBackendValueID {
			continue
		}
		next := backendGoFindDerivedOperation(ir, bucketValue, opArrayNextJump2, prepare.pc)
		if next == nil {
			continue
		}
		handlerValue := backendGoDefinitionForRegister(next, next.a+1)
		if handlerValue == invalidBackendValueID {
			continue
		}
		call := backendGoFindDerivedOperation(ir, handlerValue, opCallLocalOne, next.pc)
		if call == nil || call.callArgCount <= 0 || call.callResults != 1 {
			continue
		}

		var targetProto int32 = -1
		valid := true
		ordered := make([]backendGoFiniteClosureBucket, 0, len(handler.fields))
		names := make([]int, 0, len(handler.fields))
		for name := range handler.fields {
			names = append(names, int(name))
		}
		sort.Ints(names)
		for _, rawName := range names {
			name := machineStringID(rawName)
			bucketRoot := handler.fields[name]
			builder := buckets[bucketRoot]
			instances := make([]backendGoScalarClosureValue, len(builder.instances))
			for index := uint32(1); index <= uint32(len(instances)); index++ {
				instance, exists := builder.instances[index]
				if !exists || instance.cellCount <= 0 {
					valid = false
					break
				}
				if targetProto < 0 {
					targetProto = instance.targetProto
				} else if targetProto != instance.targetProto {
					valid = false
					break
				}
				instances[index-1] = instance
			}
			if !valid {
				break
			}
			ordered = append(ordered, backendGoFiniteClosureBucket{key: name, instances: instances})
		}
		if !valid || targetProto < 0 || int(targetProto) >= len(options.directTargets) {
			continue
		}
		target := options.directTargets[targetProto]
		receiverTables := backendGoNumericReceiverTableCount(target.receiverTable, target.receiverTables)
		if target.ir == nil || target.functionName == "" || target.selfRecursive || target.ir.variadic ||
			receiverTables <= 0 || receiverTables > int(call.callArgCount) ||
			len(target.ir.upvalues) != ordered[0].instances[0].cellCount ||
			target.ir.params != int(call.callArgCount) {
			continue
		}
		if prepare.pc >= next.pc || next.pc >= call.pc {
			continue
		}
		if !backendGoFiniteClosureIteratorControlsUnobserved(ir, prepare, next, bucketValue, handlerValue) {
			continue
		}

		set := backendGoFiniteClosureSet{
			selectorPC: selector.pc,
			preparePC:  prepare.pc,
			nextPC:     next.pc,
			callPC:     call.pc,
			key:        key,
			bucket:     bucketValue,
			handler:    handlerValue,
			target:     target,
			buckets:    ordered,
		}
		plan.sets[call.pc] = set
		plan.selectorByPC[selector.pc] = call.pc
		plan.prepareByPC[prepare.pc] = call.pc
		plan.nextByPC[next.pc] = call.pc
		plan.excludedRoots[handlerRoot] = true
		for _, bucketRoot := range handler.fields {
			plan.excludedRoots[bucketRoot] = true
			for _, setter := range buckets[bucketRoot].setters {
				plan.setterPCs[setter] = true
			}
		}
		for _, setter := range handler.setters {
			plan.setterPCs[setter] = true
		}
	}
	for valueIndex := range ir.values {
		root := roots.root(backendValueID(valueIndex + 1))
		if plan.excludedRoots[root] {
			plan.scalarValues[valueIndex] = true
		}
	}
	ignoredIteratorValues := make([]backendValueID, 0)
	for _, set := range plan.sets {
		prepare := &ir.ops[set.preparePC]
		for _, definition := range prepare.defs {
			if definition.value != set.bucket {
				plan.scalarValues[definition.value-1] = true
				ignoredIteratorValues = append(ignoredIteratorValues, definition.value)
			}
		}
		next := &ir.ops[set.nextPC]
		for _, definition := range next.defs {
			if definition.value != set.handler {
				plan.scalarValues[definition.value-1] = true
				ignoredIteratorValues = append(ignoredIteratorValues, definition.value)
			}
		}
	}
	for valueIndex := range ir.values {
		value := backendValueID(valueIndex + 1)
		for _, ignored := range ignoredIteratorValues {
			if backendGoValueDerivedFrom(ir, value, ignored, nil) {
				plan.scalarValues[valueIndex] = true
				break
			}
		}
	}
	return plan
}

func backendGoFiniteClosureIteratorControlsUnobserved(
	ir *backendProtoIR,
	prepare *backendOperationIR,
	next *backendOperationIR,
	bucket backendValueID,
	handler backendValueID,
) bool {
	ignored := make([]backendValueID, 0, len(prepare.defs)+len(next.defs))
	for _, definition := range prepare.defs {
		if definition.value != bucket {
			ignored = append(ignored, definition.value)
		}
	}
	for _, definition := range next.defs {
		if definition.value != handler {
			ignored = append(ignored, definition.value)
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		for _, use := range operation.uses {
			for _, source := range ignored {
				if !backendGoValueDerivedFrom(ir, use.value, source, nil) {
					continue
				}
				if operation.op != opMove && operation.pc != next.pc {
					return false
				}
			}
		}
	}
	return true
}

func backendGoDefinitionForRegister(operation *backendOperationIR, register int32) backendValueID {
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

func backendGoFindDerivedOperation(
	ir *backendProtoIR,
	source backendValueID,
	op opcode,
	after int32,
) *backendOperationIR {
	var found *backendOperationIR
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.pc <= after || operation.op != op {
			continue
		}
		for _, use := range operation.uses {
			if backendGoValueDerivedFrom(ir, use.value, source, nil) {
				if found != nil {
					return nil
				}
				found = operation
				break
			}
		}
	}
	return found
}

func backendGoValueDerivedFrom(
	ir *backendProtoIR,
	id backendValueID,
	source backendValueID,
	seen map[backendValueID]bool,
) bool {
	if id == source {
		return true
	}
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
	if value.kind == backendValuePhi && value.block >= 0 && int(value.block) < len(ir.blocks) {
		phi := ir.blocks[value.block].phis[value.register]
		for _, input := range phi.inputs {
			if input != id && backendGoValueDerivedFrom(ir, input, source, seen) {
				return true
			}
		}
	}
	for _, origin := range value.origins {
		if origin != id && backendGoValueDerivedFrom(ir, origin, source, seen) {
			return true
		}
	}
	if value.kind == backendValueOperation && value.pc >= 0 && int(value.pc) < len(ir.ops) {
		operation := &ir.ops[value.pc]
		if operation.op == opMove {
			return backendGoValueDerivedFrom(ir, backendOperationUse(operation, operation.b), source, seen)
		}
	}
	return false
}

func finalizeBackendGoFiniteClosureSets(
	ir *backendProtoIR,
	plan backendGoFiniteClosureSetPlan,
	records backendGoRecordTablePlan,
) (backendGoFiniteClosureSetPlan, bool) {
	for pc, set := range plan.sets {
		receiverTables := backendGoNumericReceiverTableCount(set.target.receiverTable, set.target.receiverTables)
		targetPlan, err := buildBackendGoNumericPlan(set.target.ir, backendGoNumericOptions{
			functionName:     set.target.functionName,
			directTargets:    nil,
			fixedVarargCount: set.target.fixedVarargCount,
			receiverTables:   receiverTables,
		})
		if err != nil || len(targetPlan.tables.externalRoots) != receiverTables {
			return backendGoFiniteClosureSetPlan{}, false
		}
		operation := &ir.ops[pc]
		fields := make([]backendGoFiniteClosureReceiverField, 0)
		for _, targetField := range targetPlan.tables.fields {
			parameter := -1
			for index, root := range targetPlan.tables.externalRoots {
				if targetField.key.table == root {
					parameter = index
					break
				}
			}
			if parameter < 0 || targetField.child != invalidBackendValueID || targetField.methodProto >= 0 {
				return backendGoFiniteClosureSetPlan{}, false
			}
			argument := backendOperationUse(operation, operation.callArgStart+int32(parameter))
			field, ok := records.fieldForValue(argument, targetField.key.name)
			if !ok {
				return backendGoFiniteClosureSetPlan{}, false
			}
			fields = append(fields, backendGoFiniteClosureReceiverField{field: field, tags: targetField.tags})
		}
		if len(fields) == 0 {
			return backendGoFiniteClosureSetPlan{}, false
		}
		set.receiverFields = fields
		plan.sets[pc] = set
	}
	return plan, true
}

func (plan backendGoFiniteClosureSetPlan) set(operation *backendOperationIR) (backendGoFiniteClosureSet, bool) {
	if operation == nil {
		return backendGoFiniteClosureSet{}, false
	}
	set, ok := plan.sets[operation.pc]
	return set, ok
}

func (plan backendGoFiniteClosureSetPlan) hasSet(operation *backendOperationIR) bool {
	_, ok := plan.set(operation)
	return ok
}

func (plan backendGoFiniteClosureSetPlan) selector(operation *backendOperationIR) (backendGoFiniteClosureSet, bool) {
	if operation == nil {
		return backendGoFiniteClosureSet{}, false
	}
	callPC, ok := plan.selectorByPC[operation.pc]
	if !ok {
		return backendGoFiniteClosureSet{}, false
	}
	set, ok := plan.sets[callPC]
	return set, ok
}

func (plan backendGoFiniteClosureSetPlan) prepare(operation *backendOperationIR) (backendGoFiniteClosureSet, bool) {
	if operation == nil {
		return backendGoFiniteClosureSet{}, false
	}
	callPC, ok := plan.prepareByPC[operation.pc]
	if !ok {
		return backendGoFiniteClosureSet{}, false
	}
	set, ok := plan.sets[callPC]
	return set, ok
}

func (plan backendGoFiniteClosureSetPlan) next(operation *backendOperationIR) (backendGoFiniteClosureSet, bool) {
	if operation == nil {
		return backendGoFiniteClosureSet{}, false
	}
	callPC, ok := plan.nextByPC[operation.pc]
	if !ok {
		return backendGoFiniteClosureSet{}, false
	}
	set, ok := plan.sets[callPC]
	return set, ok
}

func (plan backendGoFiniteClosureSetPlan) setter(operation *backendOperationIR) bool {
	return operation != nil && plan.setterPCs[operation.pc]
}

func (plan backendGoRecordTablePlan) fieldForValue(
	value backendValueID,
	name machineStringID,
) (backendGoRecordFieldOperation, bool) {
	if root := plan.root(value); root != invalidBackendValueID {
		if record, ok := plan.recordByRoot[root]; ok && record >= 0 && record < len(plan.records) {
			field, exists := plan.records[record].fieldIndex[name]
			return backendGoRecordFieldOperation{storage: backendGoRecordFieldScratch, index: record, field: field}, exists
		}
	}
	ref, ok := plan.ref(value)
	if !ok {
		return backendGoRecordFieldOperation{}, false
	}
	field := -1
	switch ref.kind {
	case backendGoRecordRefArray:
		field = plan.arrays[ref.index].fieldIndex[name]
	case backendGoRecordRefMap:
		field = plan.maps[ref.index].fieldIndex[name]
	case backendGoRecordRefArrayFamily:
		field = plan.families[ref.index].fieldIndex[name]
	case backendGoRecordRefChildRecord:
		field = plan.childRecords[ref.index].fieldIndex[name]
	}
	if field < 0 {
		return backendGoRecordFieldOperation{}, false
	}
	return backendGoRecordFieldOperation{
		storage: backendGoRecordFieldStorage(ref.kind + 1),
		index:   ref.index, field: field, ref: value,
	}, true
}

func backendGoFiniteClosureFieldPointer(field backendGoRecordFieldOperation) (string, bool) {
	switch field.storage {
	case backendGoRecordFieldScratch:
		return fmt.Sprintf("&r%d_%d", field.index, field.field), true
	case backendGoRecordFieldArray:
		return fmt.Sprintf("&ra%d_%d[int(v%d)-1]", field.index, field.field, field.ref), true
	default:
		return "", false
	}
}

func writeBackendGoFiniteClosureDeclarations(
	source *strings.Builder,
	ir *backendProtoIR,
	plan backendGoNumericPlan,
) {
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if _, ok := plan.closureSets.prepare(operation); ok {
			fmt.Fprintf(source, "\tvar ci%d int\n\t_ = ci%d\n", operation.pc, operation.pc)
		}
		set, ok := plan.closureSets.set(operation)
		if !ok {
			continue
		}
		for field, receiver := range set.receiverFields {
			goType, typeOK := backendGoNumericType(receiver.tags)
			if !typeOK {
				continue
			}
			fmt.Fprintf(source, "\tvar cm%d_%d %s\n\t_ = cm%d_%d\n", operation.pc, field, goType, operation.pc, field)
		}
	}
}

func (emitter *backendGoNumericEmitter) emitFiniteClosureSelector(
	operation *backendOperationIR,
	set backendGoFiniteClosureSet,
	definition func(int32) (backendValueID, error),
) error {
	destination, err := definition(operation.a)
	if err != nil {
		return err
	}
	fmt.Fprintf(&emitter.body, "\tswitch v%d {\n", set.key)
	for bucket, variant := range set.buckets {
		fmt.Fprintf(&emitter.body, "\tcase uint32(%d):\n\t\tv%d = %d\n", variant.key, destination, bucket+1)
	}
	emitter.body.WriteString("\tdefault:\n")
	fmt.Fprintf(&emitter.body, "\t\t%s\n\t}\n", emitter.failureReturn())
	return nil
}

func (emitter *backendGoNumericEmitter) emitFiniteClosurePrepare(
	operation *backendOperationIR,
	set backendGoFiniteClosureSet,
	use func(int32) (backendValueID, error),
) error {
	_ = set
	source, err := use(operation.a)
	if err != nil {
		return err
	}
	for _, result := range operation.defs {
		if !emitter.plan.used[result.value-1] {
			continue
		}
		if result.register == operation.a {
			fmt.Fprintf(&emitter.body, "\tv%d = v%d\n", result.value, source)
		} else {
			fmt.Fprintf(&emitter.body, "\tv%d = 0\n", result.value)
		}
	}
	fmt.Fprintf(&emitter.body, "\tci%d = 0\n", operation.pc)
	return nil
}

func (emitter *backendGoNumericEmitter) emitFiniteClosureNext(
	operation *backendOperationIR,
	block *backendBlockIR,
	set backendGoFiniteClosureSet,
	definition func(int32) (backendValueID, error),
	use func(int32) (backendValueID, error),
) error {
	bucket, err := use(operation.b)
	if err != nil {
		return err
	}
	handler, err := definition(operation.a + 1)
	if err != nil {
		return err
	}
	target := emitter.ir.pcToBlock[operation.targetPC]
	fmt.Fprintf(&emitter.body, "\tswitch int(v%d) {\n", bucket)
	for bucketIndex, variant := range set.buckets {
		fmt.Fprintf(&emitter.body, "\tcase %d:\n", bucketIndex+1)
		fmt.Fprintf(&emitter.body, "\t\tif ci%d >= %d {\n", set.preparePC, len(variant.instances))
		emitter.emitGoto(int32(block.id), target, 3)
		emitter.body.WriteString("\t\t}\n")
		fmt.Fprintf(&emitter.body, "\t\tswitch ci%d {\n", set.preparePC)
		for instanceIndex, instance := range variant.instances {
			fmt.Fprintf(&emitter.body, "\t\tcase %d:\n\t\t\tv%d = %d\n", instanceIndex, handler, instance.cellStart+1)
		}
		emitter.body.WriteString("\t\t}\n")
	}
	emitter.body.WriteString("\tdefault:\n")
	fmt.Fprintf(&emitter.body, "\t\t%s\n\t}\n", emitter.failureReturn())
	if control := backendGoDefinitionForRegister(operation, operation.a); control != invalidBackendValueID && emitter.plan.used[control-1] {
		fmt.Fprintf(&emitter.body, "\tv%d = float64(ci%d + 1)\n", control, set.preparePC)
	}
	fmt.Fprintf(&emitter.body, "\tci%d++\n", set.preparePC)
	nextBlock := int32(-1)
	if int(block.last) < len(emitter.ir.ops) {
		nextBlock = emitter.ir.pcToBlock[block.last]
	}
	emitter.emitGoto(int32(block.id), nextBlock, 1)
	return nil
}

func (emitter *backendGoNumericEmitter) emitFiniteClosureCall(
	operation *backendOperationIR,
	set backendGoFiniteClosureSet,
	definition func(int32) (backendValueID, error),
	use func(int32) (backendValueID, error),
) error {
	destination, err := definition(operation.a)
	if err != nil {
		return err
	}
	handler := backendOperationUse(operation, operation.b)
	receiverTables := backendGoNumericReceiverTableCount(set.target.receiverTable, set.target.receiverTables)
	fmt.Fprintf(&emitter.body, "\tswitch int(v%d) {\n", handler)
	for _, bucket := range set.buckets {
		for _, instance := range bucket.instances {
			fmt.Fprintf(&emitter.body, "\tcase %d:\n", instance.cellStart+1)
			for cell := 0; cell < instance.cellCount; cell++ {
				fmt.Fprintf(&emitter.body, "\t\ts%d = c%d\n", instance.cellStart+cell, instance.cellStart+cell)
			}
			for field, receiver := range set.receiverFields {
				pointer, ok := backendGoFiniteClosureFieldPointer(receiver.field)
				if !ok {
					return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported finite closure receiver", operation.pc)
				}
				fmt.Fprintf(&emitter.body, "\t\tcm%d_%d = *%s\n", operation.pc, field, pointer)
			}
			fmt.Fprintf(&emitter.body, "\t\tv%d, ok%d = %s(", destination, operation.pc, set.target.functionName)
			wrote := false
			for cell := 0; cell < instance.cellCount; cell++ {
				if wrote {
					emitter.body.WriteString(", ")
				}
				fmt.Fprintf(&emitter.body, "&s%d", instance.cellStart+cell)
				wrote = true
			}
			for field := range set.receiverFields {
				if wrote {
					emitter.body.WriteString(", ")
				}
				fmt.Fprintf(&emitter.body, "&cm%d_%d", operation.pc, field)
				wrote = true
			}
			for parameter := receiverTables; parameter < int(operation.callArgCount); parameter++ {
				value, useErr := use(operation.callArgStart + int32(parameter))
				if useErr != nil {
					return useErr
				}
				if wrote {
					emitter.body.WriteString(", ")
				}
				fmt.Fprintf(&emitter.body, "v%d", value)
				wrote = true
			}
			emitter.body.WriteString(")\n")
			fmt.Fprintf(&emitter.body, "\t\tif !ok%d {\n", operation.pc)
			emitter.emitReplayEntry(3)
			emitter.body.WriteString("\t\t}\n")
			for cell := 0; cell < instance.cellCount; cell++ {
				fmt.Fprintf(&emitter.body, "\t\tc%d = s%d\n", instance.cellStart+cell, instance.cellStart+cell)
			}
			for field, receiver := range set.receiverFields {
				pointer, _ := backendGoFiniteClosureFieldPointer(receiver.field)
				fmt.Fprintf(&emitter.body, "\t\t*%s = cm%d_%d\n", pointer, operation.pc, field)
			}
		}
	}
	emitter.body.WriteString("\tdefault:\n")
	emitter.emitReplayEntry(2)
	emitter.body.WriteString("\t}\n")
	return nil
}
