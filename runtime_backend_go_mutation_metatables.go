package ember

import (
	"fmt"
	"math"
	"strings"
)

type backendGoMutationMetatableGet struct {
	dirty bool
	key   machineStringID
}

type backendGoMutationMetatablePlan struct {
	enabled         bool
	rejectReason    string
	dirtyRoot       backendValueID
	backingRoot     backendValueID
	trackedRoot     backendValueID
	metatableRoot   backendValueID
	indexClosure    backendValueID
	newIndexClosure backendValueID
	indexTarget     backendGoNumericTarget
	newIndexTarget  backendGoNumericTarget
	fields          []backendGoCapturedTableField
	excludedRoots   map[backendValueID]bool
	getByPC         map[int32]backendGoMutationMetatableGet
	setByPC         map[int32]bool
	backingSetByPC  map[int32]int
	setterByPC      map[int32]bool
	metatableByPC   map[int32]bool
	roots           []backendValueID
	scalarReplaced  []bool
}

func discoverBackendGoMutationMetatable(
	ir *backendProtoIR,
	options backendGoNumericOptions,
) backendGoMutationMetatablePlan {
	empty := backendGoMutationMetatablePlan{}
	if ir == nil || len(options.directTargets) == 0 {
		return empty
	}
	plan := backendGoMutationMetatablePlan{
		excludedRoots:  make(map[backendValueID]bool),
		getByPC:        make(map[int32]backendGoMutationMetatableGet),
		setByPC:        make(map[int32]bool),
		backingSetByPC: make(map[int32]int),
		setterByPC:     make(map[int32]bool),
		metatableByPC:  make(map[int32]bool),
		roots:          make([]backendValueID, len(ir.values)),
		scalarReplaced: make([]bool, len(ir.values)),
	}
	reject := func(reason string) backendGoMutationMetatablePlan {
		plan.rejectReason = reason
		return plan
	}

	var metatableOperation *backendOperationIR
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opFastCall || nativeFuncID(operation.nativeID) != nativeFuncSetMetatable ||
			operation.c != 2 || operation.d != 1 || len(operation.defs) != 1 {
			continue
		}
		tracked := backendGoSingleTableRoot(ir, backendOperationUse(operation, operation.a))
		metatable := backendGoSingleTableRoot(ir, backendOperationUse(operation, operation.a+1))
		if tracked == invalidBackendValueID || metatable == invalidBackendValueID || tracked == metatable {
			continue
		}
		if metatableOperation != nil {
			return empty
		}
		metatableOperation = operation
		plan.trackedRoot = tracked
		plan.metatableRoot = metatable
	}
	if metatableOperation == nil || !backendGoNewTableRoot(ir, plan.trackedRoot) ||
		!backendGoNewTableRoot(ir, plan.metatableRoot) {
		return empty
	}

	var indexOperation, newIndexOperation *backendOperationIR
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opSetStringField ||
			backendGoSingleTableRoot(ir, backendOperationUse(operation, operation.a)) != plan.metatableRoot {
			continue
		}
		source := backendOperationUse(operation, operation.c)
		_, closure, ok := backendGoCapturedClosure(ir, source, nil)
		if !ok || closure.targetProto < 0 || int(closure.targetProto) >= len(options.directTargets) {
			return empty
		}
		switch {
		case backendGoStringFieldIsIndex(ir, operation.access.constant):
			if indexOperation != nil {
				return empty
			}
			indexOperation = operation
			plan.indexClosure = source
			plan.indexTarget = options.directTargets[closure.targetProto]
		case backendGoStringFieldIsNewIndex(ir, operation.access.constant):
			if newIndexOperation != nil {
				return empty
			}
			newIndexOperation = operation
			plan.newIndexClosure = source
			plan.newIndexTarget = options.directTargets[closure.targetProto]
		default:
			return empty
		}
	}
	if indexOperation == nil || newIndexOperation == nil ||
		!backendGoMutationMetatableTargetShape(plan.indexTarget, 2, 1) ||
		!backendGoMutationMetatableTargetShape(plan.newIndexTarget, 3, 0) {
		return empty
	}

	_, indexClosure, indexOK := backendGoCapturedClosure(ir, plan.indexClosure, nil)
	_, newIndexClosure, newIndexOK := backendGoCapturedClosure(ir, plan.newIndexClosure, nil)
	if !indexOK || !newIndexOK {
		return reject("closure rediscovery")
	}
	indexUpvalues := backendGoCapturedRecordUpvalues(plan.indexTarget.ir)
	newIndexUpvalues := backendGoCapturedRecordUpvalues(plan.newIndexTarget.ir)
	if len(indexUpvalues) != 1 || len(newIndexUpvalues) != 2 {
		return reject("captured upvalue counts")
	}
	indexUpvalue, ok := backendGoOnlyCapturedUpvalue(indexUpvalues)
	if !ok {
		return reject("index captured upvalue")
	}
	plan.backingRoot, ok = backendGoCallerCaptureRoot(ir, plan.indexTarget.ir, indexClosure, indexUpvalue)
	if !ok || plan.backingRoot == invalidBackendValueID {
		return reject("index caller capture root")
	}
	dirtyUpvalue := int32(-1)
	backingUpvalue := int32(-1)
	for upvalue := range newIndexUpvalues {
		root, captureOK := backendGoCallerCaptureRoot(ir, plan.newIndexTarget.ir, newIndexClosure, upvalue)
		if !captureOK {
			return reject("newindex caller capture root")
		}
		if root == plan.backingRoot {
			if backingUpvalue >= 0 {
				return reject("duplicate newindex backing capture")
			}
			backingUpvalue = upvalue
			continue
		}
		if plan.dirtyRoot != invalidBackendValueID {
			return reject("newindex distinct capture roots")
		}
		plan.dirtyRoot = root
		dirtyUpvalue = upvalue
	}
	if dirtyUpvalue < 0 || backingUpvalue < 0 || plan.dirtyRoot == invalidBackendValueID ||
		!backendGoNewTableRoot(ir, plan.dirtyRoot) ||
		plan.dirtyRoot == plan.trackedRoot || plan.dirtyRoot == plan.metatableRoot ||
		plan.backingRoot == plan.trackedRoot || plan.backingRoot == plan.metatableRoot {
		return reject("caller table roots")
	}

	indexFields := plan.indexTarget.capturedTableFields[indexUpvalue]
	if len(indexFields) == 0 {
		return reject("index captured fields")
	}
	plan.fields = append([]backendGoCapturedTableField(nil), indexFields...)
	for _, field := range plan.fields {
		if field.name == invalidMachineStringID || field.tags != backendTagNumber {
			return reject("index numeric field domain")
		}
	}
	for upvalue := range newIndexUpvalues {
		fields := plan.newIndexTarget.capturedTableFields[upvalue]
		if !backendGoCapturedFieldSlicesEqual(fields, plan.fields) {
			return reject("captured field domains")
		}
	}
	seenBackingFields := make([]bool, len(plan.fields))
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opSetStringField ||
			backendGoSingleTableRoot(ir, backendOperationUse(operation, operation.a)) != plan.backingRoot {
			continue
		}
		name, nameOK := backendGoStringFieldName(ir, operation.access.constant)
		field, fieldOK := backendGoMutationMetatableField(plan.fields, name)
		source := backendOperationUse(operation, operation.c)
		if !nameOK || !fieldOK || seenBackingFields[field] || !ir.validBackendValue(source) ||
			ir.values[source-1].tags != backendTagNumber {
			return reject("backing field initialization")
		}
		seenBackingFields[field] = true
		plan.backingSetByPC[operation.pc] = field
	}
	for _, seen := range seenBackingFields {
		if !seen {
			return reject("missing backing field initialization")
		}
	}
	if !backendGoMutationMetatableVirtualZeroSafe(
		plan.newIndexTarget.ir,
		newIndexUpvalues[dirtyUpvalue],
		newIndexUpvalues[backingUpvalue],
	) {
		return reject("virtual dirty initialization")
	}

	plan.excludedRoots[plan.dirtyRoot] = true
	plan.excludedRoots[plan.backingRoot] = true
	plan.excludedRoots[plan.trackedRoot] = true
	plan.excludedRoots[plan.metatableRoot] = true
	plan.setterByPC[indexOperation.pc] = true
	plan.setterByPC[newIndexOperation.pc] = true
	plan.metatableByPC[metatableOperation.pc] = true
	for _, root := range []backendValueID{plan.dirtyRoot, plan.backingRoot, plan.trackedRoot, plan.metatableRoot} {
		plan.roots[root-1] = root
	}
	for valueIndex := range ir.values {
		origins := ir.values[valueIndex].origins
		if len(origins) == 1 && plan.excludedRoots[origins[0]] {
			plan.roots[valueIndex] = origins[0]
		}
	}
	for _, definition := range metatableOperation.defs {
		if ir.validBackendValue(definition.value) {
			plan.roots[definition.value-1] = plan.trackedRoot
		}
	}
	if !plan.propagateRoots(ir) {
		return reject("conflicting caller table aliases")
	}
	for _, id := range []backendValueID{plan.indexClosure, plan.newIndexClosure} {
		if ir.validBackendValue(id) {
			plan.scalarReplaced[id-1] = true
		}
	}
	for valueIndex, root := range plan.roots {
		if root != invalidBackendValueID {
			plan.scalarReplaced[valueIndex] = true
		}
	}
	if ok, reason := plan.classifyCallerUses(ir); !ok {
		return reject(reason)
	}
	plan.enabled = true
	return plan
}

