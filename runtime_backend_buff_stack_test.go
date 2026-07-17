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

const backendBuffStackProofSource = `
local function kernel(seed)
    local entities = {
        {hp = 100 + seed % 7, speed = 10, armor = 4, buffs = {
            {kind = "poison", power = 3, turns = 5},
            {kind = "shield", power = 2, turns = 3},
        }},
        {hp = 140, speed = 8, armor = 6, buffs = {
            {kind = "regen", power = 4, turns = 4},
            {kind = "haste", power = 1, turns = 6},
            {kind = "poison", power = 2, turns = 2},
        }},
        {hp = 90, speed = 14, armor = 2, buffs = {
            {kind = "shield", power = 5, turns = 2},
            {kind = "regen", power = 1, turns = 8},
        }},
    }
    local score = 0
    for tick = 1, 24 + seed % 3 do
        for _, entity in entities do
            local i = 1
            while i <= rawlen(entity.buffs) do
                local buff = entity.buffs[i]
                if buff.kind == "poison" then
                    entity.hp = entity.hp - buff.power
                elseif buff.kind == "regen" then
                    entity.hp = entity.hp + buff.power
                elseif buff.kind == "shield" then
                    entity.armor = entity.armor + buff.power
                elseif buff.kind == "haste" then
                    entity.speed = entity.speed + buff.power
                end
                buff.turns = buff.turns - 1
                if buff.turns <= 0 then
                    table.remove(entity.buffs, i)
                else
                    i = i + 1
                end
            end
            score = score + entity.hp + entity.speed + entity.armor + rawlen(entity.buffs)
        end
    end
    return score
end
return kernel
`

func TestBackendGoBoundedNestedRecordArrayRemovalCanGenerate(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendBuffStackProofSource)
	records := analyzeBackendGoRecordTables(ir, analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{}))
	if !records.enabled {
		t.Fatalf("analyze bounded nested record-array removal: %s", records.rejectReason)
	}
	mutable := 0
	for index := range records.arrays {
		if records.arrays[index].mutable {
			mutable++
		}
	}
	if len(records.records) != 10 || len(records.arrays) != 4 || len(records.families) != 1 ||
		len(records.familyGetByPC) != 1 || len(records.familyRawLenPC) != 2 ||
		len(records.familyRemovePC) != 1 || mutable != 3 {
		t.Fatalf(
			"buff-stack inventory = records %d arrays %d families %d get/rawlen/remove %d/%d/%d mutable %d",
			len(records.records), len(records.arrays), len(records.families),
			len(records.familyGetByPC), len(records.familyRawLenPC), len(records.familyRemovePC), mutable,
		)
	}
	if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedBuffStack",
		preparedFunctionName: "backendGeneratedBuffStackPreparedFixture",
	}); err != nil {
		t.Fatalf("emit bounded nested record-array removal: %v", err)
	}
}

func TestBackendGoBoundedNestedRecordArrayRemovalFixtureIsFreshAndCorrect(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendBuffStackProofSource)
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedBuffStack",
		preparedFunctionName: "backendGeneratedBuffStackPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_buff_stack_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated buff-stack fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), fixture, generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated buff-stack source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var rn0 = 2", "var rn2 = 3", "var rn3 = 2", "for rrm172 :=", "float64(rn", "rrm172+1",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated buff-stack source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "GET_STRING_FIELD_INDEX", "GET_STRING_FIELD", "SET_STRING_FIELD", "FAST_CALL",
		"PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated buff-stack source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendBuffStackProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedBuffStack(seed)
		if !ok {
			t.Fatalf("generated buff-stack fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("buff-stack oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, number := oracle[0].Number()
		if !number || got != oracleNumber {
			t.Fatalf("generated/oracle buff-stack seed %v = %v/%v (%t)", seed, got, oracleNumber, number)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedBuffStack(29)
		}); allocations != 0 {
			t.Fatalf("generated buff-stack allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoBoundedNestedRecordArrayRemovalIsIdentityBlindAndFailClosed(t *testing.T) {
	emit := func(source string) []byte {
		generated, err := emitBackendGoNumericProof(
			backendRecordArrayProofIR(t, source),
			backendGoNumericOptions{
				packageName:          "ember",
				functionName:         "identityBlindBuffStack",
				preparedFunctionName: "identityBlindBuffStackPrepared",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		return generated
	}
	renamed := strings.Replace(backendBuffStackProofSource, "local function kernel(seed)", "local function opaque(seed)", 1)
	renamed = strings.Replace(renamed, "return kernel", "return opaque", 1)
	if !bytes.Equal(emit(backendBuffStackProofSource), emit(renamed)) {
		t.Fatal("buff-stack lowering depends on private function identity")
	}

	for name, source := range map[string]string{
		"mixed buff payload": strings.Replace(
			backendBuffStackProofSource,
			`{kind = "poison", power = 3, turns = 5}`,
			`{kind = "poison", power = "bad", turns = 5}`,
			1,
		),
		"escaping buff array": strings.Replace(
			backendBuffStackProofSource,
			"return score",
			"return entities[1].buffs",
			1,
		),
		"observed removed record": strings.Replace(
			backendBuffStackProofSource,
			"table.remove(entity.buffs, i)",
			"local removed = table.remove(entity.buffs, i)\n                    score = score + removed.power",
			1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := emitBackendGoNumericProof(
				backendRecordArrayProofIR(t, source),
				backendGoNumericOptions{packageName: "ember", functionName: "rejectUnprovedBuffStack"},
			); err == nil {
				t.Fatalf("buff-stack compiler accepted %s", name)
			}
		})
	}
}

func BenchmarkBackendGeneratedBuffStack(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedBuffStack(float64(iteration & 31))
		if !ok {
			b.Fatal("generated buff-stack fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
