package ember

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

const backendGoRecordMapCapacity = 128

type backendGoRecordRefKind uint8

const (
	backendGoRecordRefNone backendGoRecordRefKind = iota
	backendGoRecordRefMap
	backendGoRecordRefArray
	backendGoRecordRefArrayFamily
	backendGoRecordRefChildRecord
)

type backendGoRecordRef struct {
	kind  backendGoRecordRefKind
	index int
}

type backendGoRecord struct {
	root          backendValueID
	fieldNames    []machineStringID
	fieldIndex    map[machineStringID]int
	fieldValues   []backendValueID
	fieldPresent  []bool
	fieldOptional []bool
	fieldTags     []backendTagMask
	setByPC       map[int32]int
	writesByPC    map[int32]int
	storedAtPC    int32
}

type backendGoRecordKey struct {
	value    backendValueID
	constant backendPreparedStringKey
	dynamic  bool
}

type backendGoRecordMapOperation struct {
	index  int
	key    backendGoRecordKey
	record int
}

type backendGoRecordMap struct {
	root        backendValueID
	fieldNames  []machineStringID
	fieldIndex  map[machineStringID]int
	fieldTags   []backendTagMask
	records     []int
	domain      int32
	separator   machineStringID
	recordCount int
}

type backendGoRecordArrayOperation struct {
	index   int
	record  int
	element uint32
}

type backendGoRecordArray struct {
	root         backendValueID
	fieldNames   []machineStringID
	fieldIndex   map[machineStringID]int
	fieldTags    []backendTagMask
	fieldPresent [][]bool
	records      []int
	length       uint32
}

type backendGoRecordArrayFamily struct {
	arrays      []int
	fieldNames  []machineStringID
	fieldIndex  map[machineStringID]int
	fieldTags   []backendTagMask
	parentArray int
	parentField int
}

type backendGoRecordChildArray struct {
	family int
	member int
}

type backendGoRecordChildRecords struct {
	records     []int
	fieldNames  []machineStringID
	fieldIndex  map[machineStringID]int
	fieldTags   backendTagMask
	parentArray int
	parentField int
}

type backendGoRecordChildRecord struct {
	family int
	member int
}

type backendGoRecordFusedGet struct {
	family int
	ref    backendValueID
	member int
	key    backendValueID
}

type backendGoRecordFusedSet struct {
	family int
	ref    backendValueID
	member int
	key    backendValueID
	source backendValueID
}

type backendGoRecordDynamicField struct {
	record int
	key    backendValueID
	source backendValueID
}

type backendGoRecordParentField struct {
	array int
	field int
}

type backendGoRecordScratchField struct {
	record int
	field  int
}

type backendGoRecordFieldStorage uint8

const (
	backendGoRecordFieldScratch backendGoRecordFieldStorage = iota + 1
	backendGoRecordFieldMap
	backendGoRecordFieldArray
	backendGoRecordFieldArrayFamily
	backendGoRecordFieldChildRecord
)

type backendGoRecordFieldOperation struct {
	storage backendGoRecordFieldStorage
	index   int
	field   int
	ref     backendValueID
	source  backendValueID
}

type backendGoRecordTablePlan struct {
	enabled        bool
	rejectReason   string
	roots          []backendValueID
	recordByRoot   map[backendValueID]int
	records        []backendGoRecord
	mapByRoot      map[backendValueID]int
	maps           []backendGoRecordMap
	mapSetByPC     map[int32]backendGoRecordMapOperation
	mapGetByPC     map[int32]backendGoRecordMapOperation
	arrayByRoot    map[backendValueID]int
	arrays         []backendGoRecordArray
	arraySetByPC   map[int32]backendGoRecordArrayOperation
	arrayGetByPC   map[int32]int
	arrayKeyValues map[backendValueID]int
	arrayPreparePC map[int32]int
	arrayNextPC    map[int32]int
	families       []backendGoRecordArrayFamily
	familyByParent map[backendGoRecordParentField]int
	childByScratch map[backendGoRecordScratchField]backendGoRecordChildArray
	childSetByPC   map[int32]backendGoRecordChildArray
	familyValues   map[backendValueID]int
	familyPrepare  map[int32]int
	familyNext     map[int32]int
	childRecords   []backendGoRecordChildRecords
	childByParent  map[backendGoRecordParentField]int
	childRecord    map[backendGoRecordScratchField]backendGoRecordChildRecord
	childRecordSet map[int32]backendGoRecordChildRecord
	fusedGetByPC   map[int32]backendGoRecordFusedGet
	fusedSetByPC   map[int32]backendGoRecordFusedSet
	dynamicGetByPC map[int32]backendGoRecordDynamicField
	dynamicSetByPC map[int32]backendGoRecordDynamicField
	fieldsByPC     map[int32]backendGoRecordFieldOperation
	refs           map[backendValueID]backendGoRecordRef
	iteratorValues []bool
	iteratorArray  []int
	scalarValues   []bool
}

func analyzeBackendGoRecordTables(
	ir *backendProtoIR,
	keys backendGoStructuralKeyPlan,
) backendGoRecordTablePlan {
	if ir == nil {
		return backendGoRecordTablePlan{}
	}
	plan := backendGoRecordTablePlan{
		roots:          make([]backendValueID, len(ir.values)),
		recordByRoot:   make(map[backendValueID]int),
		mapByRoot:      make(map[backendValueID]int),
		mapSetByPC:     make(map[int32]backendGoRecordMapOperation),
		mapGetByPC:     make(map[int32]backendGoRecordMapOperation),
		arrayByRoot:    make(map[backendValueID]int),
		arraySetByPC:   make(map[int32]backendGoRecordArrayOperation),
		arrayGetByPC:   make(map[int32]int),
		arrayKeyValues: make(map[backendValueID]int),
		arrayPreparePC: make(map[int32]int),
		arrayNextPC:    make(map[int32]int),
		familyByParent: make(map[backendGoRecordParentField]int),
		childByScratch: make(map[backendGoRecordScratchField]backendGoRecordChildArray),
		childSetByPC:   make(map[int32]backendGoRecordChildArray),
		familyValues:   make(map[backendValueID]int),
		familyPrepare:  make(map[int32]int),
		familyNext:     make(map[int32]int),
		childByParent:  make(map[backendGoRecordParentField]int),
		childRecord:    make(map[backendGoRecordScratchField]backendGoRecordChildRecord),
		childRecordSet: make(map[int32]backendGoRecordChildRecord),
		fusedGetByPC:   make(map[int32]backendGoRecordFusedGet),
		fusedSetByPC:   make(map[int32]backendGoRecordFusedSet),
		dynamicGetByPC: make(map[int32]backendGoRecordDynamicField),
		dynamicSetByPC: make(map[int32]backendGoRecordDynamicField),
		fieldsByPC:     make(map[int32]backendGoRecordFieldOperation),
		refs:           make(map[backendValueID]backendGoRecordRef),
		iteratorValues: make([]bool, len(ir.values)),
		iteratorArray:  make([]int, len(ir.values)),
		scalarValues:   make([]bool, len(ir.values)),
	}
	for valueIndex := range plan.iteratorArray {
		plan.iteratorArray[valueIndex] = -1
	}
	for valueIndex := range ir.values {
		value := &ir.values[valueIndex]
		if value.object != backendObjectTable || len(value.origins) != 1 {
			continue
		}
		root := value.origins[0]
		if backendGoNewTableRoot(ir, root) {
			plan.roots[valueIndex] = root
		}
	}
	if !plan.propagateRoots(ir) {
		plan.rejectReason = "root propagation"
		return plan
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opSetStringField {
			continue
		}
		root := plan.root(backendOperationUse(operation, operation.a))
		name, ok := backendGoStringFieldName(ir, operation.access.constant)
		if root == invalidBackendValueID || !ok {
			continue
		}
		recordIndex, exists := plan.recordByRoot[root]
		if !exists {
			recordIndex = len(plan.records)
			plan.recordByRoot[root] = recordIndex
			plan.records = append(plan.records, backendGoRecord{
				root:       root,
				fieldIndex: make(map[machineStringID]int),
				setByPC:    make(map[int32]int),
				writesByPC: make(map[int32]int),
				storedAtPC: -1,
			})
		}
		record := &plan.records[recordIndex]
		if field, duplicate := record.fieldIndex[name]; duplicate {
			record.writesByPC[operation.pc] = field
			continue
		}
		field := len(record.fieldNames)
		record.fieldNames = append(record.fieldNames, name)
		record.fieldValues = append(
			record.fieldValues,
			backendOperationUse(operation, operation.c),
		)
		record.fieldPresent = append(record.fieldPresent, true)
		record.fieldOptional = append(record.fieldOptional, false)
		record.fieldIndex[name] = field
		record.setByPC[operation.pc] = field
		record.writesByPC[operation.pc] = field
	}
	if len(plan.records) == 0 {
		plan.rejectReason = "no records"
		return plan
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		switch operation.op {
		case opSetIndex:
			table := plan.root(backendOperationUse(operation, operation.a))
			recordRoot := plan.root(backendOperationUse(operation, operation.c))
			record, recordOK := plan.recordByRoot[recordRoot]
			if table == invalidBackendValueID || !recordOK {
				continue
			}
			mapIndex := plan.ensureMap(table)
			plan.mapSetByPC[operation.pc] = backendGoRecordMapOperation{
				index: mapIndex, record: record,
			}
			if plan.records[record].storedAtPC >= 0 &&
				plan.records[record].storedAtPC != operation.pc {
				plan.rejectReason = "record has multiple container stores"
				return plan
			}
			plan.records[record].storedAtPC = operation.pc
			plan.maps[mapIndex].recordCount++
		case opSetField:
			table := plan.root(backendOperationUse(operation, operation.a))
			recordRoot := plan.root(backendOperationUse(operation, operation.c))
			record, recordOK := plan.recordByRoot[recordRoot]
			element, elementOK := backendGoArrayIndexConstant(ir, operation.access.constant)
			if table == invalidBackendValueID || !recordOK || !elementOK {
				continue
			}
			arrayIndex := plan.ensureArray(table)
			if element > plan.arrays[arrayIndex].length {
				plan.arrays[arrayIndex].length = element
			}
			plan.arraySetByPC[operation.pc] = backendGoRecordArrayOperation{
				index: arrayIndex, record: record, element: element,
			}
			if plan.records[record].storedAtPC >= 0 &&
				plan.records[record].storedAtPC != operation.pc {
				plan.rejectReason = "record has multiple container stores"
				return plan
			}
			plan.records[record].storedAtPC = operation.pc
		}
	}
	plan.discoverEmptyChildArrays(ir)
	if len(plan.maps) == 0 && len(plan.arrays) == 0 {
		plan.rejectReason = "no record containers"
		return plan
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		switch operation.op {
		case opGetIndex:
			table := plan.root(backendOperationUse(operation, operation.b))
			if len(operation.defs) != 1 {
				continue
			}
			if mapIndex, ok := plan.mapByRoot[table]; ok {
				plan.mapGetByPC[operation.pc] = backendGoRecordMapOperation{index: mapIndex}
				plan.refs[operation.defs[0].value] = backendGoRecordRef{
					kind: backendGoRecordRefMap, index: mapIndex,
				}
				continue
			}
			if arrayIndex, ok := plan.arrayByRoot[table]; ok {
				plan.arrayGetByPC[operation.pc] = arrayIndex
				plan.refs[operation.defs[0].value] = backendGoRecordRef{
					kind: backendGoRecordRefArray, index: arrayIndex,
				}
			}
		case opPrepareIter:
			table := plan.root(backendOperationUse(operation, operation.a))
			arrayIndex, ok := plan.arrayByRoot[table]
			if !ok {
				continue
			}
			plan.arrayPreparePC[operation.pc] = arrayIndex
			for _, definition := range operation.defs {
				if definition.register == operation.a ||
					definition.register == operation.b ||
					definition.register == operation.c {
					plan.iteratorValues[definition.value-1] = true
					plan.iteratorArray[definition.value-1] = arrayIndex
				}
			}
		}
	}
	if !plan.finishShapesAndKeys(ir, keys) {
		plan.rejectReason = "shapes or keys"
		return plan
	}
	if !plan.discoverChildArrayFamilies(ir) {
		plan.rejectReason = "child-array families"
		return plan
	}
	if !plan.discoverChildRecordFamilies() {
		plan.rejectReason = "child-record families"
		return plan
	}
	if !plan.analyzeIterators(ir) {
		plan.rejectReason = "iterator analysis"
		return plan
	}
	if !plan.propagateRefsAndIterators(ir) {
		plan.rejectReason = "reference propagation"
		return plan
	}
	if !plan.discoverChildRecordValues(ir) || !plan.propagateRefsAndIterators(ir) {
		plan.rejectReason = "child-record value propagation"
		return plan
	}
	if !plan.discoverFamilyValues(ir) || !plan.propagateFamilyValues(ir) {
		plan.rejectReason = "child-array value propagation"
		return plan
	}
	if !plan.analyzeFamilyIterators(ir) {
		plan.rejectReason = "child-array iterator analysis"
		return plan
	}
	if !plan.propagateRefsAndIterators(ir) {
		plan.rejectReason = "child-array reference propagation"
		return plan
	}
	if !plan.classifyFusedGets(ir) {
		plan.rejectReason = "fused child-record lookup"
		return plan
	}
	if !plan.classifyFusedSets(ir) {
		plan.rejectReason = "fused child-record mutation"
		return plan
	}
	if !plan.expandFusedChildRecordDomains(ir) {
		plan.rejectReason = "fused child-record key domain"
		return plan
	}
	if !plan.propagateArrayKeys(ir) {
		plan.rejectReason = "array-key propagation"
		return plan
	}
	plan.retainObservedArrayKeys(ir)
	if !plan.classifyFields(ir) {
		if plan.rejectReason == "" {
			plan.rejectReason = "field classification"
		}
		return plan
	}
	if !plan.classifyDynamicFields(ir) {
		if plan.rejectReason == "" {
			plan.rejectReason = "dynamic field classification"
		}
		return plan
	}
	if !plan.validateUses(ir) {
		if plan.rejectReason == "" {
			plan.rejectReason = "use validation"
		}
		return plan
	}
	for valueIndex, root := range plan.roots {
		if plan.ownsRoot(root) {
			plan.scalarValues[valueIndex] = true
		}
	}
	for valueIndex, iterator := range plan.iteratorValues {
		if _, key := plan.arrayKeyValues[backendValueID(valueIndex+1)]; iterator && !key {
			plan.scalarValues[valueIndex] = true
		}
	}
	for _, operation := range plan.mapSetByPC {
		if operation.key.dynamic {
			continue
		}
		plan.scalarValues[operation.key.value-1] = true
	}
	for _, operation := range plan.mapGetByPC {
		if operation.key.dynamic {
			continue
		}
		plan.scalarValues[operation.key.value-1] = true
	}
	plan.enabled = true
	return plan
}

func (plan *backendGoRecordTablePlan) analyzeIterators(ir *backendProtoIR) bool {
	if !plan.propagateIteratorValues(ir) {
		return false
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
			plan.iteratorArray[table-1] < 0 ||
			plan.iteratorArray[table-1] != plan.iteratorArray[control-1] {
			continue
		}
		arrayIndex := plan.iteratorArray[control-1]
		plan.arrayNextPC[operation.pc] = arrayIndex
		for _, definition := range operation.defs {
			switch definition.register {
			case operation.a:
				if !plan.iteratorValues[definition.value-1] {
					return false
				}
				plan.arrayKeyValues[definition.value] = arrayIndex
			case operation.a + 1:
				plan.refs[definition.value] = backendGoRecordRef{
					kind: backendGoRecordRefArray, index: arrayIndex,
				}
			}
		}
	}
	return len(plan.arrayNextPC) != 0 || len(plan.arrayGetByPC) != 0
}

