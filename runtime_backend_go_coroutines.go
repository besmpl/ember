package ember

import (
	"fmt"
	"sort"
	"strings"
)

type backendGoScalarCoroutineResume struct {
	first bool
}

type backendGoScalarCoroutinePlan struct {
	enabled        bool
	createPC       int32
	targetProto    int32
	target         backendGoNumericTarget
	coroutineValue []bool
	closureValue   []bool
	statusValue    []bool
	deadValue      []bool
	resumes        map[int32]backendGoScalarCoroutineResume
	statuses       map[int32]bool
	yields         map[int32]int
	targetPlan     *backendGoNumericPlan
}

func backendGoNumericHasCoroutineYield(ir *backendProtoIR) bool {
	if ir == nil {
		return false
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op == opFastCall &&
			nativeFuncID(operation.nativeID) == nativeFuncCoroutineYield {
			return true
		}
	}
	return false
}

func analyzeBackendGoScalarCoroutine(
	ir *backendProtoIR,
	options backendGoNumericOptions,
) (backendGoScalarCoroutinePlan, error) {
	plan := backendGoScalarCoroutinePlan{}
	if ir == nil || options.coroutineTarget {
		return plan, nil
	}
	var coroutineOps []*backendOperationIR
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op == opFastCall && machineCoroutineNativeID(nativeFuncID(operation.nativeID)) {
			coroutineOps = append(coroutineOps, operation)
		}
	}
	if len(coroutineOps) == 0 {
		return plan, nil
	}
	var create *backendOperationIR
	for _, operation := range coroutineOps {
		if nativeFuncID(operation.nativeID) != nativeFuncCoroutineCreate {
			continue
		}
		if create != nil {
			return plan, fmt.Errorf("emit backend Go numeric proof: multiple scalar coroutine.create operations")
		}
		create = operation
	}
	if create == nil ||
		create.c != 1 ||
		create.d != 1 ||
		len(create.defs) != 1 ||
		create.defs[0].register != create.a {
		return plan, fmt.Errorf("emit backend Go numeric proof: coroutine program has no single fixed create")
	}
	closureID := backendOperationUse(create, create.a)
	targetProto, ok := backendGoScalarMethodClosure(ir, closureID)
	if !ok ||
		targetProto < 0 ||
		int(targetProto) >= len(options.directTargets) {
		return plan, fmt.Errorf("emit backend Go numeric proof: coroutine.create target is not one static closure")
	}
	target := options.directTargets[targetProto]
	if target.ir == nil || target.functionName == "" {
		return plan, fmt.Errorf("emit backend Go numeric proof: coroutine target Proto %d is unavailable", targetProto)
	}
	targetOptions := backendGoNumericOptions{
		functionName:    target.functionName,
		directTargets:   options.directTargets,
		coroutineTarget: true,
	}
	targetPlan, err := buildBackendGoNumericPlan(target.ir, targetOptions)
	if err != nil {
		return plan, fmt.Errorf("emit backend Go numeric proof: coroutine target Proto %d: %w", targetProto, err)
	}
	yields, err := analyzeBackendGoCoroutineTarget(target.ir, targetPlan)
	if err != nil {
		return plan, err
	}

	plan = backendGoScalarCoroutinePlan{
		enabled:        true,
		createPC:       create.pc,
		targetProto:    targetProto,
		target:         target,
		coroutineValue: make([]bool, len(ir.values)),
		closureValue:   make([]bool, len(ir.values)),
		statusValue:    make([]bool, len(ir.values)),
		deadValue:      make([]bool, len(ir.values)),
		resumes:        make(map[int32]backendGoScalarCoroutineResume),
		statuses:       make(map[int32]bool),
		yields:         yields,
		targetPlan:     &targetPlan,
	}
	plan.closureValue[closureID-1] = true
	plan.coroutineValue[create.defs[0].value-1] = true
	if err := propagateBackendGoCoroutineValues(ir, plan.coroutineValue); err != nil {
		return backendGoScalarCoroutinePlan{}, err
	}

	var firstResume *backendOperationIR
	for _, operation := range coroutineOps {
		switch nativeFuncID(operation.nativeID) {
		case nativeFuncCoroutineCreate:
			if operation != create {
				return backendGoScalarCoroutinePlan{}, fmt.Errorf("emit backend Go numeric proof: multiple scalar coroutine.create operations")
			}
		case nativeFuncCoroutineResume:
			if operation.d != 2 ||
				(operation.c != 1 && operation.c != 2) ||
				len(operation.defs) != 2 ||
				!plan.value(backendOperationUse(operation, operation.a)) {
				return backendGoScalarCoroutinePlan{}, fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported scalar coroutine.resume shape", operation.pc)
			}
			if firstResume == nil || operation.pc < firstResume.pc {
				firstResume = operation
			}
		case nativeFuncCoroutineStatus:
			if operation.c != 1 ||
				operation.d != 1 ||
				len(operation.defs) != 1 ||
				!plan.value(backendOperationUse(operation, operation.a)) {
				return backendGoScalarCoroutinePlan{}, fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported scalar coroutine.status shape", operation.pc)
			}
			plan.statuses[operation.pc] = true
			plan.statusValue[operation.defs[0].value-1] = true
		case nativeFuncCoroutineYield:
			return backendGoScalarCoroutinePlan{}, fmt.Errorf("emit backend Go numeric proof: PC %d yields outside the scalar coroutine target", operation.pc)
		}
	}
	if firstResume == nil || firstResume.c != 2 {
		return backendGoScalarCoroutinePlan{}, fmt.Errorf("emit backend Go numeric proof: scalar coroutine has no one-argument first resume")
	}
	for _, operation := range coroutineOps {
		if nativeFuncID(operation.nativeID) != nativeFuncCoroutineResume {
			continue
		}
		first := operation == firstResume
		if first != (operation.c == 2) {
			return backendGoScalarCoroutinePlan{}, fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar coroutine resume arguments", operation.pc)
		}
		if !backendGoScalarCoroutineOperationDominates(ir, create, operation) ||
			(!first && !backendGoScalarCoroutineOperationDominates(ir, firstResume, operation)) {
			return backendGoScalarCoroutinePlan{}, fmt.Errorf("emit backend Go numeric proof: PC %d has an unordered scalar coroutine resume", operation.pc)
		}
		plan.resumes[operation.pc] = backendGoScalarCoroutineResume{first: first}
	}
	for pc := range plan.statuses {
		operation := &ir.ops[pc]
		if !backendGoScalarCoroutineOperationDominates(ir, firstResume, operation) {
			return backendGoScalarCoroutinePlan{}, fmt.Errorf("emit backend Go numeric proof: PC %d observes coroutine status before first resume", operation.pc)
		}
	}
	if err := propagateBackendGoCoroutineValues(ir, plan.statusValue); err != nil {
		return backendGoScalarCoroutinePlan{}, err
	}
	if err := analyzeBackendGoCoroutineStatusUses(ir, options.coroutineDeadString, &plan); err != nil {
		return backendGoScalarCoroutinePlan{}, err
	}
	if err := verifyBackendGoCoroutineUses(ir, &plan); err != nil {
		return backendGoScalarCoroutinePlan{}, err
	}
	return plan, nil
}

