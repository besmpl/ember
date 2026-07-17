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

const backendDialogueConditionProofSource = `
local function kernel(seed)
    local state = {reputation = 8 + seed % 5, gold = 25, insight = 3, flags = {met_guard = true, has_badge = false, helped_mage = false}}
    local rules = {
        {speaker = "guard", checks = {{key = "met_guard", want = true}, {stat = "reputation", atLeast = 5}}, reward = 4, flag = "has_badge"},
        {speaker = "mage", checks = {{key = "has_badge", want = true}, {stat = "gold", atLeast = 12}}, reward = 7, flag = "helped_mage"},
        {speaker = "merchant", checks = {{key = "helped_mage", want = true}, {stat = "insight", atLeast = 4}}, reward = 11, flag = "trade_route"},
        {speaker = "scout", checks = {{key = "trade_route", want = true}, {stat = "reputation", atLeast = 12}}, reward = 13, flag = "map_known"},
    }
    local total = 0
    for pass = 1, 36 do
        for _, rule in rules do
            local ok = true
            for _, check in rule.checks do
                if check.key ~= nil then
                    if state.flags[check.key] ~= check.want then
                        ok = false
                    end
                else
                    if state[check.stat] < check.atLeast then
                        ok = false
                    end
                end
            end
            if ok then
                state.flags[rule.flag] = true
                state.reputation = state.reputation + rule.reward % 5
                state.insight = state.insight + 1
                state.gold = state.gold - rule.reward // 3
                total = total + rule.reward + state.reputation + state.insight
            else
                state.gold = state.gold + 1
                total = total + state.gold % 7
            end
        end
    end
    local flagScore = 0
    if state.flags.has_badge then flagScore = flagScore + 10 end
    if state.flags.helped_mage then flagScore = flagScore + 20 end
    if state.flags.trade_route then flagScore = flagScore + 30 end
    if state.flags.map_known then flagScore = flagScore + 40 end
    return total + flagScore + state.gold + state.reputation + state.insight
end
return kernel
`

func TestBackendGoOptionalUnionChildRecordsCanGenerate(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendDialogueConditionProofSource)
	records := analyzeBackendGoRecordTables(ir, analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{}))
	if !records.enabled {
		t.Fatalf("analyze optional union child records: %s", records.rejectReason)
	}
	if len(records.childRecords) != 1 {
		t.Fatalf("optional child families = %d, want 1", len(records.childRecords))
	}
	child := records.childRecords[0]
	if child.parentArray != -1 || len(child.fieldNames) != 5 ||
		len(records.fusedGetByPC) != 1 || len(records.fusedSetByPC) != 1 {
		t.Fatalf(
			"optional child inventory = parent %d fields %d fused gets %d sets %d",
			child.parentArray,
			len(child.fieldNames),
			len(records.fusedGetByPC),
			len(records.fusedSetByPC),
		)
	}
	if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedDialogueCondition",
		preparedFunctionName: "backendGeneratedDialogueConditionPreparedFixture",
	}); err != nil {
		t.Fatalf("emit optional union child records: %v", err)
	}
	plan, err := buildBackendGoNumericPlan(ir, backendGoNumericOptions{})
	if err != nil {
		t.Fatal(err)
	}
	optionalStringSpill := false
	for pc := range ir.ops {
		for _, spill := range ir.ops[pc].spillValues {
			if plan.tags[spill.value-1] != (backendTagNil | backendTagString) {
				continue
			}
			optionalStringSpill = true
			if !plan.replayEntry[pc] {
				t.Fatalf("PC %d spills an optional image string without exact entry replay", pc)
			}
		}
	}
	if !optionalStringSpill {
		t.Fatal("dialogue-condition proof does not exercise an optional image-string spill")
	}
}

func TestBackendGoOptionalUnionChildRecordsFixtureIsFreshAndCorrect(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendDialogueConditionProofSource)
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedDialogueCondition",
		preparedFunctionName: "backendGeneratedDialogueConditionPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_dialogue_condition_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated dialogue-condition fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), fixture, generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated dialogue-condition source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var vp", "var rp", "var rap", "switch v", "case uint32(",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated dialogue-condition source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "GET_INDEX", "SET_INDEX", "GET_STRING_FIELD_INDEX", "SET_STRING_FIELD_INDEX",
		"PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated dialogue-condition source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendDialogueConditionProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedDialogueCondition(seed)
		if !ok {
			t.Fatalf("generated dialogue-condition fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("dialogue-condition oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, number := oracle[0].Number()
		if !number || got != oracleNumber {
			t.Fatalf("generated/oracle dialogue-condition seed %v = %v/%v (%t)", seed, got, oracleNumber, number)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedDialogueCondition(29)
		}); allocations != 0 {
			t.Fatalf("generated dialogue-condition allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoOptionalUnionChildRecordsAreIdentityBlindAndFailClosed(t *testing.T) {
	emit := func(source string) []byte {
		generated, err := emitBackendGoNumericProof(
			backendRecordArrayProofIR(t, source),
			backendGoNumericOptions{
				packageName:          "ember",
				functionName:         "identityBlindDialogueCondition",
				preparedFunctionName: "identityBlindDialogueConditionPrepared",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		return generated
	}
	renamed := strings.Replace(backendDialogueConditionProofSource, "local function kernel(seed)", "local function opaque(seed)", 1)
	renamed = strings.Replace(renamed, "return kernel", "return opaque", 1)
	if !bytes.Equal(emit(backendDialogueConditionProofSource), emit(renamed)) {
		t.Fatal("optional union lowering depends on private function identity")
	}

	for name, source := range map[string]string{
		"mixed optional payload":  strings.Replace(backendDialogueConditionProofSource, "want = true", "want = 1", 1),
		"escaping optional child": strings.Replace(backendDialogueConditionProofSource, "return total + flagScore + state.gold + state.reputation + state.insight", "return state.flags", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := emitBackendGoNumericProof(
				backendRecordArrayProofIR(t, source),
				backendGoNumericOptions{packageName: "ember", functionName: "rejectUnprovedDialogueCondition"},
			); err == nil {
				t.Fatalf("optional union compiler accepted %s", name)
			}
		})
	}
}

func BenchmarkBackendGeneratedDialogueCondition(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedDialogueCondition(float64(iteration & 31))
		if !ok {
			b.Fatal("generated dialogue-condition fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
