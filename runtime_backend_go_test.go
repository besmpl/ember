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

func TestBackendGoNumericProofRejectsObjectProgram(t *testing.T) {
	proto, err := Compile("local value = { field = 1 }\nreturn value.field")
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
	if err == nil || !strings.Contains(err.Error(), "unsupported opcode") {
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

var backendGeneratedNumericSink float64

func FuzzBackendGoNumericProofDeterministicAndNeverPanics(f *testing.F) {
	for _, source := range []string{
		backendNumericProofSource,
		backendNumericExitProofSource,
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
