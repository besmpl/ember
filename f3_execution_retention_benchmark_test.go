package ember

import (
	"context"
	"fmt"
	"go/ast"
	parserpkg "go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// f3ExecutionBenchmarkCase is deliberately small and mechanism-oriented. The
// full Scenario/Top10 corpus remains in the external compatibility package;
// these internal fixtures make every private engine directly measurable.
type f3ExecutionBenchmarkCase struct {
	name   string
	family string
	source string
	want   float64
}

var f3ExecutionCases = []f3ExecutionBenchmarkCase{
	{
		name:   "top10_arithmetic_for",
		family: "top10",
		source: `local total = 0 for i = 1, 200 do total = total + ((i * 3 - i // 2) % 17) end return total`,
		want:   1595,
	},
	{
		name:   "top10_while_branching",
		family: "top10",
		source: `local i = 0 local total = 0 while i < 250 do i = i + 1 if i % 5 == 0 then total = total + i // 5 elseif i % 2 == 0 then total = total + 2 else total = total + 1 end end return total`,
		want:   1575,
	},
	{
		name:   "classic_iterative_fibonacci",
		family: "classic",
		source: `local a = 0 local b = 1 for i = 1, 30 do local next = a + b a = b b = next end return a`,
		want:   832040,
	},
	{
		name:   "top10_recursive_fibonacci",
		family: "top10",
		source: `local function fib(n) if n < 2 then return n end return fib(n - 1) + fib(n - 2) end return fib(20)`,
		want:   6765,
	},
	{
		name:   "top10_fixed_call_chain",
		family: "top10",
		source: `local function add(a, b) return a + b end return add(add(1, 2), add(3, 4))`,
		want:   10,
	},
	{
		name:   "scenario_combat_tick",
		family: "scenario",
		source: `local rows = {{hp = 120, shield = 12, damage = 13}, {hp = 95, shield = 24, damage = 8}, {hp = 160, shield = 5, damage = 17}} local score = 0 for tick = 1, 30 do for _, row in rows do local incoming = row.damage + tick % 5 if row.shield > 0 then local absorbed = math.min(row.shield, incoming) row.shield = row.shield - absorbed incoming = incoming - absorbed end row.hp = row.hp - incoming if row.hp > 0 then score = score + row.hp + row.shield end end end return score`,
		want:   1806,
	},
	{
		name:   "scenario_inventory_value",
		family: "scenario",
		source: `local inventory = {{count = 12, value = 5, rarity = 1}, {count = 3, value = 40, rarity = 4}, {count = 7, value = 13, rarity = 2}, {count = 1, value = 100, rarity = 5}} local score = 0 for day = 1, 40 do for _, item in inventory do local bonus = item.rarity * (day % 4 + 1) score = score + item.count * item.value + bonus end end return score`,
		want:   16040,
	},
}

var f3ExecutionResultsSink []Value

// f3ScenarioCase is loaded from the canonical 25-row Scenario corpus in
// top10_luau_benchmark_test.go. Keeping the loader test-only avoids creating a
// second source corpus that could silently drift from the compatibility tests.
type f3ScenarioCase struct {
	name   string
	source string
	want   string
}

func loadF3ScenarioCases(tb testing.TB) []f3ScenarioCase {
	tb.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("runtime.Caller failed while locating the Scenario corpus")
	}
	corpusPath := filepath.Join(filepath.Dir(currentFile), "top10_luau_benchmark_test.go")
	source, err := os.ReadFile(corpusPath)
	if err != nil {
		tb.Fatalf("read Scenario corpus %s: %v", corpusPath, err)
	}
	file, err := parserpkg.ParseFile(token.NewFileSet(), corpusPath, source, 0)
	if err != nil {
		tb.Fatalf("parse Scenario corpus %s: %v", corpusPath, err)
	}

	var scenarioLiteral *ast.CompositeLit
	ast.Inspect(file, func(node ast.Node) bool {
		decl, ok := node.(*ast.GenDecl)
		if !ok || decl.Tok.String() != "var" {
			return true
		}
		for _, spec := range decl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range valueSpec.Names {
				if name.Name != "scenarioLuauCases" || index >= len(valueSpec.Values) {
					continue
				}
				candidate, ok := valueSpec.Values[index].(*ast.CompositeLit)
				if ok {
					scenarioLiteral = candidate
				}
			}
		}
		return false
	})
	if scenarioLiteral == nil {
		tb.Fatalf("scenarioLuauCases declaration not found in %s", corpusPath)
	}

	cases := make([]f3ScenarioCase, 0, len(scenarioLiteral.Elts))
	for index, element := range scenarioLiteral.Elts {
		entry, ok := element.(*ast.CompositeLit)
		if !ok {
			tb.Fatalf("scenarioLuauCases[%d] is %T, want composite literal", index, element)
		}
		values := make(map[string]string, 3)
		for _, field := range entry.Elts {
			keyValue, ok := field.(*ast.KeyValueExpr)
			if !ok {
				tb.Fatalf("scenarioLuauCases[%d] field is %T, want keyed field", index, field)
			}
			key, ok := keyValue.Key.(*ast.Ident)
			if !ok {
				tb.Fatalf("scenarioLuauCases[%d] key is %T, want identifier", index, keyValue.Key)
			}
			value, err := f3StringLiteral(keyValue.Value)
			if err != nil {
				tb.Fatalf("scenarioLuauCases[%d].%s: %v", index, key.Name, err)
			}
			values[key.Name] = value
		}
		for _, field := range []string{"name", "source", "want"} {
			if values[field] == "" {
				tb.Fatalf("scenarioLuauCases[%d] missing %s", index, field)
			}
		}
		cases = append(cases, f3ScenarioCase{name: values["name"], source: values["source"], want: values["want"]})
	}
	return cases
}

