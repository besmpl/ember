package ember

import (
	"bytes"
	"context"
	goparser "go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

const backendPrototypeFallbackProofSource = `
local function kernel(seed)
    local prototype = {hp = 20 + seed % 3, mana = 5, armor = 2}
    local misses = 0
    local mt = {
        __index = function(_, key)
            misses = misses + 1
            if key == "power" then
                return prototype.hp + prototype.mana
            end
            return prototype[key] or 0
        end,
    }
    local actors = {
        setmetatable({hp = 80 + seed % 2}, mt),
        setmetatable({mana = 15, armor = 4}, mt),
        setmetatable({hp = 45, power = 9}, mt),
    }
    local total = 0
    for tick = 1, 80 + seed % 2 do
        for _, actor in actors do
            local value = actor.hp + actor.mana + actor.power
            if actor.armor > 3 then
                actor.hp = actor.hp + tick % 3 - actor.armor
            else
                actor.mana = actor.mana + tick % 4
            end
            total = total + value + actor.hp + actor.mana
        end
    end
    return total + misses
end
return kernel
`

func backendPrototypeFallbackProofIRs(t *testing.T, source string) []*backendProtoIR {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 3 {
		t.Fatalf("prototype-fallback Proto count = %d, want 3", len(image.prototypes))
	}
	irs := make([]*backendProtoIR, len(image.prototypes))
	for protoID := range image.prototypes {
		irs[protoID], err = buildBackendProtoIR(&image.prototypes[protoID])
		if err != nil {
			t.Fatal(err)
		}
	}
	return irs
}

func backendPrototypeFallbackProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	var closure *backendOperationIR
	for pc := range irs[1].ops {
		operation := &irs[1].ops[pc]
		if operation.op == opClosure && operation.targetProto == 2 {
			closure = operation
			break
		}
	}
	capturedFields, _ := backendGoCapturedTableFieldsForClosure(irs[1], irs[2], closure)
	targets[2] = backendGoNumericTarget{
		ir: irs[2], functionName: "backendGeneratedPrototypeIndex", receiverTable: true,
		capturedTableFields: capturedFields,
	}
	return targets
}

func backendPrototypeFallbackGeneratedSources(t *testing.T, source string) ([]byte, []byte) {
	t.Helper()
	irs := backendPrototypeFallbackProofIRs(t, source)
	targets := backendPrototypeFallbackProofTargets(irs)
	target, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName: "ember", functionName: targets[2].functionName, receiverTable: true,
		capturedTableFields: targets[2].capturedTableFields,
	})
	if err != nil {
		t.Fatal(err)
	}
	caller, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName: "ember", functionName: "backendGeneratedPrototypeFallback",
		preparedFunctionName: "backendGeneratedPrototypeFallbackPreparedFixture", directTargets: targets,
	})
	if err != nil {
		t.Fatal(err)
	}
	return target, caller
}

func TestBackendGoStaticIndexFunctionCanGenerate(t *testing.T) {
	irs := backendPrototypeFallbackProofIRs(t, backendPrototypeFallbackProofSource)
	targets := backendPrototypeFallbackProofTargets(irs)
	if _, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName: "ember", functionName: "backendGeneratedPrototypeIndex", receiverTable: true,
		capturedTableFields: targets[2].capturedTableFields,
	}); err != nil {
		t.Fatalf("emit prototype-index target: %v", err)
	}
	if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedPrototypeFallback",
		preparedFunctionName: "backendGeneratedPrototypeFallbackPreparedFixture",
		directTargets:        targets,
	}); err != nil {
		t.Fatalf("emit prototype-fallback caller: %v", err)
	}
	plan, err := buildBackendGoNumericPlan(irs[1], backendGoNumericOptions{directTargets: targets})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.indexFunctions.calls) != 9 {
		t.Fatalf("prototype-index calls = %d, want 9", len(plan.indexFunctions.calls))
	}
}

func TestBackendGoRecordRootsPreserveSetMetatableIdentity(t *testing.T) {
	irs := backendPrototypeFallbackProofIRs(t, backendPrototypeFallbackProofSource)
	keys := analyzeBackendGoStructuralKeys(irs[1], backendGoNumericOptions{})
	plan := analyzeBackendGoRecordTables(irs[1], keys)
	if len(plan.arrays) != 1 {
		t.Fatalf("record arrays after setmetatable = %d, want 1 (rejected: %s)", len(plan.arrays), plan.rejectReason)
	}
	if !plan.enabled {
		t.Fatalf("record plan after setmetatable rejected: %s", plan.rejectReason)
	}
}

