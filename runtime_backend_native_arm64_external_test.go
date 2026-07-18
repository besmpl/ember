package ember_test

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/besmpl/ember"
	"github.com/besmpl/ember/internal/parityfixture"
	"github.com/besmpl/ember/internal/preparednative"
)

func TestPreparedNativeARM64ScalarSemantics(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("prepared native proof currently targets Darwin arm64")
	}
	for _, tc := range []struct {
		name   string
		body   string
		input  float64
		output float64
	}{
		{name: "floor divide odd", body: "return x // 2", input: 5, output: 2},
		{name: "floor divide negative", body: "return x // 2", input: -5, output: -3},
		{name: "floor divide fractional", body: "return x // 2", input: 5.5, output: 2},
		{name: "modulo", body: "return x % 17", input: 35, output: 1},
		{name: "negative modulo", body: "return x % 3", input: -5, output: 1},
		{name: "fractional modulo", body: "return x % 3", input: 5.5, output: 2.5},
		{name: "numeric loop", body: "local total = 0\nfor i = 1, 3 do total = total + i end\nreturn total + x", input: 4, output: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executable, function := prepareScalarNativeARM64Kernel(t, tc.body)
			got, prepared, err := executable.CallAt(function.Offset, tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if !prepared || got != tc.output {
				t.Fatalf("kernel(%v) = %v/%t, want %v/prepared", tc.input, got, prepared, tc.output)
			}
		})
	}
}

func prepareScalarNativeARM64Kernel(
	t *testing.T,
	body string,
) (*preparednative.Executable, ember.PreparedNativeARM64FunctionForTest) {
	t.Helper()
	entry := ember.LogicalModule("prepared-native-arm64-scalar")
	source := fmt.Sprintf("local function kernel(x)\n%s\nend\nreturn kernel\n", body)
	program, _, err := ember.LoadProgram(context.Background(), paritySourceLoader{entry.String(): {
		Name: entry.String(), Text: source,
	}}, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: entry}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := ember.EmitPreparedNativeARM64ForTest(program)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Modules) != 1 || len(artifact.Modules[0].Functions) < 2 ||
		!artifact.Modules[0].Functions[1].Prepared {
		t.Fatalf("scalar kernel was not prepared: %#v", artifact.Modules)
	}
	executable, err := preparednative.Compile(artifact.Modules[0].Code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := executable.Close(); err != nil {
			t.Error(err)
		}
	})
	return executable, artifact.Modules[0].Functions[1]
}

func TestPreparedNativeARM64ArithmeticGuestBatchExecutesUnknownSource(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("prepared native proof currently targets Darwin arm64")
	}
	fixture, err := parityfixture.BuildGuestBatch(top10LuauCases[0].source, parityfixture.GuestBatchVariant{
		CaseName: "__case", BatchName: "__batch",
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := ember.LogicalModule("prepared-native-arm64-unknown-source")
	program, _, err := ember.LoadProgram(context.Background(), paritySourceLoader{entry.String(): {
		Name: entry.String(), Text: fixture.Program + "return __batch\n",
	}}, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: entry}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := ember.EmitPreparedNativeARM64ForTest(program)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.Modules) != 1 || len(artifact.Modules[0].Functions) != 3 {
		t.Fatalf("prepared native inventory = %#v", artifact.Modules)
	}
	executable, err := preparednative.Compile(artifact.Modules[0].Code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := executable.Close(); err != nil {
			t.Error(err)
		}
	})

	caseFunction := artifact.Modules[0].Functions[1]
	for _, seed := range []float64{0, 1, 2, 17, 101} {
		got, prepared, err := executable.CallAt(caseFunction.Offset, seed)
		if err != nil {
			t.Fatal(err)
		}
		want := 1595 + math.Mod(seed, 3)
		if !prepared || got != want {
			t.Fatalf("case(%v) = %v/%t, want %v/prepared", seed, got, prepared, want)
		}
	}

	batchFunction := artifact.Modules[0].Functions[2]
	got, prepared, err := executable.CallAt(batchFunction.Offset, 3, 17)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared || got != 14366 {
		t.Fatalf("batch(3, 17) = %v/%t, want 14366/prepared", got, prepared)
	}
	if _, prepared, err := executable.CallAt(batchFunction.Offset, math.NaN(), 17); err != nil || prepared {
		t.Fatalf("NaN loop bound = prepared %t, error %v; want canonical replay", prepared, err)
	}
}
