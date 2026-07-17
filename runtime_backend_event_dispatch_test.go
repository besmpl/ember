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

const backendEventDispatchProofSource = `
local function kernel(seed)
    local state = {hp = 100 + seed % 3, shield = 20, score = 0}
    local handlers = {}
    function handlers.damage(s, amount)
        if s.shield > 0 then
            local absorbed = math.min(s.shield, amount)
            s.shield = s.shield - absorbed
            amount = amount - absorbed
        end
        s.hp = s.hp - amount
        return s.hp
    end
    function handlers.heal(s, amount)
        s.hp = s.hp + amount
        return s.hp
    end
    function handlers.score(s, amount)
        s.score = s.score + amount
        return s.score
    end
    local events = {
        {kind = "damage", amount = 7},
        {kind = "score", amount = 5},
        {kind = "heal", amount = 3},
        {kind = "damage", amount = 11},
        {kind = "score", amount = 13},
    }
    local total = 0
    for round = 1, 50 + seed % 2 do
        for _, event in events do
            total = total + handlers[event.kind](state, event.amount + round % 3)
        end
    end
    return total + state.hp + state.shield + state.score
end
return kernel
`

func backendEventDispatchProofIRs(t *testing.T, source string) []*backendProtoIR {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 5 {
		t.Fatalf("event-dispatch Proto count = %d, want 5", len(image.prototypes))
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

func backendEventDispatchProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	targets[2] = backendGoNumericTarget{ir: irs[2], functionName: "backendGeneratedEventDamage", receiverTable: true}
	targets[3] = backendGoNumericTarget{ir: irs[3], functionName: "backendGeneratedEventHeal", receiverTable: true}
	targets[4] = backendGoNumericTarget{ir: irs[4], functionName: "backendGeneratedEventScore", receiverTable: true}
	return targets
}

func backendEventDispatchGeneratedSources(t *testing.T, source string) [][]byte {
	t.Helper()
	irs := backendEventDispatchProofIRs(t, source)
	targets := backendEventDispatchProofTargets(irs)
	generated := make([][]byte, 4)
	var err error
	for targetIndex, protoID := range []int{2, 3, 4} {
		target := targets[protoID]
		generated[targetIndex], err = emitBackendGoNumericProof(target.ir, backendGoNumericOptions{
			packageName: "ember", functionName: target.functionName, receiverTable: true,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	generated[3], err = emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedEventDispatch",
		preparedFunctionName: "backendGeneratedEventDispatchPreparedFixture",
		directTargets:        targets,
	})
	if err != nil {
		t.Fatal(err)
	}
	return generated
}

func TestBackendGoFiniteEventDispatchCanGenerate(t *testing.T) {
	irs := backendEventDispatchProofIRs(t, backendEventDispatchProofSource)
	targets := backendEventDispatchProofTargets(irs)
	plan, err := buildBackendGoNumericPlan(irs[1], backendGoNumericOptions{directTargets: targets})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.callSets.calls) != 1 || len(plan.callSets.excludedRoots) != 1 ||
		!plan.records.enabled || len(plan.records.records) != 6 || len(plan.records.arrays) != 1 {
		t.Fatalf(
			"event-dispatch plan = call sets %d excluded roots %d records %d arrays %d enabled %t",
			len(plan.callSets.calls), len(plan.callSets.excludedRoots), len(plan.records.records),
			len(plan.records.arrays), plan.records.enabled,
		)
	}
	for _, call := range plan.callSets.calls {
		if len(call.variants) != 3 {
			t.Fatalf("event-dispatch variants = %d, want 3", len(call.variants))
		}
		for _, variant := range call.variants {
			if len(variant.callerFields) == 0 || !variant.target.receiverTable {
				t.Fatalf("event-dispatch variant = fields %d receiver %t", len(variant.callerFields), variant.target.receiverTable)
			}
		}
	}
	_ = backendEventDispatchGeneratedSources(t, backendEventDispatchProofSource)
}

func TestBackendGoFiniteEventDispatchFixturesAreFreshAndCorrect(t *testing.T) {
	generated := backendEventDispatchGeneratedSources(t, backendEventDispatchProofSource)
	paths := []string{
		"runtime_backend_event_damage_generated_test.go",
		"runtime_backend_event_heal_generated_test.go",
		"runtime_backend_event_score_generated_test.go",
		"runtime_backend_event_dispatch_generated_test.go",
	}
	for index, path := range paths {
		onDisk, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated[index], onDisk) {
			t.Fatalf("generated event-dispatch fixture %s is stale", path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), path, generated[index], goparser.AllErrors); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
	}
	joined := string(bytes.Join(generated, nil))
	for _, required := range []string{
		"switch v", "backendGeneratedEventDamage", "backendGeneratedEventHeal",
		"backendGeneratedEventScore", "case uint32(", "default:",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("generated event-dispatch source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"GET_INDEX", "CALL_ONE", "FAST_CALL",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("generated event-dispatch source contains runtime materialization/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendEventDispatchProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedEventDispatch(seed)
		if !ok {
			t.Fatalf("generated event dispatch exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("oracle event dispatch seed %v returned %d values, want 1", seed, len(oracle))
		}
		want, number := oracle[0].Number()
		if !number || got != want {
			t.Fatalf("generated/oracle event dispatch seed %v = %v/%v (%t)", seed, got, want, number)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedEventDispatch(29)
		}); allocations != 0 {
			t.Fatalf("generated event-dispatch allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoFiniteEventDispatchIsIdentityBlindAndFailClosed(t *testing.T) {
	emit := func(source string) []byte {
		t.Helper()
		return backendEventDispatchGeneratedSources(t, source)[3]
	}
	renamed := strings.Replace(backendEventDispatchProofSource, "local function kernel(seed)", "local function opaque(seed)", 1)
	renamed = strings.Replace(renamed, "return kernel", "return opaque", 1)
	if !bytes.Equal(emit(backendEventDispatchProofSource), emit(renamed)) {
		t.Fatal("event-dispatch lowering depends on private function identity")
	}

	for name, source := range map[string]string{
		"handler reassignment": strings.Replace(
			backendEventDispatchProofSource,
			"local events = {",
			"handlers.heal = handlers.damage\n    local events = {",
			1,
		),
		"mismatched target arity": strings.Replace(
			backendEventDispatchProofSource,
			"function handlers.heal(s, amount)",
			"function handlers.heal(s, amount, extra)",
			1,
		),
		"escaping handler table": strings.Replace(
			backendEventDispatchProofSource,
			"return total + state.hp + state.shield + state.score",
			"return handlers",
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
			if len(irs) != 5 {
				return
			}
			if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
				packageName: "ember", functionName: "rejectUnprovedEventDispatch",
				directTargets: backendEventDispatchProofTargets(irs),
			}); err == nil {
				t.Fatalf("event-dispatch compiler accepted %s", name)
			}
		})
	}
}

func BenchmarkBackendGeneratedEventDispatch(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedEventDispatch(float64(iteration & 31))
		if !ok {
			b.Fatal("generated event dispatch exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
