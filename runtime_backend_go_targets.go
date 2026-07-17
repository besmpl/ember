package ember

import (
	"errors"
	"fmt"
	"go/token"
	"strconv"
)

var errBackendGoUnboundedCapturedTableFields = errors.New("unbounded captured table fields")

// inferBackendGoNumericTargets derives the target ABI facts needed by the Go
// lowerer from verified module IR. Callers choose the candidate Proto IDs; this
// function does not make reachability or product-policy decisions for them.
func inferBackendGoNumericTargets(
	irs []*backendProtoIR,
	protoIDs []int32,
	functionPrefix string,
) ([]backendGoNumericTarget, error) {
	if !token.IsIdentifier(functionPrefix) || token.Lookup(functionPrefix).IsKeyword() {
		return nil, fmt.Errorf("infer backend Go targets: invalid function prefix %q", functionPrefix)
	}
	for protoID, ir := range irs {
		if err := verifyBackendProtoIR(ir); err != nil {
			return nil, fmt.Errorf("infer backend Go targets: Proto %d: %w", protoID, err)
		}
	}
	targets := make([]backendGoNumericTarget, len(irs))
	seen := make([]bool, len(irs))
	captureErrors := make([]error, len(irs))
	for _, protoID := range protoIDs {
		if protoID < 0 || int(protoID) >= len(irs) || irs[protoID] == nil {
			return nil, fmt.Errorf("infer backend Go targets: invalid Proto %d", protoID)
		}
		if seen[protoID] {
			return nil, fmt.Errorf("infer backend Go targets: duplicate Proto %d", protoID)
		}
		seen[protoID] = true

		ir := irs[protoID]
		fixedVarargCount, err := inferBackendGoFixedVarargCount(irs, protoID, ir)
		if err != nil {
			return nil, err
		}
		capturedTableFields, err := inferBackendGoCapturedTableFields(irs, protoID, ir)
		if err != nil && !errors.Is(err, errBackendGoUnboundedCapturedTableFields) {
			return nil, err
		}
		captureErrors[protoID] = err

		target := backendGoNumericTarget{
			ir:                  ir,
			functionName:        functionPrefix + "Proto" + strconv.Itoa(int(protoID)),
			selfRecursive:       backendGoNumericSelfRecursiveTarget(ir),
			fixedVarargCount:    fixedVarargCount,
			capturedTableFields: capturedTableFields,
		}
		targets[protoID] = target
	}
	for _, protoID := range protoIDs {
		if captureErrors[protoID] == nil {
			continue
		}
		fields, ok := inferBackendGoMutationMetatableFields(irs, protoIDs, targets, protoID)
		if !ok {
			return nil, captureErrors[protoID]
		}
		targets[protoID].capturedTableFields = fields
	}
	for _, protoID := range protoIDs {
		target := &targets[protoID]
		receiverTables, err := inferBackendGoReceiverTableCount(
			irs, protoID, target.ir, target.fixedVarargCount, target.capturedTableFields,
		)
		if err != nil {
			return nil, fmt.Errorf("infer backend Go targets: Proto %d: %w", protoID, err)
		}
		if receiverTables == 1 {
			target.receiverTable = true
		} else if receiverTables > 1 {
			target.receiverTables = receiverTables
		}
	}
	return targets, nil
}

func inferBackendGoMutationMetatableFields(
	irs []*backendProtoIR,
	protoIDs []int32,
	targets []backendGoNumericTarget,
	protoID int32,
) (map[int32][]backendGoCapturedTableField, bool) {
	target := targets[protoID]
	upvalues := backendGoCapturedRecordUpvalues(target.ir)
	if len(upvalues) == 0 || inferBackendGoProtocolReceiverTableCount(irs, protoID) != 1 {
		return nil, false
	}
	candidates := make([][]backendGoCapturedTableField, 0)
	for _, candidateID := range protoIDs {
		candidate := targets[candidateID]
		for upvalue := 0; upvalue < len(candidate.ir.upvalues); upvalue++ {
			fields := candidate.capturedTableFields[int32(upvalue)]
			if len(fields) == 0 || backendGoCapturedFieldSlicePresent(candidates, fields) {
				continue
			}
			candidates = append(candidates, append([]backendGoCapturedTableField(nil), fields...))
		}
	}
	var resolved map[int32][]backendGoCapturedTableField
	for _, candidate := range candidates {
		hypothesis := make(map[int32][]backendGoCapturedTableField, len(upvalues))
		for upvalue := 0; upvalue < len(target.ir.upvalues); upvalue++ {
			if len(upvalues[int32(upvalue)]) != 0 {
				hypothesis[int32(upvalue)] = append([]backendGoCapturedTableField(nil), candidate...)
			}
		}
		provisional := append([]backendGoNumericTarget(nil), targets...)
		provisional[protoID].capturedTableFields = hypothesis
		for _, candidateID := range protoIDs {
			if inferBackendGoProtocolReceiverTableCount(irs, candidateID) == 1 {
				provisional[candidateID].receiverTable = true
			}
		}
		proved := false
		for _, caller := range irs {
			plan := discoverBackendGoMutationMetatable(caller, backendGoNumericOptions{
				directTargets: provisional,
			})
			if plan.enabled && plan.newIndexTarget.ir == target.ir &&
				backendGoCapturedFieldSlicesEqual(plan.fields, candidate) {
				proved = true
				break
			}
		}
		if !proved {
			continue
		}
		if resolved != nil {
			return nil, false
		}
		resolved = hypothesis
	}
	return resolved, resolved != nil
}

