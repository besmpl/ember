package ember

import (
	"bytes"
	"context"
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

func TestBackendGoNumericProofEmitsDeterministicDirectSource(t *testing.T) {
	ir := backendNumericProofIR(t)
	options := backendGoNumericOptions{
		packageName:  "ember",
		functionName: "backendGeneratedNumericFixture",
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
		!strings.Contains(text, "return") {
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

func TestBackendGoNumericProofGeneratedFixtureIsFreshAndCorrect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendNumericProofIR(t), backendGoNumericOptions{
		packageName:  "ember",
		functionName: "backendGeneratedNumericFixture",
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
		for protoIndex := range image.prototypes {
			prepared := &image.prototypes[protoIndex]
			if !prepared.eligible {
				continue
			}
			ir, err := buildBackendProtoIR(prepared)
			if err != nil {
				t.Fatalf("build Proto %d: %v", protoIndex, err)
			}
			options := backendGoNumericOptions{packageName: "proof", functionName: "Run"}
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
