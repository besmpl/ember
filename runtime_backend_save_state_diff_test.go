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

const backendSaveStateDiffProofSource = `
local function kernel(seed)
    local before = {
        {id = "p1", hp = 100, zone = "town", inv = {coins = 20, herbs = 3, ore = 0}},
        {id = "p2", hp = 85, zone = "mine", inv = {coins = 5, herbs = 0, ore = 8}},
        {id = "npc1", hp = 40, zone = "road", inv = {coins = 2, herbs = 1, ore = 0}},
    }
    local after = {
        {id = "p1", hp = 92, zone = "road", inv = {coins = 17, herbs = 5, ore = 0}},
        {id = "p2", hp = 85, zone = "mine", inv = {coins = 12, herbs = 0, ore = 4}},
        {id = "npc1", hp = 0, zone = "road", inv = {coins = 2, herbs = 1, ore = 0}},
    }
    local fields = {"coins", "herbs", "ore"}
    local total = seed - seed
    for pass = 1, 90 do
        for i, left in before do
            local right = after[i]
            if left.hp ~= right.hp then
                local delta = left.hp - right.hp
                if delta < 0 then delta = -delta end
                total = total + delta + pass % 5
            end
            if left.zone ~= right.zone then
                total = total + 17
            end
            for _, field in fields do
                local delta = left.inv[field] - right.inv[field]
                if delta < 0 then delta = -delta end
                total = total + delta * (pass % 3 + 1)
            end
        end
    end
    return total
end
return kernel
`

func TestBackendGoSaveStateDiffFusedRecordsAreRecognized(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendSaveStateDiffProofSource)
	records := analyzeBackendGoRecordTables(ir, analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{}))
	if !records.enabled {
		t.Fatalf("save-state records were not recognized: %s", records.rejectReason)
	}
	if len(records.maps) != 0 || len(records.arrays) != 2 || len(records.records) != 12 ||
		len(records.childRecords) != 2 || len(records.fusedGetByPC) != 2 {
		t.Fatalf(
			"save-state inventory = maps %d arrays %d records %d children %d fused %d, want 0/2/12/2/2",
			len(records.maps), len(records.arrays), len(records.records), len(records.childRecords), len(records.fusedGetByPC),
		)
	}
	plan, err := buildBackendGoNumericPlan(ir, backendGoNumericOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scalarLength := uint32(0)
	if len(plan.tables.arrays) != 0 {
		scalarLength = plan.tables.arrays[0].length
	}
	if !plan.records.enabled || len(plan.tables.arrays) != 1 || scalarLength != 3 {
		t.Fatalf(
			"save-state mixed scalarizers = records %t scalar arrays %d length %d, want true/1/3",
			plan.records.enabled,
			len(plan.tables.arrays),
			scalarLength,
		)
	}
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedSaveStateDiff",
		preparedFunctionName: "backendGeneratedSaveStateDiffPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "generated_save_state_diff.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated save-state source: %v", err)
	}
}

func TestBackendGoSaveStateDiffFixtureIsFreshAndCorrect(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendSaveStateDiffProofSource)
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedSaveStateDiff",
		preparedFunctionName: "backendGeneratedSaveStateDiffPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_save_state_diff_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated save-state fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), fixture, generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated save-state source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var a0 [3]uint32",
		"var ra0_0 [3]uint32",
		"var ra1_3 [3]float64",
		"switch int(v",
		"case uint32(6):",
		"case uint32(7):",
		"case uint32(8):",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated save-state source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "SET_FIELD", "GET_FIELD", "GET_STRING_FIELD_INDEX", "PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated save-state source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendSaveStateDiffProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedSaveStateDiff(seed)
		if !ok {
			t.Fatalf("generated save-state fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("save-state oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle save-state seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedSaveStateDiff(29)
		}); allocations != 0 {
			t.Fatalf("generated save-state allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoSaveStateDiffIsIdentityBlindAndRejectsUnprovedChildren(t *testing.T) {
	emit := func(source string) []byte {
		generated, err := emitBackendGoNumericProof(
			backendRecordArrayProofIR(t, source),
			backendGoNumericOptions{
				packageName:          "ember",
				functionName:         "identityBlindSaveStateDiff",
				preparedFunctionName: "identityBlindSaveStateDiffPrepared",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		return generated
	}
	renamed := strings.Replace(
		backendSaveStateDiffProofSource,
		"local function kernel(seed)",
		"local function opaque(seed)",
		1,
	)
	baseGenerated := emit(backendSaveStateDiffProofSource)
	renamedGenerated := emit(renamed)
	for _, marker := range []string{
		"var a0 [3]uint32",
		"var ra0_0 [3]uint32",
		"case uint32(",
	} {
		if strings.Count(string(baseGenerated), marker) != strings.Count(string(renamedGenerated), marker) {
			t.Fatalf("save-state private rename changed structural lowering marker %q", marker)
		}
	}
	if strings.Contains(string(renamedGenerated), "machineTable") ||
		strings.Contains(string(renamedGenerated), "opcode") {
		t.Fatal("renamed save-state source lost structural lowering")
	}

	tests := map[string]string{
		"escaping child record": strings.Replace(
			backendSaveStateDiffProofSource,
			"return total",
			"return before[1].inv",
			1,
		),
		"child shape mismatch": strings.Replace(
			backendSaveStateDiffProofSource,
			"inv = {coins = 2, herbs = 1, ore = 0}",
			"inv = {coins = 2, herbs = 1, gems = 0}",
			1,
		),
		"mixed child field tags": strings.Replace(
			backendSaveStateDiffProofSource,
			"inv = {coins = 2, herbs = 1, ore = 0}",
			"inv = {coins = 2, herbs = 1, ore = false}",
			1,
		),
		"mixed dynamic key tags": strings.Replace(
			backendSaveStateDiffProofSource,
			`{"coins", "herbs", "ore"}`,
			`{"coins", "herbs", 3}`,
			1,
		),
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			ir := backendRecordArrayProofIR(t, source)
			if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
				packageName:  "ember",
				functionName: "rejectUnprovedSaveStateDiff",
			}); err == nil {
				t.Fatalf("fused child-record compiler accepted %s", name)
			}
		})
	}
}

func BenchmarkBackendGeneratedSaveStateDiff(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedSaveStateDiff(float64(iteration & 31))
		if !ok {
			b.Fatal("generated save-state fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
