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

// backendCooldownSchedulerProofSource is the parameterized nested-record-array holdout.
const backendCooldownSchedulerProofSource = `
local function kernel(seed)
    local actors = {
        {energy = 30 + seed % 3, haste = 1, abilities = {
            {cost = 6, cooldown = 0, reset = 3, uses = 0},
            {cost = 11, cooldown = 2, reset = 5, uses = 0},
            {cost = 4, cooldown = 1, reset = 2, uses = 0},
        }},
        {energy = 22, haste = 2, abilities = {
            {cost = 8, cooldown = 0, reset = 4, uses = 0},
            {cost = 5, cooldown = 3, reset = 3, uses = 0},
        }},
        {energy = 45, haste = 0, abilities = {
            {cost = 13, cooldown = 1, reset = 6, uses = 0},
            {cost = 7, cooldown = 0, reset = 2, uses = 0},
            {cost = 9, cooldown = 4, reset = 4, uses = 0},
        }},
    }
    local score = 0
    for tick = 1, 72 do
        for _, actor in actors do
            actor.energy = actor.energy + 2 + actor.haste
            for _, ability in actor.abilities do
                if ability.cooldown > 0 then
                    ability.cooldown = ability.cooldown - 1 - actor.haste
                    if ability.cooldown < 0 then
                        ability.cooldown = 0
                    end
                end
                if ability.cooldown == 0 and actor.energy >= ability.cost then
                    actor.energy = actor.energy - ability.cost
                    ability.uses = ability.uses + 1
                    ability.cooldown = ability.reset
                    score = score + actor.energy + ability.uses * ability.cost
                else
                    score = score + ability.cooldown + actor.energy
                end
            end
        end
    end
    return score
end
return kernel
`

func TestBackendGoCooldownSchedulerNestedRecordsAreRecognized(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendCooldownSchedulerProofSource)
	records := analyzeBackendGoRecordTables(ir, analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{}))
	if !records.enabled {
		t.Fatalf("nested records rejected: %s (arrays=%d records=%d families=%d)", records.rejectReason, len(records.arrays), len(records.records), len(records.families))
	}
	if len(records.maps) != 0 || len(records.arrays) != 4 || len(records.records) != 11 || len(records.families) != 1 {
		t.Fatalf(
			"cooldown-scheduler record inventory = maps %d arrays %d records %d families %d, want 0/4/11/1",
			len(records.maps), len(records.arrays), len(records.records), len(records.families),
		)
	}
	if len(records.familyPrepare) != 1 || len(records.familyNext) != 1 {
		t.Fatalf("cooldown-scheduler child iterators = prepare %d next %d, want 1/1", len(records.familyPrepare), len(records.familyNext))
	}
	if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedCooldownScheduler",
		preparedFunctionName: "backendGeneratedCooldownSchedulerPreparedFixture",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBackendGoCooldownSchedulerFixtureIsFreshAndCorrect(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendCooldownSchedulerProofSource)
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedCooldownScheduler",
		preparedFunctionName: "backendGeneratedCooldownSchedulerPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_cooldown_scheduler_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated cooldown-scheduler fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), fixture, generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated cooldown-scheduler source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var ra0_0 [3]float64",
		"var ra1_0 [3]float64",
		"var ra2_0 [2]float64",
		"var ra3_0 [3]float64",
		"var rfi0 int",
		"var rfs0 int",
		"switch rfs0",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated cooldown-scheduler source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "SET_FIELD", "GET_FIELD", "PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated cooldown-scheduler source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendCooldownSchedulerProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedCooldownScheduler(seed)
		if !ok {
			t.Fatalf("generated cooldown-scheduler fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("cooldown-scheduler oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle cooldown-scheduler seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedCooldownScheduler(29)
		}); allocations != 0 {
			t.Fatalf("generated cooldown-scheduler allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoCooldownSchedulerRejectsUnprovedChildArrays(t *testing.T) {
	tests := map[string]string{
		"escaping child array": strings.Replace(
			backendCooldownSchedulerProofSource,
			"return score",
			"return actors[1].abilities",
			1,
		),
		"escaping child record": strings.Replace(
			backendCooldownSchedulerProofSource,
			"if ability.cooldown > 0 then",
			"if seed > 1000000000 then return ability end\n                if ability.cooldown > 0 then",
			1,
		),
		"child shape mismatch": strings.Replace(
			backendCooldownSchedulerProofSource,
			"{cost = 5, cooldown = 3, reset = 3, uses = 0}",
			"{price = 5, cooldown = 3, reset = 3, uses = 0}",
			1,
		),
		"child identity replacement": strings.Replace(
			backendCooldownSchedulerProofSource,
			"for _, ability in actor.abilities do",
			"actor.abilities = {}\n            for _, ability in actor.abilities do",
			1,
		),
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			ir := backendRecordArrayProofIR(t, source)
			if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
				packageName:  "ember",
				functionName: "rejectUnprovedCooldownScheduler",
			}); err == nil {
				t.Fatalf("nested record-array compiler accepted %s", name)
			}
		})
	}
}

func BenchmarkBackendGeneratedCooldownScheduler(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedCooldownScheduler(float64(iteration & 31))
		if !ok {
			b.Fatal("generated cooldown-scheduler fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
