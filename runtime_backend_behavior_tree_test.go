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

const backendBehaviorTreeProofSource = `
local function kernel(seed)
    local blackboard = {hp = 65 + seed % 7, ammo = 6, enemyDistance = 12, cover = 3, alert = 0}
    local nodes = {
        {kind = "condition", key = "hp", threshold = 35, pass = 2, fail = 3},
        {kind = "action", name = "attack", weight = 15},
        {kind = "condition", key = "ammo", threshold = 1, pass = 4, fail = 5},
        {kind = "action", name = "reload", weight = 9},
        {kind = "action", name = "retreat", weight = 12},
    }
    if seed == 777 then
        nodes[1].kind = "action"
    end
    local total = 0
    for tick = 1, 80 do
        local index = 1
        local depth = 0
        while index > 0 and depth < 4 do
            local node = nodes[index]
            if node.kind == "condition" then
                local value = blackboard[node.key]
                if value > node.threshold then
                    index = node.pass
                else
                    index = node.fail
                end
            else
                if node.name == "attack" then
                    blackboard.ammo = blackboard.ammo - 1
                    blackboard.alert = blackboard.alert + 2
                elseif node.name == "reload" then
                    blackboard.ammo = blackboard.ammo + 3
                    blackboard.alert = blackboard.alert + 1
                else
                    blackboard.hp = blackboard.hp + blackboard.cover
                    blackboard.enemyDistance = blackboard.enemyDistance + 2
                end
                total = total + node.weight + blackboard.hp + blackboard.ammo + blackboard.alert
                index = 0
            end
            depth = depth + 1
        end
        blackboard.hp = blackboard.hp - tick % 4
        blackboard.enemyDistance = blackboard.enemyDistance - tick % 3
        if blackboard.enemyDistance < 3 then
            blackboard.enemyDistance = 12
        end
    end
    return total + blackboard.hp + blackboard.ammo + blackboard.alert
end
return kernel
`

func TestBackendGoGuardedUnionRecordArrayCanGenerate(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendBehaviorTreeProofSource)
	records := analyzeBackendGoRecordTables(ir, analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{}))
	if !records.enabled {
		t.Fatalf("guarded union records were not recognized: %s", records.rejectReason)
	}
	if len(records.maps) != 0 || len(records.arrays) != 1 || len(records.records) != 6 ||
		len(records.dynamicGetByPC) != 1 || len(records.dynamicSetByPC) != 0 {
		t.Fatalf(
			"guarded union inventory = maps %d arrays %d records %d dynamic gets %d sets %d, want 0/1/6/1/0",
			len(records.maps), len(records.arrays), len(records.records),
			len(records.dynamicGetByPC), len(records.dynamicSetByPC),
		)
	}
	array := records.arrays[0]
	presenceCounts := make(map[int]int)
	for _, present := range array.fieldPresent {
		count := 0
		for _, member := range present {
			if member {
				count++
			}
		}
		presenceCounts[count]++
	}
	if len(array.fieldNames) != 7 || presenceCounts[2] != 4 || presenceCounts[3] != 2 || presenceCounts[5] != 1 {
		t.Fatalf("guarded union field presence = fields %d counts %#v, want 7 and {2:4 3:2 5:1}", len(array.fieldNames), presenceCounts)
	}
	if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedBehaviorTree",
		preparedFunctionName: "backendGeneratedBehaviorTreePreparedFixture",
	}); err != nil {
		t.Fatalf("emit guarded union record array: %v", err)
	}
}

func TestBackendGoGuardedUnionRecordArrayFixtureIsFreshAndCorrect(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendBehaviorTreeProofSource)
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedBehaviorTree",
		preparedFunctionName: "backendGeneratedBehaviorTreePreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_behavior_tree_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated behavior-tree fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), fixture, generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated behavior-tree source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var ra0_0 [5]uint32",
		"var ra0_6 [5]float64",
		"var rap",
		"switch v",
		"case uint32(1):",
		"case uint32(5):",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated behavior-tree source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "GET_INDEX", "SET_FIELD", "GET_FIELD", "PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated behavior-tree source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendBehaviorTreeProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedBehaviorTree(seed)
		if !ok {
			t.Fatalf("generated behavior-tree fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || got != oracleNumber {
			t.Fatalf("generated/oracle behavior-tree seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if _, ok := backendGeneratedBehaviorTree(777); ok {
		t.Fatal("generated behavior-tree accepted a missing union member field")
	}
	if _, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
		args: []Value{NumberValue(777)},
	}); err == nil {
		t.Fatal("behavior-tree oracle accepted the deliberately missing union member field")
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedBehaviorTree(29)
		}); allocations != 0 {
			t.Fatalf("generated behavior-tree allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoGuardedUnionRecordArrayIsIdentityBlindAndRejectsMixedFields(t *testing.T) {
	emit := func(source string) []byte {
		generated, err := emitBackendGoNumericProof(
			backendRecordArrayProofIR(t, source),
			backendGoNumericOptions{
				packageName:          "ember",
				functionName:         "identityBlindBehaviorTree",
				preparedFunctionName: "identityBlindBehaviorTreePrepared",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		return generated
	}
	renamed := strings.Replace(backendBehaviorTreeProofSource, "local function kernel(seed)", "local function renamed(seed)", 1)
	renamed = strings.Replace(renamed, "return kernel", "return renamed", 1)
	if !bytes.Equal(emit(backendBehaviorTreeProofSource), emit(renamed)) {
		t.Fatal("guarded union lowering depends on private function identity")
	}

	for name, source := range map[string]string{
		"mixed union field":     strings.Replace(backendBehaviorTreeProofSource, "weight = 15", "weight = true", 1),
		"mixed dynamic fields":  strings.Replace(backendBehaviorTreeProofSource, "cover = 3", "cover = \"three\"", 1),
		"escaping record array": strings.Replace(backendBehaviorTreeProofSource, "return total + blackboard.hp + blackboard.ammo + blackboard.alert", "return nodes", 1),
	} {
		t.Run(name, func(t *testing.T) {
			ir := backendRecordArrayProofIR(t, source)
			if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
				packageName:  "ember",
				functionName: "rejectUnprovedBehaviorTree",
			}); err == nil {
				t.Fatal("guarded union compiler accepted an unproved shape")
			}
		})
	}
}

func BenchmarkBackendGeneratedBehaviorTree(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedBehaviorTree(float64(iteration & 31))
		if !ok {
			b.Fatal("generated behavior-tree fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
