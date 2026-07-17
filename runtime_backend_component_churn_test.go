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

const backendComponentChurnProofSource = `
local function kernel(seed)
    local entities = {
        {id = 1, components = {hp = 100 + seed % 9, mana = 20, poison = 0}, dirty = false},
        {id = 2, components = {hp = 85, shield = 12, speed = 4}, dirty = false},
        {id = 3, components = {hp = 130, mana = 5, poison = 2}, dirty = false},
        {id = 4, components = {hp = 60, shield = 30, speed = 7}, dirty = false},
    }
    local keys = {"hp", "mana", "poison", "shield", "speed"}
    local score = 0
    for tick = 1, 60 + seed % 3 do
        for _, entity in entities do
            local key = keys[(tick + entity.id) % rawlen(keys) + 1]
            local value = entity.components[key]
            if value == nil then
                entity.components[key] = tick % 7 + entity.id
                entity.dirty = true
                score = score + entity.components[key]
            else
                entity.components[key] = value + tick % 5 - 1
                score = score + entity.components[key]
                if key ~= "hp" and entity.components[key] % 11 == 0 then
                    entity.components[key] = nil
                    entity.dirty = true
                end
            end
            if entity.components.hp ~= nil and entity.components.poison ~= nil and entity.components.poison > 0 then
                entity.components.hp = entity.components.hp - entity.components.poison
            end
            if entity.dirty then
                score = score + entity.id
                entity.dirty = false
            end
        end
    end
    return score + (entities[1].components.hp or 0) + (entities[2].components.hp or 0) + (entities[3].components.hp or 0) + (entities[4].components.hp or 0)
end
return kernel
`

func TestBackendGoDynamicOptionalComponentRecordsCanGenerate(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendComponentChurnProofSource)
	records := analyzeBackendGoRecordTables(ir, analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{}))
	if !records.enabled {
		t.Fatalf("analyze component records: %s", records.rejectReason)
	}
	if len(records.arrays) != 1 || len(records.records) != 8 || len(records.childRecords) != 1 ||
		len(records.fusedGetByPC) != 4 || len(records.fusedSetByPC) != 3 ||
		len(records.childRecords[0].fieldNames) != 5 {
		t.Fatalf(
			"component inventory = arrays %d records %d children %d fused %d/%d fields %d",
			len(records.arrays), len(records.records), len(records.childRecords),
			len(records.fusedGetByPC), len(records.fusedSetByPC),
			len(records.childRecords[0].fieldNames),
		)
	}
	plan, err := buildBackendGoNumericPlan(ir, backendGoNumericOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.tables.arrays) != 1 || plan.tables.arrays[0].tags != backendTagString ||
		plan.tables.arrays[0].length != 5 || len(plan.tables.arrayGetByPC) != 1 {
		t.Fatalf("component scalar-key array = %#v gets %d", plan.tables.arrays, len(plan.tables.arrayGetByPC))
	}
	if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedComponentChurn",
		preparedFunctionName: "backendGeneratedComponentChurnPreparedFixture",
	}); err != nil {
		t.Fatalf("emit dynamic optional component records: %v", err)
	}
}

func TestBackendGoDynamicOptionalComponentRecordsFixtureIsFreshAndCorrect(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendComponentChurnProofSource)
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedComponentChurn",
		preparedFunctionName: "backendGeneratedComponentChurnPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_component_churn_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated component-churn fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), fixture, generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated component-churn source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var a0 [5]uint32", "math.Trunc", "var rp", "switch v", " = false",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated component-churn source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "GET_INDEX", "SET_INDEX", "GET_STRING_FIELD_INDEX", "SET_STRING_FIELD_INDEX",
		"PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated component-churn source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendComponentChurnProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedComponentChurn(seed)
		if !ok {
			t.Fatalf("generated component-churn fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("component-churn oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, number := oracle[0].Number()
		if !number || got != oracleNumber {
			t.Fatalf("generated/oracle component-churn seed %v = %v/%v (%t)", seed, got, oracleNumber, number)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedComponentChurn(29)
		}); allocations != 0 {
			t.Fatalf("generated component-churn allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoDynamicOptionalComponentRecordsAreIdentityBlindAndFailClosed(t *testing.T) {
	emit := func(source string) []byte {
		generated, err := emitBackendGoNumericProof(
			backendRecordArrayProofIR(t, source),
			backendGoNumericOptions{
				packageName:          "ember",
				functionName:         "identityBlindComponentChurn",
				preparedFunctionName: "identityBlindComponentChurnPrepared",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		return generated
	}
	renamed := strings.Replace(backendComponentChurnProofSource, "local function kernel(seed)", "local function opaque(seed)", 1)
	renamed = strings.Replace(renamed, "return kernel", "return opaque", 1)
	if !bytes.Equal(emit(backendComponentChurnProofSource), emit(renamed)) {
		t.Fatal("component-churn lowering depends on private function identity")
	}

	for name, source := range map[string]string{
		"mixed scalar key array": strings.Replace(backendComponentChurnProofSource, `"speed"}`, `1}`, 1),
		"mixed component payload": strings.Replace(
			backendComponentChurnProofSource,
			"entity.components[key] = tick % 7 + entity.id",
			`entity.components[key] = "bad"`,
			1,
		),
		"escaping component record": strings.Replace(
			backendComponentChurnProofSource,
			"return score + (entities[1].components.hp or 0) + (entities[2].components.hp or 0) + (entities[3].components.hp or 0) + (entities[4].components.hp or 0)",
			"return entities[1].components",
			1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := emitBackendGoNumericProof(
				backendRecordArrayProofIR(t, source),
				backendGoNumericOptions{packageName: "ember", functionName: "rejectUnprovedComponentChurn"},
			); err == nil {
				t.Fatalf("component compiler accepted %s", name)
			}
		})
	}
}

func BenchmarkBackendGeneratedComponentChurn(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedComponentChurn(float64(iteration & 31))
		if !ok {
			b.Fatal("generated component-churn fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
