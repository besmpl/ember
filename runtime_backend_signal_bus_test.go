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

const backendSignalBusProofSource = `
local function kernel(seed)
    local state = {hp = 120 + seed % 3, score = 0, armor = 3}
    local function makeHandler(mult)
        local seen = 0
        return function(s, event)
            seen = seen + 1
            if event.kind == "damage" then
                s.hp = s.hp - event.amount * mult + s.armor
            elseif event.kind == "heal" then
                s.hp = s.hp + event.amount + mult
            else
                s.score = s.score + event.amount * mult
            end
            return seen + s.hp + s.score
        end
    end
    local handlers = {
        damage = {makeHandler(1), makeHandler(2)},
        heal = {makeHandler(1)},
        score = {makeHandler(1), makeHandler(3)},
    }
    local events = {
        {kind = "damage", amount = 7},
        {kind = "score", amount = 4},
        {kind = "heal", amount = 5},
        {kind = "damage", amount = 3},
    }
    local total = 0
    for tick = 1, 45 + seed % 2 do
        for _, event in events do
            local bucket = handlers[event.kind]
            for _, handler in bucket do
                total = total + handler(state, event)
            end
        end
    end
    return total + state.hp + state.score
end
return kernel
`

func backendSignalBusProofIRs(t *testing.T, source string) []*backendProtoIR {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 4 {
		t.Fatalf("signal-bus Proto count = %d, want 4", len(image.prototypes))
	}
	irs := make([]*backendProtoIR, len(image.prototypes))
	for protoID := range image.prototypes {
		irs[protoID], err = buildBackendProtoIR(&image.prototypes[protoID])
		if err != nil {
			t.Fatal(err)
		}
	}
	return irs
}

func backendSignalBusProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	targets[2] = backendGoNumericTarget{ir: irs[2], functionName: "backendGeneratedSignalFactory"}
	targets[3] = backendGoNumericTarget{
		ir: irs[3], functionName: "backendGeneratedSignalHandler", receiverTables: 2,
	}
	return targets
}

func backendSignalBusGeneratedSources(t *testing.T, source string) (target []byte, caller []byte) {
	t.Helper()
	irs := backendSignalBusProofIRs(t, source)
	targets := backendSignalBusProofTargets(irs)
	var err error
	target, err = emitBackendGoNumericProof(irs[3], backendGoNumericOptions{
		packageName: "ember", functionName: "backendGeneratedSignalHandler", receiverTables: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	caller, err = emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedSignalBus",
		preparedFunctionName: "backendGeneratedSignalBusPreparedFixture",
		directTargets:        targets,
	})
	if err != nil {
		t.Fatal(err)
	}
	return target, caller
}

