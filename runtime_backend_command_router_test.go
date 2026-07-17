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

const backendCommandRouterProofSource = `
local function kernel(seed)
    local state = {x = seed % 3, y = 0, score = 0, gold = 10}
    local function apply(name, ...)
        if name == "move" then
            local dx, dy = ...
            state.x = state.x + dx
            state.y = state.y + dy
            return state.x, state.y, state.score
        elseif name == "loot" then
            local a, b, c = ...
            state.gold = state.gold + a + b + c
            state.score = state.score + state.gold
            return state.gold, state.score, select("#", ...)
        elseif name == "spend" then
            local amount = ...
            state.gold = state.gold - amount
            return state.gold, state.x, state.y
        else
            return state.score, state.gold, 0
        end
    end
    local commands = {
        {"move", 1, 2, 0},
        {"loot", 3, 4, 5},
        {"spend", 6, 0, 0},
        {"wait", 0, 0, 0},
    }
    local total = 0
    for tick = 1, 60 + seed % 2 do
        for _, command in commands do
            local a, b, c = apply(command[1], command[2] + tick % 3, command[3], command[4])
            total = total + a + b + c
        end
    end
    return total + state.x + state.y + state.score + state.gold
end
return kernel
`

func backendCommandRouterProofIRs(t *testing.T) []*backendProtoIR {
	t.Helper()
	proto, err := Compile(backendCommandRouterProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 3 {
		t.Fatalf("command-router Proto count = %d, want 3", len(image.prototypes))
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

func backendCommandRouterProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	targets[2] = backendGoNumericTarget{
		ir:               irs[2],
		functionName:     "backendGeneratedCommandApply",
		fixedVarargCount: 3,
	}
	return targets
}

func TestBackendGoCommandRouterCanGenerate(t *testing.T) {
	irs := backendCommandRouterProofIRs(t)
	targets := backendCommandRouterProofTargets(irs)
	parameterTags, ok := backendGoNumericParameterTags(irs[2], 3)
	if !ok || len(parameterTags) != 4 || parameterTags[0] != backendTagString ||
		parameterTags[1] != backendTagNumber || parameterTags[2] != backendTagNumber ||
		parameterTags[3] != backendTagNumber {
		t.Fatalf("command target parameter tags = %x, want string/number/number/number", parameterTags)
	}
	if count, ok := backendGoNumericFixedResultCountFor(irs[2], 3); !ok || count != 3 {
		t.Fatalf("command target result count = %d, %t; want 3, true", count, ok)
	}
	openReturn := -1
	for pc := range irs[2].ops {
		if operation := &irs[2].ops[pc]; operation.op == opReturn && operation.returnCount < 0 {
			openReturn = pc
			break
		}
	}
	if openReturn <= 0 {
		t.Fatal("command target lacks its expected open select return")
	}
	unsupported := *irs[2]
	unsupported.ops = append([]backendOperationIR(nil), irs[2].ops...)
	unsupported.ops[openReturn-1].nativeID = int32(nativeFuncRawLen)
	if count, ok := backendGoNumericFixedResultCountFor(&unsupported, 3); ok {
		t.Fatalf("arbitrary open return normalized to %d fixed results", count)
	}
	targetPlan, err := buildBackendGoNumericPlan(irs[2], backendGoNumericOptions{fixedVarargCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	if targetPlan.tables.externalRoot == invalidBackendValueID || len(targetPlan.tables.fields) != 4 {
		t.Fatalf("captured target fields = external %d fields %d, want external and 4", targetPlan.tables.externalRoot, len(targetPlan.tables.fields))
	}
	callerPlan, err := buildBackendGoNumericPlan(irs[1], backendGoNumericOptions{directTargets: targets})
	if err != nil {
		t.Fatal(err)
	}
	if !callerPlan.records.enabled || len(callerPlan.records.records) != 5 ||
		len(callerPlan.records.arrays) != 1 || len(callerPlan.captured.calls) != 1 {
		t.Fatalf(
			"command caller inventory = records %d arrays %d captured calls %d (enabled %t)",
			len(callerPlan.records.records), len(callerPlan.records.arrays),
			len(callerPlan.captured.calls), callerPlan.records.enabled,
		)
	}
	if _, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName:      "ember",
		functionName:     "backendGeneratedCommandApply",
		fixedVarargCount: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedCommandRouter",
		preparedFunctionName: "backendGeneratedCommandRouterPreparedFixture",
		directTargets:        targets,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBackendGoCommandRouterFixturesAreFreshAndCorrect(t *testing.T) {
	irs := backendCommandRouterProofIRs(t)
	targets := backendCommandRouterProofTargets(irs)
	fixtures := []struct {
		path      string
		generated []byte
	}{
		{path: "runtime_backend_command_apply_generated_test.go"},
		{path: "runtime_backend_command_router_generated_test.go"},
	}
	var err error
	fixtures[0].generated, err = emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName: "ember", functionName: "backendGeneratedCommandApply", fixedVarargCount: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixtures[1].generated, err = emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName: "ember", functionName: "backendGeneratedCommandRouter",
		preparedFunctionName: "backendGeneratedCommandRouterPreparedFixture", directTargets: targets,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		onDisk, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixture.generated, onDisk) {
			t.Fatalf("generated command-router fixture %s is stale", fixture.path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), fixture.path, fixture.generated, goparser.AllErrors); err != nil {
			t.Fatalf("parse %s: %v", fixture.path, err)
		}
	}
	generated := string(fixtures[0].generated) + string(fixtures[1].generated)
	for _, required := range []string{
		"p0 uint32", "v78 = 3", "var ra0_0 [4]uint32", "var ra0_1 [4]float64",
		"m78_0 = r0_0", "backendGeneratedCommandApply(&m78_0", "r0_0 = m78_0",
	} {
		if !strings.Contains(generated, required) {
			t.Fatalf("generated command-router source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "machineString", "opcode", "descriptor",
		"NEW_TABLE", "GET_INDEX", "GET_STRING_FIELD", "SET_STRING_FIELD", "FAST_CALL", "CALL",
	} {
		if strings.Contains(generated, forbidden) {
			t.Fatalf("generated command-router source contains runtime materialization/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendCommandRouterProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedCommandRouter(seed)
		if !ok {
			t.Fatalf("generated command router exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("oracle command router seed %v returned %d values, want 1", seed, len(oracle))
		}
		want, number := oracle[0].Number()
		if !number || got != want {
			t.Fatalf("generated/oracle command router seed %v = %v/%v (%t)", seed, got, want, number)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedCommandRouter(29)
		}); allocations != 0 {
			t.Fatalf("generated command-router allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoCommandRouterIsIdentityBlindAndFailClosed(t *testing.T) {
	emit := func(source string) []byte {
		t.Helper()
		proto, err := Compile(source)
		if err != nil {
			t.Fatal(err)
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
		generated, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
			packageName: "ember", functionName: "identityBlindCommandRouter",
			directTargets: backendCommandRouterProofTargets(irs),
		})
		if err != nil {
			t.Fatal(err)
		}
		return generated
	}
	renamed := strings.Replace(backendCommandRouterProofSource, "local function kernel(seed)", "local function opaque(seed)", 1)
	renamed = strings.Replace(renamed, "return kernel", "return opaque", 1)
	if !bytes.Equal(emit(backendCommandRouterProofSource), emit(renamed)) {
		t.Fatal("command-router lowering depends on private function identity")
	}

	for name, source := range map[string]string{
		"mixed captured field":    strings.Replace(backendCommandRouterProofSource, "score = 0", `score = "bad"`, 1),
		"escaping captured state": strings.Replace(backendCommandRouterProofSource, "return total + state.x + state.y + state.score + state.gold", "return state", 1),
		"dynamic tuple position":  strings.Replace(backendCommandRouterProofSource, "command[1]", "command[seed % 4 + 1]", 1),
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
			if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
				packageName: "ember", functionName: "rejectUnprovedCommandRouter",
				directTargets: backendCommandRouterProofTargets(irs),
			}); err == nil {
				t.Fatalf("command-router compiler accepted %s", name)
			}
		})
	}
}

func BenchmarkBackendGeneratedCommandRouter(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedCommandRouter(float64(iteration & 31))
		if !ok {
			b.Fatal("generated command router exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}