func analyzeBackendGoCoroutineTarget(
	ir *backendProtoIR,
	plan backendGoNumericPlan,
) (map[int32]int, error) {
	if ir == nil ||
		ir.variadic ||
		len(ir.upvalues) != 0 ||
		ir.params != 1 {
		return nil, fmt.Errorf("emit backend Go numeric proof: scalar coroutine target must be a capture-free one-parameter function")
	}
	results, ok := backendGoNumericFixedResultCount(ir)
	if !ok || results != 1 {
		return nil, fmt.Errorf("emit backend Go numeric proof: scalar coroutine target must return one fixed value")
	}
	yields := make(map[int32]int)
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		switch operation.op {
		case opLoadConst, opMove,
			opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opNeg,
			opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
			opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual,
			opJumpIfNotEqualK,
			opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK,
			opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater,
			opJumpIfFalse, opJump,
			opNumericForCheck, opNumericForLoop,
			opReturnOne, opReturn:
			continue
		case opFastCall:
			if nativeFuncID(operation.nativeID) != nativeFuncCoroutineYield {
				return nil, fmt.Errorf("emit backend Go numeric proof: PC %d calls a non-yield intrinsic inside the scalar coroutine", operation.pc)
			}
		default:
			return nil, fmt.Errorf("emit backend Go numeric proof: PC %d uses unsupported scalar coroutine opcode %s", operation.pc, opcodeName(operation.op))
		}
		if operation.c != 1 ||
			operation.d != 1 ||
			len(operation.defs) != 1 {
			return nil, fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported scalar coroutine.yield shape", operation.pc)
		}
		argument := backendOperationUse(operation, operation.a)
		if !ir.validBackendValue(argument) || plan.tags[argument-1] != backendTagNumber {
			return nil, fmt.Errorf("emit backend Go numeric proof: PC %d yields a nonnumeric value", operation.pc)
		}
		for _, definition := range operation.defs {
			if plan.used[definition.value-1] {
				return nil, fmt.Errorf("emit backend Go numeric proof: PC %d consumes resumed coroutine values", operation.pc)
			}
		}
		yields[operation.pc] = len(yields) + 1
	}
	if len(yields) == 0 {
		return nil, fmt.Errorf("emit backend Go numeric proof: scalar coroutine target never yields")
	}
	return yields, nil
}

