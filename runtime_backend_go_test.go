package ember

import (
	"bytes"
	"context"
	"fmt"
	goparser "go/parser"
	"go/token"
	"math"
	"os"
	"strings"
	"testing"
)

const backendNumericProofSource = `
local function kernel(seed)
    local total = seed
    for index = 1, 64 do
        if index % 2 == 0 then
            total = total + index * seed
        else
            total = total - 1
        end
    end
    return total
end
return kernel
`

const backendNumericExitProofSource = `
local function guarded(seed)
    local adjusted = seed + 1
    if adjusted < 10 then
        return adjusted
    end
    return adjusted * 2
end
return guarded
`

const backendNumericCallProofSource = `
local function kernel(seed)
    local function add(value)
        if value < 1000000000000 then
            return value + 1
        end
        return value + 1
    end
    local total = seed
    for index = 1, 64 do
        total = add(total)
    end
    return total
end
return kernel
`

const backendTableFieldProofSource = `
local function kernel(seed)
    local player = {stats = {hp = 100 + seed % 7, shield = 25}, inventory = {coins = 3}}
    local i = 0
    while i < 80 do
        i = i + 1
        player.stats.hp = player.stats.hp + player.stats.shield - player.inventory.coins
    end
    return player.stats.hp
end
return kernel
`

const backendArrayIterationProofSource = `
local function kernel(seed)
    local values = {1 + seed % 5, 2, 3, 4, 5, 6, 7, 8}
    local total = 0
    for _, value in values do
        total = total + value * value
    end
    return total
end
return kernel
`

const backendArrayOpsProofSource = `
local function kernel(seed)
    local values = {}
    for i = 1, 80 do
        table.insert(values, i % 9 + seed % 3)
    end
    local removed = 0
    for i = 1, 20 do
        removed = removed + table.remove(values, 1)
    end
    return removed + rawlen(values)
end
return kernel
`

const backendClosureProofSource = `
local function kernel(seed)
    local function makeCounter(initial)
        local value = initial
        return function(step)
            value = value + step
            return value
        end
    end
    local counter = makeCounter(10 + seed % 3)
    local total = 0
    for i = 1, 60 do
        total = total + counter(i % 4)
    end
    return total
end
return kernel
`

func TestBackendGoNumericProofEmitsDeterministicDirectSource(t *testing.T) {
	ir := backendNumericProofIR(t)
	options := backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedNumericFixture",
		preparedFunctionName: "backendGeneratedNumericPreparedFixture",
	}
	first, err := emitBackendGoNumericProof(ir, options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := emitBackendGoNumericProof(ir, options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("numeric Go proof source is not deterministic")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "generated.go", first, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated source: %v", err)
	}
	text := string(first)
	for _, forbidden := range []string{"switch", "opcode", "descriptor", "for {"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated source contains dispatch marker %q", forbidden)
		}
	}
	if !strings.Contains(text, "math.Floor") ||
		!strings.Contains(text, "goto b") ||
		!strings.Contains(text, "context.numberParameter(0)") ||
		!strings.Contains(text, "context.replayBeforeOperation(") ||
		!strings.Contains(text, "machinePreparedReturnOneNumber(v") {
		t.Fatalf("generated source does not contain direct arithmetic CFG:\n%s", text)
	}
}

func TestBackendGoNumericProofRejectsEscapingObjectProgram(t *testing.T) {
	proto, err := Compile("local value = { field = 1 }\nreturn value")
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProtoIR(&image.prototypes[0])
	if err != nil {
		t.Fatal(err)
	}
	_, err = emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:  "ember",
		functionName: "rejected",
	})
	if err == nil || !strings.Contains(err.Error(), "not scalar replaceable") {
		t.Fatalf("emit object program = %v", err)
	}
}

