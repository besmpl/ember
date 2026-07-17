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

const backendArrayHoleCompactionProofSource = `
local function kernel(seed)
    local values = {}
    for i = 1, 30 + seed % 2 do
        values[i] = {score = i * 3 + seed % 3, live = true}
    end
    local total = 0
    for tick = 1, 70 + seed % 2 do
        local i = 1
        while i <= rawlen(values) do
            local row = values[i]
            row.score = row.score + tick % 6
            total = total + row.score
            if row.score % 13 == 0 then
                table.remove(values, i)
            else
                i = i + 1
            end
        end
        if tick % 5 == 0 then
            table.insert(values, {score = tick + seed % 3, live = true})
        end
    end
    return total + rawlen(values)
end
return kernel
`

func TestBackendGoBoundedRootRecordArrayCompactionCanGenerate(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendArrayHoleCompactionProofSource)
	records := analyzeBackendGoRecordTables(ir, analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{}))
	if !records.enabled {
		t.Fatalf("analyze bounded root record-array compaction: %s", records.rejectReason)
	}
	if len(records.records) != 2 || len(records.arrays) != 1 ||
		len(records.arraySetByPC) != 1 || len(records.arrayInsertPC) != 1 ||
		len(records.arrayRemovePC) != 1 || len(records.arrayRawLenPC) != 2 ||
		!records.arrays[0].mutable || records.arrays[0].capacity != 104 {
		t.Fatalf(
			"array-hole inventory = records %d arrays %d set/insert/remove/rawlen %d/%d/%d/%d mutable/capacity %t/%d",
			len(records.records), len(records.arrays), len(records.arraySetByPC), len(records.arrayInsertPC),
			len(records.arrayRemovePC), len(records.arrayRawLenPC), records.arrays[0].mutable, records.arrays[0].capacity,
		)
	}
	if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedArrayHoleCompaction",
		preparedFunctionName: "backendGeneratedArrayHoleCompactionPreparedFixture",
	}); err != nil {
		t.Fatalf("emit bounded root record-array compaction: %v", err)
	}
}

func TestBackendGoBoundedRootRecordArrayCompactionFixtureIsFreshAndCorrect(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendArrayHoleCompactionProofSource)
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedArrayHoleCompaction",
		preparedFunctionName: "backendGeneratedArrayHoleCompactionPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_array_hole_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated array-hole fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), fixture, generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated array-hole source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var ra0_0 [104]float64", "var rn0 = 0", "float64(rn0+1)",
		"for rrm51 :=", "ra0_0[rrm51+1]", "rn0--", "rn0++",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated array-hole source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "GET_INDEX", "SET_INDEX", "FAST_CALL",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated array-hole source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendArrayHoleCompactionProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedArrayHoleCompaction(seed)
		if !ok {
			t.Fatalf("generated array-hole fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("array-hole oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, number := oracle[0].Number()
		if !number || got != oracleNumber {
			t.Fatalf("generated/oracle array-hole seed %v = %v/%v (%t)", seed, got, oracleNumber, number)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedArrayHoleCompaction(29)
		}); allocations != 0 {
			t.Fatalf("generated array-hole allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoBoundedRootRecordArrayCompactionIsIdentityBlindAndFailClosed(t *testing.T) {
	emit := func(source string) []byte {
		generated, err := emitBackendGoNumericProof(
			backendRecordArrayProofIR(t, source),
			backendGoNumericOptions{
				packageName:          "ember",
				functionName:         "identityBlindArrayHoleCompaction",
				preparedFunctionName: "identityBlindArrayHoleCompactionPrepared",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		return generated
	}
	renamed := strings.Replace(backendArrayHoleCompactionProofSource, "local function kernel(seed)", "local function opaque(seed)", 1)
	renamed = strings.Replace(renamed, "return kernel", "return opaque", 1)
	if !bytes.Equal(emit(backendArrayHoleCompactionProofSource), emit(renamed)) {
		t.Fatal("array-hole lowering depends on private function identity")
	}

	for name, source := range map[string]string{
		"parameter-unbounded fill": strings.Replace(
			backendArrayHoleCompactionProofSource, "30 + seed % 2", "30 + seed", 1,
		),
		"heterogeneous append shape": strings.Replace(
			backendArrayHoleCompactionProofSource,
			"{score = tick + seed % 3, live = true}",
			"{score = tick + seed % 3, other = true}",
			1,
		),
		"escaping array": strings.Replace(
			backendArrayHoleCompactionProofSource, "return total + rawlen(values)", "return values", 1,
		),
		"observed removed record": strings.Replace(
			backendArrayHoleCompactionProofSource,
			"table.remove(values, i)",
			"local removed = table.remove(values, i)\n                total = total + removed.score",
			1,
		),
		"live row alias after remove": strings.Replace(
			backendArrayHoleCompactionProofSource,
			"table.remove(values, i)",
			"table.remove(values, i)\n                total = total + row.score",
			1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := emitBackendGoNumericProof(
				backendRecordArrayProofIR(t, source),
				backendGoNumericOptions{packageName: "ember", functionName: "rejectUnprovedArrayHoleCompaction"},
			); err == nil {
				t.Fatalf("array-hole compiler accepted %s", name)
			}
		})
	}

	sparse := strings.Replace(backendArrayHoleCompactionProofSource, "values[i] =", "values[i + 1] =", 1)
	sparseIR := backendRecordArrayProofIR(t, sparse)
	generated, err := emitBackendGoNumericProof(sparseIR, backendGoNumericOptions{
		packageName: "ember", functionName: "rejectSparseArrayHoleCompaction",
	})
	if err != nil {
		return
	}
	if !strings.Contains(string(generated), "!= float64(rn0+1)") {
		t.Fatal("sparse array-hole write lacks a runtime dense-index guard")
	}
}

func BenchmarkBackendGeneratedArrayHoleCompaction(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedArrayHoleCompaction(float64(iteration & 31))
		if !ok {
			b.Fatal("generated array-hole fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
