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
)

type backendGoRecordRef struct {
	kind  backendGoRecordRefKind
	index int
}

type backendGoRecord struct {
	root       backendValueID
	fieldNames []machineStringID
	fieldIndex map[machineStringID]int
	setByPC    map[int32]int
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
	root       backendValueID
	fieldNames []machineStringID
	fieldIndex map[machineStringID]int
	length     uint32
}

type backendGoRecordFieldStorage uint8

const (
	backendGoRecordFieldScratch backendGoRecordFieldStorage = iota + 1
	backendGoRecordFieldMap
	backendGoRecordFieldArray
)

type backendGoRecordFieldOperation struct {
	storage backendGoRecordFieldStorage
	index   int
	field   int
	ref     backendValueID
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
	arrayPreparePC map[int32]int
	arrayNextPC    map[int32]int
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
		arrayPreparePC: make(map[int32]int),
		arrayNextPC:    make(map[int32]int),
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
			})
		}
		record := &plan.records[recordIndex]
		if _, duplicate := record.fieldIndex[name]; duplicate {
			plan.rejectReason = "duplicate record field"
			return plan
		}
		field := len(record.fieldNames)
		record.fieldNames = append(record.fieldNames, name)
		record.fieldIndex[name] = field
		record.setByPC[operation.pc] = field
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
		}
	}
	if len(plan.maps) == 0 {
		plan.rejectReason = "no maps"
		return plan
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		switch operation.op {
		case opGetIndex:
			table := plan.root(backendOperationUse(operation, operation.b))
			mapIndex, ok := plan.mapByRoot[table]
			if !ok || len(operation.defs) != 1 {
				continue
			}
			plan.mapGetByPC[operation.pc] = backendGoRecordMapOperation{index: mapIndex}
			plan.refs[operation.defs[0].value] = backendGoRecordRef{
				kind: backendGoRecordRefMap, index: mapIndex,
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
	if !plan.analyzeIterators(ir) {
		plan.rejectReason = "iterator analysis"
		return plan
	}
	if !plan.propagateRefsAndIterators(ir) {
		plan.rejectReason = "reference propagation"
		return plan
	}
	if !plan.finishShapesAndKeys(ir, keys) {
		plan.rejectReason = "shapes or keys"
		return plan
	}
	if !plan.classifyFields(ir) {
		if plan.rejectReason == "" {
			plan.rejectReason = "field classification"
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
		if root != invalidBackendValueID {
			plan.scalarValues[valueIndex] = true
		}
	}
	for valueIndex, iterator := range plan.iteratorValues {
		if iterator {
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
			return false
		}
		arrayIndex := plan.iteratorArray[control-1]
		plan.arrayNextPC[operation.pc] = arrayIndex
		for _, definition := range operation.defs {
			switch definition.register {
			case operation.a:
				if !plan.iteratorValues[definition.value-1] {
					return false
				}
			case operation.a + 1:
				plan.refs[definition.value] = backendGoRecordRef{
					kind: backendGoRecordRefArray, index: arrayIndex,
				}
			}
		}
	}
	return len(plan.arrayNextPC) != 0
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
		if current.length == 0 || current.length > 32 {
			return false
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
		if !plan.setShapeFromRecords(&current.fieldNames, &current.fieldIndex, recordIndexes) {
			return false
		}
	}
	for _, uses := range recordUses {
		if uses != 1 {
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
	for pc := range record.setByPC {
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
			if !exists || operation.op == opGetStringField {
				plan.rejectReason = "record field mismatch at PC " + strconv.Itoa(int(operation.pc))
				return false
			}
			plan.fieldsByPC[operation.pc] = backendGoRecordFieldOperation{
				storage: backendGoRecordFieldScratch,
				index:   record,
				field:   field,
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
		}
		if field < 0 {
			plan.rejectReason = "record reference field is missing at PC " + strconv.Itoa(int(operation.pc))
			return false
		}
		plan.fieldsByPC[operation.pc] = backendGoRecordFieldOperation{
			storage: backendGoRecordFieldStorage(ref.kind + 1),
			index:   ref.index,
			field:   field,
			ref:     base,
		}
	}
	return true
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
		if operation.op == opNewTable {
			for _, definition := range operation.defs {
				if !accountedRoots[definition.value] {
					return false
				}
			}
		}
		for _, use := range operation.uses {
			root := plan.root(use.value)
			if root != invalidBackendValueID {
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
				case opSetIndex:
					if _, ok := plan.mapSetByPC[operation.pc]; !ok {
						plan.rejectReason = "unclassified map set at PC " + strconv.Itoa(int(operation.pc))
						return false
					}
				case opGetIndex:
					if _, ok := plan.mapGetByPC[operation.pc]; !ok {
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
				default:
					plan.rejectReason = "unsupported table use by " + opcodeName(operation.op) + " at PC " + strconv.Itoa(int(operation.pc))
					return false
				}
			}
			if _, ref := plan.refs[use.value]; ref {
				switch operation.op {
				case opMove, opGetStringField, opSetStringField, opEqual, opNotEqual, opJumpIfFalse:
				default:
					plan.rejectReason = "unsupported record reference use by " + opcodeName(operation.op) + " at PC " + strconv.Itoa(int(operation.pc))
					return false
				}
			}
		}
	}
	return true
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
			fmt.Fprintf(source, "\tvar r%d_%d float64\n", recordIndex, field)
			fmt.Fprintf(source, "\t_ = r%d_%d\n", recordIndex, field)
		}
	}
	for mapIndex, recordMap := range plan.maps {
		fmt.Fprintf(source, "\tvar mk%d [%d]backendPreparedStringKey\n", mapIndex, backendGoRecordMapCapacity)
		fmt.Fprintf(source, "\tvar mu%d [%d]bool\n", mapIndex, backendGoRecordMapCapacity)
		for field := range recordMap.fieldNames {
			fmt.Fprintf(source, "\tvar mf%d_%d [%d]float64\n", mapIndex, field, backendGoRecordMapCapacity)
		}
	}
	for arrayIndex, array := range plan.arrays {
		for field := range array.fieldNames {
			fmt.Fprintf(source, "\tvar ra%d_%d [%d]float64\n", arrayIndex, field, array.length)
		}
		fmt.Fprintf(source, "\tvar ri%d int\n", arrayIndex)
		fmt.Fprintf(source, "\t_ = ri%d\n", arrayIndex)
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
	source, err := use(operation.c)
	if err != nil {
		return true, err
	}
	switch field.storage {
	case backendGoRecordFieldScratch:
		fmt.Fprintf(&emitter.body, "\tr%d_%d = v%d\n", field.index, field.field, source)
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
		fmt.Fprintf(&emitter.body, "\tra%d_%d[int(v%d)-1] = v%d\n", field.index, field.field, field.ref, source)
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
	default:
		return true, fmt.Errorf("emit backend Go numeric proof: PC %d reads invalid record field storage", operation.pc)
	}
	return true, nil
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
			return true, fmt.Errorf("emit backend Go numeric proof: PC %d changes record-array shape", operation.pc)
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
	}
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordArrayPrepare(
	operation *backendOperationIR,
) (bool, error) {
	arrayIndex, ok := emitter.plan.records.arrayPreparePC[operation.pc]
	if !ok {
		return false, nil
	}
	fmt.Fprintf(&emitter.body, "\tri%d = 0\n", arrayIndex)
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitRecordArrayNext(
	operation *backendOperationIR,
	block *backendBlockIR,
	definition func(int32) (backendValueID, error),
) (bool, bool, error) {
	arrayIndex, ok := emitter.plan.records.arrayNextPC[operation.pc]
	if !ok {
		return false, false, nil
	}
	value, err := definition(operation.a + 1)
	if err != nil {
		return true, false, err
	}
	array := emitter.plan.records.arrays[arrayIndex]
	target := emitter.ir.pcToBlock[operation.targetPC]
	fmt.Fprintf(&emitter.body, "\tif ri%d >= %d {\n", arrayIndex, array.length)
	emitter.emitGoto(int32(block.id), target, 2)
	emitter.body.WriteString("\t}\n")
	fmt.Fprintf(&emitter.body, "\tv%d = float64(ri%d + 1)\n", value, arrayIndex)
	fmt.Fprintf(&emitter.body, "\tri%d++\n", arrayIndex)
	nextBlock := int32(-1)
	if int(block.last) < len(emitter.ir.ops) {
		nextBlock = emitter.ir.pcToBlock[block.last]
	}
	emitter.emitGoto(int32(block.id), nextBlock, 1)
	return true, true, nil
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