func TestBackendGoNumericProofRejectsMissingOrNonnumericDirectTarget(t *testing.T) {
	caller, _ := backendNumericCallProofIRs(t)
	_, err := emitBackendGoNumericProof(caller, backendGoNumericOptions{
		packageName:  "ember",
		functionName: "missingTarget",
	})
	if err == nil {
		t.Fatal("emitted direct call without a bound target")
	}

	proto, err := Compile("local function object(value) return { value } end\nreturn object")
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	objectIR, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]backendGoNumericTarget, 3)
	targets[2] = backendGoNumericTarget{
		ir:           objectIR,
		functionName: "objectTarget",
	}
	_, err = emitBackendGoNumericProof(caller, backendGoNumericOptions{
		packageName:   "ember",
		functionName:  "nonnumericTarget",
		directTargets: targets,
	})
	if err == nil || !strings.Contains(err.Error(), "not a numeric leaf") {
		t.Fatalf("emit nonnumeric direct target = %v", err)
	}
}

func TestBackendGoNumericProofIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendNumericProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendNumericProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated Programs unexpectedly share a binding hash")
	}
	options := backendGoNumericOptions{packageName: "ember", functionName: "identityBlind"}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	renamedSource, err := emitBackendGoNumericProof(renamed.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, renamedSource) {
		t.Fatal("source or entrypoint identity selected generated executable code")
	}
}

func TestBackendGoNumericDirectCallIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendNumericCallProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendNumericCallProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated direct-call Programs unexpectedly share a binding hash")
	}
	emit := func(program *backendProgramIR) []byte {
		t.Helper()
		targets := make([]backendGoNumericTarget, 3)
		targets[2] = backendGoNumericTarget{
			ir:           program.modules[0].protos[2],
			functionName: "identityBlindDirectTarget",
		}
		source, err := emitBackendGoNumericProof(program.modules[0].protos[1], backendGoNumericOptions{
			packageName:   "ember",
			functionName:  "identityBlindDirectCaller",
			directTargets: targets,
		})
		if err != nil {
			t.Fatal(err)
		}
		return source
	}
	if !bytes.Equal(emit(base), emit(renamed)) {
		t.Fatal("source or entrypoint identity selected generated direct-call code")
	}
}

func TestBackendGoScalarTableFieldsIgnoreSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendTableFieldProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendTableFieldProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated table-field Programs unexpectedly share a binding hash")
	}
	options := backendGoNumericOptions{packageName: "ember", functionName: "identityBlindTableFields"}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	renamedSource, err := emitBackendGoNumericProof(renamed.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, renamedSource) {
		t.Fatal("source or entrypoint identity selected scalar table-field code")
	}
}

func TestBackendGoScalarArrayIterationIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendArrayIterationProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendArrayIterationProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated array-iteration Programs unexpectedly share a binding hash")
	}
	options := backendGoNumericOptions{packageName: "ember", functionName: "identityBlindArrayIteration"}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	renamedSource, err := emitBackendGoNumericProof(renamed.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, renamedSource) {
		t.Fatal("source or entrypoint identity selected scalar array-iteration code")
	}
}

func TestBackendGoScalarArrayOpsIgnoreSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendArrayOpsProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendArrayOpsProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated array-ops Programs unexpectedly share a binding hash")
	}
	options := backendGoNumericOptions{packageName: "ember", functionName: "identityBlindArrayOps"}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	renamedSource, err := emitBackendGoNumericProof(renamed.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, renamedSource) {
		t.Fatal("source or entrypoint identity selected scalar array-ops code")
	}
}

func TestBackendGoScalarClosureIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendClosureProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendClosureProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated closure Programs unexpectedly share a binding hash")
	}
	emit := func(program *backendProgramIR) []byte {
		t.Helper()
		targets := backendClosureProofTargets(program.modules[0].protos)
		source, err := emitBackendGoNumericProof(program.modules[0].protos[1], backendGoNumericOptions{
			packageName:   "ember",
			functionName:  "identityBlindClosureKernel",
			directTargets: targets,
		})
		if err != nil {
			t.Fatal(err)
		}
		return source
	}
	if !bytes.Equal(emit(base), emit(renamed)) {
		t.Fatal("source or entrypoint identity selected scalar closure code")
	}
}

