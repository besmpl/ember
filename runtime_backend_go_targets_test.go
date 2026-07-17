package ember

import (
	"strconv"
	"strings"
	"testing"
)

func TestInferBackendGoNumericTargetsFromVerifiedIR(t *testing.T) {
	t.Run("method receiver", func(t *testing.T) {
		irs := backendMethodProofIRs(t)
		target := inferredBackendGoTarget(t, irs, 2)
		if !target.receiverTable || target.receiverTables != 0 {
			t.Fatalf("method receiver ownership = %t/%d, want true/0", target.receiverTable, target.receiverTables)
		}
	})

	t.Run("bounded recursion", func(t *testing.T) {
		irs := backendRecursiveProofIRs(t)
		target := inferredBackendGoTarget(t, irs, 2)
		if !target.selfRecursive {
			t.Fatal("bounded recursive target was not inferred")
		}
	})

	t.Run("fixed varargs", func(t *testing.T) {
		irs := backendVarargProofIRs(t)
		target := inferredBackendGoTarget(t, irs, 2)
		if target.fixedVarargCount != 5 {
			t.Fatalf("fixed vararg count = %d, want 5", target.fixedVarargCount)
		}
	})

	t.Run("multiple receivers", func(t *testing.T) {
		irs := backendSignalBusProofIRs(t, backendSignalBusProofSource)
		target := inferredBackendGoTarget(t, irs, 3)
		if target.receiverTable || target.receiverTables != 2 {
			t.Fatalf("signal receiver ownership = %t/%d, want false/2", target.receiverTable, target.receiverTables)
		}
	})

	t.Run("captured prototype fields", func(t *testing.T) {
		irs := backendPrototypeFallbackProofIRs(t, backendPrototypeFallbackProofSource)
		target := inferredBackendGoTarget(t, irs, 2)
		want := backendPrototypeFallbackProofTargets(irs)[2]
		if !backendGoCapturedTableFieldsEqual(target.capturedTableFields, want.capturedTableFields) {
			t.Fatalf("captured prototype fields = %#v, want %#v", target.capturedTableFields, want.capturedTableFields)
		}
	})
}

func TestInferBackendGoNumericTargetsRejectsInvalidFunctionPrefix(t *testing.T) {
	irs := backendMethodProofIRs(t)
	if _, err := inferBackendGoNumericTargets(irs, []int32{2}, "not-valid"); err == nil {
		t.Fatal("inferred a target with an invalid generated function prefix")
	}
}

func TestInferBackendGoNumericTargetsRejectsAmbiguousVarargArity(t *testing.T) {
	irs := backendVarargProofIRs(t)
	mutated := *irs[1]
	mutated.ops = append([]backendOperationIR(nil), irs[1].ops...)
	for pc := range mutated.ops {
		operation := &mutated.ops[pc]
		if operation.call.kind == backendCallDirectProto && operation.call.targetProto == 2 {
			operation.callArgCount++
			break
		}
	}
	callers := append([]*backendProtoIR(nil), irs...)
	callers = append(callers, &mutated)
	if _, err := inferBackendGoNumericTargets(callers, []int32{2}, "backendGeneratedModule0"); err == nil {
		t.Fatal("inferred a variadic target with inconsistent direct call arities")
	}
}

func TestBackendGoCapturedRecordCallPreservesTargetFieldABIOrder(t *testing.T) {
	tc := loadLuauBenchmarkCases(t, "scenarioLuauCases", []string{"command_vararg_router"})[0]
	irs, _ := backendExactCorpusIRs(t, backendExactGuestBatchSource(t, tc.source, false))
	targets, err := inferBackendGoNumericTargets(irs, []int32{1, 2}, "backendGeneratedCapturedOrder")
	if err != nil {
		t.Fatal(err)
	}
	callerPlan, err := buildBackendGoNumericPlan(irs[1], backendGoNumericOptions{
		functionName:        targets[1].functionName,
		directTargets:       targets,
		capturedTableFields: targets[1].capturedTableFields,
	})
	if err != nil {
		t.Fatal(err)
	}
	targetPlan, err := buildBackendGoNumericPlan(irs[2], backendGoNumericOptions{
		functionName:        targets[2].functionName,
		fixedVarargCount:    targets[2].fixedVarargCount,
		capturedTableFields: targets[2].capturedTableFields,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(callerPlan.captured.calls) != 1 {
		t.Fatalf("captured record calls = %d, want 1", len(callerPlan.captured.calls))
	}
	for _, call := range callerPlan.captured.calls {
		if len(call.callerFields) != len(targetPlan.tables.fields) {
			t.Fatalf("captured caller fields = %d, target ABI fields = %d", len(call.callerFields), len(targetPlan.tables.fields))
		}
		for index, callerField := range call.callerFields {
			record := &callerPlan.records.records[callerField.index]
			got := record.fieldNames[callerField.field]
			want := targetPlan.tables.fields[index].key.name
			if got != want {
				t.Fatalf("captured caller field %d name = %d, target ABI name = %d", index, got, want)
			}
		}
	}
}

func TestInferBackendGoNumericTargetsInfersMutationMetatableSharedFields(t *testing.T) {
	irs := backendDirtyMetatableProofIRs(t, backendDirtyMetatableProofSource)
	targets, err := inferBackendGoNumericTargets(irs, []int32{2, 3}, "backendGeneratedModule0")
	if err != nil {
		t.Fatal(err)
	}
	want := backendDirtyMetatableProofTargets(irs)
	for _, protoID := range []int32{2, 3} {
		if !backendGoCapturedTableFieldsEqual(targets[protoID].capturedTableFields, want[protoID].capturedTableFields) {
			t.Fatalf("Proto %d captured fields = %#v, want %#v", protoID, targets[protoID].capturedTableFields, want[protoID].capturedTableFields)
		}
	}
}

func TestInferBackendGoNumericTargetsRejectsUnprovedMutationFieldDomain(t *testing.T) {
	for name, source := range map[string]string{
		"mixed sibling domain": strings.Replace(
			backendDirtyMetatableProofSource, "flags = 1", "flags = true", 1,
		),
		"broken protocol link": strings.Replace(
			backendDirtyMetatableProofSource, "__newindex =", "__call =", 1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			irs := backendDirtyMetatableProofIRs(t, source)
			if _, err := inferBackendGoNumericTargets(irs, []int32{2, 3}, "backendGeneratedModule0"); err == nil {
				t.Fatal("inferred dirty mutation fields without a proved shared domain")
			}
		})
	}
}

func TestInferBackendGoNumericTargetsRejectsAmbiguousUnusedParameter(t *testing.T) {
	irs, _ := backendExactCorpusIRs(t, `
local function kernel(seed)
    local function constant(unused)
        return 1
    end
    return constant(seed)
end
return kernel
`)
	if _, err := inferBackendGoNumericTargets(irs, []int32{2}, "backendGeneratedModule0"); err == nil {
		t.Fatal("inferred table ownership for an unused parameter without caller protocol evidence")
	}
}

func inferredBackendGoTarget(t *testing.T, irs []*backendProtoIR, protoID int32) backendGoNumericTarget {
	t.Helper()
	targets, err := inferBackendGoNumericTargets(irs, []int32{protoID}, "backendGeneratedModule0")
	if err != nil {
		t.Fatal(err)
	}
	target := targets[protoID]
	if target.ir != irs[protoID] || target.functionName != "backendGeneratedModule0Proto"+strconv.Itoa(int(protoID)) {
		t.Fatalf("inferred target identity = %p/%q", target.ir, target.functionName)
	}
	return target
}
