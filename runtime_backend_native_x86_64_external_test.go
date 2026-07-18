package ember_test

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/besmpl/ember"
	"github.com/besmpl/ember/internal/parityfixture"
	"github.com/besmpl/ember/internal/preparednative"
)

func TestPreparedNativeX8664ArtifactIsDeterministic(t *testing.T) {
	entry := ember.LogicalModule("prepared-native-x86-64-deterministic")
	program, _, err := ember.LoadProgram(context.Background(), paritySourceLoader{entry.String(): {
		Name: entry.String(),
		Text: "local function add(x) return x + 1 end\nlocal function batch(n, seed) local total = 0 for i = 1, n do total = total + add(seed + i) end return total end\nreturn batch\n",
	}}, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: entry}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := ember.EmitPreparedNativeX8664ForTest(program)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ember.EmitPreparedNativeX8664ForTest(program)
	if err != nil {
		t.Fatal(err)
	}
	if first.ProgramHash != second.ProgramHash || len(first.Modules) != 1 || len(second.Modules) != 1 ||
		!bytes.Equal(first.Modules[0].Code, second.Modules[0].Code) {
		t.Fatal("x86-64 artifact is not deterministic")
	}
	if len(first.Modules[0].Functions) != 3 ||
		!first.Modules[0].Functions[1].Prepared || !first.Modules[0].Functions[2].Prepared {
		t.Fatalf("x86-64 function inventory = %#v", first.Modules[0].Functions)
	}
}

func TestPreparedNativeX8664ScalarSemantics(t *testing.T) {
	if (runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows") || runtime.GOARCH != "amd64" {
		t.Skip("prepared x86-64 execution proof requires Darwin, Linux, or Windows on amd64")
	}
	for _, tc := range []struct {
		name   string
		body   string
		input  float64
		output float64
	}{
		{name: "floor divide negative", body: "return x // 2", input: -5, output: -3},
		{name: "fractional modulo", body: "return x % 3", input: 5.5, output: 2.5},
		{name: "numeric loop", body: "local total = 0\nfor i = 1, 3 do total = total + i end\nreturn total + x", input: 4, output: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executable, function := prepareScalarNativeX8664Kernel(t, tc.body)
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

func prepareScalarNativeX8664Kernel(
	t *testing.T,
	body string,
) (*preparednative.Executable, ember.PreparedNativeX8664FunctionForTest) {
	t.Helper()
	entry := ember.LogicalModule("prepared-native-x86-64-scalar")
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
	artifact, err := ember.EmitPreparedNativeX8664ForTest(program)
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

func TestPreparedNativeX8664ArithmeticGuestBatchExecutesUnknownSource(t *testing.T) {
	if (runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows") || runtime.GOARCH != "amd64" {
		t.Skip("prepared x86-64 execution proof requires Darwin, Linux, or Windows on amd64")
	}
	fixture, err := parityfixture.BuildGuestBatch(top10LuauCases[0].source, parityfixture.GuestBatchVariant{
		CaseName: "__case", BatchName: "__batch",
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := ember.LogicalModule("prepared-native-x86-64-unknown-source")
	program, _, err := ember.LoadProgram(context.Background(), paritySourceLoader{entry.String(): {
		Name: entry.String(), Text: fixture.Program + "return __batch\n",
	}}, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: entry}},
		Parallelism: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := ember.EmitPreparedNativeX8664ForTest(program)
	if err != nil {
		t.Fatal(err)
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
	function := artifact.Modules[0].Functions[artifact.Modules[0].RootClosures[1]]
	got, prepared, err := executable.CallAt(function.Offset, 3, 17)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared || got != 14366 {
		t.Fatalf("batch(3, 17) = %v/%t, want 14366/prepared", got, prepared)
	}
	if _, prepared, err := executable.CallAt(function.Offset, math.NaN(), 17); err != nil || prepared {
		t.Fatalf("NaN loop bound = prepared %t, error %v; want canonical replay", prepared, err)
	}
}