func TestBackendGoNumericProofGeneratedFixtureIsFreshAndCorrect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendNumericProofIR(t), backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedNumericFixture",
		preparedFunctionName: "backendGeneratedNumericPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("runtime_backend_numeric_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated numeric proof fixture is stale")
	}
	root, err := Compile(backendNumericProofSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.prototypes) != 1 {
		t.Fatalf("numeric proof source child count = %d, want 1", len(root.prototypes))
	}
	for _, seed := range []float64{0, 1, 7, 29} {
		got, ok := backendGeneratedNumericFixture(seed)
		if !ok {
			t.Fatalf("generated numeric proof exited for seed %v", seed)
		}
		want := seed
		for index := 1.0; index <= 64; index++ {
			if index-math.Floor(index/2)*2 == 0 {
				want += index * seed
			} else {
				want--
			}
		}
		if got != want {
			t.Fatalf("generated numeric proof seed %v = %v, want %v", seed, got, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("numeric proof oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedNumericFixture(29)
		}); allocations != 0 {
			t.Fatalf("generated numeric proof allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoNumericPreparedExitFixtureIsFreshAndDirect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendNumericExitProofIR(t), backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedNumericExitFixture",
		preparedFunctionName: "backendGeneratedNumericExitPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("runtime_backend_numeric_exit_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated numeric exit fixture is stale")
	}
	for _, test := range []struct {
		seed float64
		want float64
	}{
		{seed: 1, want: 2},
		{seed: 9, want: 20},
		{seed: 29, want: 60},
	} {
		got, ok := backendGeneratedNumericExitFixture(test.seed)
		if !ok || got != test.want {
			t.Fatalf("numeric exit fixture(%v) = (%v, %t), want (%v, true)", test.seed, got, ok, test.want)
		}
	}
	if _, ok := backendGeneratedNumericExitFixture(math.NaN()); ok {
		t.Fatal("numeric exit fixture accepted NaN comparison")
	}
}

func TestBackendGoNumericDirectCallFixturesAreFreshAndCorrect(t *testing.T) {
	caller, callee := backendNumericCallProofIRs(t)
	calleeOptions := backendGoNumericOptions{
		packageName:  "ember",
		functionName: "backendGeneratedNumericCallAdd",
	}
	generatedCallee, err := emitBackendGoNumericProof(callee, calleeOptions)
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]backendGoNumericTarget, 3)
	targets[2] = backendGoNumericTarget{
		ir:           callee,
		functionName: calleeOptions.functionName,
	}
	callerOptions := backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedNumericCallKernel",
		preparedFunctionName: "backendGeneratedNumericCallPreparedFixture",
		directTargets:        targets,
	}
	generatedCaller, err := emitBackendGoNumericProof(caller, callerOptions)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		path      string
		generated []byte
	}{
		{path: "runtime_backend_numeric_call_add_generated_test.go", generated: generatedCallee},
		{path: "runtime_backend_numeric_call_kernel_generated_test.go", generated: generatedCaller},
	} {
		onDisk, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixture.generated, onDisk) {
			t.Fatalf("generated numeric call fixture %s is stale", fixture.path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), fixture.path, fixture.generated, goparser.AllErrors); err != nil {
			t.Fatalf("parse %s: %v", fixture.path, err)
		}
	}
	callerText := string(generatedCaller)
	if !strings.Contains(callerText, "backendGeneratedNumericCallAdd(v") ||
		!strings.Contains(callerText, "return machinePreparedReplayEntry()") {
		t.Fatalf("generated caller lacks direct call or replay-safe fallback:\n%s", callerText)
	}
	for _, forbidden := range []string{"switch", "opcode", "descriptor", "closure"} {
		if strings.Contains(callerText, forbidden) {
			t.Fatalf("generated caller contains dispatch or materialized closure marker %q", forbidden)
		}
	}

	root, err := Compile(backendNumericCallProofSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.prototypes) != 1 {
		t.Fatalf("numeric call source child count = %d, want 1", len(root.prototypes))
	}
	for _, seed := range []float64{0, 1, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedNumericCallKernel(seed)
		if !ok || got != seed+64 {
			t.Fatalf("generated numeric call kernel(%v) = (%v, %t), want (%v, true)", seed, got, ok, seed+64)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("numeric call oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if _, ok := backendGeneratedNumericCallKernel(math.NaN()); ok {
		t.Fatal("generated direct caller failed to propagate the callee guard")
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedNumericCallKernel(29)
		}); allocations != 0 {
			t.Fatalf("generated numeric direct-call allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoScalarTableFieldFixtureIsFreshAndCorrect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendTableFieldProofIR(t), backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedTableFieldFixture",
		preparedFunctionName: "backendGeneratedTableFieldPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("runtime_backend_table_field_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated scalar table-field fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "runtime_backend_table_field_generated_test.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated scalar table-field source: %v", err)
	}
	text := string(generated)
	if !strings.Contains(text, "var f0 float64") || !strings.Contains(text, "f0 = v") {
		t.Fatalf("generated scalar table-field source lacks typed field locals:\n%s", text)
	}
	for _, forbidden := range []string{"switch", "opcode", "descriptor", "machineTable", "NEW_TABLE", "GET_STRING_FIELD"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated scalar table-field source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendTableFieldProofSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.prototypes) != 1 {
		t.Fatalf("table-field source child count = %d, want 1", len(root.prototypes))
	}
	for _, seed := range []float64{0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedTableFieldFixture(seed)
		want := 1860 + seed - math.Floor(seed/7)*7
		if !ok || got != want {
			t.Fatalf("generated scalar table-field fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("table-field oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle table-field seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedTableFieldFixture(29)
		}); allocations != 0 {
			t.Fatalf("generated scalar table-field allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoScalarArrayIterationFixtureIsFreshAndCorrect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendArrayIterationProofIR(t), backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedArrayIterationFixture",
		preparedFunctionName: "backendGeneratedArrayIterationPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("runtime_backend_array_iteration_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated scalar array-iteration fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "runtime_backend_array_iteration_generated_test.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated scalar array-iteration source: %v", err)
	}
	text := string(generated)
	if !strings.Contains(text, "var a0 [8]float64") ||
		!strings.Contains(text, "v39 = a0[i0]") ||
		!strings.Contains(text, "i0++") {
		t.Fatalf("generated scalar array-iteration source lacks a direct typed loop:\n%s", text)
	}
	for _, forbidden := range []string{
		"switch", "opcode", "descriptor", "machineTable",
		"NEW_TABLE", "SET_FIELD", "PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated scalar array-iteration source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendArrayIterationProofSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.prototypes) != 1 {
		t.Fatalf("array-iteration source child count = %d, want 1", len(root.prototypes))
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedArrayIterationFixture(seed)
		first := 1 + seed - math.Floor(seed/5)*5
		want := 203 + first*first
		if !ok || got != want {
			t.Fatalf("generated scalar array-iteration fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("array-iteration oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle array-iteration seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedArrayIterationFixture(29)
		}); allocations != 0 {
			t.Fatalf("generated scalar array-iteration allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoScalarArrayIterationRejectsUnprovedShapes(t *testing.T) {
	tests := map[string]string{
		"write after iteration": `
local function kernel(seed)
    local values = {seed, 2}
    local total = 0
    for _, value in values do
        total = total + value
    end
    values[3] = 3
    return total
end
return kernel
`,
		"mixed array and hash fields": `
local function kernel(seed)
    local values = {seed, 2}
    values.extra = 3
    local total = 0
    for _, value in values do
        total = total + value
    end
    return total
end
return kernel
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			proto, err := Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := proto.preparedCodeImage()
			if err != nil {
				t.Fatal(err)
			}
			ir, err := buildBackendProtoIR(&image.prototypes[1])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
				packageName:  "ember",
				functionName: "rejectUnprovedArrayShape",
			}); err == nil {
				t.Fatal("emitted scalar array iteration for an unproved shape")
			}
		})
	}
}

func TestBackendGoScalarArrayOpsFixtureIsFreshAndCorrect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendArrayOpsProofIR(t), backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedArrayOpsFixture",
		preparedFunctionName: "backendGeneratedArrayOpsPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("runtime_backend_array_ops_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated scalar array-ops fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "runtime_backend_array_ops_generated_test.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated scalar array-ops source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var a0 [80]float64",
		"t0 = h0 + n0",
		"a0[t0] = v",
		"v70 = a0[h0]",
		"v75 = float64(n0)",
		"context.intrinsicUnchanged(14)",
		"context.intrinsicUnchanged(23)",
		"context.intrinsicUnchanged(28)",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated scalar array-ops source lacks %q:\n%s", required, text)
		}
	}
	for _, forbidden := range []string{
		"switch", "opcode", "descriptor", "machineTable",
		"FAST_CALL", "tableInsert", "tableRemove", "rawLen",
		"append(", "copy(",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated scalar array-ops source contains runtime mutation/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendArrayOpsProofSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.prototypes) != 1 {
		t.Fatalf("array-ops source child count = %d, want 1", len(root.prototypes))
	}
	for _, seed := range []float64{0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedArrayOpsFixture(seed)
		want := backendArrayOpsExpected(seed)
		if !ok || got != want {
			t.Fatalf("generated scalar array-ops fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("array-ops oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle array-ops seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedArrayOpsFixture(29)
		}); allocations != 0 {
			t.Fatalf("generated scalar array-ops allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoScalarArrayOpsRejectUnprovedMutationShapes(t *testing.T) {
	tests := map[string]string{
		"position insert": `
local function kernel(seed)
    local values = {1}
    table.insert(values, 1, seed)
    return rawlen(values)
end
return kernel
`,
		"non-front remove": `
local function kernel(seed)
    local values = {seed, 2}
    return table.remove(values, 2)
end
return kernel
`,
		"unbounded append": `
local function kernel(seed)
    local values = {}
    while seed > 0 do
        table.insert(values, seed)
        seed = seed - 1
    end
    return rawlen(values)
end
return kernel
`,
		"nonprogressing numeric loop": `
local function kernel(seed)
    local values = {}
    for i = 9007199254740992, 9007199254740994 do
        table.insert(values, seed)
    end
    return rawlen(values)
end
return kernel
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			proto, err := Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := proto.preparedCodeImage()
			if err != nil {
				t.Fatal(err)
			}
			ir, err := buildBackendProtoIR(&image.prototypes[1])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
				packageName:  "ember",
				functionName: "rejectUnprovedArrayMutation",
			}); err == nil {
				t.Fatal("emitted scalar array operations for an unproved mutation shape")
			}
		})
	}
}