func propagateBackendGoCoroutineValues(ir *backendProtoIR, values []bool) error {
	for {
		changed := false
		for valueIndex := range ir.values {
			if values[valueIndex] {
				continue
			}
			value := &ir.values[valueIndex]
			switch value.kind {
			case backendValueOperation:
				if value.pc < 0 || int(value.pc) >= len(ir.ops) {
					continue
				}
				operation := &ir.ops[value.pc]
				if operation.op == opMove {
					source := backendOperationUse(operation, operation.b)
					if ir.validBackendValue(source) && values[source-1] {
						values[valueIndex] = true
						changed = true
					}
				}
			case backendValuePhi:
				block := &ir.blocks[value.block]
				phi := block.phis[value.register]
				hasValue := false
				hasOther := false
				for inputIndex, input := range phi.inputs {
					if !ir.blocks[block.predecessors[inputIndex]].reachable ||
						!ir.validBackendValue(input) ||
						input == value.id {
						continue
					}
					if values[input-1] {
						hasValue = true
					} else {
						hasOther = true
					}
				}
				if hasValue && hasOther {
					return fmt.Errorf("emit backend Go numeric proof: scalar coroutine identity merges with another value")
				}
				if hasValue {
					values[valueIndex] = true
					changed = true
				}
			}
		}
		if !changed {
			return nil
		}
	}
}

func analyzeBackendGoCoroutineStatusUses(
	ir *backendProtoIR,
	deadString machineStringID,
	plan *backendGoScalarCoroutinePlan,
) error {
	if deadString == invalidMachineStringID {
		return fmt.Errorf("emit backend Go numeric proof: scalar coroutine dead-string identity is unavailable")
	}
	seenComparison := false
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opEqual && operation.op != opNotEqual {
			continue
		}
		left := backendOperationUse(operation, operation.b)
		right := backendOperationUse(operation, operation.c)
		leftStatus := plan.status(left)
		rightStatus := plan.status(right)
		if leftStatus == rightStatus {
			continue
		}
		constant := right
		if rightStatus {
			constant = left
		}
		if !backendGoCoroutineDeadConstant(ir, constant, deadString) {
			return fmt.Errorf("emit backend Go numeric proof: PC %d compares coroutine status with a non-dead value", operation.pc)
		}
		plan.deadValue[constant-1] = true
		seenComparison = true
	}
	if !seenComparison {
		return fmt.Errorf("emit backend Go numeric proof: scalar coroutine status is never compared with dead")
	}
	return nil
}

func backendGoCoroutineDeadConstant(
	ir *backendProtoIR,
	id backendValueID,
	deadString machineStringID,
) bool {
	if !ir.validBackendValue(id) {
		return false
	}
	value := &ir.values[id-1]
	if value.kind != backendValueOperation ||
		value.pc < 0 ||
		int(value.pc) >= len(ir.ops) {
		return false
	}
	operation := &ir.ops[value.pc]
	return operation.op == opLoadConst &&
		operation.b >= 0 &&
		int(operation.b) < len(ir.constants) &&
		ir.constants[operation.b].kind == StringKind &&
		machineStringID(ir.constants[operation.b].bits) == deadString
}

