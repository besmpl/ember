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

const backendStateMachineProofSource = `
local function kernel(seed)
    local transitions = {
        idle = {see = "chase", hit = "evade", rest = "idle"},
        chase = {near = "attack", lost = "search", hit = "evade"},
        attack = {cooldown = "chase", hit = "evade", lost = "search"},
        evade = {safe = "search", hit = "evade", rest = "idle"},
        search = {see = "chase", rest = "idle", lost = "search"},
    }
    local weights = {idle = 2, chase = 8, attack = 15, evade = 9, search = 5}
    local events = {"see", "near", "cooldown", "lost", "hit", "safe", "rest"}
    local state = "idle"
    local energy = 20 + seed % 3
    local total = 0
    for tick = 1, 120 + seed % 2 do
        local event = events[tick % rawlen(events) + 1]
        local nextState = transitions[state][event]
        if nextState == nil then
            nextState = "idle"
        end
        local weight = weights[nextState] or 0
        if nextState == "attack" then
            energy = energy - 3
        elseif nextState == "evade" then
            energy = energy - 1
        else
            energy = energy + 1
        end
        if energy < 0 then
            energy = 4
        elseif energy > 35 then
            energy = 20
        end
        state = nextState
        total = total + weight + energy + tick % 7
    end
    return total + weights[state] + energy
end
return kernel
`

func TestBackendGoFiniteStringStateTablesCanGenerate(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendStateMachineProofSource)
	records := analyzeBackendGoRecordTables(ir, analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{}))
	if !records.enabled {
		t.Fatalf("analyze finite string state tables: %s", records.rejectReason)
	}
	if len(records.records) != 7 || len(records.arrays) != 0 || len(records.childRecords) != 1 ||
		len(records.dynamicChildSelectByPC) != 1 || len(records.dynamicChildGetByPC) != 1 ||
		len(records.dynamicGetByPC) != 2 || len(records.childRecords[0].records) != 5 {
		t.Fatalf(
			"state-machine inventory = records %d arrays %d children %d selectors/child-gets/dynamic %d/%d/%d members %d",
			len(records.records), len(records.arrays), len(records.childRecords),
			len(records.dynamicChildSelectByPC), len(records.dynamicChildGetByPC),
			len(records.dynamicGetByPC), len(records.childRecords[0].records),
		)
	}
	plan, err := buildBackendGoNumericPlan(ir, backendGoNumericOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.tables.arrays) != 1 || plan.tables.arrays[0].tags != backendTagString ||
		plan.tables.arrays[0].length != 7 || len(plan.tables.arrayGetByPC) != 1 {
		t.Fatalf("state-machine event array = %#v gets %d", plan.tables.arrays, len(plan.tables.arrayGetByPC))
	}
	if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedStateMachine",
		preparedFunctionName: "backendGeneratedStateMachinePreparedFixture",
	}); err != nil {
		t.Fatalf("emit finite string state tables: %v", err)
	}
}

func TestBackendGoFiniteStringStateTablesFixtureIsFreshAndCorrect(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendStateMachineProofSource)
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedStateMachine",
		preparedFunctionName: "backendGeneratedStateMachinePreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_state_machine_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated state-machine fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), fixture, generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated state-machine source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var a0 [7]uint32", "switch int(v", "case uint32(", "var rp", " = false",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated state-machine source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "GET_INDEX", "GET_STRING_FIELD", "SET_STRING_FIELD", "FAST_CALL",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated state-machine source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendStateMachineProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedStateMachine(seed)
		if !ok {
			t.Fatalf("generated state-machine fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("state-machine oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, number := oracle[0].Number()
		if !number || got != oracleNumber {
			t.Fatalf("generated/oracle state-machine seed %v = %v/%v (%t)", seed, got, oracleNumber, number)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedStateMachine(29)
		}); allocations != 0 {
			t.Fatalf("generated state-machine allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoFiniteStringStateTablesAreIdentityBlindAndFailClosed(t *testing.T) {
	emit := func(source string) []byte {
		generated, err := emitBackendGoNumericProof(
			backendRecordArrayProofIR(t, source),
			backendGoNumericOptions{
				packageName:          "ember",
				functionName:         "identityBlindStateMachine",
				preparedFunctionName: "identityBlindStateMachinePrepared",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		return generated
	}
	renamed := strings.Replace(backendStateMachineProofSource, "local function kernel(seed)", "local function opaque(seed)", 1)
	renamed = strings.Replace(renamed, "return kernel", "return opaque", 1)
	if !bytes.Equal(emit(backendStateMachineProofSource), emit(renamed)) {
		t.Fatal("state-machine lowering depends on private function identity")
	}

	for name, source := range map[string]string{
		"mixed transition payload": strings.Replace(
			backendStateMachineProofSource,
			`see = "chase"`,
			`see = 3`,
			1,
		),
		"mixed event array": strings.Replace(
			backendStateMachineProofSource,
			`"see", "near"`,
			`1, "near"`,
			1,
		),
		"escaping transition graph": strings.Replace(
			backendStateMachineProofSource,
			"return total + weights[state] + energy",
			"return transitions",
			1,
		),
		"escaping selected transition": strings.Replace(
			backendStateMachineProofSource,
			"return total + weights[state] + energy",
			"return transitions[state]",
			1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := emitBackendGoNumericProof(
				backendRecordArrayProofIR(t, source),
				backendGoNumericOptions{packageName: "ember", functionName: "rejectUnprovedStateMachine"},
			); err == nil {
				t.Fatalf("state-machine compiler accepted %s", name)
			}
		})
	}
}

func BenchmarkBackendGeneratedStateMachine(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedStateMachine(float64(iteration & 31))
		if !ok {
			b.Fatal("generated state-machine fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