func f3StringLiteral(expr ast.Expr) (string, error) {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", fmt.Errorf("want string literal, got %T", expr)
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", fmt.Errorf("unquote %q: %w", literal.Value, err)
	}
	return value, nil
}

func BenchmarkF3ExecutionPaths(b *testing.B) {
	for _, tc := range f3ExecutionCases {
		b.Run(tc.name, func(b *testing.B) {
			proto, err := Compile(tc.source)
			if err != nil {
				b.Fatal(err)
			}
			for _, path := range f3ExecutionPaths(proto) {
				path := path
				b.Run(path.name, func(b *testing.B) {
					candidate := path.proto
					values, err := executeF3Path(candidate, path.mode, nil)
					if err != nil {
						b.Fatal(err)
					}
					validateF3Scalar(b, values, tc.want)
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						values, err := executeF3Path(candidate, path.mode, nil)
						if err != nil {
							b.Fatal(err)
						}
						f3ExecutionResultsSink = values
					}
				})
			}
		})
	}
}

// TestF3ExecutionPathCoverage records eligibility and dynamic instruction
// coverage separately from timing. It is intentionally informational: the ADR
// applies the plan's retention gates to the complete Scenario probe, while
// this test keeps the private mode harness reproducible in the internal
// package.
func TestF3ExecutionPathCoverage(t *testing.T) {
	seen := map[executionMode]bool{}
	for _, tc := range f3ExecutionCases {
		proto, err := Compile(tc.source)
		if err != nil {
			t.Fatalf("%s: Compile: %v", tc.name, err)
		}
		directStats := &executionPathStats{}
		values, err := executeF3Path(proto, executionModeDirect, directStats)
		if err != nil {
			t.Fatalf("%s: direct: %v", tc.name, err)
		}
		validateF3Scalar(t, values, tc.want)
		directInstructions := directStats.directProductionInstructions + directStats.loopKernelInstructions + directStats.coldInstructions
		if directInstructions == 0 {
			t.Fatalf("%s: direct path recorded no instructions", tc.name)
		}
		t.Logf("%s family=%s direct=%d kernel=%d slotEligible=%t numericEligible=%t compact=%t", tc.name, tc.family, directInstructions, directStats.loopKernelInstructions, proto.slotExecutionEligible, proto.slotExecutionNumeric, proto.compact != nil)
		for _, path := range f3ExecutionPaths(proto) {
			if path.mode == executionModeDirect {
				continue
			}
			stats := &executionPathStats{}
			values, err := executeF3Path(path.proto, path.mode, stats)
			if err != nil {
				t.Fatalf("%s/%s: %v", tc.name, path.name, err)
			}
			validateF3Scalar(t, values, tc.want)
			count := f3ModeInstructionCount(stats, path.mode)
			seen[path.mode] = true
			t.Logf("%s/%s instructions=%d coverage=%.2f%% allocs=measured-by-benchmark", tc.name, path.name, count, 100*float64(count)/float64(directInstructions))
		}
	}
	for _, mode := range []executionMode{executionModeSlot, executionModeNumericSlot, executionModeCompactCall} {
		if !seen[mode] {
			t.Errorf("F3 corpus did not execute %s", executionModeName(mode))
		}
	}
}

