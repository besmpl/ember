package ember

import (
	"strconv"
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

func TestInferBackendGoNumericTargetsRejectsUnboundedCapturedFields(t *testing.T) {
	irs := backendDirtyMetatableProofIRs(t, backendDirtyMetatableProofSource)
	if _, err := inferBackendGoNumericTargets(irs, []int32{3}, "backendGeneratedModule0"); err == nil {
		t.Fatal("inferred captured fields for a dirty table without a proved shared field domain")
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
