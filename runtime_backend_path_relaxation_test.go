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

const backendPathRelaxationProofSource = `
local function kernel(seed)
    local nodes = {
        {cost = 1 + seed % 3, dist = 0, blocked = false, edges = {{to = 2, weight = 3}, {to = 3, weight = 7}}},
        {cost = 2, dist = 999, blocked = false, edges = {{to = 4, weight = 2}, {to = 5, weight = 5}}},
        {cost = 4, dist = 999, blocked = false, edges = {{to = 5, weight = 1}, {to = 6, weight = 9}}},
        {cost = 1, dist = 999, blocked = false, edges = {{to = 7, weight = 4}}},
        {cost = 3, dist = 999, blocked = false, edges = {{to = 7, weight = 2}, {to = 8, weight = 8}}},
        {cost = 2, dist = 999, blocked = true, edges = {{to = 8, weight = 1}}},
        {cost = 5, dist = 999, blocked = false, edges = {{to = 9, weight = 3}}},
        {cost = 1, dist = 999, blocked = false, edges = {{to = 9, weight = 2}}},
        {cost = 2, dist = 999, blocked = false, edges = {}},
    }
    local total = 0
    for pass = 1, 40 + seed % 3 do
        for i, node in nodes do
            if not node.blocked then
                for _, edge in node.edges do
                    local nextNode = nodes[edge.to]
                    if not nextNode.blocked then
                        local candidate = node.dist + edge.weight + nextNode.cost + pass % 3
                        if candidate < nextNode.dist then
                            nextNode.dist = candidate
                        end
                        total = total + nextNode.dist % 17 + i
                    end
                end
            end
        end
        if pass % 10 == 0 then
            nodes[3].dist = nodes[3].dist + 4
            nodes[5].dist = nodes[5].dist + 2
        end
    end
    local sum = 0
    for _, node in nodes do
        if node.dist < 999 then
            sum = sum + node.dist
        end
    end
    return total + sum
end
return kernel
`

func TestBackendGoNestedRecordArraysWithEmptyMemberCanGenerate(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendPathRelaxationProofSource)
	records := analyzeBackendGoRecordTables(ir, analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{}))
	if !records.enabled {
		t.Fatalf("analyze nested record arrays: %s", records.rejectReason)
	}
	if len(records.records) != 21 || len(records.arrays) != 10 || len(records.families) != 1 ||
		len(records.families[0].fieldNames) != 2 {
		t.Fatalf(
			"nested record inventory = records %d arrays %d families %d fields %d",
			len(records.records), len(records.arrays), len(records.families),
			len(records.families[0].fieldNames),
		)
	}
	emptyArrays := 0
	for index := range records.arrays {
		if records.arrays[index].length != 0 {
			continue
		}
		emptyArrays++
		if len(records.arrays[index].fieldPresent) != 2 {
			t.Fatalf("empty nested array presence fields = %d, want 2", len(records.arrays[index].fieldPresent))
		}
	}
	if emptyArrays != 1 {
		t.Fatalf("empty nested arrays = %d, want 1", emptyArrays)
	}
	if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedPathRelaxation",
		preparedFunctionName: "backendGeneratedPathRelaxationPreparedFixture",
	}); err != nil {
		t.Fatalf("emit nested record arrays with empty member: %v", err)
	}
}

func TestBackendGoNestedRecordArraysWithEmptyMemberFixtureIsFreshAndCorrect(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendPathRelaxationProofSource)
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedPathRelaxation",
		preparedFunctionName: "backendGeneratedPathRelaxationPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_path_relaxation_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated path-relaxation fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), fixture, generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated path-relaxation source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var ra9_0 [0]float64", "var ra9_1 [0]float64", "var rfs0 int", "switch rr", "case 9:",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated path-relaxation source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "GET_INDEX", "SET_INDEX", "GET_STRING_FIELD", "SET_STRING_FIELD",
		"PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated path-relaxation source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendPathRelaxationProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedPathRelaxation(seed)
		if !ok {
			t.Fatalf("generated path-relaxation fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("path-relaxation oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, number := oracle[0].Number()
		if !number || got != oracleNumber {
			t.Fatalf("generated/oracle path-relaxation seed %v = %v/%v (%t)", seed, got, oracleNumber, number)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedPathRelaxation(29)
		}); allocations != 0 {
			t.Fatalf("generated path-relaxation allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoNestedRecordArraysWithEmptyMemberAreIdentityBlindAndFailClosed(t *testing.T) {
	emit := func(source string) []byte {
		generated, err := emitBackendGoNumericProof(
			backendRecordArrayProofIR(t, source),
			backendGoNumericOptions{
				packageName:          "ember",
				functionName:         "identityBlindPathRelaxation",
				preparedFunctionName: "identityBlindPathRelaxationPrepared",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		return generated
	}
	renamed := strings.Replace(backendPathRelaxationProofSource, "local function kernel(seed)", "local function opaque(seed)", 1)
	renamed = strings.Replace(renamed, "return kernel", "return opaque", 1)
	if !bytes.Equal(emit(backendPathRelaxationProofSource), emit(renamed)) {
		t.Fatal("path-relaxation lowering depends on private function identity")
	}

	for name, source := range map[string]string{
		"mixed edge payload": strings.Replace(
			backendPathRelaxationProofSource,
			"{to = 2, weight = 3}",
			`{to = 2, weight = "heavy"}`,
			1,
		),
		"edge shape drift": strings.Replace(
			backendPathRelaxationProofSource,
			"{to = 2, weight = 3}",
			"{to = 2, cost = 3}",
			1,
		),
		"escaping edge array": strings.Replace(
			backendPathRelaxationProofSource,
			"return total + sum",
			"return nodes[1].edges",
			1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := emitBackendGoNumericProof(
				backendRecordArrayProofIR(t, source),
				backendGoNumericOptions{packageName: "ember", functionName: "rejectUnprovedPathRelaxation"},
			); err == nil {
				t.Fatalf("path-relaxation compiler accepted %s", name)
			}
		})
	}
}

func BenchmarkBackendGeneratedPathRelaxation(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedPathRelaxation(float64(iteration & 31))
		if !ok {
			b.Fatal("generated path-relaxation fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