// TestF3ScenarioPathCoverage is the full-corpus evidence gate. The direct
// production path is compared with auto mode for every Scenario row, while
// eligibility and direct-loop-kernel counters establish the denominator used
// by ADR 0005 without relying on benchmark timing.
func TestF3ScenarioPathCoverage(t *testing.T) {
	cases := loadF3ScenarioCases(t)
	if len(cases) != 25 {
		t.Fatalf("Scenario corpus has %d rows, want 25", len(cases))
	}
	alternateEligible := 0
	kernelTargets := 0
	var totalInstructions, kernelInstructions uint64
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proto, err := Compile(tc.source)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			want, err := strconv.ParseFloat(tc.want, 64)
			if err != nil {
				t.Fatalf("parse expected result %q: %v", tc.want, err)
			}
			directStats := &executionPathStats{}
			directValues, directErr := executeF3Path(proto, executionModeDirect, directStats)
			autoStats := &executionPathStats{}
			autoValues, autoErr := executeF3Path(proto, executionModeAuto, autoStats)
			if difference := errorsEquivalent(directErr, autoErr); difference != "" {
				t.Fatalf("direct/auto errors differ: %s (direct=%v auto=%v)", difference, directErr, autoErr)
			}
			if !valuesSliceEquivalent(directValues, autoValues) {
				t.Fatalf("direct/auto results differ: direct=%s auto=%s", valuesDiagnostic(directValues), valuesDiagnostic(autoValues))
			}
			if directErr != nil {
				t.Fatalf("direct execution failed: %v", directErr)
			}
			validateF3Scalar(t, directValues, want)

			eligible := eligibleDifferentialModes(proto)
			if len(eligible) != 0 {
				alternateEligible++
				t.Errorf("Scenario row is alternate-eligible: %v", eligible)
			}
			rowInstructions := directStats.directProductionInstructions + directStats.coldInstructions + directStats.loopKernelInstructions
			if rowInstructions == 0 {
				t.Fatalf("direct path recorded no instructions")
			}
			totalInstructions += rowInstructions
			kernelInstructions += directStats.loopKernelInstructions
			if proto.directLoopKernels != nil {
				kernelTargets++
				if directStats.loopKernelInstructions == 0 {
					t.Errorf("compiled kernel target did not execute a loop kernel")
				}
			}
		})
	}
	if alternateEligible != 0 {
		t.Fatalf("Scenario alternate-eligible rows = %d, want 0", alternateEligible)
	}
	if kernelTargets != 8 {
		t.Fatalf("Scenario corpus kernel-target rows = %d, want 8", kernelTargets)
	}
	if totalInstructions == 0 {
		t.Fatal("Scenario corpus recorded no direct instructions")
	}
	kernelPercent := 100 * float64(kernelInstructions) / float64(totalInstructions)
	t.Logf("Scenario rows=%d alternate-eligible=%d kernel-targets=%d dynamic-kernel=%.2f%% (%d/%d instructions)", len(cases), alternateEligible, kernelTargets, kernelPercent, kernelInstructions, totalInstructions)
}