func TestBackendGoFiniteSignalBusCanGenerate(t *testing.T) {
	irs := backendSignalBusProofIRs(t, backendSignalBusProofSource)
	plan, err := buildBackendGoNumericPlan(irs[1], backendGoNumericOptions{
		directTargets: backendSignalBusProofTargets(irs),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.closureSets.sets) != 1 || len(plan.closureSets.excludedRoots) != 4 ||
		plan.closures.cellCount != 10 || len(plan.closures.factoriesByPC) != 5 ||
		!plan.records.enabled || len(plan.records.records) != 5 || len(plan.records.arrays) != 1 {
		t.Fatalf(
			"signal-bus plan = sets %d roots %d cells %d factories %d records %d arrays %d enabled %t",
			len(plan.closureSets.sets), len(plan.closureSets.excludedRoots), plan.closures.cellCount,
			len(plan.closures.factoriesByPC), len(plan.records.records), len(plan.records.arrays), plan.records.enabled,
		)
	}
	for _, set := range plan.closureSets.sets {
		if len(set.buckets) != 3 || len(set.receiverFields) != 5 || set.target.receiverTables != 2 {
			t.Fatalf("signal-bus set = buckets %d fields %d receivers %d", len(set.buckets), len(set.receiverFields), set.target.receiverTables)
		}
	}
	backendSignalBusGeneratedSources(t, backendSignalBusProofSource)
}

func TestBackendGoFiniteSignalBusFixturesAreFreshAndCorrect(t *testing.T) {
	target, caller := backendSignalBusGeneratedSources(t, backendSignalBusProofSource)
	fixtures := []struct {
		path      string
		generated []byte
	}{
		{path: "runtime_backend_signal_handler_generated_test.go", generated: target},
		{path: "runtime_backend_signal_bus_generated_test.go", generated: caller},
	}
	for _, fixture := range fixtures {
		onDisk, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(onDisk, fixture.generated) {
			t.Fatalf("generated signal-bus fixture %s is stale", fixture.path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), fixture.path, fixture.generated, goparser.AllErrors); err != nil {
			t.Fatalf("parse %s: %v", fixture.path, err)
		}
	}
	joined := string(append(append([]byte(nil), target...), caller...))
	for _, required := range []string{
		"u0 *float64", "u1 *float64", "r0 *uint32", "backendGeneratedSignalHandler",
		"switch int(v", "var c9 float64", "machinePreparedReplayEntry",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("generated signal-bus source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"CALL_LOCAL_ONE", "ARRAY_NEXT_JUMP2", "GET_INDEX",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("generated signal-bus source contains runtime materialization/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendSignalBusProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedSignalBus(seed)
		if !ok {
			t.Fatalf("generated signal bus exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		want, number := oracle[0].Number()
		if len(oracle) != 1 || !number || got != want {
			t.Fatalf("generated/oracle signal bus seed %v = %v/%v (%t)", seed, got, want, number)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedSignalBus(29)
		}); allocations != 0 {
			t.Fatalf("generated signal-bus allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoFiniteSignalBusIsIdentityBlindAndFailClosed(t *testing.T) {
	emit := func(source string) []byte {
		t.Helper()
		_, caller := backendSignalBusGeneratedSources(t, source)
		return caller
	}
	renamed := strings.Replace(backendSignalBusProofSource, "local function kernel(seed)", "local function opaque(seed)", 1)
	renamed = strings.Replace(renamed, "local function makeHandler(mult)", "local function forge(mult)", 1)
	renamed = strings.ReplaceAll(renamed, "makeHandler(", "forge(")
	renamed = strings.Replace(renamed, "return kernel", "return opaque", 1)
	if !bytes.Equal(emit(backendSignalBusProofSource), emit(renamed)) {
		t.Fatal("signal-bus lowering depends on private function identity")
	}

	for name, source := range map[string]string{
		"bucket mutation": strings.Replace(
			backendSignalBusProofSource,
			"local events = {",
			"table.insert(handlers.damage, makeHandler(4))\n    local events = {",
			1,
		),
		"closure escape": strings.Replace(
			backendSignalBusProofSource,
			"return total + state.hp + state.score",
			"return handlers.damage[1]",
			1,
		),
		"mixed callback target": strings.Replace(
			backendSignalBusProofSource,
			"heal = {makeHandler(1)},",
			"heal = {function() return 1 end},",
			1,
		),
		"observed callback index": strings.Replace(
			strings.Replace(
				backendSignalBusProofSource,
				"for _, handler in bucket do",
				"for index, handler in bucket do",
				1,
			),
			"total = total + handler(state, event)",
			"total = total + index + handler(state, event)",
			1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			proto, err := Compile(source)
			if err != nil {
				return
			}
			image, err := proto.preparedCodeImage()
			if err != nil {
				t.Fatal(err)
			}
			irs := make([]*backendProtoIR, len(image.prototypes))
			for protoID := range image.prototypes {
				irs[protoID], err = buildBackendProtoIR(&image.prototypes[protoID])
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(irs) != 4 {
				return
			}
			if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
				packageName: "ember", functionName: "rejectUnprovedSignalBus",
				directTargets: backendSignalBusProofTargets(irs),
			}); err == nil {
				t.Fatalf("signal-bus compiler accepted %s", name)
			}
		})
	}
}

func BenchmarkBackendGeneratedSignalBus(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedSignalBus(float64(iteration & 31))
		if !ok {
			b.Fatal("generated signal bus exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