func (plan *backendGoRecordTablePlan) propagateIteratorValues(ir *backendProtoIR) bool {
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
				if plan.iteratorArray[next-1] != plan.iteratorArray[id-1] {
					return false
				}
				continue
			}
			plan.iteratorValues[next-1] = true
			plan.iteratorArray[next-1] = plan.iteratorArray[id-1]
			queue = append(queue, next)
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) discoverFamilyValues(ir *backendProtoIR) bool {
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opGetStringField || len(operation.defs) == 0 {
			continue
		}
		base := backendOperationUse(operation, operation.b)
		ref, ok := plan.refs[base]
		if !ok || ref.kind != backendGoRecordRefArray ||
			ref.index < 0 || ref.index >= len(plan.arrays) {
			continue
		}
		name, ok := backendGoStringFieldName(ir, operation.access.constant)
		if !ok {
			return false
		}
		field, ok := plan.arrays[ref.index].fieldIndex[name]
		if !ok {
			continue
		}
		family, ok := plan.familyByParent[backendGoRecordParentField{
			array: ref.index,
			field: field,
		}]
		if !ok {
			continue
		}
		for _, definition := range operation.defs {
			if current, exists := plan.familyValues[definition.value]; exists && current != family {
				return false
			}
			plan.familyValues[definition.value] = family
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) discoverChildRecordValues(ir *backendProtoIR) bool {
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opGetStringField || len(operation.defs) == 0 {
			continue
		}
		base := backendOperationUse(operation, operation.b)
		name, ok := backendGoStringFieldName(ir, operation.access.constant)
		if !ok {
			return false
		}
		family := -1
		if recordIndex, exists := plan.recordByRoot[plan.root(base)]; exists {
			field, exists := plan.records[recordIndex].fieldIndex[name]
			if exists {
				if child, childExists := plan.childRecord[backendGoRecordScratchField{
					record: recordIndex,
					field:  field,
				}]; childExists {
					family = child.family
				}
			}
		} else if ref, exists := plan.refs[base]; exists &&
			ref.kind == backendGoRecordRefArray &&
			ref.index >= 0 && ref.index < len(plan.arrays) {
			field, fieldExists := plan.arrays[ref.index].fieldIndex[name]
			if fieldExists {
				if child, childExists := plan.childByParent[backendGoRecordParentField{
					array: ref.index,
					field: field,
				}]; childExists {
					family = child
				}
			}
		}
		if family < 0 {
			continue
		}
		for _, definition := range operation.defs {
			child := backendGoRecordRef{kind: backendGoRecordRefChildRecord, index: family}
			if current, exists := plan.refs[definition.value]; exists && current != child {
				return false
			}
			plan.refs[definition.value] = child
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) propagateFamilyValues(ir *backendProtoIR) bool {
	for iteration := 0; iteration <= len(ir.values); iteration++ {
		changed := false
		for valueIndex := range ir.values {
			value := &ir.values[valueIndex]
			if _, ok := plan.familyValues[value.id]; ok {
				continue
			}
			var origins []backendValueID
			switch value.kind {
			case backendValuePhi:
				block := &ir.blocks[value.block]
				origins = block.phis[value.register].inputs
			case backendValueOperation:
				if value.pc < 0 || int(value.pc) >= len(ir.ops) || ir.ops[value.pc].op != opMove {
					continue
				}
				origins = []backendValueID{
					backendOperationUse(&ir.ops[value.pc], ir.ops[value.pc].b),
				}
			default:
				continue
			}
			family := -1
			found := false
			all := len(origins) != 0
			for _, origin := range origins {
				if origin == value.id {
					continue
				}
				candidate, ok := plan.familyValues[origin]
				if ok {
					if found && candidate != family {
						return false
					}
					family = candidate
					found = true
				} else if value.kind != backendValuePhi ||
					!backendGoRecordRefAliasCandidate(ir, origin, value.register) {
					all = false
				}
			}
			if found && all {
				plan.familyValues[value.id] = family
				changed = true
			}
		}
		if !changed {
			return true
		}
	}
	return false
}

func (plan *backendGoRecordTablePlan) analyzeFamilyIterators(ir *backendProtoIR) bool {
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opPrepareIter {
			continue
		}
		source := backendOperationUse(operation, operation.a)
		family, ok := plan.familyValues[source]
		if !ok || family < 0 || family >= len(plan.families) {
			continue
		}
		encoded := -family - 2
		plan.familyPrepare[operation.pc] = family
		for _, definition := range operation.defs {
			if definition.register != operation.a &&
				definition.register != operation.b &&
				definition.register != operation.c {
				continue
			}
			if plan.iteratorValues[definition.value-1] &&
				plan.iteratorArray[definition.value-1] != encoded {
				return false
			}
			plan.iteratorValues[definition.value-1] = true
			plan.iteratorArray[definition.value-1] = encoded
		}
	}
	if !plan.propagateIteratorValues(ir) {
		return false
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opArrayNextJump2 {
			continue
		}
		table := backendOperationUse(operation, operation.b)
		control := backendOperationUse(operation, operation.c)
		if !plan.iteratorValue(table) || !plan.iteratorValue(control) ||
			plan.iteratorArray[table-1] >= -1 ||
			plan.iteratorArray[table-1] != plan.iteratorArray[control-1] {
			continue
		}
		family := -plan.iteratorArray[control-1] - 2
		if family < 0 || family >= len(plan.families) {
			return false
		}
		plan.familyNext[operation.pc] = family
		for _, definition := range operation.defs {
			switch definition.register {
			case operation.a:
				if !plan.iteratorValues[definition.value-1] {
					return false
				}
				plan.arrayKeyValues[definition.value] = -family - 2
			case operation.a + 1:
				plan.refs[definition.value] = backendGoRecordRef{
					kind:  backendGoRecordRefArrayFamily,
					index: family,
				}
			}
		}
	}
	return len(plan.familyPrepare) == len(plan.familyNext)
}

func (plan *backendGoRecordTablePlan) propagateArrayKeys(ir *backendProtoIR) bool {
	for iteration := 0; iteration <= len(ir.values); iteration++ {
		changed := false
		for valueIndex := range ir.values {
			value := &ir.values[valueIndex]
			if _, ok := plan.arrayKeyValues[value.id]; ok {
				continue
			}
			var origins []backendValueID
			switch value.kind {
			case backendValuePhi:
				block := &ir.blocks[value.block]
				origins = block.phis[value.register].inputs
			case backendValueOperation:
				if value.pc < 0 || int(value.pc) >= len(ir.ops) ||
					ir.ops[value.pc].op != opMove {
					continue
				}
				origins = []backendValueID{
					backendOperationUse(&ir.ops[value.pc], ir.ops[value.pc].b),
				}
			default:
				continue
			}
			arrayIndex := -1
			foundKey := false
			allKeys := len(origins) != 0
			for _, origin := range origins {
				if origin == value.id {
					continue
				}
				candidate, ok := plan.arrayKeyValues[origin]
				if ok {
					if foundKey && candidate != arrayIndex {
						return false
					}
					arrayIndex = candidate
					foundKey = true
				} else if value.kind != backendValuePhi ||
					!backendGoRecordRefAliasCandidate(ir, origin, value.register) {
					allKeys = false
				}
			}
			if foundKey && allKeys {
				plan.arrayKeyValues[value.id] = arrayIndex
				changed = true
			}
		}
		if !changed {
			return true
		}
	}
	return false
}

func (plan *backendGoRecordTablePlan) retainObservedArrayKeys(ir *backendProtoIR) {
	adjacent := make(map[backendValueID][]backendValueID, len(plan.arrayKeyValues))
	observed := make(map[backendValueID]bool, len(plan.arrayKeyValues))
	queue := make([]backendValueID, 0, len(plan.arrayKeyValues))
	observe := func(id backendValueID) {
		if _, ok := plan.arrayKeyValues[id]; !ok || observed[id] {
			return
		}
		observed[id] = true
		queue = append(queue, id)
	}
	connect := func(left, right backendValueID) {
		if _, ok := plan.arrayKeyValues[left]; !ok {
			return
		}
		if _, ok := plan.arrayKeyValues[right]; !ok {
			return
		}
		adjacent[left] = append(adjacent[left], right)
		adjacent[right] = append(adjacent[right], left)
	}
	for valueIndex := range ir.values {
		value := &ir.values[valueIndex]
		switch value.kind {
		case backendValuePhi:
			block := &ir.blocks[value.block]
			for _, input := range block.phis[value.register].inputs {
				if _, valueIsKey := plan.arrayKeyValues[value.id]; valueIsKey {
					connect(value.id, input)
				} else if !plan.iteratorValue(value.id) {
					observe(input)
				}
			}
		case backendValueOperation:
			if value.pc >= 0 && int(value.pc) < len(ir.ops) && ir.ops[value.pc].op == opMove {
				source := backendOperationUse(&ir.ops[value.pc], ir.ops[value.pc].b)
				if _, valueIsKey := plan.arrayKeyValues[value.id]; valueIsKey {
					connect(value.id, source)
				} else if !plan.iteratorValue(value.id) {
					observe(source)
				}
			}
		}
	}

	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op == opMove || operation.op == opArrayNextJump2 {
			continue
		}
		for _, use := range operation.uses {
			observe(use.value)
		}
	}
	for len(queue) != 0 {
		id := queue[0]
		queue = queue[1:]
		for _, next := range adjacent[id] {
			if observed[next] {
				continue
			}
			observed[next] = true
			queue = append(queue, next)
		}
	}
	for id := range plan.arrayKeyValues {
		if !observed[id] {
			delete(plan.arrayKeyValues, id)
		}
	}
}

func (plan *backendGoRecordTablePlan) propagateRoots(ir *backendProtoIR) bool {
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
	for iteration := 0; iteration <= len(ir.values); iteration++ {
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
			found := false
			for inputIndex, input := range phi.inputs {
				if !ir.blocks[block.predecessors[inputIndex]].reachable || input == value.id {
					continue
				}
				candidate := plan.root(input)
				if candidate == invalidBackendValueID {
					ready = false
					break
				}
				if found && candidate != root {
					return false
				}
				root = candidate
				found = true
			}
			if ready && found {
				updated, ok := setRoot(value.id, root)
				if !ok {
					return false
				}
				changed = changed || updated
			}
		}
		for pc := range ir.ops {
			operation := &ir.ops[pc]
			if operation.op != opMove && operation.op != opPrepareIter {
				continue
			}
			sourceRegister := operation.b
			if operation.op == opPrepareIter {
				sourceRegister = operation.a
			}
			root := plan.root(backendOperationUse(operation, sourceRegister))
			for _, definition := range operation.defs {
				if operation.op == opPrepareIter && definition.register != operation.a {
					continue
				}
				updated, ok := setRoot(definition.value, root)
				if !ok {
					return false
				}
				changed = changed || updated
			}
		}
		if !changed {
			return true
		}
	}
	return false
}

func (plan *backendGoRecordTablePlan) propagateRefsAndIterators(ir *backendProtoIR) bool {
	for iteration := 0; iteration <= len(ir.values); iteration++ {
		changed := false
		for valueIndex := range ir.values {
			value := &ir.values[valueIndex]
			var origins []backendValueID
			switch value.kind {
			case backendValuePhi:
				block := &ir.blocks[value.block]
				origins = block.phis[value.register].inputs
			case backendValueOperation:
				if value.pc < 0 || int(value.pc) >= len(ir.ops) ||
					ir.ops[value.pc].op != opMove {
					continue
				}
				origins = []backendValueID{
					backendOperationUse(&ir.ops[value.pc], ir.ops[value.pc].b),
				}
			default:
				continue
			}
			var ref backendGoRecordRef
			foundRef := false
			allRef := len(origins) != 0
			for _, origin := range origins {
				if origin == value.id {
					continue
				}
				candidate, ok := plan.refs[origin]
				if ok {
					if foundRef && candidate != ref {
						return false
					}
					ref = candidate
					foundRef = true
				} else {
					if value.kind != backendValuePhi ||
						!backendGoRecordRefAliasCandidate(ir, origin, value.register) {
						allRef = false
					}
				}
			}
			if foundRef && allRef {
				if current, exists := plan.refs[value.id]; exists && current != ref {
					return false
				} else if !exists {
					plan.refs[value.id] = ref
					changed = true
				}
			}
		}
		if !changed {
			return true
		}
	}
	return false
}

func backendGoRecordRefAliasCandidate(
	ir *backendProtoIR,
	id backendValueID,
	register int32,
) bool {
	if !ir.validBackendValue(id) {
		return false
	}
	value := &ir.values[id-1]
	if value.register != register {
		return false
	}
	if value.kind == backendValuePhi {
		return true
	}
	return value.kind == backendValueOperation &&
		value.pc >= 0 &&
		int(value.pc) < len(ir.ops) &&
		ir.ops[value.pc].op == opMove
}