func (plan *backendGoMutationMetatablePlan) classifyCallerUses(ir *backendProtoIR) (bool, string) {
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		allowed := make(map[backendValueID]bool)
		switch operation.op {
		case opGetIndex:
			root := plan.valueRoot(backendOperationUse(operation, operation.b))
			if root == plan.trackedRoot {
				plan.getByPC[operation.pc] = backendGoMutationMetatableGet{}
				allowed[root] = true
			} else if root == plan.dirtyRoot {
				plan.getByPC[operation.pc] = backendGoMutationMetatableGet{dirty: true}
				allowed[root] = true
			}
		case opGetStringField:
			root := plan.valueRoot(backendOperationUse(operation, operation.b))
			if root == plan.trackedRoot {
				name, ok := backendGoStringFieldName(ir, operation.access.constant)
				if !ok || !backendGoMutationMetatableHasField(plan.fields, name) {
					return false, fmt.Sprintf("unknown tracked field at PC %d", operation.pc)
				}
				plan.getByPC[operation.pc] = backendGoMutationMetatableGet{key: name}
				allowed[root] = true
			}
		case opSetIndex:
			root := plan.valueRoot(backendOperationUse(operation, operation.a))
			if root == plan.trackedRoot {
				plan.setByPC[operation.pc] = true
				allowed[root] = true
			}
		case opSetStringField:
			root := plan.valueRoot(backendOperationUse(operation, operation.a))
			if _, ok := plan.backingSetByPC[operation.pc]; ok {
				allowed[root] = true
			} else if plan.setterByPC[operation.pc] {
				allowed[root] = true
			}
		case opFastCall:
			if plan.metatableByPC[operation.pc] {
				allowed[plan.trackedRoot] = true
				allowed[plan.metatableRoot] = true
			}
		}
		for _, use := range operation.uses {
			root := plan.valueRoot(use.value)
			if plan.excludedRoots[root] && !allowed[root] {
				return false, fmt.Sprintf("excluded table root %d escapes at PC %d (%s)", root, operation.pc, opcodeName(operation.op))
			}
		}
	}
	if len(plan.getByPC) == 0 || len(plan.setByPC) == 0 {
		return false, "missing tracked reads or writes"
	}
	return true, ""
}