func verifyBackendGoCoroutineUses(
	ir *backendProtoIR,
	plan *backendGoScalarCoroutinePlan,
) error {
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		for _, use := range operation.uses {
			if plan.value(use.value) {
				switch operation.op {
				case opMove:
					if use.register == operation.b {
						continue
					}
				case opFastCall:
					if use.register == operation.a &&
						(plan.resumes[operation.pc].first || plan.statuses[operation.pc]) {
						continue
					}
					if _, ok := plan.resumes[operation.pc]; ok && use.register == operation.a {
						continue
					}
				}
				return fmt.Errorf("emit backend Go numeric proof: PC %d lets the scalar coroutine escape", operation.pc)
			}
			if plan.closure(use.value) {
				if operation == &ir.ops[plan.createPC] && use.register == operation.a {
					continue
				}
				return fmt.Errorf("emit backend Go numeric proof: PC %d lets the scalar coroutine closure escape", operation.pc)
			}
			if plan.status(use.value) {
				switch operation.op {
				case opMove, opEqual, opNotEqual:
					continue
				}
				return fmt.Errorf("emit backend Go numeric proof: PC %d observes unsupported scalar coroutine status", operation.pc)
			}
			if plan.dead(use.value) {
				if operation.op == opEqual || operation.op == opNotEqual {
					continue
				}
				return fmt.Errorf("emit backend Go numeric proof: PC %d reuses the scalar coroutine dead constant", operation.pc)
			}
		}
	}
	return nil
}