func TestBackendGoCapturedTableRoleStaysSeparateFromNumericCell(t *testing.T) {
	irs := backendPrototypeFallbackProofIRs(t, backendPrototypeFallbackProofSource)
	targets := backendPrototypeFallbackProofTargets(irs)
	plan, err := analyzeBackendGoScalarTablesExcludingCountWithFields(
		irs[2], 1, nil, targets[2].capturedTableFields,
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.tableUpvalue(0) || !plan.tableUpvalue(1) {
		t.Fatalf("captured table roles = %v (inventory %v), want only upvalue 1", plan.tableUpvalues, backendGoCapturedRecordUpvalues(irs[2]))
	}
	if len(plan.fields) < 2 {
		t.Fatalf("captured prototype fields = %d, want at least 2", len(plan.fields))
	}
}

func TestBackendGoCapturedTableRoleRejectsMixedUpvalueUses(t *testing.T) {
	source := strings.Replace(
		backendPrototypeFallbackProofSource,
		"__index = function(_, key)\n            misses = misses + 1",
		"__index = function(_, key)\n            if prototype == nil then return 0 end\n            misses = misses + 1",
		1,
	)
	irs := backendPrototypeFallbackProofIRs(t, source)
	if upvalues := backendGoCapturedRecordUpvalues(irs[2]); len(upvalues) != 0 {
		t.Fatalf("mixed-use captured table upvalues = %v, want none", upvalues)
	}
}

func TestBackendGoPrototypeFallbackFixturesAreFreshAndCorrect(t *testing.T) {
	target, caller := backendPrototypeFallbackGeneratedSources(t, backendPrototypeFallbackProofSource)
	for _, fixture := range []struct {
		name      string
		generated []byte
	}{
		{"runtime_backend_prototype_index_generated_test.go", target},
		{"runtime_backend_prototype_fallback_generated_test.go", caller},
	} {
		onDisk, err := os.ReadFile(fixture.name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixture.generated, onDisk) {
			t.Fatalf("generated prototype-fallback fixture %s is stale", fixture.name)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), fixture.name, fixture.generated, goparser.AllErrors); err != nil {
			t.Fatalf("parse generated prototype-fallback source %s: %v", fixture.name, err)
		}
	}
	text := string(append(append([]byte(nil), target...), caller...))
	for _, required := range []string{
		"func backendGeneratedPrototypeIndex(u0 *float64", "switch v28", "*u0 = v18",
		"backendGeneratedPrototypeIndex(&v61", "backendGeneratedPrototypeIndex(&v89",
		"if rap0_0", "if rap0_1", "if rap0_2", "if rap0_3",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated prototype-fallback source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"GET_STRING_FIELD", "SET_METATABLE", "FAST_CALL",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated prototype-fallback source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendPrototypeFallbackProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedPrototypeFallback(seed)
		if !ok {
			t.Fatalf("generated prototype-fallback fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("prototype-fallback oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, number := oracle[0].Number()
		if !number || got != oracleNumber {
			t.Fatalf("generated/oracle prototype-fallback seed %v = %v/%v (%t)", seed, got, oracleNumber, number)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedPrototypeFallback(29)
		}); allocations != 0 {
			t.Fatalf("generated prototype-fallback allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoPrototypeFallbackIsIdentityBlindAndFailClosed(t *testing.T) {
	emit := func(source string) []byte {
		target, caller := backendPrototypeFallbackGeneratedSources(t, source)
		return append(target, caller...)
	}
	renamed := strings.Replace(backendPrototypeFallbackProofSource, "local function kernel(seed)", "local function opaque(seed)", 1)
	renamed = strings.Replace(renamed, "return kernel", "return opaque", 1)
	if !bytes.Equal(emit(backendPrototypeFallbackProofSource), emit(renamed)) {
		t.Fatal("prototype-fallback lowering depends on private function identity")
	}

	for name, source := range map[string]string{
		"mixed captured field": strings.Replace(
			backendPrototypeFallbackProofSource, "mana = 5, armor = 2", "mana = true, armor = 2", 1,
		),
		"changed index field": strings.Replace(
			backendPrototypeFallbackProofSource, "local actors = {", "mt.__index = mt\n    local actors = {", 1,
		),
		"mixed metatable": strings.Replace(
			backendPrototypeFallbackProofSource,
			"setmetatable({hp = 45, power = 9}, mt)",
			"setmetatable({hp = 45, power = 9}, {})",
			1,
		),
		"observed metatable": strings.Replace(
			backendPrototypeFallbackProofSource,
			"return total + misses",
			"return total + misses + (getmetatable(actors[1]) == mt and 0 or 1)",
			1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() != nil {
					t.Fatalf("prototype-fallback compiler panicked for %s", name)
				}
			}()
			irs := backendPrototypeFallbackProofIRs(t, source)
			targets := backendPrototypeFallbackProofTargets(irs)
			if _, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
				packageName: "ember", functionName: "rejectPrototypeIndex", receiverTable: true,
				capturedTableFields: targets[2].capturedTableFields,
			}); err == nil {
				if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
					packageName: "ember", functionName: "rejectPrototypeFallback", directTargets: targets,
				}); err == nil {
					t.Fatalf("prototype-fallback compiler accepted %s", name)
				}
			}
		})
	}
}

func BenchmarkBackendGeneratedPrototypeFallback(b *testing.B) {
	for b.Loop() {
		_, _ = backendGeneratedPrototypeFallback(29)
	}
}
