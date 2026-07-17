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

const backendEconomyMarketProofSource = `
local function kernel(seed)
    local markets = {
        {stock = {wood = 40 + seed % 7, ore = 18, food = 30}, demand = {wood = 8, ore = 14, food = 6}, price = {wood = 3, ore = 9, food = 4}},
        {stock = {wood = 12, ore = 35, food = 20}, demand = {wood = 15, ore = 5, food = 10}, price = {wood = 5, ore = 7, food = 6}},
        {stock = {wood = 25, ore = 11, food = 50}, demand = {wood = 10, ore = 18, food = 4}, price = {wood = 4, ore = 10, food = 3}},
    }
    local orders = {
        {good = "wood", amount = 7, kind = "buy"},
        {good = "ore", amount = 4, kind = "buy"},
        {good = "food", amount = 9, kind = "sell"},
        {good = "ore", amount = 3, kind = "sell"},
    }
    local cash = 0
    for day = 1, 54 do
        for _, market in markets do
            for _, order in orders do
                local good = order.good
                local pressure = market.demand[good] - market.stock[good] // 5
                local price = market.price[good] + pressure
                if price < 1 then
                    price = 1
                end
                if order.kind == "buy" then
                    local amount = math.min(order.amount + day % 3, market.stock[good])
                    market.stock[good] = market.stock[good] - amount
                    cash = cash - amount * price
                else
                    local amount = order.amount + day % 2
                    market.stock[good] = market.stock[good] + amount
                    cash = cash + amount * price
                end
                market.price[good] = price + day % 2
            end
        end
    end
    local inventoryScore = 0
    for _, market in markets do
        inventoryScore = inventoryScore + market.stock.wood + market.stock.ore * 2 + market.stock.food
    end
    return cash + inventoryScore
end
return kernel
`

func TestBackendGoEconomyMarketChildSelectorCanGenerate(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendEconomyMarketProofSource)
	records := analyzeBackendGoRecordTables(ir, analyzeBackendGoStructuralKeys(ir, backendGoNumericOptions{}))
	if !records.enabled {
		t.Fatalf("economy-market records were not recognized: %s", records.rejectReason)
	}
	childRefs := 0
	for _, ref := range records.refs {
		if ref.kind == backendGoRecordRefChildRecord {
			childRefs++
		}
	}
	if len(records.maps) != 0 || len(records.arrays) != 2 || len(records.records) != 16 ||
		len(records.childRecords) != 3 || len(records.fusedGetByPC) != 6 || len(records.fusedSetByPC) != 3 || childRefs != 3 {
		t.Fatalf(
			"economy-market inventory = maps %d arrays %d records %d children %d gets %d sets %d child refs %d, want 0/2/16/3/6/3/3",
			len(records.maps), len(records.arrays), len(records.records), len(records.childRecords),
			len(records.fusedGetByPC), len(records.fusedSetByPC), childRefs,
		)
	}
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedEconomyMarket",
		preparedFunctionName: "backendGeneratedEconomyMarketPreparedFixture",
	})
	if err != nil {
		t.Fatalf("emit economy-market child selector: %v", err)
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "generated_economy_market.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated economy-market source: %v", err)
	}
}

func TestBackendGoEconomyMarketFixtureIsFreshAndCorrect(t *testing.T) {
	ir := backendRecordArrayProofIR(t, backendEconomyMarketProofSource)
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedEconomyMarket",
		preparedFunctionName: "backendGeneratedEconomyMarketPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_economy_market_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated economy-market fixture is stale")
	}
	text := string(generated)
	for _, required := range []string{
		"var ra0_0 [3]float64",
		"var ra0_2 [3]float64",
		"var ra1_0 [4]uint32",
		"switch int(v",
		"case uint32(1):",
		"case uint32(2):",
		"case uint32(3):",
		"math.Min(",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated economy-market source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "SET_FIELD", "GET_FIELD", "SET_STRING_FIELD_INDEX", "GET_STRING_FIELD_INDEX",
		"PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated economy-market source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendEconomyMarketProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedEconomyMarket(seed)
		if !ok {
			t.Fatalf("generated economy-market fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("economy-market oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle economy-market seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedEconomyMarket(29)
		}); allocations != 0 {
			t.Fatalf("generated economy-market allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoEconomyMarketIsIdentityBlindAndRejectsUnprovedSelectors(t *testing.T) {
	emit := func(source string) []byte {
		generated, err := emitBackendGoNumericProof(
			backendRecordArrayProofIR(t, source),
			backendGoNumericOptions{
				packageName:          "ember",
				functionName:         "identityBlindEconomyMarket",
				preparedFunctionName: "identityBlindEconomyMarketPrepared",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		return generated
	}
	renamed := strings.Replace(
		backendEconomyMarketProofSource,
		"local function kernel(seed)",
		"local function opaque(seed)",
		1,
	)
	baseGenerated := emit(backendEconomyMarketProofSource)
	renamedGenerated := emit(renamed)
	for _, marker := range []string{"var ra0_0 [3]float64", "var ra1_0 [4]uint32", "case uint32("} {
		if strings.Count(string(baseGenerated), marker) != strings.Count(string(renamedGenerated), marker) {
			t.Fatalf("economy-market private rename changed structural lowering marker %q", marker)
		}
	}

	tests := map[string]string{
		"mixed child field tags": strings.Replace(
			backendEconomyMarketProofSource,
			"food = 30",
			"food = false",
			1,
		),
		"mutation changes child tags": strings.Replace(
			backendEconomyMarketProofSource,
			"market.price[good] = price + day % 2",
			"market.price[good] = good",
			1,
		),
		"escaping child selector": strings.Replace(
			backendEconomyMarketProofSource,
			"return cash + inventoryScore",
			"return markets[1].stock",
			1,
		),
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			ir := backendRecordArrayProofIR(t, source)
			if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
				packageName:  "ember",
				functionName: "rejectUnprovedEconomyMarket",
			}); err == nil {
				t.Fatalf("child-record selector compiler accepted %s", name)
			}
		})
	}
	heterogeneous := strings.Replace(
		backendEconomyMarketProofSource,
		"stock = {wood = 25, ore = 11, food = 50}",
		"stock = {wood = 25, ore = 11, grain = 50}",
		1,
	)
	generated, err := emitBackendGoNumericProof(
		backendRecordArrayProofIR(t, heterogeneous),
		backendGoNumericOptions{packageName: "ember", functionName: "heterogeneousEconomyMarket"},
	)
	if err != nil {
		t.Fatalf("heterogeneous child-record holdout was rejected: %v", err)
	}
	if !strings.Contains(string(generated), "var rp") {
		t.Fatal("heterogeneous child-record holdout lacks explicit field presence")
	}
}

func BenchmarkBackendGeneratedEconomyMarket(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedEconomyMarket(float64(iteration & 31))
		if !ok {
			b.Fatal("generated economy-market fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
