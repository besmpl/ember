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

const backendThreatAggroProofSource = `
local function kernel(seed)
    local actors = {
        {id = "tank", role = "front", alive = true},
        {id = "mage", role = "burst", alive = true},
        {id = "healer", role = "support", alive = true},
        {id = "rogue", role = "burst", alive = true},
    }
    local enemies = {
        {hp = 180, enraged = false, threat = {tank = 20 + seed % 7, mage = 0, healer = 4, rogue = 8}},
        {hp = 140, enraged = false, threat = {tank = 10, mage = 12, healer = 0, rogue = 5}},
        {hp = 220, enraged = false, threat = {tank = 30, mage = 8, healer = 6, rogue = 2}},
    }
    local events = {
        {actor = "tank", kind = "taunt", amount = 9},
        {actor = "mage", kind = "damage", amount = 17},
        {actor = "healer", kind = "heal", amount = 12},
        {actor = "rogue", kind = "damage", amount = 11},
        {actor = "tank", kind = "damage", amount = 7},
    }
    local total = 0
    for tick = 1, 48 do
        for _, enemy in enemies do
            for _, event in events do
                local gain = event.amount + tick % 4
                if event.kind == "taunt" then
                    gain = gain * 2
                elseif event.kind == "heal" then
                    gain = gain // 2 + 3
                end
                if enemy.enraged then
                    gain = gain + 2
                end
                enemy.threat[event.actor] = enemy.threat[event.actor] + gain
            end
            local top = -1
            local focusRole = ""
            for _, actor in actors do
                local value = enemy.threat[actor.id]
                if actor.alive and value > top then
                    top = value
                    focusRole = actor.role
                end
            end
            if focusRole == "front" then
                total = total + top + enemy.hp
            elseif focusRole == "support" then
                total = total + top * 2
            else
                total = total + top + enemy.hp // 2
            end
            enemy.hp = enemy.hp - tick % 5
            if enemy.hp < 120 then
                enemy.enraged = true
            end
        end
    end
    return total
end
return kernel
`

func TestBackendGoThreatAggroDynamicMutationCanGenerate(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendThreatAggroProofSource)
	records := analyzeBackendGoRecordTables(ir, analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{}))
	if !records.enabled {
		t.Fatalf("threat-aggro records were not recognized: %s", records.rejectReason)
	}
	if len(records.maps) != 0 || len(records.arrays) != 3 || len(records.records) != 15 ||
		len(records.childRecords) != 1 || len(records.fusedGetByPC) != 2 || len(records.fusedSetByPC) != 1 {
		t.Fatalf(
			"threat-aggro inventory = maps %d arrays %d records %d children %d gets %d sets %d, want 0/3/15/1/2/1",
			len(records.maps), len(records.arrays), len(records.records), len(records.childRecords),
			len(records.fusedGetByPC), len(records.fusedSetByPC),
		)
	}
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedThreatAggro",
		preparedFunctionName: "backendGeneratedThreatAggroPreparedFixture",
	})
	if err != nil {
		t.Fatalf("emit threat-aggro dynamic mutation: %v", err)
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "generated_threat_aggro.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated threat-aggro source: %v", err)
	}
}

func TestBackendGoThreatAggroFixtureIsFreshAndCorrect(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendThreatAggroProofSource)
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedThreatAggro",
		preparedFunctionName: "backendGeneratedThreatAggroPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_threat_aggro_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated threat-aggro fixture is stale")
	}
	text := string(generated)
	for _, required := range []string{
		"var ra0_0 [4]uint32",
		"var ra1_2 [3]float64",
		"var ra2_0 [5]uint32",
		"switch int(v",
		"case uint32(1):",
		"case uint32(6):",
		"case uint32(8):",
		"case uint32(10):",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated threat-aggro source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "SET_FIELD", "GET_FIELD", "SET_STRING_FIELD_INDEX", "GET_STRING_FIELD_INDEX",
		"PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated threat-aggro source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendThreatAggroProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedThreatAggro(seed)
		if !ok {
			t.Fatalf("generated threat-aggro fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("threat-aggro oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle threat-aggro seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedThreatAggro(29)
		}); allocations != 0 {
			t.Fatalf("generated threat-aggro allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoThreatAggroIsIdentityBlindAndRejectsUnprovedMutations(t *testing.T) {
	emit := func(source string) []byte {
		generated, err := emitBackendGoNumericProof(
			backendRecordArrayProofIR(t, source),
			backendGoNumericOptions{
				packageName:          "ember",
				functionName:         "identityBlindThreatAggro",
				preparedFunctionName: "identityBlindThreatAggroPrepared",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		return generated
	}
	renamed := strings.Replace(
		backendThreatAggroProofSource,
		"local function kernel(seed)",
		"local function opaque(seed)",
		1,
	)
	baseGenerated := emit(backendThreatAggroProofSource)
	renamedGenerated := emit(renamed)
	for _, marker := range []string{"var ra0_0 [4]uint32", "var ra1_2 [3]float64", "case uint32("} {
		if strings.Count(string(baseGenerated), marker) != strings.Count(string(renamedGenerated), marker) {
			t.Fatalf("threat-aggro private rename changed structural lowering marker %q", marker)
		}
	}

	tests := map[string]string{
		"mixed child field tags": strings.Replace(
			backendThreatAggroProofSource,
			"rogue = 8",
			"rogue = false",
			1,
		),
		"mutation changes field tags": strings.Replace(
			backendThreatAggroProofSource,
			"enemy.threat[event.actor] + gain",
			"event.actor",
			1,
		),
		"escaping child record": strings.Replace(
			backendThreatAggroProofSource,
			"return total",
			"return enemies[1].threat",
			1,
		),
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			ir := backendRecordArrayProofIR(t, source)
			if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
				packageName:  "ember",
				functionName: "rejectUnprovedThreatAggro",
			}); err == nil {
				t.Fatalf("fused child-record mutation compiler accepted %s", name)
			}
		})
	}
}

func BenchmarkBackendGeneratedThreatAggro(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedThreatAggro(float64(iteration & 31))
		if !ok {
			b.Fatal("generated threat-aggro fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
