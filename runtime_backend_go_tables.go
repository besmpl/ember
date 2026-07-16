package ember

import "fmt"

type backendGoScalarFieldKey struct {
	table backendValueID
	name  machineStringID
}

type backendGoScalarField struct {
	key   backendGoScalarFieldKey
	tags  backendTagMask
	child backendValueID
}

type backendGoScalarTablePlan struct {
	roots  []backendValueID
	fields []backendGoScalarField
	index  map[backendGoScalarFieldKey]int
}

func analyzeBackendGoScalarTables(ir *backendProtoIR) (backendGoScalarTablePlan, error) {
	if ir == nil {
		return backendGoScalarTablePlan{}, nil
	}
	plan := backendGoScalarTablePlan{
		roots: make([]backendValueID, len(ir.values)),
		index: make(map[backendGoScalarFieldKey]int),
	}
	for valueIndex := range ir.values {
		value := &ir.values[valueIndex]
		if value.object != backendObjectTable || len(value.origins) != 1 {
			continue
		}
		root := value.origins[0]
		if !backendGoNewTableRoot(ir, root) {
			continue
		}
		plan.roots[valueIndex] = root
	}

	for iteration := 0; iteration <= len(ir.values)+len(ir.ops); iteration++ {
		changed := false
		for pc := range ir.ops {
			operation := &ir.ops[pc]
			switch operation.op {
			case opSetStringField:
				table := plan.root(backendOperationUse(operation, operation.a))
				if table == invalidBackendValueID {
					continue
				}
				name, ok := backendGoStringFieldName(ir, operation.access.constant)
				if !ok {
					return backendGoScalarTablePlan{}, nil
				}
				source := backendOperationUse(operation, operation.c)
				if !ir.validBackendValue(source) {
					return backendGoScalarTablePlan{}, nil
				}
				key := backendGoScalarFieldKey{table: table, name: name}
				fieldIndex, exists := plan.index[key]
				if !exists {
					fieldIndex = len(plan.fields)
					plan.index[key] = fieldIndex
					plan.fields = append(plan.fields, backendGoScalarField{key: key})
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
				tags := ir.values[source-1].tags
				if tags == 0 || tags&^(backendTagNumber|backendTagBool) != 0 || field.child != invalidBackendValueID {
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
				fieldIndex, exists := plan.index[backendGoScalarFieldKey{table: table, name: name}]
				if !exists {
					continue
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
			}
		}
		if !changed {
			break
		}
		if iteration == len(ir.values)+len(ir.ops) {
			return backendGoScalarTablePlan{}, fmt.Errorf("emit backend Go numeric proof: scalar table analysis did not converge")
		}
	}
	if len(plan.fields) == 0 {
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
		field, ok := plan.field(backendGoScalarFieldKey{table: table, name: name})
		if !ok || field.child == invalidBackendValueID && field.tags == 0 {
			return backendGoScalarTablePlan{}, nil
		}
	}
	return plan, nil
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

func (plan backendGoScalarTablePlan) root(id backendValueID) backendValueID {
	if id == invalidBackendValueID || int(id) > len(plan.roots) {
		return invalidBackendValueID
	}
	return plan.roots[id-1]
}

func (plan backendGoScalarTablePlan) field(key backendGoScalarFieldKey) (backendGoScalarField, bool) {
	index, ok := plan.index[key]
	if !ok || index < 0 || index >= len(plan.fields) {
		return backendGoScalarField{}, false
	}
	return plan.fields[index], true
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
	key := backendGoScalarFieldKey{table: table, name: name}
	index, ok := plan.index[key]
	if !ok || index < 0 || index >= len(plan.fields) {
		return 0, backendGoScalarField{}, false
	}
	return index, plan.fields[index], true
}