func TestBackendGoScalarClosureFixturesAreFreshAndCorrect(t *testing.T) {
	irs := backendClosureProofIRs(t)
	targetOptions := backendGoNumericOptions{
		packageName:  "ember",
		functionName: "backendGeneratedCounterBody",
	}
	generatedTarget, err := emitBackendGoNumericProof(irs[3], targetOptions)
	if err != nil {
		t.Fatal(err)
	}
	targets := backendClosureProofTargets(irs)
	callerOptions := backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedClosureKernel",
		preparedFunctionName: "backendGeneratedClosurePreparedFixture",
		directTargets:        targets,
	}
	generatedCaller, err := emitBackendGoNumericProof(irs[1], callerOptions)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		path      string
		generated []byte
	}{
		{path: "runtime_backend_closure_body_generated_test.go", generated: generatedTarget},
		{path: "runtime_backend_closure_kernel_generated_test.go", generated: generatedCaller},
	} {
		onDisk, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixture.generated, onDisk) {
			t.Fatalf("generated scalar closure fixture %s is stale", fixture.path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), fixture.path, fixture.generated, goparser.AllErrors); err != nil {
			t.Fatalf("parse %s: %v", fixture.path, err)
		}
	}
	targetText := string(generatedTarget)
	for _, required := range []string{"u0 *float64", "v5 = *u0", "*u0 = v7"} {
		if !strings.Contains(targetText, required) {
			t.Fatalf("generated closure body lacks %q:\n%s", required, targetText)
		}
	}
	callerText := string(generatedCaller)
	for _, required := range []string{
		"var c0 float64",
		"c0 = v23",
		"s0 = c0",
		"backendGeneratedCounterBody(&s0",
		"c0 = s0",
	} {
		if !strings.Contains(callerText, required) {
			t.Fatalf("generated closure caller lacks %q:\n%s", required, callerText)
		}
	}
	scratch := strings.Index(callerText, "s0 = c0")
	call := strings.Index(callerText, "backendGeneratedCounterBody(&s0")
	guard := strings.Index(callerText, "if !ok16")
	commit := strings.Index(callerText, "c0 = s0")
	if scratch < 0 || call <= scratch || guard <= call || commit <= guard {
		t.Fatalf("generated closure caller does not copy, guard, then commit captured state:\n%s", callerText)
	}
	for _, forbidden := range []string{
		"switch", "opcode", "descriptor", "machineClosure", "machineUpvalue",
		"CALL_LOCAL_ONE", "GET_UPVALUE", "SET_UPVALUE",
	} {
		if strings.Contains(targetText, forbidden) || strings.Contains(callerText, forbidden) {
			t.Fatalf("generated scalar closure source contains runtime dispatch/materialization marker %q", forbidden)
		}
	}

	root, err := Compile(backendClosureProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedClosureKernel(seed)
		want := backendClosureExpected(seed)
		if !ok || got != want {
			t.Fatalf("generated scalar closure fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("closure oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle closure seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if got, ok := backendGeneratedClosureKernel(math.NaN()); !ok || !math.IsNaN(got) {
		t.Fatalf("generated scalar closure NaN result = (%v, %t), want (NaN, true)", got, ok)
	}
	oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
		args: []Value{NumberValue(math.NaN())},
	})
	if err != nil {
		t.Fatal(err)
	}
	oracleNaN, ok := oracle[0].Number()
	if !ok || !math.IsNaN(oracleNaN) {
		t.Fatalf("closure oracle NaN result = %v (%t), want NaN", oracleNaN, ok)
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedClosureKernel(29)
		}); allocations != 0 {
			t.Fatalf("generated scalar closure allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoScalarClosureRejectsUnprovedShapes(t *testing.T) {
	tests := map[string]string{
		"two captures": `
local function kernel(seed)
    local function makeCounter(initial)
        local value = initial
        local calls = 0
        return function(step)
            calls = calls + 1
            value = value + step
            return value + calls
        end
    end
    local counter = makeCounter(seed)
    return counter(1)
end
return kernel
`,
		"derived capture": `
local function kernel(seed)
    local function makeCounter(initial)
        local value = initial + 1
        return function(step)
            value = value + step
            return value
        end
    end
    local counter = makeCounter(seed)
    return counter(1)
end
return kernel
`,
		"read only copied capture": `
local function kernel(seed)
    local function makeCounter(initial)
        local value = initial
        return function()
            return value
        end
    end
    local counter = makeCounter(seed)
    return counter()
end
return kernel
`,
		"merged independent closures": `
local function kernel(seed)
    local function makeCounter(initial)
        local value = initial
        return function(step)
            value = value + step
            return value
        end
    end
    local counter = nil
    if seed > 0 then
        counter = makeCounter(seed)
    else
        counter = makeCounter(-seed)
    end
    return counter(1)
end
return kernel
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
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
			targets := make([]backendGoNumericTarget, len(irs))
			for protoID := 2; protoID < len(irs); protoID++ {
				targets[protoID] = backendGoNumericTarget{
					ir:           irs[protoID],
					functionName: fmt.Sprintf("rejectedClosureTarget%d", protoID),
				}
			}
			if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
				packageName:   "ember",
				functionName:  "rejectUnprovedClosure",
				directTargets: targets,
			}); err == nil {
				t.Fatal("emitted scalar closure for an unproved capture shape")
			}
		})
	}
}

func backendClosureExpected(seed float64) float64 {
	value := 10 + seed - math.Floor(seed/3)*3
	total := 0.0
	for index := 1.0; index <= 60; index++ {
		value += index - math.Floor(index/4)*4
		total += value
	}
	return total
}

func backendArrayOpsExpected(seed float64) float64 {
	seedMod := seed - math.Floor(seed/3)*3
	removed := 0.0
	for index := 1.0; index <= 20; index++ {
		removed += index - math.Floor(index/9)*9 + seedMod
	}
	return removed + 60
}

func backendNumericProofIR(t *testing.T) *backendProtoIR {
	t.Helper()
	proto, err := Compile(backendNumericProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("numeric proof Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func backendNumericExitProofIR(t *testing.T) *backendProtoIR {
	t.Helper()
	proto, err := Compile(backendNumericExitProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("numeric exit proof Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func backendNumericCallProofIRs(t *testing.T) (caller, callee *backendProtoIR) {
	t.Helper()
	proto, err := Compile(backendNumericCallProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 3 {
		t.Fatalf("numeric call proof Proto count = %d, want 3", len(image.prototypes))
	}
	caller, err = buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	callee, err = buildBackendProtoIR(&image.prototypes[2])
	if err != nil {
		t.Fatal(err)
	}
	return caller, callee
}

func backendTableFieldProofIR(t *testing.T) *backendProtoIR {
	t.Helper()
	proto, err := Compile(backendTableFieldProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("table-field proof Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func backendArrayIterationProofIR(t *testing.T) *backendProtoIR {
	t.Helper()
	proto, err := Compile(backendArrayIterationProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("array-iteration proof Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func backendArrayOpsProofIR(t *testing.T) *backendProtoIR {
	t.Helper()
	proto, err := Compile(backendArrayOpsProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("array-ops proof Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func backendClosureProofIRs(t *testing.T) []*backendProtoIR {
	t.Helper()
	proto, err := Compile(backendClosureProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 4 {
		t.Fatalf("closure proof Proto count = %d, want 4", len(image.prototypes))
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

func backendClosureProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	targets[2] = backendGoNumericTarget{
		ir:           irs[2],
		functionName: "backendGeneratedCounterFactory",
	}
	targets[3] = backendGoNumericTarget{
		ir:           irs[3],
		functionName: "backendGeneratedCounterBody",
	}
	return targets
}

func BenchmarkBackendGeneratedNumericFixture(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedNumericFixture(float64(iteration & 31))
		if !ok {
			b.Fatal("generated numeric fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedArrayIterationFixture(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedArrayIterationFixture(float64(iteration & 31))
		if !ok {
			b.Fatal("generated scalar array-iteration fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedArrayOpsFixture(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedArrayOpsFixture(float64(iteration & 31))
		if !ok {
			b.Fatal("generated scalar array-ops fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedClosureKernel(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedClosureKernel(float64(iteration & 31))
		if !ok {
			b.Fatal("generated scalar closure fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

var backendGeneratedNumericSink float64

func FuzzBackendGoNumericProofDeterministicAndNeverPanics(f *testing.F) {
	for _, source := range []string{
		backendNumericProofSource,
		backendNumericExitProofSource,
		backendTableFieldProofSource,
		backendArrayIterationProofSource,
		backendArrayOpsProofSource,
		backendClosureProofSource,
		"local function add(value) return value + 1 end return add",
		"return { field = 1 }",
	} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		proto, err := Compile(source)
		if err != nil {
			return
		}
		image, err := proto.preparedCodeImage()
		if err != nil {
			return
		}
		irs := make([]*backendProtoIR, len(image.prototypes))
		for protoIndex := range image.prototypes {
			prepared := &image.prototypes[protoIndex]
			if !prepared.eligible {
				continue
			}
			ir, err := buildBackendProtoIR(prepared)
			if err != nil {
				t.Fatalf("build Proto %d: %v", protoIndex, err)
			}
			irs[protoIndex] = ir
		}
		for protoIndex, ir := range irs {
			if ir == nil {
				continue
			}
			targets := make([]backendGoNumericTarget, len(irs))
			for operationIndex := range ir.ops {
				operation := &ir.ops[operationIndex]
				if operation.call.kind != backendCallDirectProto ||
					operation.call.targetProto < 0 ||
					int(operation.call.targetProto) >= len(irs) ||
					irs[operation.call.targetProto] == nil {
					continue
				}
				targets[operation.call.targetProto] = backendGoNumericTarget{
					ir:           irs[operation.call.targetProto],
					functionName: fmt.Sprintf("Target%d", operation.call.targetProto),
				}
			}
			options := backendGoNumericOptions{
				packageName:          "proof",
				functionName:         "Run",
				preparedFunctionName: "RunPrepared",
				directTargets:        targets,
			}
			first, firstErr := emitBackendGoNumericProof(ir, options)
			second, secondErr := emitBackendGoNumericProof(ir, options)
			if (firstErr == nil) != (secondErr == nil) ||
				firstErr != nil && firstErr.Error() != secondErr.Error() ||
				!bytes.Equal(first, second) {
				t.Fatalf("Proto %d generated nondeterministically: %v / %v", protoIndex, firstErr, secondErr)
			}
			if firstErr == nil {
				if _, err := goparser.ParseFile(token.NewFileSet(), "generated.go", first, goparser.AllErrors); err != nil {
					t.Fatalf("parse generated Proto %d: %v", protoIndex, err)
				}
			}
		}
	})
}