func (plan *backendGoRecordTablePlan) finishShapesAndKeys(
	ir *backendProtoIR,
	keys backendGoStructuralKeyPlan,
) bool {
	recordUses := make([]int, len(plan.records))
	for mapIndex := range plan.maps {
		recordIndexes := make([]int, 0)
		for pc, operation := range plan.mapSetByPC {
			if operation.index != mapIndex {
				continue
			}
			if !plan.recordInitializedBefore(ir, operation.record, &ir.ops[pc]) {
				return false
			}
			recordIndexes = append(recordIndexes, operation.record)
			recordUses[operation.record]++
			key, ok := plan.recordKey(ir, keys, backendOperationUse(&ir.ops[pc], ir.ops[pc].b))
			if !ok {
				return false
			}
			operation.key = key
			plan.mapSetByPC[pc] = operation
			if key.dynamic {
				structural, _ := keys.key(key.value)
				if !plan.setMapDomain(mapIndex, structural) {
					return false
				}
			}
		}
		for pc, operation := range plan.mapGetByPC {
			if operation.index != mapIndex {
				continue
			}
			key, ok := plan.recordKey(ir, keys, backendOperationUse(&ir.ops[pc], ir.ops[pc].c))
			if !ok {
				return false
			}
			operation.key = key
			plan.mapGetByPC[pc] = operation
			if key.dynamic {
				structural, _ := keys.key(key.value)
				if !plan.setMapDomain(mapIndex, structural) {
					return false
				}
			}
		}
		current := &plan.maps[mapIndex]
		if current.domain == 0 || current.separator == invalidMachineStringID ||
			!plan.setShapeFromRecords(&current.fieldNames, &current.fieldIndex, recordIndexes) {
			return false
		}
		current.records = append([]int(nil), recordIndexes...)
		for pc, operation := range plan.mapSetByPC {
			if operation.index != mapIndex || operation.key.dynamic {
				continue
			}
			key, ok := backendGoParseRecordKey(ir, operation.key.value, current.separator)
			if !ok {
				return false
			}
			operation.key.constant = key
			plan.mapSetByPC[pc] = operation
		}
		for pc, operation := range plan.mapGetByPC {
			if operation.index != mapIndex || operation.key.dynamic {
				continue
			}
			key, ok := backendGoParseRecordKey(ir, operation.key.value, current.separator)
			if !ok {
				return false
			}
			operation.key.constant = key
			plan.mapGetByPC[pc] = operation
		}
	}
	for arrayIndex := range plan.arrays {
		current := &plan.arrays[arrayIndex]
		if current.length > 32 {
			return false
		}
		if current.length == 0 {
			continue
		}
		recordIndexes := make([]int, current.length)
		present := make([]bool, current.length)
		for pc, operation := range plan.arraySetByPC {
			if operation.index != arrayIndex || operation.element == 0 ||
				operation.element > current.length || present[operation.element-1] {
				continue
			}
			if !plan.recordInitializedBefore(ir, operation.record, &ir.ops[pc]) {
				return false
			}
			recordIndexes[operation.element-1] = operation.record
			recordUses[operation.record]++
			present[operation.element-1] = true
		}
		for _, ok := range present {
			if !ok {
				return false
			}
		}
		if !plan.setArrayShapeFromRecords(current, recordIndexes) {
			return false
		}
		current.records = append([]int(nil), recordIndexes...)
	}
	for recordIndex, uses := range recordUses {
		if uses > 1 ||
			uses == 0 && plan.records[recordIndex].storedAtPC >= 0 ||
			uses == 1 && plan.records[recordIndex].storedAtPC < 0 {
			return false
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) recordInitializedBefore(
	ir *backendProtoIR,
	recordIndex int,
	consumer *backendOperationIR,
) bool {
	if recordIndex < 0 || recordIndex >= len(plan.records) {
		return false
	}
	record := plan.records[recordIndex]
	if len(record.fieldNames) == 0 || len(record.setByPC) != len(record.fieldNames) {
		return false
	}
	for pc := range record.writesByPC {
		if pc < 0 || int(pc) >= len(ir.ops) ||
			!backendGoScalarTableOperationDominates(ir, &ir.ops[pc], consumer) {
			return false
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) classifyFields(ir *backendProtoIR) bool {
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opGetStringField && operation.op != opSetStringField {
			continue
		}
		baseRegister := operation.b
		if operation.op == opSetStringField {
			baseRegister = operation.a
		}
		base := backendOperationUse(operation, baseRegister)
		name, ok := backendGoStringFieldName(ir, operation.access.constant)
		if !ok {
			plan.rejectReason = "invalid field name at PC " + strconv.Itoa(int(operation.pc))
			return false
		}
		if record, exists := plan.recordByRoot[plan.root(base)]; exists {
			field, exists := plan.records[record].fieldIndex[name]
			if !exists {
				plan.rejectReason = "record field mismatch at PC " + strconv.Itoa(int(operation.pc))
				return false
			}
			initialField, initial := plan.records[record].setByPC[operation.pc]
			if !initial || initialField != field {
				initialPC := int32(-1)
				for pc, candidate := range plan.records[record].setByPC {
					if candidate == field {
						initialPC = pc
						break
					}
				}
				if initialPC < 0 ||
					!backendGoScalarTableOperationDominates(ir, &ir.ops[initialPC], operation) {
					plan.rejectReason = "record field used before initialization at PC " + strconv.Itoa(int(operation.pc))
					return false
				}
				storedAtPC := plan.records[record].storedAtPC
				if storedAtPC >= 0 &&
					!backendGoScalarTableOperationDominates(ir, operation, &ir.ops[storedAtPC]) {
					plan.rejectReason = "stored record root remains live at PC " + strconv.Itoa(int(operation.pc))
					return false
				}
			}
			key := backendGoRecordScratchField{
				record: record,
				field:  field,
			}
			_, childArray := plan.childByScratch[key]
			childRecordRef, childRecord := plan.childRecord[key]
			if operation.op == opSetStringField && (childArray || childRecord) {
				if _, initial := plan.childSetByPC[operation.pc]; !initial {
					if _, initial = plan.childRecordSet[operation.pc]; !initial {
						plan.rejectReason = "child-table identity changes at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				}
			}
			if operation.op == opGetStringField && childRecord {
				for _, definition := range operation.defs {
					current, exists := plan.refs[definition.value]
					if !exists || current.kind != backendGoRecordRefChildRecord ||
						current.index != childRecordRef.family {
						plan.rejectReason = "child-record identity is not preserved at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				}
			}
			source := invalidBackendValueID
			if operation.op == opSetStringField {
				source = backendOperationUse(operation, operation.c)
			}
			plan.fieldsByPC[operation.pc] = backendGoRecordFieldOperation{
				storage: backendGoRecordFieldScratch,
				index:   record,
				field:   field,
				source:  source,
			}
			continue
		}
		ref, exists := plan.refs[base]
		if !exists {
			plan.rejectReason = "field base has no record reference at PC " + strconv.Itoa(int(operation.pc))
			return false
		}
		field := -1
		switch ref.kind {
		case backendGoRecordRefMap:
			field = plan.maps[ref.index].fieldIndex[name]
		case backendGoRecordRefArray:
			field = plan.arrays[ref.index].fieldIndex[name]
		case backendGoRecordRefArrayFamily:
			field = plan.families[ref.index].fieldIndex[name]
		case backendGoRecordRefChildRecord:
			field = plan.childRecords[ref.index].fieldIndex[name]
		}
		if field < 0 {
			plan.rejectReason = "record reference field is missing at PC " + strconv.Itoa(int(operation.pc))
			return false
		}
		source := invalidBackendValueID
		if operation.op == opSetStringField {
			source = backendOperationUse(operation, operation.c)
		}
		plan.fieldsByPC[operation.pc] = backendGoRecordFieldOperation{
			storage: backendGoRecordFieldStorage(ref.kind + 1),
			index:   ref.index,
			field:   field,
			ref:     base,
			source:  source,
		}
		if operation.op == opGetStringField && ref.kind == backendGoRecordRefArray {
			if child, ok := plan.childByParent[backendGoRecordParentField{
				array: ref.index,
				field: field,
			}]; ok {
				for _, definition := range operation.defs {
					if current, exists := plan.refs[definition.value]; !exists ||
						current.kind != backendGoRecordRefChildRecord || current.index != child {
						plan.rejectReason = "child-record identity is not preserved at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				}
			}
			if family, ok := plan.familyByParent[backendGoRecordParentField{
				array: ref.index,
				field: field,
			}]; ok {
				for _, definition := range operation.defs {
					if current, exists := plan.familyValues[definition.value]; !exists || current != family {
						plan.rejectReason = "child-array identity is not preserved at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				}
			}
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) classifyFusedGets(ir *backendProtoIR) bool {
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opGetStringFieldIndex || len(operation.defs) != 1 {
			continue
		}
		base := backendOperationUse(operation, operation.b)
		name, ok := backendGoStringFieldName(ir, operation.access.constant)
		if !ok {
			return false
		}
		family, ref, member, ok := plan.fusedChildRecordBase(base, name)
		if !ok {
			continue
		}
		key := backendOperationUse(operation, operation.d)
		if key == invalidBackendValueID {
			return false
		}
		plan.fusedGetByPC[operation.pc] = backendGoRecordFusedGet{
			family: family,
			ref:    ref,
			member: member,
			key:    key,
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) classifyFusedSets(ir *backendProtoIR) bool {
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opSetStringFieldIndex {
			continue
		}
		base := backendOperationUse(operation, operation.a)
		name, ok := backendGoStringFieldName(ir, operation.access.constant)
		if !ok {
			return false
		}
		family, ref, member, ok := plan.fusedChildRecordBase(base, name)
		if !ok {
			continue
		}
		key := backendOperationUse(operation, operation.c)
		source := backendOperationUse(operation, operation.d)
		if key == invalidBackendValueID || source == invalidBackendValueID {
			return false
		}
		plan.fusedSetByPC[operation.pc] = backendGoRecordFusedSet{
			family: family,
			ref:    ref,
			member: member,
			key:    key,
			source: source,
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) fusedChildRecordBase(
	base backendValueID,
	name machineStringID,
) (family int, ref backendValueID, member int, ok bool) {
	if recordIndex, exists := plan.recordByRoot[plan.root(base)]; exists {
		field, fieldExists := plan.records[recordIndex].fieldIndex[name]
		if !fieldExists {
			return -1, invalidBackendValueID, -1, false
		}
		child, childExists := plan.childRecord[backendGoRecordScratchField{
			record: recordIndex,
			field:  field,
		}]
		if !childExists {
			return -1, invalidBackendValueID, -1, false
		}
		return child.family, invalidBackendValueID, child.member, true
	}
	parent, exists := plan.refs[base]
	if !exists || parent.kind != backendGoRecordRefArray ||
		parent.index < 0 || parent.index >= len(plan.arrays) {
		return -1, invalidBackendValueID, -1, false
	}
	field, fieldExists := plan.arrays[parent.index].fieldIndex[name]
	if !fieldExists {
		return -1, invalidBackendValueID, -1, false
	}
	child, childExists := plan.childByParent[backendGoRecordParentField{
		array: parent.index,
		field: field,
	}]
	if !childExists {
		return -1, invalidBackendValueID, -1, false
	}
	return child, base, -1, true
}

func (plan *backendGoRecordTablePlan) expandFusedChildRecordDomains(ir *backendProtoIR) bool {
	expand := func(familyIndex int, key backendValueID) bool {
		if familyIndex < 0 || familyIndex >= len(plan.childRecords) {
			return false
		}
		domain, ok := plan.finiteStringDomain(ir, key, make(map[backendValueID]bool))
		if !ok || len(domain) == 0 {
			return true
		}
		family := &plan.childRecords[familyIndex]
		for _, name := range domain {
			if _, exists := family.fieldIndex[name]; !exists {
				family.fieldIndex[name] = len(family.fieldNames)
				family.fieldNames = append(family.fieldNames, name)
			}
			for _, recordIndex := range family.records {
				field := plan.ensureRecordField(recordIndex, name)
				if field < 0 {
					return false
				}
				plan.records[recordIndex].fieldOptional[field] = true
			}
		}
		return true
	}
	for _, fused := range plan.fusedGetByPC {
		if !expand(fused.family, fused.key) {
			return false
		}
	}
	for _, fused := range plan.fusedSetByPC {
		if !expand(fused.family, fused.key) {
			return false
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) finiteStringDomain(
	ir *backendProtoIR,
	id backendValueID,
	visiting map[backendValueID]bool,
) ([]machineStringID, bool) {
	if !ir.validBackendValue(id) || visiting[id] {
		return nil, false
	}
	if static, ok := backendGoStaticStringValueID(ir, id); ok {
		return []machineStringID{static}, true
	}
	value := &ir.values[id-1]
	visiting[id] = true
	defer delete(visiting, id)
	var sources []backendValueID
	switch value.kind {
	case backendValuePhi:
		block := &ir.blocks[value.block]
		phi := block.phis[value.register]
		for inputIndex, input := range phi.inputs {
			if ir.blocks[block.predecessors[inputIndex]].reachable && input != id {
				sources = append(sources, input)
			}
		}
	case backendValueOperation:
		if value.pc < 0 || int(value.pc) >= len(ir.ops) {
			return nil, false
		}
		operation := &ir.ops[value.pc]
		switch operation.op {
		case opLoadConst:
			if operation.b >= 0 && int(operation.b) < len(ir.constants) &&
				ir.constants[operation.b].kind == NilKind {
				return nil, true
			}
			return nil, false
		case opMove:
			sources = append(sources, backendOperationUse(operation, operation.b))
		case opGetStringField:
			name, ok := backendGoStringFieldName(ir, operation.access.constant)
			if !ok {
				return nil, false
			}
			sources, ok = plan.recordFieldSources(backendOperationUse(operation, operation.b), name)
			if !ok {
				return nil, false
			}
		case opGetIndex:
			var ok bool
			sources, ok = plan.fixedScalarArraySources(
				ir,
				backendOperationUse(operation, operation.b),
				operation,
			)
			if !ok {
				return nil, false
			}
		default:
			return nil, false
		}
	default:
		return nil, false
	}
	domain := make([]machineStringID, 0)
	seen := make(map[machineStringID]bool)
	for _, source := range sources {
		if source == invalidBackendValueID {
			continue
		}
		values, ok := plan.finiteStringDomain(ir, source, visiting)
		if !ok {
			return nil, false
		}
		for _, name := range values {
			if seen[name] {
				continue
			}
			seen[name] = true
			domain = append(domain, name)
			if len(domain) > 32 {
				return nil, false
			}
		}
	}
	return domain, true
}

func (plan *backendGoRecordTablePlan) fixedScalarArraySources(
	ir *backendProtoIR,
	table backendValueID,
	consumer *backendOperationIR,
) ([]backendValueID, bool) {
	root := plan.root(table)
	if root == invalidBackendValueID || consumer == nil {
		return nil, false
	}
	elements := make(map[uint32]backendValueID)
	var length uint32
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opSetField ||
			plan.root(backendOperationUse(operation, operation.a)) != root {
			continue
		}
		index, ok := backendGoArrayIndexConstant(ir, operation.access.constant)
		if !ok || index == 0 || index > 32 ||
			!backendGoScalarTableOperationDominates(ir, operation, consumer) {
			return nil, false
		}
		source := backendOperationUse(operation, operation.c)
		if !ir.validBackendValue(source) {
			return nil, false
		}
		if _, duplicate := elements[index]; duplicate {
			return nil, false
		}
		elements[index] = source
		if index > length {
			length = index
		}
	}
	if length == 0 || int(length) != len(elements) {
		return nil, false
	}
	sources := make([]backendValueID, length)
	for index := uint32(1); index <= length; index++ {
		source, ok := elements[index]
		if !ok {
			return nil, false
		}
		sources[index-1] = source
	}
	return sources, true
}

func (plan *backendGoRecordTablePlan) recordFieldSources(
	base backendValueID,
	name machineStringID,
) ([]backendValueID, bool) {
	appendField := func(sources []backendValueID, recordIndex int) ([]backendValueID, bool) {
		if recordIndex < 0 || recordIndex >= len(plan.records) {
			return nil, false
		}
		record := &plan.records[recordIndex]
		field, ok := record.fieldIndex[name]
		if !ok || field < 0 || field >= len(record.fieldValues) {
			return append(sources, invalidBackendValueID), true
		}
		return append(sources, record.fieldValues[field]), true
	}
	if recordIndex, ok := plan.recordByRoot[plan.root(base)]; ok {
		return appendField(nil, recordIndex)
	}
	ref, ok := plan.refs[base]
	if !ok {
		return nil, false
	}
	var records []int
	switch ref.kind {
	case backendGoRecordRefArray:
		if ref.index < 0 || ref.index >= len(plan.arrays) {
			return nil, false
		}
		records = plan.arrays[ref.index].records
	case backendGoRecordRefArrayFamily:
		if ref.index < 0 || ref.index >= len(plan.families) {
			return nil, false
		}
		for _, arrayIndex := range plan.families[ref.index].arrays {
			if arrayIndex < 0 || arrayIndex >= len(plan.arrays) {
				return nil, false
			}
			records = append(records, plan.arrays[arrayIndex].records...)
		}
	case backendGoRecordRefChildRecord:
		if ref.index < 0 || ref.index >= len(plan.childRecords) {
			return nil, false
		}
		records = plan.childRecords[ref.index].records
	default:
		return nil, false
	}
	sources := make([]backendValueID, 0, len(records))
	for _, recordIndex := range records {
		var ok bool
		sources, ok = appendField(sources, recordIndex)
		if !ok {
			return nil, false
		}
	}
	return sources, true
}

func (plan *backendGoRecordTablePlan) classifyDynamicFields(ir *backendProtoIR) bool {
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		var base, key, source backendValueID
		switch operation.op {
		case opGetIndex:
			if len(operation.defs) != 1 {
				continue
			}
			base = backendOperationUse(operation, operation.b)
			key = backendOperationUse(operation, operation.c)
		case opSetIndex:
			base = backendOperationUse(operation, operation.a)
			key = backendOperationUse(operation, operation.b)
			source = backendOperationUse(operation, operation.c)
		default:
			continue
		}
		record, ok := plan.recordByRoot[plan.root(base)]
		if !ok {
			continue
		}
		if key == invalidBackendValueID || operation.op == opSetIndex && source == invalidBackendValueID {
			plan.rejectReason = "invalid dynamic record field at PC " + strconv.Itoa(int(operation.pc))
			return false
		}
		field := backendGoRecordDynamicField{record: record, key: key, source: source}
		if operation.op == opGetIndex {
			plan.dynamicGetByPC[operation.pc] = field
		} else {
			plan.dynamicSetByPC[operation.pc] = field
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) dynamicFieldTags(
	tags []backendTagMask,
	field backendGoRecordDynamicField,
) backendTagMask {
	if field.record < 0 || field.record >= len(plan.records) {
		return 0
	}
	var result backendTagMask
	for recordField := range plan.records[field.record].fieldNames {
		result |= plan.scratchFieldTags(tags, field.record, recordField)
	}
	return result
}

func (plan *backendGoRecordTablePlan) fieldTagsFor(
	tags []backendTagMask,
	field backendGoRecordFieldOperation,
) backendTagMask {
	switch field.storage {
	case backendGoRecordFieldScratch:
		if field.index < 0 || field.index >= len(plan.records) {
			return 0
		}
		if _, ok := plan.childByScratch[backendGoRecordScratchField{
			record: field.index,
			field:  field.field,
		}]; ok {
			return backendTagNumber
		}
		if _, ok := plan.childRecord[backendGoRecordScratchField{
			record: field.index,
			field:  field.field,
		}]; ok {
			return backendTagNumber
		}
		return plan.scratchFieldTags(tags, field.index, field.field)
	case backendGoRecordFieldMap:
		if field.index < 0 || field.index >= len(plan.maps) {
			return 0
		}
		return plan.containerFieldTags(
			tags,
			backendGoRecordFieldMap,
			field.index,
			field.field,
			plan.maps[field.index].fieldNames,
			plan.maps[field.index].records,
		)
	case backendGoRecordFieldArray:
		if field.index < 0 || field.index >= len(plan.arrays) {
			return 0
		}
		if _, ok := plan.familyByParent[backendGoRecordParentField{
			array: field.index,
			field: field.field,
		}]; ok {
			return backendTagNumber
		}
		if _, ok := plan.childByParent[backendGoRecordParentField{
			array: field.index,
			field: field.field,
		}]; ok {
			return backendTagNumber
		}
		return plan.containerFieldTags(
			tags,
			backendGoRecordFieldArray,
			field.index,
			field.field,
			plan.arrays[field.index].fieldNames,
			plan.arrays[field.index].records,
		)
	case backendGoRecordFieldArrayFamily:
		if field.index < 0 || field.index >= len(plan.families) {
			return 0
		}
		return plan.familyFieldTags(tags, field.index, field.field)
	case backendGoRecordFieldChildRecord:
		if field.index < 0 || field.index >= len(plan.childRecords) {
			return 0
		}
		return plan.childRecordStaticFieldTags(tags, field.index, field.field)
	default:
		return 0
	}
}

func (plan *backendGoRecordTablePlan) scratchFieldTags(
	tags []backendTagMask,
	recordIndex int,
	field int,
) backendTagMask {
	if recordIndex < 0 || recordIndex >= len(plan.records) {
		return 0
	}
	record := &plan.records[recordIndex]
	if field < 0 || field >= len(record.fieldValues) {
		return 0
	}
	result := backendGoRecordValueTags(tags, record.fieldValues[field])
	for _, operationField := range plan.fieldsByPC {
		if operationField.storage == backendGoRecordFieldScratch &&
			operationField.index == recordIndex &&
			operationField.field == field &&
			operationField.source != invalidBackendValueID {
			result |= backendGoRecordValueTags(tags, operationField.source)
		}
	}
	name := record.fieldNames[field]
	for familyIndex := range plan.childRecords {
		family := &plan.childRecords[familyIndex]
		member := false
		for _, candidate := range family.records {
			if candidate == recordIndex {
				member = true
				break
			}
		}
		if !member {
			continue
		}
		for _, candidate := range family.records {
			candidateRecord := &plan.records[candidate]
			candidateField, ok := candidateRecord.fieldIndex[name]
			if ok {
				result |= backendGoRecordValueTags(tags, candidateRecord.fieldValues[candidateField])
			}
		}
		for _, fused := range plan.fusedSetByPC {
			if fused.family == familyIndex {
				result |= backendGoRecordValueTags(tags, fused.source)
			}
		}
	}
	if field < len(record.fieldOptional) && record.fieldOptional[field] {
		result |= backendTagNil
	}
	return result
}

func (plan backendGoRecordTablePlan) fieldTags(
	field backendGoRecordFieldOperation,
) backendTagMask {
	switch field.storage {
	case backendGoRecordFieldScratch:
		if field.index >= 0 && field.index < len(plan.records) &&
			field.field >= 0 && field.field < len(plan.records[field.index].fieldTags) {
			return plan.records[field.index].fieldTags[field.field]
		}
	case backendGoRecordFieldMap:
		if field.index >= 0 && field.index < len(plan.maps) &&
			field.field >= 0 && field.field < len(plan.maps[field.index].fieldTags) {
			return plan.maps[field.index].fieldTags[field.field]
		}
	case backendGoRecordFieldArray:
		if field.index >= 0 && field.index < len(plan.arrays) &&
			field.field >= 0 && field.field < len(plan.arrays[field.index].fieldTags) {
			return plan.arrays[field.index].fieldTags[field.field]
		}
	case backendGoRecordFieldArrayFamily:
		if field.index >= 0 && field.index < len(plan.families) &&
			field.field >= 0 && field.field < len(plan.families[field.index].fieldTags) {
			return plan.families[field.index].fieldTags[field.field]
		}
	case backendGoRecordFieldChildRecord:
		if field.index >= 0 && field.index < len(plan.childRecords) {
			return plan.childRecords[field.index].fieldTags
		}
	}
	return 0
}

func (plan *backendGoRecordTablePlan) familyFieldTags(
	tags []backendTagMask,
	familyIndex int,
	field int,
) backendTagMask {
	if familyIndex < 0 || familyIndex >= len(plan.families) {
		return 0
	}
	family := &plan.families[familyIndex]
	if field < 0 || field >= len(family.fieldNames) {
		return 0
	}
	name := family.fieldNames[field]
	var result backendTagMask
	for _, arrayIndex := range family.arrays {
		if arrayIndex < 0 || arrayIndex >= len(plan.arrays) {
			return 0
		}
		array := &plan.arrays[arrayIndex]
		arrayField, ok := array.fieldIndex[name]
		if !ok {
			return 0
		}
		if _, childFamily := plan.familyByParent[backendGoRecordParentField{
			array: arrayIndex,
			field: arrayField,
		}]; childFamily {
			result |= backendTagNumber
			continue
		}
		result |= plan.containerFieldTags(
			tags,
			backendGoRecordFieldArray,
			arrayIndex,
			arrayField,
			array.fieldNames,
			array.records,
		)
	}
	for _, operationField := range plan.fieldsByPC {
		if operationField.storage != backendGoRecordFieldArrayFamily ||
			operationField.index != familyIndex ||
			operationField.field != field ||
			operationField.source == invalidBackendValueID {
			continue
		}
		result |= backendGoRecordValueTags(tags, operationField.source)
	}
	return result
}

func (plan *backendGoRecordTablePlan) containerFieldTags(
	tags []backendTagMask,
	storage backendGoRecordFieldStorage,
	index int,
	field int,
	names []machineStringID,
	records []int,
) backendTagMask {
	if field < 0 || field >= len(names) {
		return 0
	}
	name := names[field]
	var result backendTagMask
	missing := false
	for _, recordIndex := range records {
		if recordIndex < 0 || recordIndex >= len(plan.records) {
			return 0
		}
		record := &plan.records[recordIndex]
		recordField, ok := record.fieldIndex[name]
		if !ok {
			missing = true
			continue
		}
		if recordField < 0 || recordField >= len(record.fieldValues) {
			return 0
		}
		result |= backendGoRecordValueTags(tags, record.fieldValues[recordField])
	}
	for _, operationField := range plan.fieldsByPC {
		if operationField.storage != storage ||
			operationField.index != index ||
			operationField.field != field ||
			operationField.source == invalidBackendValueID {
			continue
		}
		result |= backendGoRecordValueTags(tags, operationField.source)
	}
	if missing && (storage == backendGoRecordFieldArray || storage == backendGoRecordFieldArrayFamily) {
		result |= backendTagNil
	}
	return result
}

func backendGoRecordValueTags(tags []backendTagMask, value backendValueID) backendTagMask {
	if value == invalidBackendValueID || int(value) > len(tags) {
		return 0
	}
	return tags[value-1]
}

func (plan *backendGoRecordTablePlan) childRecordFieldTags(
	tags []backendTagMask,
	familyIndex int,
) backendTagMask {
	if familyIndex < 0 || familyIndex >= len(plan.childRecords) {
		return 0
	}
	var result backendTagMask
	for _, recordIndex := range plan.childRecords[familyIndex].records {
		record := &plan.records[recordIndex]
		for field := range record.fieldNames {
			result |= plan.scratchFieldTags(tags, recordIndex, field)
		}
	}
	return result
}

func (plan *backendGoRecordTablePlan) childRecordStaticFieldTags(
	tags []backendTagMask,
	familyIndex int,
	field int,
) backendTagMask {
	if familyIndex < 0 || familyIndex >= len(plan.childRecords) {
		return 0
	}
	family := &plan.childRecords[familyIndex]
	if field < 0 || field >= len(family.fieldNames) {
		return 0
	}
	name := family.fieldNames[field]
	var result backendTagMask
	for _, recordIndex := range family.records {
		if recordIndex < 0 || recordIndex >= len(plan.records) {
			return 0
		}
		record := &plan.records[recordIndex]
		recordField, ok := record.fieldIndex[name]
		if !ok {
			return 0
		}
		result |= plan.scratchFieldTags(tags, recordIndex, recordField)
	}
	return result
}

func (plan *backendGoRecordTablePlan) finalizeFieldTags(
	ir *backendProtoIR,
	tags []backendTagMask,
) bool {
	for recordIndex := range plan.records {
		record := &plan.records[recordIndex]
		record.fieldTags = make([]backendTagMask, len(record.fieldNames))
		for field := range record.fieldValues {
			fieldTags := plan.scratchFieldTags(tags, recordIndex, field)
			if _, ok := plan.childByScratch[backendGoRecordScratchField{
				record: recordIndex,
				field:  field,
			}]; ok {
				fieldTags = backendTagNumber
			}
			if _, ok := plan.childRecord[backendGoRecordScratchField{
				record: recordIndex,
				field:  field,
			}]; ok {
				fieldTags = backendTagNumber
			}
			if _, ok := backendGoScalarPayloadType(fieldTags); !ok {
				plan.rejectReason = "unsupported record field tags"
				return false
			}
			record.fieldTags[field] = fieldTags
		}
	}
	for mapIndex := range plan.maps {
		recordMap := &plan.maps[mapIndex]
		recordMap.fieldTags = make([]backendTagMask, len(recordMap.fieldNames))
		for field := range recordMap.fieldNames {
			fieldTags := plan.containerFieldTags(
				tags,
				backendGoRecordFieldMap,
				mapIndex,
				field,
				recordMap.fieldNames,
				recordMap.records,
			)
			if _, ok := backendGoNumericType(fieldTags); !ok {
				plan.rejectReason = "unsupported record-map field tags"
				return false
			}
			recordMap.fieldTags[field] = fieldTags
		}
	}
	for arrayIndex := range plan.arrays {
		array := &plan.arrays[arrayIndex]
		array.fieldTags = make([]backendTagMask, len(array.fieldNames))
		for field := range array.fieldNames {
			fieldTags := plan.containerFieldTags(
				tags,
				backendGoRecordFieldArray,
				arrayIndex,
				field,
				array.fieldNames,
				array.records,
			)
			if _, ok := plan.familyByParent[backendGoRecordParentField{
				array: arrayIndex,
				field: field,
			}]; ok {
				fieldTags = backendTagNumber
			}
			if _, ok := plan.childByParent[backendGoRecordParentField{
				array: arrayIndex,
				field: field,
			}]; ok {
				fieldTags = backendTagNumber
			}
			if array.length == 0 {
				array.fieldTags[field] = 0
				continue
			}
			if _, ok := backendGoScalarPayloadType(fieldTags); !ok {
				plan.rejectReason = "unsupported record-array field tags"
				return false
			}
			array.fieldTags[field] = fieldTags
		}
	}
	for familyIndex := range plan.families {
		family := &plan.families[familyIndex]
		family.fieldTags = make([]backendTagMask, len(family.fieldNames))
		for field := range family.fieldNames {
			fieldTags := plan.familyFieldTags(tags, familyIndex, field)
			if _, ok := backendGoScalarPayloadType(fieldTags); !ok {
				plan.rejectReason = "unsupported child-array field tags"
				return false
			}
			family.fieldTags[field] = fieldTags
		}
		for _, arrayIndex := range family.arrays {
			array := &plan.arrays[arrayIndex]
			if array.length != 0 {
				continue
			}
			for familyField, name := range family.fieldNames {
				arrayField := array.fieldIndex[name]
				array.fieldTags[arrayField] = family.fieldTags[familyField]
			}
		}
	}
	for familyIndex := range plan.childRecords {
		family := &plan.childRecords[familyIndex]
		fieldTags := plan.childRecordFieldTags(tags, familyIndex)
		family.fieldTags = fieldTags
	}
	usedChildFamilies := make(map[int]bool)
	for _, fused := range plan.fusedGetByPC {
		usedChildFamilies[fused.family] = true
	}
	for _, fused := range plan.fusedSetByPC {
		usedChildFamilies[fused.family] = true
	}
	for familyIndex := range usedChildFamilies {
		if familyIndex < 0 || familyIndex >= len(plan.childRecords) {
			plan.rejectReason = "invalid fused child-record family"
			return false
		}
		if _, ok := backendGoScalarPayloadType(plan.childRecords[familyIndex].fieldTags); !ok {
			plan.rejectReason = "unsupported fused child-record field tags"
			return false
		}
	}
	for _, fused := range plan.fusedSetByPC {
		if fused.family < 0 || fused.family >= len(plan.childRecords) ||
			!backendGoRecordStoreCompatible(
				backendGoRecordValueTags(tags, fused.source),
				plan.childRecords[fused.family].fieldTags,
			) {
			plan.rejectReason = "fused child-record mutation changes scalar tags"
			return false
		}
	}
	for pc, dynamic := range plan.dynamicGetByPC {
		if !backendGoRecordOptionalKeyTags(backendGoRecordValueTags(tags, dynamic.key)) {
			plan.rejectReason = "dynamic record field key is not a string at PC " + strconv.Itoa(int(pc))
			return false
		}
		if _, ok := backendGoNumericType(plan.dynamicFieldTags(tags, dynamic)); !ok {
			plan.rejectReason = "dynamic record fields have mixed scalar tags at PC " + strconv.Itoa(int(pc))
			return false
		}
	}
	for pc, dynamic := range plan.dynamicSetByPC {
		if !backendGoRecordOptionalKeyTags(backendGoRecordValueTags(tags, dynamic.key)) {
			plan.rejectReason = "dynamic record field key is not a string at PC " + strconv.Itoa(int(pc))
			return false
		}
		fieldTags := plan.dynamicFieldTags(tags, dynamic)
		if _, ok := backendGoScalarPayloadType(fieldTags); !ok ||
			!backendGoRecordStoreCompatible(backendGoRecordValueTags(tags, dynamic.source), fieldTags) {
			plan.rejectReason = "dynamic record mutation changes scalar tags at PC " + strconv.Itoa(int(pc))
			return false
		}
	}
	for pc, field := range plan.fieldsByPC {
		operation := &ir.ops[pc]
		if operation.op != opSetStringField {
			continue
		}
		if _, ok := plan.childSetByPC[operation.pc]; ok {
			continue
		}
		if _, ok := plan.childRecordSet[operation.pc]; ok {
			continue
		}
		source := backendOperationUse(operation, operation.c)
		if !backendGoRecordStoreCompatible(
			backendGoRecordValueTags(tags, source),
			plan.fieldTagsFor(tags, field),
		) {
			plan.rejectReason = "record field changes scalar tags"
			return false
		}
	}
	return true
}

func backendGoRecordOptionalKeyTags(tags backendTagMask) bool {
	if tags == backendTagString {
		return true
	}
	payload, optional := backendGoOptionalScalarTags(tags)
	return optional && payload == backendTagString
}

func backendGoRecordStoreCompatible(source, field backendTagMask) bool {
	if source == field {
		return true
	}
	payload, optional := backendGoOptionalScalarTags(field)
	if !optional {
		return false
	}
	if source == backendTagNil || source == payload {
		return true
	}
	sourcePayload, sourceOptional := backendGoOptionalScalarTags(source)
	return sourceOptional && sourcePayload == payload
}

func (plan *backendGoRecordTablePlan) validateUses(ir *backendProtoIR) bool {
	accountedRoots := make(map[backendValueID]bool)
	for root := range plan.recordByRoot {
		accountedRoots[root] = true
	}
	for root := range plan.mapByRoot {
		accountedRoots[root] = true
	}
	for root := range plan.arrayByRoot {
		accountedRoots[root] = true
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op == opGetStringFieldIndex {
			base := backendOperationUse(operation, operation.b)
			if _, ref := plan.refs[base]; ref {
				if _, ok := plan.fusedGetByPC[operation.pc]; !ok {
					plan.rejectReason = "unclassified fused child-record lookup at PC " + strconv.Itoa(int(operation.pc))
					return false
				}
			}
		}
		if operation.op == opSetStringFieldIndex {
			base := backendOperationUse(operation, operation.a)
			if _, ref := plan.refs[base]; ref {
				if _, ok := plan.fusedSetByPC[operation.pc]; !ok {
					plan.rejectReason = "unclassified fused child-record mutation at PC " + strconv.Itoa(int(operation.pc))
					return false
				}
			}
		}
		for _, use := range operation.uses {
			root := plan.root(use.value)
			if root != invalidBackendValueID && accountedRoots[root] {
				switch operation.op {
				case opMove:
					if use.register != operation.b {
						plan.rejectReason = "table move use at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				case opSetStringField:
					if use.register != operation.a && use.register != operation.c {
						plan.rejectReason = "record field set use at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				case opGetStringField:
					if use.register != operation.b {
						plan.rejectReason = "record field get use at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				case opSetStringFieldIndex:
					if _, ok := plan.fusedSetByPC[operation.pc]; !ok {
						plan.rejectReason = "unclassified fused child-record mutation at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				case opGetStringFieldIndex:
					if _, ok := plan.fusedGetByPC[operation.pc]; !ok {
						plan.rejectReason = "unclassified fused child-record lookup at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				case opSetIndex:
					if _, ok := plan.mapSetByPC[operation.pc]; !ok {
						if _, dynamic := plan.dynamicSetByPC[operation.pc]; dynamic {
							break
						}
						plan.rejectReason = "unclassified map set at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				case opGetIndex:
					if _, dynamic := plan.dynamicGetByPC[operation.pc]; dynamic {
						break
					}
					if _, mapGet := plan.mapGetByPC[operation.pc]; !mapGet {
						if _, arrayGet := plan.arrayGetByPC[operation.pc]; arrayGet {
							break
						}
						plan.rejectReason = "unclassified map get at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				case opSetField:
					if _, ok := plan.arraySetByPC[operation.pc]; !ok {
						plan.rejectReason = "unclassified record array set at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				case opPrepareIter:
					if _, ok := plan.arrayPreparePC[operation.pc]; !ok {
						plan.rejectReason = "unclassified record iterator prepare at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				case opArrayNextJump2:
					if _, ok := plan.arrayNextPC[operation.pc]; !ok {
						plan.rejectReason = "unclassified record iterator next at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				case opJumpIfTableHasMetatable:
					if use.register != operation.a {
						plan.rejectReason = "record metatable guard use at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				default:
					plan.rejectReason = "unsupported table use by " + opcodeName(operation.op) + " at PC " + strconv.Itoa(int(operation.pc))
					return false
				}
			}
			if _, ref := plan.refs[use.value]; ref {
				switch operation.op {
				case opMove, opGetStringField, opGetStringFieldIndex, opSetStringField, opSetStringFieldIndex, opEqual, opNotEqual,
					opJumpIfFalse, opJumpIfTableHasMetatable:
				default:
					plan.rejectReason = "unsupported record reference use by " + opcodeName(operation.op) + " at PC " + strconv.Itoa(int(operation.pc))
					return false
				}
			}
			if _, family := plan.familyValues[use.value]; family {
				switch operation.op {
				case opMove:
					if use.register != operation.b {
						plan.rejectReason = "child-array selector move use at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				case opPrepareIter:
					if use.register != operation.a {
						plan.rejectReason = "child-array selector iterator use at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				default:
					plan.rejectReason = "unsupported child-array selector use by " + opcodeName(operation.op) + " at PC " + strconv.Itoa(int(operation.pc))
					return false
				}
			}
		}
	}
	return true
}

func (plan backendGoRecordTablePlan) ownsRoot(root backendValueID) bool {
	if root == invalidBackendValueID {
		return false
	}
	if _, ok := plan.recordByRoot[root]; ok {
		return true
	}
	if _, ok := plan.mapByRoot[root]; ok {
		return true
	}
	_, ok := plan.arrayByRoot[root]
	return ok
}

func (plan *backendGoRecordTablePlan) ensureMap(root backendValueID) int {
	if index, ok := plan.mapByRoot[root]; ok {
		return index
	}
	index := len(plan.maps)
	plan.mapByRoot[root] = index
	plan.maps = append(plan.maps, backendGoRecordMap{root: root})
	return index
}

func (plan *backendGoRecordTablePlan) ensureArray(root backendValueID) int {
	if index, ok := plan.arrayByRoot[root]; ok {
		return index
	}
	index := len(plan.arrays)
	plan.arrayByRoot[root] = index
	plan.arrays = append(plan.arrays, backendGoRecordArray{root: root})
	return index
}

func (plan *backendGoRecordTablePlan) discoverEmptyChildArrays(ir *backendProtoIR) {
	for recordIndex := range plan.records {
		record := &plan.records[recordIndex]
		for _, value := range record.fieldValues {
			root := plan.root(value)
			if root == invalidBackendValueID || !backendGoNewTableRoot(ir, root) {
				continue
			}
			if _, recordRoot := plan.recordByRoot[root]; recordRoot {
				continue
			}
			if _, mapRoot := plan.mapByRoot[root]; mapRoot {
				continue
			}
			plan.ensureArray(root)
		}
	}
}

func (plan *backendGoRecordTablePlan) discoverChildArrayFamilies(ir *backendProtoIR) bool {
	for parentIndex := range plan.arrays {
		parent := &plan.arrays[parentIndex]
		if len(parent.records) == 0 {
			continue
		}
		for parentField, name := range parent.fieldNames {
			children := make([]int, len(parent.records))
			allChildren := true
			for member, recordIndex := range parent.records {
				record := &plan.records[recordIndex]
				recordField, ok := record.fieldIndex[name]
				if !ok {
					allChildren = false
					break
				}
				root := plan.root(record.fieldValues[recordField])
				child, ok := plan.arrayByRoot[root]
				if !ok || child == parentIndex {
					allChildren = false
					break
				}
				children[member] = child
			}
			if !allChildren {
				continue
			}
			seenChildren := make(map[int]bool, len(children))
			for _, child := range children {
				if seenChildren[child] {
					return false
				}
				seenChildren[child] = true
			}

			var shapeNames []machineStringID
			var shapeIndex map[machineStringID]int
			for _, childIndex := range children {
				child := &plan.arrays[childIndex]
				if child.length == 0 {
					continue
				}
				if shapeIndex == nil {
					shapeNames = append([]machineStringID(nil), child.fieldNames...)
					shapeIndex = make(map[machineStringID]int, len(child.fieldIndex))
					for childName, field := range child.fieldIndex {
						shapeIndex[childName] = field
					}
					continue
				}
				if !backendGoRecordSameShape(shapeIndex, child.fieldIndex) {
					return false
				}
			}
			if shapeIndex == nil {
				shapeIndex = make(map[machineStringID]int)
			}
			for _, childIndex := range children {
				child := &plan.arrays[childIndex]
				if child.length != 0 {
					continue
				}
				child.fieldNames = append([]machineStringID(nil), shapeNames...)
				child.fieldIndex = make(map[machineStringID]int, len(shapeIndex))
				child.fieldPresent = make([][]bool, len(shapeNames))
				for childName, field := range shapeIndex {
					child.fieldIndex[childName] = field
				}
			}

			familyIndex := len(plan.families)
			family := backendGoRecordArrayFamily{
				arrays:      append([]int(nil), children...),
				fieldNames:  shapeNames,
				fieldIndex:  shapeIndex,
				parentArray: parentIndex,
				parentField: parentField,
			}
			plan.families = append(plan.families, family)
			plan.familyByParent[backendGoRecordParentField{
				array: parentIndex,
				field: parentField,
			}] = familyIndex
			for member, recordIndex := range parent.records {
				record := &plan.records[recordIndex]
				recordField := record.fieldIndex[name]
				child := backendGoRecordChildArray{family: familyIndex, member: member}
				key := backendGoRecordScratchField{record: recordIndex, field: recordField}
				if current, exists := plan.childByScratch[key]; exists && current != child {
					return false
				}
				plan.childByScratch[key] = child
				foundSet := false
				for pc, field := range record.setByPC {
					if field != recordField {
						continue
					}
					plan.childSetByPC[pc] = child
					foundSet = true
					break
				}
				if !foundSet {
					return false
				}
			}
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) discoverChildRecordFamilies() bool {
	usedChildren := make(map[int]bool)
	for parentIndex := range plan.arrays {
		parent := &plan.arrays[parentIndex]
		if len(parent.records) == 0 {
			continue
		}
		for parentField, name := range parent.fieldNames {
			children := make([]int, len(parent.records))
			allChildren := true
			for member, recordIndex := range parent.records {
				record := &plan.records[recordIndex]
				recordField, ok := record.fieldIndex[name]
				if !ok {
					allChildren = false
					break
				}
				root := plan.root(record.fieldValues[recordField])
				child, ok := plan.recordByRoot[root]
				if !ok || child == recordIndex || plan.records[child].storedAtPC >= 0 {
					allChildren = false
					break
				}
				children[member] = child
			}
			if !allChildren {
				continue
			}
			familyIndex, ok := plan.addChildRecordFamily(
				children,
				parentIndex,
				parentField,
				usedChildren,
			)
			if !ok {
				return false
			}
			plan.childByParent[backendGoRecordParentField{
				array: parentIndex,
				field: parentField,
			}] = familyIndex
			for member, recordIndex := range parent.records {
				record := &plan.records[recordIndex]
				recordField := record.fieldIndex[name]
				child := backendGoRecordChildRecord{family: familyIndex, member: member}
				key := backendGoRecordScratchField{record: recordIndex, field: recordField}
				if current, exists := plan.childRecord[key]; exists && current != child {
					return false
				}
				plan.childRecord[key] = child
				foundSet := false
				for pc, field := range record.setByPC {
					if field != recordField {
						continue
					}
					plan.childRecordSet[pc] = child
					foundSet = true
					break
				}
				if !foundSet {
					return false
				}
			}
		}
	}
	for parentIndex := range plan.records {
		if usedChildren[parentIndex] {
			continue
		}
		parent := &plan.records[parentIndex]
		for parentField := range parent.fieldNames {
			root := plan.root(parent.fieldValues[parentField])
			child, ok := plan.recordByRoot[root]
			if !ok || child == parentIndex || plan.records[child].storedAtPC >= 0 || usedChildren[child] {
				continue
			}
			familyIndex, ok := plan.addChildRecordFamily(
				[]int{child},
				-1,
				parentField,
				usedChildren,
			)
			if !ok {
				return false
			}
			childRef := backendGoRecordChildRecord{family: familyIndex, member: 0}
			key := backendGoRecordScratchField{record: parentIndex, field: parentField}
			if current, exists := plan.childRecord[key]; exists && current != childRef {
				return false
			}
			plan.childRecord[key] = childRef
			foundSet := false
			for pc, field := range parent.setByPC {
				if field != parentField {
					continue
				}
				plan.childRecordSet[pc] = childRef
				foundSet = true
				break
			}
			if !foundSet {
				return false
			}
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) addChildRecordFamily(
	records []int,
	parentArray int,
	parentField int,
	usedChildren map[int]bool,
) (int, bool) {
	if len(records) == 0 {
		return -1, false
	}
	fieldNames := make([]machineStringID, 0)
	fieldIndex := make(map[machineStringID]int)
	for _, recordIndex := range records {
		if recordIndex < 0 || recordIndex >= len(plan.records) || usedChildren[recordIndex] {
			return -1, false
		}
		for _, name := range plan.records[recordIndex].fieldNames {
			if _, exists := fieldIndex[name]; exists {
				continue
			}
			fieldIndex[name] = len(fieldNames)
			fieldNames = append(fieldNames, name)
		}
	}
	for _, name := range fieldNames {
		missing := false
		for _, recordIndex := range records {
			if _, exists := plan.records[recordIndex].fieldIndex[name]; !exists {
				missing = true
				break
			}
		}
		for _, recordIndex := range records {
			field := plan.ensureRecordField(recordIndex, name)
			if field < 0 {
				return -1, false
			}
			if missing {
				plan.records[recordIndex].fieldOptional[field] = true
			}
		}
	}
	for _, recordIndex := range records {
		usedChildren[recordIndex] = true
	}
	familyIndex := len(plan.childRecords)
	plan.childRecords = append(plan.childRecords, backendGoRecordChildRecords{
		records:     append([]int(nil), records...),
		fieldNames:  fieldNames,
		fieldIndex:  fieldIndex,
		parentArray: parentArray,
		parentField: parentField,
	})
	return familyIndex, true
}

func (plan *backendGoRecordTablePlan) ensureRecordField(
	recordIndex int,
	name machineStringID,
) int {
	if recordIndex < 0 || recordIndex >= len(plan.records) || name == invalidMachineStringID {
		return -1
	}
	record := &plan.records[recordIndex]
	if field, ok := record.fieldIndex[name]; ok {
		return field
	}
	field := len(record.fieldNames)
	record.fieldNames = append(record.fieldNames, name)
	record.fieldIndex[name] = field
	record.fieldValues = append(record.fieldValues, invalidBackendValueID)
	record.fieldPresent = append(record.fieldPresent, false)
	record.fieldOptional = append(record.fieldOptional, true)
	return field
}

func backendGoRecordSameShape(
	left map[machineStringID]int,
	right map[machineStringID]int,
) bool {
	if len(left) != len(right) {
		return false
	}
	for name := range left {
		if _, ok := right[name]; !ok {
			return false
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) setShapeFromRecords(
	names *[]machineStringID,
	index *map[machineStringID]int,
	records []int,
) bool {
	if len(records) == 0 {
		return false
	}
	first := plan.records[records[0]]
	*names = append([]machineStringID(nil), first.fieldNames...)
	*index = make(map[machineStringID]int, len(first.fieldIndex))
	for name, field := range first.fieldIndex {
		(*index)[name] = field
	}
	for _, recordIndex := range records[1:] {
		record := plan.records[recordIndex]
		if len(record.fieldNames) != len(*names) {
			return false
		}
		for name := range *index {
			if _, ok := record.fieldIndex[name]; !ok {
				return false
			}
		}
	}
	return true
}

func (plan *backendGoRecordTablePlan) setArrayShapeFromRecords(
	array *backendGoRecordArray,
	records []int,
) bool {
	if array == nil || len(records) == 0 {
		return false
	}
	array.fieldNames = nil
	array.fieldIndex = make(map[machineStringID]int)
	for _, recordIndex := range records {
		if recordIndex < 0 || recordIndex >= len(plan.records) {
			return false
		}
		for _, name := range plan.records[recordIndex].fieldNames {
			if _, exists := array.fieldIndex[name]; exists {
				continue
			}
			array.fieldIndex[name] = len(array.fieldNames)
			array.fieldNames = append(array.fieldNames, name)
		}
	}
	array.fieldPresent = make([][]bool, len(array.fieldNames))
	for field := range array.fieldPresent {
		array.fieldPresent[field] = make([]bool, len(records))
	}
	for member, recordIndex := range records {
		for name := range plan.records[recordIndex].fieldIndex {
			array.fieldPresent[array.fieldIndex[name]][member] = true
		}
	}
	return len(array.fieldNames) != 0
}

func (plan *backendGoRecordTablePlan) setMapDomain(
	mapIndex int,
	key backendGoStructuralKey,
) bool {
	current := &plan.maps[mapIndex]
	if key.domain == 0 || key.separator == invalidMachineStringID {
		return false
	}
	if current.domain == 0 {
		current.domain = key.domain
		current.separator = key.separator
		return true
	}
	return current.domain == key.domain && current.separator == key.separator
}

func (plan *backendGoRecordTablePlan) recordKey(
	ir *backendProtoIR,
	keys backendGoStructuralKeyPlan,
	id backendValueID,
) (backendGoRecordKey, bool) {
	if structural, ok := keys.key(id); ok {
		return backendGoRecordKey{value: id, dynamic: true}, structural.domain != 0
	}
	if _, ok := backendGoStaticStringValueID(ir, id); ok {
		return backendGoRecordKey{value: id}, true
	}
	return backendGoRecordKey{}, false
}

func backendGoStaticStringValueID(
	ir *backendProtoIR,
	id backendValueID,
) (machineStringID, bool) {
	if !ir.validBackendValue(id) {
		return invalidMachineStringID, false
	}
	value := &ir.values[id-1]
	if value.kind != backendValueOperation || value.pc < 0 || int(value.pc) >= len(ir.ops) {
		return invalidMachineStringID, false
	}
	operation := &ir.ops[value.pc]
	if operation.op != opLoadConst || operation.b < 0 || int(operation.b) >= len(ir.constants) {
		return invalidMachineStringID, false
	}
	constant := ir.constants[operation.b]
	if constant.kind != StringKind || constant.bits == 0 || constant.bits > uint64(^uint32(0)) {
		return invalidMachineStringID, false
	}
	return machineStringID(constant.bits), true
}

func backendGoParseRecordKey(
	ir *backendProtoIR,
	id backendValueID,
	separator machineStringID,
) (backendPreparedStringKey, bool) {
	stringID, ok := backendGoStaticStringValueID(ir, id)
	if !ok {
		return backendPreparedStringKey{}, false
	}
	text, ok := backendGoIRStringBytes(ir, stringID)
	if !ok {
		return backendPreparedStringKey{}, false
	}
	delimiter, ok := backendGoIRStringBytes(ir, separator)
	if !ok || len(delimiter) == 0 {
		return backendPreparedStringKey{}, false
	}
	split := bytes.Index(text, delimiter)
	if split < 0 || bytes.Index(text[split+len(delimiter):], delimiter) >= 0 {
		return backendPreparedStringKey{}, false
	}
	first, ok := backendGoParseCanonicalInt32(text[:split])
	if !ok {
		return backendPreparedStringKey{}, false
	}
	second, ok := backendGoParseCanonicalInt32(text[split+len(delimiter):])
	if !ok {
		return backendPreparedStringKey{}, false
	}
	return backendPreparedStringKey{first: first, second: second}, true
}

func backendGoParseCanonicalInt32(text []byte) (int32, bool) {
	if len(text) == 0 {
		return 0, false
	}
	value, err := strconv.ParseInt(string(text), 10, 32)
	if err != nil || strconv.FormatInt(value, 10) != string(text) {
		return 0, false
	}
	return int32(value), true
}

func backendGoIRStringBytes(ir *backendProtoIR, id machineStringID) ([]byte, bool) {
	if ir == nil || id == invalidMachineStringID || uint64(id-1) >= uint64(len(ir.stringRecords)) {
		return nil, false
	}
	record := ir.stringRecords[id-1]
	end := uint64(record.offset) + uint64(record.length)
	if end > uint64(len(ir.stringData)) {
		return nil, false
	}
	return ir.stringData[record.offset:end], true
}

func (plan backendGoRecordTablePlan) root(id backendValueID) backendValueID {
	if id == invalidBackendValueID || int(id) > len(plan.roots) {
		return invalidBackendValueID
	}
	return plan.roots[id-1]
}

func (plan backendGoRecordTablePlan) ref(id backendValueID) (backendGoRecordRef, bool) {
	ref, ok := plan.refs[id]
	return ref, ok
}

func (plan backendGoRecordTablePlan) iteratorValue(id backendValueID) bool {
	return id != invalidBackendValueID &&
		int(id) <= len(plan.iteratorValues) &&
		plan.iteratorValues[id-1]
}

func writeBackendGoRecordDeclarations(
	source *strings.Builder,
	plan backendGoRecordTablePlan,
) {
	for recordIndex, record := range plan.records {
		for field := range record.fieldNames {
			goType, _ := backendGoScalarPayloadType(record.fieldTags[field])
			fmt.Fprintf(source, "\tvar r%d_%d %s\n", recordIndex, field, goType)
			fmt.Fprintf(source, "\t_ = r%d_%d\n", recordIndex, field)
			if field < len(record.fieldOptional) && record.fieldOptional[field] {
				fmt.Fprintf(source, "\tvar rp%d_%d bool\n", recordIndex, field)
				fmt.Fprintf(source, "\t_ = rp%d_%d\n", recordIndex, field)
			}
		}
	}
	for mapIndex, recordMap := range plan.maps {
		fmt.Fprintf(source, "\tvar mk%d [%d]backendPreparedStringKey\n", mapIndex, backendGoRecordMapCapacity)
		fmt.Fprintf(source, "\tvar mu%d [%d]bool\n", mapIndex, backendGoRecordMapCapacity)
		for field := range recordMap.fieldNames {
			goType, _ := backendGoNumericType(recordMap.fieldTags[field])
			fmt.Fprintf(source, "\tvar mf%d_%d [%d]%s\n", mapIndex, field, backendGoRecordMapCapacity, goType)
		}
	}
	for arrayIndex, array := range plan.arrays {
		for field := range array.fieldNames {
			goType, _ := backendGoScalarPayloadType(array.fieldTags[field])
			fmt.Fprintf(source, "\tvar ra%d_%d [%d]%s\n", arrayIndex, field, array.length, goType)
			if _, optional := backendGoOptionalScalarTags(array.fieldTags[field]); optional {
				fmt.Fprintf(source, "\tvar rap%d_%d [%d]bool\n", arrayIndex, field, array.length)
			}
		}
		fmt.Fprintf(source, "\tvar ri%d int\n", arrayIndex)
		fmt.Fprintf(source, "\t_ = ri%d\n", arrayIndex)
	}
	for familyIndex := range plan.families {
		fmt.Fprintf(source, "\tvar rfi%d int\n", familyIndex)
		fmt.Fprintf(source, "\tvar rfs%d int\n", familyIndex)
		fmt.Fprintf(source, "\t_ = rfi%d\n", familyIndex)
		fmt.Fprintf(source, "\t_ = rfs%d\n", familyIndex)
	}
}

func (emitter *backendGoNumericEmitter) recordFieldOptional(recordIndex, field int) bool {
	if recordIndex < 0 || recordIndex >= len(emitter.plan.records.records) {
		return false
	}
	record := &emitter.plan.records.records[recordIndex]
	return field >= 0 && field < len(record.fieldOptional) && record.fieldOptional[field]
}

func (emitter *backendGoNumericEmitter) emitRecordScratchStore(
	recordIndex int,
	field int,
	source backendValueID,
	indent int,
) {
	prefix := strings.Repeat("\t", indent)
	if !emitter.recordFieldOptional(recordIndex, field) {
		fmt.Fprintf(&emitter.body, "%sr%d_%d = v%d\n", prefix, recordIndex, field, source)
		return
	}
	sourceTags := emitter.plan.tags[source-1]
	if sourceTags == backendTagNil {
		fmt.Fprintf(&emitter.body, "%srp%d_%d = false\n", prefix, recordIndex, field)
		return
	}
	fmt.Fprintf(&emitter.body, "%sr%d_%d = v%d\n", prefix, recordIndex, field, source)
	if _, optional := backendGoOptionalScalarTags(sourceTags); optional {
		fmt.Fprintf(&emitter.body, "%srp%d_%d = vp%d\n", prefix, recordIndex, field, source)
	} else {
		fmt.Fprintf(&emitter.body, "%srp%d_%d = true\n", prefix, recordIndex, field)
	}
}

func (emitter *backendGoNumericEmitter) emitRecordArrayStore(
	arrayIndex int,
	field int,
	index string,
	source backendValueID,
	indent int,
) {
	prefix := strings.Repeat("\t", indent)
	sourceTags := emitter.plan.tags[source-1]
	if sourceTags == backendTagNil {
		fmt.Fprintf(&emitter.body, "%srap%d_%d[%s] = false\n", prefix, arrayIndex, field, index)
		return
	}
	fmt.Fprintf(&emitter.body, "%sra%d_%d[%s] = v%d\n", prefix, arrayIndex, field, index, source)
	if _, optional := backendGoOptionalScalarTags(sourceTags); optional {
		fmt.Fprintf(&emitter.body, "%srap%d_%d[%s] = vp%d\n", prefix, arrayIndex, field, index, source)
	} else {
		fmt.Fprintf(&emitter.body, "%srap%d_%d[%s] = true\n", prefix, arrayIndex, field, index)
	}
}

func (emitter *backendGoNumericEmitter) emitRecordSetStringField(
	operation *backendOperationIR,
	use func(int32) (backendValueID, error),
) (bool, error) {
	field, ok := emitter.plan.records.fieldsByPC[operation.pc]
	if !ok {
		return false, nil
	}
	if child, ok := emitter.plan.records.childSetByPC[operation.pc]; ok {
		fmt.Fprintf(
			&emitter.body,
			"\tr%d_%d = %d\n",
			field.index,
			field.field,
			child.member+1,
		)
		return true, nil
	}
	if child, ok := emitter.plan.records.childRecordSet[operation.pc]; ok {
		fmt.Fprintf(
			&emitter.body,
			"\tr%d_%d = %d\n",
			field.index,
			field.field,
			child.member+1,
		)
		return true, nil
	}
	source, err := use(operation.c)
	if err != nil {
		return true, err
	}
	switch field.storage {
	case backendGoRecordFieldScratch:
		emitter.emitRecordScratchStore(field.index, field.field, source, 1)
	case backendGoRecordFieldMap:
		if err := emitter.emitRecordRefGuard(field.ref, backendGoRecordMapCapacity); err != nil {
			return true, err
		}
		fmt.Fprintf(&emitter.body, "\tmf%d_%d[int(v%d)-1] = v%d\n", field.index, field.field, field.ref, source)
	case backendGoRecordFieldArray:
		length := emitter.plan.records.arrays[field.index].length
		if err := emitter.emitRecordRefGuard(field.ref, int(length)); err != nil {
			return true, err
		}
		if _, optional := backendGoOptionalScalarTags(emitter.plan.records.arrays[field.index].fieldTags[field.field]); optional {
			emitter.emitRecordArrayStore(field.index, field.field, fmt.Sprintf("int(v%d)-1", field.ref), source, 1)
		} else {
			if err := emitter.emitRecordArrayFieldGuard(field.index, field.field, field.ref, -1); err != nil {
				return true, err
			}
			fmt.Fprintf(&emitter.body, "\tra%d_%d[int(v%d)-1] = v%d\n", field.index, field.field, field.ref, source)
		}
	case backendGoRecordFieldArrayFamily:
		if err := emitter.emitRecordFamilyField(field, source, true); err != nil {
			return true, err
		}
	case backendGoRecordFieldChildRecord:
		if err := emitter.emitRecordChildField(field, source, true); err != nil {
			return true, err
		}
	default:
		return true, fmt.Errorf("emit backend Go numeric proof: PC %d has invalid record field storage", operation.pc)
	}
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordGetStringField(
	operation *backendOperationIR,
	definition func(int32) (backendValueID, error),
) (bool, error) {
	field, ok := emitter.plan.records.fieldsByPC[operation.pc]
	if !ok {
		return false, nil
	}
	destination, err := definition(operation.a)
	if err != nil {
		return true, err
	}
	switch field.storage {
	case backendGoRecordFieldScratch:
		fmt.Fprintf(&emitter.body, "\tv%d = r%d_%d\n", destination, field.index, field.field)
		if emitter.recordFieldOptional(field.index, field.field) {
			fmt.Fprintf(&emitter.body, "\tvp%d = rp%d_%d\n", destination, field.index, field.field)
		}
	case backendGoRecordFieldMap:
		if err := emitter.emitRecordRefGuard(field.ref, backendGoRecordMapCapacity); err != nil {
			return true, err
		}
		fmt.Fprintf(&emitter.body, "\tv%d = mf%d_%d[int(v%d)-1]\n", destination, field.index, field.field, field.ref)
	case backendGoRecordFieldArray:
		length := emitter.plan.records.arrays[field.index].length
		if err := emitter.emitRecordRefGuard(field.ref, int(length)); err != nil {
			return true, err
		}
		fmt.Fprintf(&emitter.body, "\tv%d = ra%d_%d[int(v%d)-1]\n", destination, field.index, field.field, field.ref)
		if _, optional := backendGoOptionalScalarTags(emitter.plan.records.arrays[field.index].fieldTags[field.field]); optional {
			fmt.Fprintf(&emitter.body, "\tvp%d = rap%d_%d[int(v%d)-1]\n", destination, field.index, field.field, field.ref)
		} else if err := emitter.emitRecordArrayFieldGuard(field.index, field.field, field.ref, -1); err != nil {
			return true, err
		}
	case backendGoRecordFieldArrayFamily:
		if err := emitter.emitRecordFamilyField(field, destination, false); err != nil {
			return true, err
		}
	case backendGoRecordFieldChildRecord:
		if err := emitter.emitRecordChildField(field, destination, false); err != nil {
			return true, err
		}
	default:
		return true, fmt.Errorf("emit backend Go numeric proof: PC %d reads invalid record field storage", operation.pc)
	}
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordFusedGet(
	operation *backendOperationIR,
	definition func(int32) (backendValueID, error),
) (bool, error) {
	fused, ok := emitter.plan.records.fusedGetByPC[operation.pc]
	if !ok {
		return false, nil
	}
	if fused.family < 0 || fused.family >= len(emitter.plan.records.childRecords) {
		return true, fmt.Errorf("emit backend Go numeric proof: PC %d has invalid child-record family", operation.pc)
	}
	destination, err := definition(operation.a)
	if err != nil {
		return true, err
	}
	family := emitter.plan.records.childRecords[fused.family]
	emitter.emitOptionalPresenceGuard(operation, 1, fused.key)
	if fused.member >= 0 {
		if fused.member >= len(family.records) {
			return true, fmt.Errorf("emit backend Go numeric proof: PC %d has invalid fixed child-record member", operation.pc)
		}
		return true, emitter.emitRecordFusedGetMember(
			operation,
			destination,
			fused.key,
			family,
			family.records[fused.member],
			1,
		)
	}
	if family.parentArray < 0 || family.parentArray >= len(emitter.plan.records.arrays) {
		return true, fmt.Errorf("emit backend Go numeric proof: PC %d has invalid child-record parent", operation.pc)
	}
	parent := emitter.plan.records.arrays[family.parentArray]
	if err := emitter.emitRecordRefGuard(fused.ref, len(parent.records)); err != nil {
		return true, err
	}
	fmt.Fprintf(&emitter.body, "\tswitch int(v%d) - 1 {\n", fused.ref)
	for member, recordIndex := range family.records {
		if recordIndex < 0 || recordIndex >= len(emitter.plan.records.records) {
			return true, fmt.Errorf("emit backend Go numeric proof: PC %d has invalid child record", operation.pc)
		}
		fmt.Fprintf(&emitter.body, "\tcase %d:\n", member)
		if err := emitter.emitRecordFusedGetMember(operation, destination, fused.key, family, recordIndex, 2); err != nil {
			return true, err
		}
	}
	emitter.body.WriteString("\tdefault:\n")
	fmt.Fprintf(&emitter.body, "\t\t%s\n", emitter.failureReturn())
	emitter.body.WriteString("\t}\n")
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordFusedGetMember(
	operation *backendOperationIR,
	destination backendValueID,
	key backendValueID,
	family backendGoRecordChildRecords,
	recordIndex int,
	indent int,
) error {
	if recordIndex < 0 || recordIndex >= len(emitter.plan.records.records) {
		return fmt.Errorf("emit backend Go numeric proof: PC %d has invalid child record", operation.pc)
	}
	prefix := strings.Repeat("\t", indent)
	record := emitter.plan.records.records[recordIndex]
	fmt.Fprintf(&emitter.body, "%sswitch v%d {\n", prefix, key)
	for _, name := range family.fieldNames {
		field, ok := record.fieldIndex[name]
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has child-record shape mismatch", operation.pc)
		}
		fmt.Fprintf(&emitter.body, "%scase uint32(%d):\n", prefix, name)
		fmt.Fprintf(&emitter.body, "%s\tv%d = r%d_%d\n", prefix, destination, recordIndex, field)
		if emitter.recordFieldOptional(recordIndex, field) {
			fmt.Fprintf(&emitter.body, "%s\tvp%d = rp%d_%d\n", prefix, destination, recordIndex, field)
		}
	}
	fmt.Fprintf(&emitter.body, "%sdefault:\n", prefix)
	fmt.Fprintf(&emitter.body, "%s\t%s\n", prefix, emitter.failureReturn())
	fmt.Fprintf(&emitter.body, "%s}\n", prefix)
	return nil
}

func (emitter *backendGoNumericEmitter) emitRecordFusedSet(
	operation *backendOperationIR,
	use func(int32) (backendValueID, error),
) (bool, error) {
	fused, ok := emitter.plan.records.fusedSetByPC[operation.pc]
	if !ok {
		return false, nil
	}
	if fused.family < 0 || fused.family >= len(emitter.plan.records.childRecords) {
		return true, fmt.Errorf("emit backend Go numeric proof: PC %d has invalid child-record family", operation.pc)
	}
	source, err := use(operation.d)
	if err != nil {
		return true, err
	}
	family := emitter.plan.records.childRecords[fused.family]
	emitter.emitOptionalPresenceGuard(operation, 1, fused.key)
	if fused.member >= 0 {
		if fused.member >= len(family.records) {
			return true, fmt.Errorf("emit backend Go numeric proof: PC %d has invalid fixed child-record member", operation.pc)
		}
		return true, emitter.emitRecordFusedSetMember(
			operation,
			source,
			fused.key,
			family,
			family.records[fused.member],
			1,
		)
	}
	if family.parentArray < 0 || family.parentArray >= len(emitter.plan.records.arrays) {
		return true, fmt.Errorf("emit backend Go numeric proof: PC %d has invalid child-record parent", operation.pc)
	}
	parent := emitter.plan.records.arrays[family.parentArray]
	if err := emitter.emitRecordRefGuard(fused.ref, len(parent.records)); err != nil {
		return true, err
	}
	fmt.Fprintf(&emitter.body, "\tswitch int(v%d) - 1 {\n", fused.ref)
	for member, recordIndex := range family.records {
		if recordIndex < 0 || recordIndex >= len(emitter.plan.records.records) {
			return true, fmt.Errorf("emit backend Go numeric proof: PC %d has invalid child record", operation.pc)
		}
		fmt.Fprintf(&emitter.body, "\tcase %d:\n", member)
		if err := emitter.emitRecordFusedSetMember(operation, source, fused.key, family, recordIndex, 2); err != nil {
			return true, err
		}
	}
	emitter.body.WriteString("\tdefault:\n")
	fmt.Fprintf(&emitter.body, "\t\t%s\n", emitter.failureReturn())
	emitter.body.WriteString("\t}\n")
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordFusedSetMember(
	operation *backendOperationIR,
	source backendValueID,
	key backendValueID,
	family backendGoRecordChildRecords,
	recordIndex int,
	indent int,
) error {
	if recordIndex < 0 || recordIndex >= len(emitter.plan.records.records) {
		return fmt.Errorf("emit backend Go numeric proof: PC %d has invalid child record", operation.pc)
	}
	prefix := strings.Repeat("\t", indent)
	record := emitter.plan.records.records[recordIndex]
	fmt.Fprintf(&emitter.body, "%sswitch v%d {\n", prefix, key)
	for _, name := range family.fieldNames {
		field, ok := record.fieldIndex[name]
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has child-record shape mismatch", operation.pc)
		}
		fmt.Fprintf(&emitter.body, "%scase uint32(%d):\n", prefix, name)
		emitter.emitRecordScratchStore(recordIndex, field, source, indent+1)
	}
	fmt.Fprintf(&emitter.body, "%sdefault:\n", prefix)
	fmt.Fprintf(&emitter.body, "%s\t%s\n", prefix, emitter.failureReturn())
	fmt.Fprintf(&emitter.body, "%s}\n", prefix)
	return nil
}

func (emitter *backendGoNumericEmitter) emitRecordFamilyField(
	field backendGoRecordFieldOperation,
	value backendValueID,
	set bool,
) error {
	if field.index < 0 || field.index >= len(emitter.plan.records.families) ||
		field.ref == invalidBackendValueID {
		return fmt.Errorf("emit backend Go numeric proof: invalid child-array record reference")
	}
	family := emitter.plan.records.families[field.index]
	if field.field < 0 || field.field >= len(family.fieldNames) || len(family.arrays) == 0 {
		return fmt.Errorf("emit backend Go numeric proof: invalid child-array record field")
	}
	maxArray := 0
	for _, arrayIndex := range family.arrays {
		if arrayIndex > maxArray {
			maxArray = arrayIndex
		}
	}
	ref := field.ref
	fmt.Fprintf(&emitter.body, "\t{\n\t\trr%d := int(v%d) - 1\n", field.ref, ref)
	fmt.Fprintf(
		&emitter.body,
		"\t\tif v%d < 1 || v%d > %d || v%d != float64(int(v%d)) {\n",
		ref,
		ref,
		(maxArray+1)*32,
		ref,
		ref,
	)
	fmt.Fprintf(&emitter.body, "\t\t\t%s\n", emitter.failureReturn())
	emitter.body.WriteString("\t\t}\n")
	fmt.Fprintf(&emitter.body, "\t\tswitch rr%d / 32 {\n", field.ref)
	for _, arrayIndex := range family.arrays {
		if arrayIndex < 0 || arrayIndex >= len(emitter.plan.records.arrays) {
			return fmt.Errorf("emit backend Go numeric proof: invalid child-array family member")
		}
		array := emitter.plan.records.arrays[arrayIndex]
		arrayField, ok := array.fieldIndex[family.fieldNames[field.field]]
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: child-array field shape mismatch")
		}
		fmt.Fprintf(&emitter.body, "\t\tcase %d:\n", arrayIndex)
		fmt.Fprintf(&emitter.body, "\t\t\tif rr%d%%32 >= %d {\n", field.ref, array.length)
		fmt.Fprintf(&emitter.body, "\t\t\t\t%s\n", emitter.failureReturn())
		emitter.body.WriteString("\t\t\t}\n")
		_, optional := backendGoOptionalScalarTags(array.fieldTags[arrayField])
		if !optional {
			if err := emitter.emitRecordArrayFieldGuard(arrayIndex, arrayField, field.ref, 3); err != nil {
				return err
			}
		}
		if set && optional {
			emitter.emitRecordArrayStore(arrayIndex, arrayField, fmt.Sprintf("rr%d%%32", field.ref), value, 3)
		} else if set {
			fmt.Fprintf(
				&emitter.body,
				"\t\t\tra%d_%d[rr%d%%32] = v%d\n",
				arrayIndex,
				arrayField,
				field.ref,
				value,
			)
		} else {
			fmt.Fprintf(
				&emitter.body,
				"\t\t\tv%d = ra%d_%d[rr%d%%32]\n",
				value,
				arrayIndex,
				arrayField,
				field.ref,
			)
			if optional {
				fmt.Fprintf(
					&emitter.body,
					"\t\t\tvp%d = rap%d_%d[rr%d%%32]\n",
					value,
					arrayIndex,
					arrayField,
					field.ref,
				)
			}
		}
	}
	emitter.body.WriteString("\t\tdefault:\n")
	fmt.Fprintf(&emitter.body, "\t\t\t%s\n", emitter.failureReturn())
	emitter.body.WriteString("\t\t}\n\t}\n")
	return nil
}

func (emitter *backendGoNumericEmitter) emitRecordChildField(
	field backendGoRecordFieldOperation,
	value backendValueID,
	set bool,
) error {
	if field.index < 0 || field.index >= len(emitter.plan.records.childRecords) ||
		field.ref == invalidBackendValueID {
		return fmt.Errorf("emit backend Go numeric proof: invalid child-record reference")
	}
	family := emitter.plan.records.childRecords[field.index]
	if field.field < 0 || field.field >= len(family.fieldNames) {
		return fmt.Errorf("emit backend Go numeric proof: invalid child-record field")
	}
	if err := emitter.emitRecordRefGuard(field.ref, len(family.records)); err != nil {
		return err
	}
	name := family.fieldNames[field.field]
	fmt.Fprintf(&emitter.body, "\tswitch int(v%d) - 1 {\n", field.ref)
	for member, recordIndex := range family.records {
		if recordIndex < 0 || recordIndex >= len(emitter.plan.records.records) {
			return fmt.Errorf("emit backend Go numeric proof: invalid child record")
		}
		record := emitter.plan.records.records[recordIndex]
		recordField, ok := record.fieldIndex[name]
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: child-record shape mismatch")
		}
		fmt.Fprintf(&emitter.body, "\tcase %d:\n", member)
		if set {
			emitter.emitRecordScratchStore(recordIndex, recordField, value, 2)
		} else {
			fmt.Fprintf(&emitter.body, "\t\tv%d = r%d_%d\n", value, recordIndex, recordField)
			if emitter.recordFieldOptional(recordIndex, recordField) {
				fmt.Fprintf(&emitter.body, "\t\tvp%d = rp%d_%d\n", value, recordIndex, recordField)
			}
		}
	}
	emitter.body.WriteString("\tdefault:\n")
	fmt.Fprintf(&emitter.body, "\t\t%s\n", emitter.failureReturn())
	emitter.body.WriteString("\t}\n")
	return nil
}

func (emitter *backendGoNumericEmitter) emitRecordRefGuard(
	ref backendValueID,
	limit int,
) error {
	if ref == invalidBackendValueID || limit <= 0 {
		return fmt.Errorf("emit backend Go numeric proof: invalid scalar record reference")
	}
	fmt.Fprintf(&emitter.body, "\tif v%d < 1 || v%d > %d {\n", ref, ref, limit)
	fmt.Fprintf(&emitter.body, "\t\t%s\n", emitter.failureReturn())
	emitter.body.WriteString("\t}\n")
	return nil
}

func (emitter *backendGoNumericEmitter) emitRecordArrayFieldGuard(
	arrayIndex int,
	field int,
	ref backendValueID,
	indent int,
) error {
	if arrayIndex < 0 || arrayIndex >= len(emitter.plan.records.arrays) ||
		field < 0 || field >= len(emitter.plan.records.arrays[arrayIndex].fieldPresent) {
		return fmt.Errorf("emit backend Go numeric proof: invalid record-array field presence")
	}
	present := emitter.plan.records.arrays[arrayIndex].fieldPresent[field]
	if len(present) != len(emitter.plan.records.arrays[arrayIndex].records) {
		return fmt.Errorf("emit backend Go numeric proof: invalid record-array field presence inventory")
	}
	presentCount := 0
	for _, ok := range present {
		if ok {
			presentCount++
		}
	}
	if presentCount == len(present) {
		return nil
	}
	if presentCount == 0 {
		return fmt.Errorf("emit backend Go numeric proof: record-array field is absent from every member")
	}
	prefix := "\t"
	selector := fmt.Sprintf("int(v%d)-1", ref)
	if indent >= 0 {
		prefix = strings.Repeat("\t", indent)
		selector = fmt.Sprintf("rr%d%%32", ref)
	}
	fmt.Fprintf(&emitter.body, "%sswitch %s {\n", prefix, selector)
	emitter.body.WriteString(prefix + "case ")
	wrote := false
	for member, ok := range present {
		if !ok {
			continue
		}
		if wrote {
			emitter.body.WriteString(", ")
		}
		fmt.Fprintf(&emitter.body, "%d", member)
		wrote = true
	}
	emitter.body.WriteString(":\n")
	emitter.body.WriteString(prefix + "default:\n")
	fmt.Fprintf(&emitter.body, "%s\t%s\n", prefix, emitter.failureReturn())
	emitter.body.WriteString(prefix + "}\n")
	return nil
}

func (emitter *backendGoNumericEmitter) emitRecordArraySet(
	operation *backendOperationIR,
) (bool, error) {
	arrayOperation, ok := emitter.plan.records.arraySetByPC[operation.pc]
	if !ok {
		return false, nil
	}
	array := emitter.plan.records.arrays[arrayOperation.index]
	record := emitter.plan.records.records[arrayOperation.record]
	for field, name := range array.fieldNames {
		recordField, ok := record.fieldIndex[name]
		if !ok {
			continue
		}
		fmt.Fprintf(
			&emitter.body,
			"\tra%d_%d[%d] = r%d_%d\n",
			arrayOperation.index,
			field,
			arrayOperation.element-1,
			arrayOperation.record,
			recordField,
		)
		if _, optional := backendGoOptionalScalarTags(array.fieldTags[field]); optional {
			present := recordField < len(record.fieldPresent) && record.fieldPresent[recordField]
			if recordField < len(record.fieldOptional) && record.fieldOptional[recordField] {
				fmt.Fprintf(
					&emitter.body,
					"\trap%d_%d[%d] = rp%d_%d\n",
					arrayOperation.index,
					field,
					arrayOperation.element-1,
					arrayOperation.record,
					recordField,
				)
			} else if present {
				fmt.Fprintf(
					&emitter.body,
					"\trap%d_%d[%d] = true\n",
					arrayOperation.index,
					field,
					arrayOperation.element-1,
				)
			}
		}
	}
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordArrayPrepare(
	operation *backendOperationIR,
	use func(int32) (backendValueID, error),
) (bool, error) {
	arrayIndex, ok := emitter.plan.records.arrayPreparePC[operation.pc]
	if ok {
		fmt.Fprintf(&emitter.body, "\tri%d = 0\n", arrayIndex)
		return true, nil
	}
	familyIndex, ok := emitter.plan.records.familyPrepare[operation.pc]
	if !ok {
		return false, nil
	}
	selector, err := use(operation.a)
	if err != nil {
		return true, err
	}
	family := emitter.plan.records.families[familyIndex]
	fmt.Fprintf(
		&emitter.body,
		"\tif v%d < 1 || v%d > %d || v%d != float64(int(v%d)) {\n",
		selector,
		selector,
		len(family.arrays),
		selector,
		selector,
	)
	fmt.Fprintf(&emitter.body, "\t\t%s\n", emitter.failureReturn())
	emitter.body.WriteString("\t}\n")
	fmt.Fprintf(&emitter.body, "\trfs%d = int(v%d) - 1\n", familyIndex, selector)
	fmt.Fprintf(&emitter.body, "\trfi%d = 0\n", familyIndex)
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordArrayNext(
	operation *backendOperationIR,
	block *backendBlockIR,
	definition func(int32) (backendValueID, error),
) (bool, bool, error) {
	arrayIndex, ok := emitter.plan.records.arrayNextPC[operation.pc]
	familyIndex, familyOK := emitter.plan.records.familyNext[operation.pc]
	if !ok && !familyOK {
		return false, false, nil
	}
	keyObserved := false
	for _, current := range operation.defs {
		if current.register == operation.a {
			_, keyObserved = emitter.plan.records.arrayKeyValues[current.value]
			break
		}
	}
	key := invalidBackendValueID
	var err error
	if keyObserved {
		key, err = definition(operation.a)
		if err != nil {
			return true, false, err
		}
	}
	value, err := definition(operation.a + 1)
	if err != nil {
		return true, false, err
	}
	target := emitter.ir.pcToBlock[operation.targetPC]
	if ok {
		array := emitter.plan.records.arrays[arrayIndex]
		fmt.Fprintf(&emitter.body, "\tif ri%d >= %d {\n", arrayIndex, array.length)
		emitter.emitGoto(int32(block.id), target, 2)
		emitter.body.WriteString("\t}\n")
		if keyObserved {
			fmt.Fprintf(&emitter.body, "\tv%d = float64(ri%d + 1)\n", key, arrayIndex)
			fmt.Fprintf(&emitter.body, "\tv%d = v%d\n", value, key)
		} else {
			fmt.Fprintf(&emitter.body, "\tv%d = float64(ri%d + 1)\n", value, arrayIndex)
		}
		fmt.Fprintf(&emitter.body, "\tri%d++\n", arrayIndex)
	} else {
		family := emitter.plan.records.families[familyIndex]
		fmt.Fprintf(&emitter.body, "\t{\n\t\trfl%d := 0\n", operation.pc)
		fmt.Fprintf(&emitter.body, "\t\tswitch rfs%d {\n", familyIndex)
		for member, childIndex := range family.arrays {
			length := emitter.plan.records.arrays[childIndex].length
			fmt.Fprintf(&emitter.body, "\t\tcase %d:\n\t\t\trfl%d = %d\n", member, operation.pc, length)
		}
		emitter.body.WriteString("\t\tdefault:\n")
		fmt.Fprintf(&emitter.body, "\t\t\t%s\n", emitter.failureReturn())
		emitter.body.WriteString("\t\t}\n")
		fmt.Fprintf(&emitter.body, "\t\tif rfi%d >= rfl%d {\n", familyIndex, operation.pc)
		emitter.emitGoto(int32(block.id), target, 3)
		emitter.body.WriteString("\t\t}\n")
		if keyObserved {
			fmt.Fprintf(&emitter.body, "\t\tv%d = float64(rfi%d + 1)\n", key, familyIndex)
		}
		fmt.Fprintf(&emitter.body, "\t\tswitch rfs%d {\n", familyIndex)
		for member, childIndex := range family.arrays {
			fmt.Fprintf(
				&emitter.body,
				"\t\tcase %d:\n\t\t\tv%d = float64(%d + rfi%d + 1)\n",
				member,
				value,
				childIndex*32,
				familyIndex,
			)
		}
		emitter.body.WriteString("\t\t}\n")
		fmt.Fprintf(&emitter.body, "\t\trfi%d++\n", familyIndex)
		emitter.body.WriteString("\t}\n")
	}
	nextBlock := int32(-1)
	if int(block.last) < len(emitter.ir.ops) {
		nextBlock = emitter.ir.pcToBlock[block.last]
	}
	emitter.emitGoto(int32(block.id), nextBlock, 1)
	return true, true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordDynamicGet(
	operation *backendOperationIR,
	definition func(int32) (backendValueID, error),
) (bool, error) {
	dynamic, ok := emitter.plan.records.dynamicGetByPC[operation.pc]
	if !ok {
		return false, nil
	}
	if dynamic.record < 0 || dynamic.record >= len(emitter.plan.records.records) {
		return true, fmt.Errorf("emit backend Go numeric proof: PC %d has invalid dynamic record", operation.pc)
	}
	destination, err := definition(operation.a)
	if err != nil {
		return true, err
	}
	record := emitter.plan.records.records[dynamic.record]
	emitter.emitOptionalPresenceGuard(operation, 1, dynamic.key)
	fmt.Fprintf(&emitter.body, "\tswitch v%d {\n", dynamic.key)
	for field, name := range record.fieldNames {
		fmt.Fprintf(&emitter.body, "\tcase uint32(%d):\n", name)
		fmt.Fprintf(&emitter.body, "\t\tv%d = r%d_%d\n", destination, dynamic.record, field)
		if emitter.recordFieldOptional(dynamic.record, field) {
			fmt.Fprintf(&emitter.body, "\t\tvp%d = rp%d_%d\n", destination, dynamic.record, field)
		}
	}
	emitter.body.WriteString("\tdefault:\n")
	fmt.Fprintf(&emitter.body, "\t\t%s\n", emitter.failureReturn())
	emitter.body.WriteString("\t}\n")
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordDynamicSet(
	operation *backendOperationIR,
	use func(int32) (backendValueID, error),
) (bool, error) {
	dynamic, ok := emitter.plan.records.dynamicSetByPC[operation.pc]
	if !ok {
		return false, nil
	}
	if dynamic.record < 0 || dynamic.record >= len(emitter.plan.records.records) {
		return true, fmt.Errorf("emit backend Go numeric proof: PC %d has invalid dynamic record", operation.pc)
	}
	source, err := use(operation.c)
	if err != nil {
		return true, err
	}
	record := emitter.plan.records.records[dynamic.record]
	emitter.emitOptionalPresenceGuard(operation, 1, dynamic.key)
	fmt.Fprintf(&emitter.body, "\tswitch v%d {\n", dynamic.key)
	for field, name := range record.fieldNames {
		fmt.Fprintf(&emitter.body, "\tcase uint32(%d):\n", name)
		emitter.emitRecordScratchStore(dynamic.record, field, source, 2)
	}
	emitter.body.WriteString("\tdefault:\n")
	fmt.Fprintf(&emitter.body, "\t\t%s\n", emitter.failureReturn())
	emitter.body.WriteString("\t}\n")
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordMapSet(
	operation *backendOperationIR,
) (bool, error) {
	mapOperation, ok := emitter.plan.records.mapSetByPC[operation.pc]
	if !ok {
		return false, nil
	}
	recordMap := emitter.plan.records.maps[mapOperation.index]
	record := emitter.plan.records.records[mapOperation.record]
	key := backendGoRecordKeyExpression(mapOperation.key)
	fmt.Fprintf(&emitter.body, "\t{\n\t\trk%d := %s\n", operation.pc, key)
	fmt.Fprintf(
		&emitter.body,
		"\t\trs%d := (uint32(rk%d.first)*0x9e3779b1 ^ uint32(rk%d.second)*0x85ebca6b) & %d\n",
		operation.pc,
		operation.pc,
		operation.pc,
		backendGoRecordMapCapacity-1,
	)
	fmt.Fprintf(&emitter.body, "\t\trok%d := false\n", operation.pc)
	fmt.Fprintf(&emitter.body, "\t\tfor rp%d := 0; rp%d < %d; rp%d++ {\n", operation.pc, operation.pc, backendGoRecordMapCapacity, operation.pc)
	fmt.Fprintf(
		&emitter.body,
		"\t\t\trx%d := int((rs%d + uint32(rp%d)) & %d)\n",
		operation.pc,
		operation.pc,
		operation.pc,
		backendGoRecordMapCapacity-1,
	)
	fmt.Fprintf(&emitter.body, "\t\t\tif !mu%d[rx%d] {\n", mapOperation.index, operation.pc)
	fmt.Fprintf(&emitter.body, "\t\t\t\tmu%d[rx%d] = true\n", mapOperation.index, operation.pc)
	fmt.Fprintf(&emitter.body, "\t\t\t\tmk%d[rx%d] = rk%d\n", mapOperation.index, operation.pc, operation.pc)
	emitter.body.WriteString("\t\t\t}\n")
	fmt.Fprintf(&emitter.body, "\t\t\tif mk%d[rx%d] == rk%d {\n", mapOperation.index, operation.pc, operation.pc)
	for field, name := range recordMap.fieldNames {
		recordField, ok := record.fieldIndex[name]
		if !ok {
			return true, fmt.Errorf("emit backend Go numeric proof: PC %d changes record-map shape", operation.pc)
		}
		fmt.Fprintf(
			&emitter.body,
			"\t\t\t\tmf%d_%d[rx%d] = r%d_%d\n",
			mapOperation.index,
			field,
			operation.pc,
			mapOperation.record,
			recordField,
		)
	}
	fmt.Fprintf(&emitter.body, "\t\t\t\trok%d = true\n\t\t\t\tbreak\n\t\t\t}\n\t\t}\n", operation.pc)
	fmt.Fprintf(&emitter.body, "\t\tif !rok%d {\n", operation.pc)
	fmt.Fprintf(&emitter.body, "\t\t\t%s\n", emitter.failureReturn())
	emitter.body.WriteString("\t\t}\n\t}\n")
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordMapGet(
	operation *backendOperationIR,
	definition func(int32) (backendValueID, error),
) (bool, error) {
	mapOperation, ok := emitter.plan.records.mapGetByPC[operation.pc]
	if !ok {
		return false, nil
	}
	destination, err := definition(operation.a)
	if err != nil {
		return true, err
	}
	key := backendGoRecordKeyExpression(mapOperation.key)
	fmt.Fprintf(&emitter.body, "\tv%d = 0\n", destination)
	fmt.Fprintf(&emitter.body, "\t{\n\t\trk%d := %s\n", operation.pc, key)
	fmt.Fprintf(
		&emitter.body,
		"\t\trs%d := (uint32(rk%d.first)*0x9e3779b1 ^ uint32(rk%d.second)*0x85ebca6b) & %d\n",
		operation.pc,
		operation.pc,
		operation.pc,
		backendGoRecordMapCapacity-1,
	)
	fmt.Fprintf(&emitter.body, "\t\tfor rp%d := 0; rp%d < %d; rp%d++ {\n", operation.pc, operation.pc, backendGoRecordMapCapacity, operation.pc)
	fmt.Fprintf(
		&emitter.body,
		"\t\t\trx%d := int((rs%d + uint32(rp%d)) & %d)\n",
		operation.pc,
		operation.pc,
		operation.pc,
		backendGoRecordMapCapacity-1,
	)
	fmt.Fprintf(&emitter.body, "\t\t\tif !mu%d[rx%d] {\n\t\t\t\tbreak\n\t\t\t}\n", mapOperation.index, operation.pc)
	fmt.Fprintf(&emitter.body, "\t\t\tif mk%d[rx%d] == rk%d {\n", mapOperation.index, operation.pc, operation.pc)
	fmt.Fprintf(&emitter.body, "\t\t\t\tv%d = float64(rx%d + 1)\n\t\t\t\tbreak\n\t\t\t}\n", destination, operation.pc)
	emitter.body.WriteString("\t\t}\n\t}\n")
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordArrayGet(
	operation *backendOperationIR,
	definition func(int32) (backendValueID, error),
) (bool, error) {
	arrayIndex, ok := emitter.plan.records.arrayGetByPC[operation.pc]
	if !ok {
		return false, nil
	}
	destination, err := definition(operation.a)
	if err != nil {
		return true, err
	}
	key := backendOperationUse(operation, operation.c)
	if key == invalidBackendValueID || arrayIndex < 0 || arrayIndex >= len(emitter.plan.records.arrays) {
		return true, fmt.Errorf("emit backend Go numeric proof: PC %d has invalid record-array lookup", operation.pc)
	}
	emitter.needsMath = true
	length := emitter.plan.records.arrays[arrayIndex].length
	emitter.emitOptionalPresenceGuard(operation, 1, key)
	fmt.Fprintf(&emitter.body, "\tv%d = 0\n", destination)
	fmt.Fprintf(
		&emitter.body,
		"\tif v%d >= 1 && v%d <= %d && v%d == math.Trunc(v%d) {\n",
		key,
		key,
		length,
		key,
		key,
	)
	fmt.Fprintf(&emitter.body, "\t\tv%d = v%d\n", destination, key)
	emitter.body.WriteString("\t}\n")
	return true, nil
}

func backendGoRecordKeyExpression(key backendGoRecordKey) string {
	if key.dynamic {
		return fmt.Sprintf("v%d", key.value)
	}
	return fmt.Sprintf(
		"backendPreparedStringKey{first: %d, second: %d}",
		key.constant.first,
		key.constant.second,
	)
}