func backendGoScalarCoroutineOperationDominates(
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

func (plan backendGoScalarCoroutinePlan) value(id backendValueID) bool {
	return irBoolAt(plan.coroutineValue, id)
}

func (plan backendGoScalarCoroutinePlan) closure(id backendValueID) bool {
	return irBoolAt(plan.closureValue, id)
}

func (plan backendGoScalarCoroutinePlan) status(id backendValueID) bool {
	return irBoolAt(plan.statusValue, id)
}

func (plan backendGoScalarCoroutinePlan) dead(id backendValueID) bool {
	return irBoolAt(plan.deadValue, id)
}

func irBoolAt(values []bool, id backendValueID) bool {
	return id != invalidBackendValueID && int(id) <= len(values) && values[id-1]
}

func (plan backendGoScalarCoroutinePlan) resume(
	operation *backendOperationIR,
) (backendGoScalarCoroutineResume, bool) {
	if operation == nil {
		return backendGoScalarCoroutineResume{}, false
	}
	resume, ok := plan.resumes[operation.pc]
	return resume, ok
}

func (plan backendGoScalarCoroutinePlan) statusOperation(operation *backendOperationIR) bool {
	return operation != nil && plan.statuses[operation.pc]
}

func (plan backendGoScalarCoroutinePlan) createOperation(operation *backendOperationIR) bool {
	return operation != nil && plan.enabled && operation.pc == plan.createPC
}

func emitBackendGoCoroutineTarget(
	source *strings.Builder,
	target backendGoNumericTarget,
	plan backendGoNumericPlan,
	yields map[int32]int,
) (bool, error) {
	emitter := backendGoNumericEmitter{
		ir:          target.ir,
		plan:        plan,
		resultCount: 1,
		options: backendGoNumericOptions{
			functionName:    target.functionName,
			coroutineTarget: true,
		},
	}
	stateName := target.functionName + "State"
	fmt.Fprintf(source, "type %s struct {\n\tstate uint32\n", stateName)
	for valueIndex, used := range plan.used {
		if !used || plan.tags[valueIndex] == backendTagNil {
			continue
		}
		goType, ok := backendGoNumericType(plan.tags[valueIndex])
		if !ok {
			return false, fmt.Errorf("emit backend Go numeric proof: coroutine state SSA value %d has unsupported tags %x", valueIndex+1, plan.tags[valueIndex])
		}
		fmt.Fprintf(source, "\tv%d %s\n", valueIndex+1, goType)
	}
	source.WriteString("}\n\n")
	fmt.Fprintf(source, "func %s(state *%s, p0 float64, first bool) (float64, bool, bool) {\n", target.functionName, stateName)
	source.WriteString("\tif state == nil {\n\t\treturn 0, false, false\n\t}\n")
	for valueIndex, used := range plan.used {
		if !used || plan.tags[valueIndex] == backendTagNil {
			continue
		}
		goType, _ := backendGoNumericType(plan.tags[valueIndex])
		fmt.Fprintf(source, "\tvar v%d %s\n", valueIndex+1, goType)
	}
	initial := target.ir.initial[0]
	source.WriteString("\tswitch state.state {\n")
	fmt.Fprintf(source, "\tcase 0:\n\t\tif !first {\n\t\t\treturn 0, false, false\n\t\t}\n\t\tv%d = p0\n", initial)
	yieldPCs := make([]int, 0, len(yields))
	for pc := range yields {
		yieldPCs = append(yieldPCs, int(pc))
	}
	sort.Ints(yieldPCs)
	for _, rawPC := range yieldPCs {
		pc := int32(rawPC)
		stateID := yields[pc]
		operation := &target.ir.ops[pc]
		fmt.Fprintf(source, "\tcase %d:\n\t\tif first {\n\t\t\treturn 0, false, false\n\t\t}\n", stateID)
		for valueIndex, used := range plan.used {
			if !used || plan.tags[valueIndex] == backendTagNil {
				continue
			}
			fmt.Fprintf(source, "\t\tv%d = state.v%d\n", valueIndex+1, valueIndex+1)
		}
		if len(target.ir.blocks[operation.block].successors) != 1 {
			return false, fmt.Errorf("emit backend Go numeric proof: coroutine yield PC %d has no single continuation", pc)
		}
		emitter.emitGoto(operation.block, target.ir.blocks[operation.block].successors[0], 2)
		source.WriteString(emitter.body.String())
		emitter.body.Reset()
	}
	fmt.Fprintf(source, "\tcase %d:\n\t\treturn 0, true, false\n", len(yields)+1)
	source.WriteString("\tdefault:\n\t\treturn 0, false, false\n\t}\n")
	if err := emitter.emitCoroutineBody(yields); err != nil {
		return false, err
	}
	source.WriteString(emitter.body.String())
	source.WriteString("}\n\n")
	return emitter.needsMath, nil
}

func (emitter *backendGoNumericEmitter) emitCoroutineBody(yields map[int32]int) error {
	for blockIndex := range emitter.ir.blocks {
		block := &emitter.ir.blocks[blockIndex]
		if !block.reachable {
			continue
		}
		if blockIndex != 0 {
			fmt.Fprintf(&emitter.body, "b%d:\n", blockIndex)
		}
		terminated := false
		for pc := block.first; pc < block.last; pc++ {
			operation := &emitter.ir.ops[pc]
			if stateID, ok := yields[operation.pc]; ok {
				argument := backendOperationUse(operation, operation.a)
				if !emitter.ir.validBackendValue(argument) {
					return fmt.Errorf("emit backend Go numeric proof: PC %d has no coroutine yield argument", operation.pc)
				}
				fmt.Fprintf(&emitter.body, "\tstate.state = %d\n", stateID)
				for valueIndex, used := range emitter.plan.used {
					if !used || emitter.plan.tags[valueIndex] == backendTagNil {
						continue
					}
					fmt.Fprintf(&emitter.body, "\tstate.v%d = v%d\n", valueIndex+1, valueIndex+1)
				}
				fmt.Fprintf(&emitter.body, "\treturn v%d, false, true\n", argument)
				terminated = true
				break
			}
			if operation.op == opReturn || operation.op == opReturnOne {
				value := backendOperationUse(operation, operation.a)
				if !emitter.ir.validBackendValue(value) {
					return fmt.Errorf("emit backend Go numeric proof: PC %d has no coroutine return value", operation.pc)
				}
				fmt.Fprintf(&emitter.body, "\tstate.state = %d\n", len(yields)+1)
				fmt.Fprintf(&emitter.body, "\treturn v%d, true, true\n", value)
				terminated = true
				break
			}
			var err error
			terminated, err = emitter.emitOperation(operation, block)
			if err != nil {
				return err
			}
			if terminated {
				break
			}
		}
		if terminated {
			continue
		}
		if len(block.successors) != 1 {
			return fmt.Errorf("emit backend Go numeric proof: coroutine block %d has no terminator and %d successors", blockIndex, len(block.successors))
		}
		emitter.emitGoto(int32(blockIndex), block.successors[0], 1)
	}
	return nil
}