func backendGoCapturedFieldSlicePresent(
	candidates [][]backendGoCapturedTableField,
	fields []backendGoCapturedTableField,
) bool {
	for _, candidate := range candidates {
		if backendGoCapturedFieldSlicesEqual(candidate, fields) {
			return true
		}
	}
	return false
}

func inferBackendGoReceiverTableCount(
	irs []*backendProtoIR,
	protoID int32,
	ir *backendProtoIR,
	fixedVarargCount int,
	capturedTableFields map[int32][]backendGoCapturedTableField,
) (int, error) {
	required := inferBackendGoProtocolReceiverTableCount(irs, protoID)
	valid := -1
	for receiverTables := 0; receiverTables <= ir.params; receiverTables++ {
		if required >= 0 && receiverTables != required {
			continue
		}
		options := backendGoNumericOptions{
			functionName:        "backendInferredTarget",
			selfRecursive:       backendGoNumericSelfRecursiveTarget(ir),
			fixedVarargCount:    fixedVarargCount,
			capturedTableFields: capturedTableFields,
		}
		if receiverTables == 1 {
			options.receiverTable = true
		} else if receiverTables > 1 {
			options.receiverTables = receiverTables
		}
		if _, err := buildBackendGoNumericPlan(ir, options); err != nil {
			continue
		}
		if valid >= 0 {
			return 0, fmt.Errorf("ambiguous receiver-table counts %d and %d", valid, receiverTables)
		}
		valid = receiverTables
	}
	if valid < 0 {
		// Closure factories and coroutine targets can be valid only in their
		// caller/role context. With no positive receiver evidence, keep the
		// descriptor owner-neutral and let target verification decide eligibility.
		return 0, nil
	}
	return valid, nil
}

func inferBackendGoProtocolReceiverTableCount(irs []*backendProtoIR, protoID int32) int {
	for _, caller := range irs {
		if caller == nil {
			continue
		}
		for pc := range caller.ops {
			operation := &caller.ops[pc]
			if operation.op != opSetStringField ||
				(!backendGoStringFieldIsIndex(caller, operation.access.constant) &&
					!backendGoStringFieldIsNewIndex(caller, operation.access.constant)) {
				continue
			}
			_, closure, ok := backendGoCapturedClosure(
				caller, backendOperationUse(operation, operation.c), nil,
			)
			if !ok || closure.targetProto != protoID {
				continue
			}
			return 1
		}
	}
	return -1
}

func inferBackendGoFixedVarargCount(
	irs []*backendProtoIR,
	protoID int32,
	target *backendProtoIR,
) (int, error) {
	if !target.variadic {
		return 0, nil
	}
	fixed := -1
	for _, caller := range irs {
		if caller == nil {
			continue
		}
		for pc := range caller.ops {
			operation := &caller.ops[pc]
			if operation.call.kind != backendCallDirectProto || operation.call.targetProto != protoID {
				continue
			}
			if operation.callArgCount < int32(target.params) {
				return 0, fmt.Errorf("infer backend Go targets: Proto %d call at PC %d has invalid arity %d", protoID, operation.pc, operation.callArgCount)
			}
			count := int(operation.callArgCount) - target.params
			if fixed >= 0 && count != fixed {
				return 0, fmt.Errorf("infer backend Go targets: Proto %d has inconsistent fixed vararg counts %d and %d", protoID, fixed, count)
			}
			fixed = count
		}
	}
	if fixed < 0 {
		return 0, fmt.Errorf("infer backend Go targets: variadic Proto %d has no fixed direct call arity", protoID)
	}
	return fixed, nil
}

func inferBackendGoCapturedTableFields(
	irs []*backendProtoIR,
	protoID int32,
	target *backendProtoIR,
) (map[int32][]backendGoCapturedTableField, error) {
	var fields map[int32][]backendGoCapturedTableField
	for _, caller := range irs {
		if caller == nil {
			continue
		}
		for pc := range caller.ops {
			closure := &caller.ops[pc]
			if closure.op != opClosure || closure.targetProto != protoID {
				continue
			}
			candidate, ok := backendGoCapturedTableFieldsForClosure(caller, target, closure)
			if !ok {
				return nil, fmt.Errorf(
					"infer backend Go targets: Proto %d closure at PC %d: %w",
					protoID, closure.pc, errBackendGoUnboundedCapturedTableFields,
				)
			}
			if len(candidate) == 0 {
				continue
			}
			if fields != nil && !backendGoCapturedTableFieldsEqual(fields, candidate) {
				return nil, fmt.Errorf("infer backend Go targets: Proto %d closure sites disagree on captured table fields", protoID)
			}
			fields = candidate
		}
	}
	return fields, nil
}
