package ember

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestCompilerCorpusDiamondGraphsHaveExpectedReachability(t *testing.T) {
	for _, size := range []int{10, 100, 1000} {
		graph := prepareCompilerCorpusGraphShape(t, size, compilerCorpusGraphDiamond)
		if graph.shape != compilerCorpusGraphDiamond {
			t.Fatalf("prepared graph shape = %q, want %q", graph.shape, compilerCorpusGraphDiamond)
		}
		root := graph.loader.sources[graph.root.String()]
		if !strings.Contains(root, `require("./module0001")`) || !strings.Contains(root, `require("./module0002")`) {
			t.Fatalf("diamond root source = %q, want two branch dependencies", root)
		}
		for index := 0; index < size; index++ {
			name := LogicalModule("corpus/" + strconv.Itoa(size) + "/module" + fmt.Sprintf("%04d", index)).String()
			source := graph.loader.sources[name]
			wantDependencies := compilerExpectedDiamondDependencies(size, index)
			for dependency := 0; dependency < size; dependency++ {
				needle := `require("./module` + fmt.Sprintf("%04d", dependency) + `")`
				want := false
				for _, expected := range wantDependencies {
					if dependency == expected {
						want = true
						break
					}
				}
				if strings.Contains(source, needle) != want {
					t.Fatalf("module %d source = %q, dependency %d presence = %t, want %t", index, source, dependency, strings.Contains(source, needle), want)
				}
			}
		}
		validateCompilerCorpusGraph(t, graph)
	}
}

func compilerExpectedDiamondDependencies(size, index int) []int {
	if index+1 >= size {
		return nil
	}
	if index%3 == 0 && index+2 < size {
		return []int{index + 1, index + 2}
	}
	if index%3 == 1 && index+2 < size {
		return []int{index + 2}
	}
	if index%3 == 2 && index+1 < size {
		return []int{index + 1}
	}
	return []int{index + 1}
}

func TestCompilerStageOutputMetricsAgreeWithFullCompile(t *testing.T) {
	fixtures, err := prepareCompilerStageFixtures()
	if err != nil {
		t.Fatalf("prepareCompilerStageFixtures returned error: %v", err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			full, err := Compile(fixture.source)
			if err != nil {
				t.Fatalf("Compile returned error: %v", err)
			}
			got := CompilerBenchmarkMetricsForTest(full)
			stage, err := assembleAndSealCompilerStage(fixture.emission, fixture.optimized)
			if err != nil {
				t.Fatalf("assembleAndSealCompilerStage returned error: %v", err)
			}
			stageGot := CompilerBenchmarkMetricsForTest(stage)
			if !reflect.DeepEqual(got, stageGot) {
				t.Fatalf("full compiler metrics = %#v, fixed stage output metrics = %#v", got, stageGot)
			}
			fullValues, err := Run(full)
			if err != nil {
				t.Fatalf("full Compile result failed: %v", err)
			}
			stageValues, err := Run(stage)
			if err != nil {
				t.Fatalf("fixed stage result failed: %v", err)
			}
			if !equalCompilerCorpusValues(stageValues, fullValues) {
				t.Fatalf("fixed stage result = %#v, full Compile result = %#v", stageValues, fullValues)
			}
		})
	}
}

func TestCompileLexerAllocationBudgetsBySourceSize(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with the normal compiler/runtime instrumentation")
	}
	for _, test := range []struct {
		size      int
		maxAllocs float64
	}{
		{size: 1 << 10, maxAllocs: 1},
		{size: 4 << 10, maxAllocs: 1},
		{size: 16 << 10, maxAllocs: 2},
		{size: 64 << 10, maxAllocs: 7},
		{size: 256 << 10, maxAllocs: 13},
	} {
		t.Run(compilerStageBucketName(test.size), func(t *testing.T) {
			source := compilerStageSource(test.size)
			allocs := testing.AllocsPerRun(20, func() {
				lexed, err := lexSourceForCompile(source)
				if err != nil {
					t.Fatalf("lexSourceForCompile returned error: %v", err)
				}
				compilerStageTokensSink = lexed.tokens
			})
			if allocs > test.maxAllocs {
				t.Fatalf("lexing %d-byte source used %.0f allocs/op, want at most %.0f", test.size, allocs, test.maxAllocs)
			}
		})
	}
}

func TestAnalyzerPreservesCompileLexicalErrorByteRanges(t *testing.T) {
	for _, source := range []string{
		"--!strict\nreturn \"bad\\q\"",
		"--!strict\nreturn \"bad",
		"--!strict\n--[[",
	} {
		t.Run(strings.ReplaceAll(source, "\n", "/"), func(t *testing.T) {
			_, compileErr := Compile(source)
			if compileErr == nil {
				t.Fatal("Compile succeeded, want lexical error")
			}
			analyzerErr := func() error {
				_, err := NewAnalyzer().Check(context.Background(), Source{Text: source})
				return err
			}()
			if analyzerErr == nil {
				t.Fatal("Analyzer.Check succeeded, want lexical error")
			}
			if analyzerErr.Error() != compileErr.Error() {
				t.Fatalf("Analyzer.Check error = %q, Compile error = %q", analyzerErr, compileErr)
			}
		})
	}
}

func TestInstructionRegisterEffectsSparseIDsHaveRegisterIndependentEffectCount(t *testing.T) {
	for _, register := range []int{2, 20_000} {
		ins := instruction{op: opGetIndex, a: register, b: 1, c: 2}
		got := collectInstructionRegistersForTest(ins, instructionRegisterWrite)
		want := []int{register}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("GET_INDEX write effects for r%d = %#v, want %#v", register, got, want)
		}
	}
}

func TestCompilerStagePeakRegistersCountsOpenWindowsWithinFrame(t *testing.T) {
	ir := []bytecodeIRInstruction{
		lowerInstructionToBytecodeIR(instruction{op: opCall, a: 3, b: 1, c: -3, d: -1}, sourceRange{}),
	}
	if got, want := compilerStagePeakRegisters(ir, 8), 8; got != want {
		t.Fatalf("stage peak registers = %d, want open call frame bound %d", got, want)
	}
}

func compilerStageBucketName(size int) string {
	return strconv.Itoa(size/1024) + "KiB"
}