// BenchmarkF3ScenarioPaths measures the full Scenario shape. The no-kernel
// clone is included only for rows that actually compile a direct loop kernel;
// benchmarking an identical clone for every other row would add noise without
// measuring a decision.
func BenchmarkF3ScenarioPaths(b *testing.B) {
	cases := loadF3ScenarioCases(b)
	for _, tc := range cases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			proto, err := Compile(tc.source)
			if err != nil {
				b.Fatal(err)
			}
			want, err := strconv.ParseFloat(tc.want, 64)
			if err != nil {
				b.Fatal(err)
			}
			paths := []f3ExecutionPath{
				{name: "auto", proto: proto, mode: executionModeAuto},
				{name: "direct", proto: proto, mode: executionModeDirect},
			}
			if proto.directLoopKernels != nil {
				paths = append(paths, f3ExecutionPath{name: "direct_no_kernel", proto: cloneF3WithoutKernels(proto), mode: executionModeDirect})
			}
			for _, path := range paths {
				path := path
				b.Run(path.name, func(b *testing.B) {
					values, err := executeF3Path(path.proto, path.mode, nil)
					if err != nil {
						b.Fatal(err)
					}
					validateF3Scalar(b, values, want)
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						values, err := executeF3Path(path.proto, path.mode, nil)
						if err != nil {
							b.Fatal(err)
						}
						f3ExecutionResultsSink = values
					}
				})
			}
		})
	}
}

type f3ExecutionPath struct {
	name  string
	proto *Proto
	mode  executionMode
}

func f3ExecutionPaths(proto *Proto) []f3ExecutionPath {
	paths := []f3ExecutionPath{
		{name: "direct", proto: proto, mode: executionModeDirect},
		{name: "direct_no_kernel", proto: cloneF3WithoutKernels(proto), mode: executionModeDirect},
	}
	if proto.slotExecutionEligible {
		paths = append(paths, f3ExecutionPath{name: "slot", proto: proto, mode: executionModeSlot})
	}
	if proto.slotExecutionNumeric {
		paths = append(paths, f3ExecutionPath{name: "numeric_slot", proto: proto, mode: executionModeNumericSlot})
	}
	if proto.compact != nil {
		paths = append(paths, f3ExecutionPath{name: "compact_call", proto: proto, mode: executionModeCompactCall})
	}
	return paths
}

func executeF3Path(proto *Proto, mode executionMode, stats *executionPathStats) ([]Value, error) {
	return executeProto(context.Background(), proto, nil, executeOptions{mode: mode, stats: stats})
}

func cloneF3WithoutKernels(proto *Proto) *Proto {
	return cloneF3WithoutKernelsMemo(proto, make(map[*Proto]*Proto))
}

func cloneF3WithoutKernelsMemo(proto *Proto, memo map[*Proto]*Proto) *Proto {
	if proto == nil {
		return nil
	}
	if clone := memo[proto]; clone != nil {
		return clone
	}
	clone := *proto
	clone.directLoopKernels = nil
	clone.prototypes = make([]*Proto, len(proto.prototypes))
	memo[proto] = &clone
	for index, child := range proto.prototypes {
		clone.prototypes[index] = cloneF3WithoutKernelsMemo(child, memo)
	}
	return &clone
}

func f3ModeInstructionCount(stats *executionPathStats, mode executionMode) uint64 {
	if stats == nil {
		return 0
	}
	switch mode {
	case executionModeSlot:
		return stats.slotInstructions
	case executionModeNumericSlot:
		return stats.numericSlotInstructions
	case executionModeCompactCall:
		return stats.compactCallInstructions
	default:
		return 0
	}
}

func validateF3Scalar(tb testing.TB, values []Value, want float64) {
	tb.Helper()
	if len(values) != 1 {
		tb.Fatalf("returned %d values, want 1", len(values))
	}
	got, ok := values[0].Number()
	if !ok || math.Float64bits(got) != math.Float64bits(want) {
		tb.Fatalf("result = %v, want %v", values[0], want)
	}
}