func (plan *backendGoMutationMetatablePlan) propagateRoots(ir *backendProtoIR) bool {
	setRoot := func(id, root backendValueID) (bool, bool) {
		if !ir.validBackendValue(id) || root == invalidBackendValueID {
			return false, false
		}
		current := plan.roots[id-1]
		if current == invalidBackendValueID {
			plan.roots[id-1] = root
			return true, true
		}
		return false, current == root
	}
	for iteration := 0; iteration <= len(ir.values)+len(ir.ops); iteration++ {
		changed := false
		for valueIndex := range ir.values {
			value := &ir.values[valueIndex]
			if value.kind != backendValuePhi || plan.roots[valueIndex] != invalidBackendValueID {
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
				candidate := plan.valueRoot(input)
				if candidate == invalidBackendValueID {
					if !backendGoIdentityAliasCandidate(ir, input, value.register) {
						ready = false
						break
					}
					continue
				}
				if found && root != candidate {
					ready = false
					break
				}
				root = candidate
				found = true
			}
			if !ready || !found {
				continue
			}
			if updated, ok := setRoot(value.id, root); !ok {
				return false
			} else if updated {
				changed = true
			}
		}
		for pc := range ir.ops {
			operation := &ir.ops[pc]
			if operation.op != opMove {
				continue
			}
			root := plan.valueRoot(backendOperationUse(operation, operation.b))
			if root == invalidBackendValueID {
				continue
			}
			for _, definition := range operation.defs {
				if updated, ok := setRoot(definition.value, root); !ok {
					return false
				} else if updated {
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

func (plan backendGoMutationMetatablePlan) valueRoot(id backendValueID) backendValueID {
	if id == invalidBackendValueID || int(id) > len(plan.roots) {
		return invalidBackendValueID
	}
	return plan.roots[id-1]
}

func backendGoMutationMetatableTargetShape(target backendGoNumericTarget, params, results int) bool {
	if target.ir == nil || target.functionName == "" || !target.receiverTable ||
		target.receiverTables != 0 || target.selfRecursive || target.ir.variadic || target.ir.params != params {
		return false
	}
	count, ok := backendGoNumericFixedResultCount(target.ir)
	return ok && count == results
}

func backendGoOnlyCapturedUpvalue(upvalues map[int32][]backendValueID) (int32, bool) {
	if len(upvalues) != 1 {
		return 0, false
	}
	for upvalue := range upvalues {
		return upvalue, true
	}
	return 0, false
}

func backendGoCallerCaptureRoot(
	caller *backendProtoIR,
	target *backendProtoIR,
	closure *backendOperationIR,
	upvalue int32,
) (backendValueID, bool) {
	if caller == nil || target == nil || closure == nil || upvalue < 0 || int(upvalue) >= len(target.upvalues) {
		return invalidBackendValueID, false
	}
	descriptor := target.upvalues[upvalue]
	if descriptor.local == 0 || descriptor.index >= uint32(caller.registers) {
		return invalidBackendValueID, false
	}
	root := backendGoSingleTableRoot(caller, backendValueBeforeOperation(caller, closure, int32(descriptor.index)))
	return root, root != invalidBackendValueID
}

func backendGoCapturedFieldSlicesEqual(left, right []backendGoCapturedTableField) bool {
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

func backendGoMutationMetatableVirtualZeroSafe(
	target *backendProtoIR,
	dirtyRoots []backendValueID,
	backingRoots []backendValueID,
) bool {
	if target == nil || target.params != 3 || len(target.initial) < 3 ||
		len(dirtyRoots) == 0 || len(backingRoots) == 0 {
		return false
	}
	keyParameter := target.initial[1]
	valueParameter := target.initial[2]
	var dirtyGet *backendOperationIR
	var dirtyStore *backendOperationIR
	var backingStore *backendOperationIR
	for pc := range target.ops {
		operation := &target.ops[pc]
		switch operation.op {
		case opGetIndex:
			if dirtyGet != nil || len(operation.defs) != 1 ||
				!backendGoMutationMetatableCapturedValue(target, backendOperationUse(operation, operation.b), dirtyRoots) ||
				!backendGoMutationMetatableAliasOf(target, backendOperationUse(operation, operation.c), keyParameter, nil) {
				return false
			}
			dirtyGet = operation
		case opSetIndex:
			table := backendOperationUse(operation, operation.a)
			switch {
			case backendGoMutationMetatableCapturedValue(target, table, dirtyRoots):
				if dirtyStore != nil {
					return false
				}
				dirtyStore = operation
			case backendGoMutationMetatableCapturedValue(target, table, backingRoots):
				if backingStore != nil {
					return false
				}
				backingStore = operation
			default:
				return false
			}
		}
	}
	if dirtyGet == nil || dirtyStore == nil || backingStore == nil ||
		!backendGoMutationMetatableAliasOf(target, backendOperationUse(dirtyStore, dirtyStore.b), keyParameter, nil) ||
		!backendGoMutationMetatableAliasOf(target, backendOperationUse(backingStore, backingStore.b), keyParameter, nil) ||
		!backendGoMutationMetatableAliasOf(target, backendOperationUse(backingStore, backingStore.c), valueParameter, nil) {
		return false
	}
	return backendGoMutationMetatableIncrementOf(
		target,
		backendOperationUse(dirtyStore, dirtyStore.c),
		dirtyGet.defs[0].value,
	)
}

func backendGoMutationMetatableCapturedValue(
	ir *backendProtoIR,
	id backendValueID,
	roots []backendValueID,
) bool {
	for _, root := range roots {
		if backendGoMutationMetatableAliasOf(ir, id, root, nil) {
			return true
		}
	}
	return false
}

func backendGoMutationMetatableIncrementOf(
	ir *backendProtoIR,
	id backendValueID,
	get backendValueID,
) bool {
	if !ir.validBackendValue(id) {
		return false
	}
	value := &ir.values[id-1]
	if value.kind != backendValueOperation || value.pc < 0 || int(value.pc) >= len(ir.ops) {
		return false
	}
	operation := &ir.ops[value.pc]
	var base backendValueID
	switch operation.op {
	case opAddK:
		if operation.c < 0 || int(operation.c) >= len(ir.constants) ||
			ir.constants[operation.c].kind != NumberKind ||
			ir.constants[operation.c].bits != math.Float64bits(1) {
			return false
		}
		base = backendOperationUse(operation, operation.b)
	case opAdd:
		left := backendOperationUse(operation, operation.b)
		right := backendOperationUse(operation, operation.c)
		switch {
		case backendGoStaticNumberEquals(ir, left, 1):
			base = right
		case backendGoStaticNumberEquals(ir, right, 1):
			base = left
		default:
			return false
		}
	default:
		return false
	}
	return backendGoMutationMetatableZeroFallback(ir, base, get)
}

func backendGoMutationMetatableZeroFallback(
	ir *backendProtoIR,
	id backendValueID,
	get backendValueID,
) bool {
	if !ir.validBackendValue(id) {
		return false
	}
	value := &ir.values[id-1]
	if value.kind == backendValueOperation && value.pc >= 0 && int(value.pc) < len(ir.ops) &&
		ir.ops[value.pc].op == opMove {
		return backendGoMutationMetatableZeroFallback(
			ir,
			backendOperationUse(&ir.ops[value.pc], ir.ops[value.pc].b),
			get,
		)
	}
	if value.kind != backendValuePhi || value.block < 0 || int(value.block) >= len(ir.blocks) {
		return false
	}
	block := &ir.blocks[value.block]
	phi := block.phis[value.register]
	foundGet := false
	foundZero := false
	for inputIndex, input := range phi.inputs {
		if !ir.blocks[block.predecessors[inputIndex]].reachable || input == id {
			continue
		}
		switch {
		case backendGoMutationMetatableAliasOf(ir, input, get, nil):
			foundGet = true
		case backendGoStaticNumberEquals(ir, input, 0):
			foundZero = true
		default:
			return false
		}
	}
	return foundGet && foundZero
}

func backendGoMutationMetatableAliasOf(
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
	if value.kind == backendValueOperation && value.pc >= 0 && int(value.pc) < len(ir.ops) &&
		ir.ops[value.pc].op == opMove {
		return backendGoMutationMetatableAliasOf(
			ir,
			backendOperationUse(&ir.ops[value.pc], ir.ops[value.pc].b),
			source,
			seen,
		)
	}
	if value.kind != backendValuePhi || value.block < 0 || int(value.block) >= len(ir.blocks) {
		return false
	}
	block := &ir.blocks[value.block]
	phi := block.phis[value.register]
	found := false
	for inputIndex, input := range phi.inputs {
		if !ir.blocks[block.predecessors[inputIndex]].reachable || input == id {
			continue
		}
		branchSeen := make(map[backendValueID]bool, len(seen))
		for candidate := range seen {
			branchSeen[candidate] = true
		}
		if !backendGoMutationMetatableAliasOf(ir, input, source, branchSeen) {
			return false
		}
		found = true
	}
	return found
}

func backendGoMutationMetatableHasField(fields []backendGoCapturedTableField, name machineStringID) bool {
	_, ok := backendGoMutationMetatableField(fields, name)
	return ok
}

func backendGoMutationMetatableField(fields []backendGoCapturedTableField, name machineStringID) (int, bool) {
	for index, field := range fields {
		if field.name == name {
			return index, true
		}
	}
	return 0, false
}

func backendGoSingleTableRoot(ir *backendProtoIR, id backendValueID) backendValueID {
	if !ir.validBackendValue(id) {
		return invalidBackendValueID
	}
	value := &ir.values[id-1]
	if value.object != backendObjectTable || len(value.origins) != 1 {
		return invalidBackendValueID
	}
	return value.origins[0]
}

func (plan backendGoMutationMetatablePlan) get(operation *backendOperationIR) (backendGoMutationMetatableGet, bool) {
	if operation == nil {
		return backendGoMutationMetatableGet{}, false
	}
	get, ok := plan.getByPC[operation.pc]
	return get, ok
}

func (plan backendGoMutationMetatablePlan) set(operation *backendOperationIR) bool {
	return operation != nil && plan.setByPC[operation.pc]
}

func (plan backendGoMutationMetatablePlan) setter(operation *backendOperationIR) bool {
	return operation != nil && plan.setterByPC[operation.pc]
}

func (plan backendGoMutationMetatablePlan) backingSetter(operation *backendOperationIR) (int, bool) {
	if operation == nil {
		return 0, false
	}
	field, ok := plan.backingSetByPC[operation.pc]
	return field, ok
}

func (plan backendGoMutationMetatablePlan) metatable(operation *backendOperationIR) bool {
	return operation != nil && plan.metatableByPC[operation.pc]
}

func (plan backendGoMutationMetatablePlan) scalarValue(id backendValueID) bool {
	return plan.enabled && id != invalidBackendValueID && int(id) <= len(plan.scalarReplaced) && plan.scalarReplaced[id-1]
}

func writeBackendGoMutationMetatableDeclarations(source *strings.Builder, plan backendGoMutationMetatablePlan) {
	if !plan.enabled {
		return
	}
	for field := range plan.fields {
		fmt.Fprintf(source, "\tvar dm%d float64\n", field)
		fmt.Fprintf(source, "\t_ = dm%d\n", field)
		fmt.Fprintf(source, "\tvar bm%d float64\n", field)
		fmt.Fprintf(source, "\t_ = bm%d\n", field)
	}
}

func (emitter *backendGoNumericEmitter) emitMutationMetatableGet(
	operation *backendOperationIR,
	definition func(int32) (backendValueID, error),
	use func(int32) (backendValueID, error),
) (bool, error) {
	get, ok := emitter.plan.mutationMetatable.get(operation)
	if !ok {
		return false, nil
	}
	destination, err := definition(operation.a)
	if err != nil {
		return true, err
	}
	keyExpression := fmt.Sprintf("uint32(%d)", get.key)
	if get.key == invalidMachineStringID {
		key, keyErr := use(operation.c)
		if keyErr != nil {
			return true, keyErr
		}
		emitter.emitOptionalPresenceGuard(operation, 1, key)
		keyExpression = fmt.Sprintf("v%d", key)
	}
	if get.dirty {
		fmt.Fprintf(&emitter.body, "\tswitch %s {\n", keyExpression)
		for field, descriptor := range emitter.plan.mutationMetatable.fields {
			fmt.Fprintf(&emitter.body, "\tcase uint32(%d):\n\t\tv%d = dm%d\n", descriptor.name, destination, field)
		}
		emitter.body.WriteString("\tdefault:\n")
		fmt.Fprintf(&emitter.body, "\t\t%s\n", emitter.failureReturn())
		emitter.body.WriteString("\t}\n")
		return true, nil
	}
	fmt.Fprintf(&emitter.body, "\tv%d, ok%d = %s(", destination, operation.pc, emitter.plan.mutationMetatable.indexTarget.functionName)
	for field := range emitter.plan.mutationMetatable.fields {
		if field != 0 {
			emitter.body.WriteString(", ")
		}
		fmt.Fprintf(&emitter.body, "&bm%d", field)
	}
	if len(emitter.plan.mutationMetatable.fields) != 0 {
		emitter.body.WriteString(", ")
	}
	fmt.Fprintf(&emitter.body, "%s)\n", keyExpression)
	fmt.Fprintf(&emitter.body, "\tif !ok%d {\n", operation.pc)
	emitter.emitReplayEntry(2)
	emitter.body.WriteString("\t}\n")
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitMutationMetatableSet(
	operation *backendOperationIR,
	use func(int32) (backendValueID, error),
) (bool, error) {
	if !emitter.plan.mutationMetatable.set(operation) {
		return false, nil
	}
	key, err := use(operation.b)
	if err != nil {
		return true, err
	}
	source, err := use(operation.c)
	if err != nil {
		return true, err
	}
	emitter.emitOptionalPresenceGuard(operation, 1, key)
	fmt.Fprintf(&emitter.body, "\tok%d = %s(", operation.pc, emitter.plan.mutationMetatable.newIndexTarget.functionName)
	wrote := false
	for field := range emitter.plan.mutationMetatable.fields {
		if wrote {
			emitter.body.WriteString(", ")
		}
		fmt.Fprintf(&emitter.body, "&dm%d", field)
		wrote = true
	}
	for field := range emitter.plan.mutationMetatable.fields {
		if wrote {
			emitter.body.WriteString(", ")
		}
		fmt.Fprintf(&emitter.body, "&bm%d", field)
		wrote = true
	}
	if wrote {
		emitter.body.WriteString(", ")
	}
	fmt.Fprintf(&emitter.body, "v%d, v%d)\n", key, source)
	fmt.Fprintf(&emitter.body, "\tif !ok%d {\n", operation.pc)
	emitter.emitReplayEntry(2)
	emitter.body.WriteString("\t}\n")
	return true, nil
}

func (emitter *backendGoNumericEmitter) emitMutationMetatableBackingSet(
	operation *backendOperationIR,
	use func(int32) (backendValueID, error),
) (bool, error) {
	field, ok := emitter.plan.mutationMetatable.backingSetter(operation)
	if !ok {
		return false, nil
	}
	source, err := use(operation.c)
	if err != nil {
		return true, err
	}
	fmt.Fprintf(&emitter.body, "\tbm%d = v%d\n", field, source)
	return true, nil
}
