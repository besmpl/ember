package sprigproof_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"testing"

	"github.com/besmpl/ember/internal/sprigproof"
	"github.com/besmpl/ember/internal/sprigproof/generated"
	projectgenerated "github.com/besmpl/ember/internal/sprigproof/generatedproject/counter"
)

var benchmarkInput = []sprigproof.Reading{{Value: 81, Divisor: 9}, {Value: 64, Divisor: 8}, {Value: 49, Divisor: 7}, {Value: 36, Divisor: 6}, {Value: 25, Divisor: 5}, {Value: 16, Divisor: 4}, {Value: 9, Divisor: 3}, {Value: 4, Divisor: 2}}
var benchmarkGeneratedInput = func() []generated.Reading {
	out := make([]generated.Reading, len(benchmarkInput))
	for i, r := range benchmarkInput {
		out[i] = generated.Reading{Value: r.Value, Divisor: r.Divisor}
	}
	return out
}()
var benchmarkResult sprigproof.Result
var benchmarkGeneratedResult generated.Result
var benchmarkProjectInput = func() []projectgenerated.Reading {
	out := make([]projectgenerated.Reading, len(benchmarkInput))
	for i, r := range benchmarkInput {
		out[i] = projectgenerated.Reading{Value: r.Value, Divisor: r.Divisor}
	}
	return out
}()
var benchmarkProjectResult projectgenerated.Result

func BenchmarkEvaluator(b *testing.B) {
	program := proofProgram(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := program.Reduce(context.Background(), benchmarkInput, 9)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkResult = result
	}
	if benchmarkResult.Total != 44 {
		b.Fatalf("sink=%#v", benchmarkResult)
	}
}
func BenchmarkGenerated(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result, err := generated.Reduce(context.Background(), benchmarkGeneratedInput, 9)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkGeneratedResult = result
	}
	if benchmarkGeneratedResult.Total != 44 {
		b.Fatalf("sink=%#v", benchmarkGeneratedResult)
	}
}
func BenchmarkProjectEvaluator(b *testing.B) {
	program, diagnostics := sprigproof.CompileProject(sprigproof.ProofProjectSources())
	if len(diagnostics) != 0 {
		b.Fatal(diagnostics)
	}
	counter, _ := program.Program("counter")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := counter.Reduce(context.Background(), benchmarkInput, 9)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkResult = result
	}
	if benchmarkResult.Total != 44 {
		b.Fatal(benchmarkResult)
	}
}
func BenchmarkProjectGenerated(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result, err := projectgenerated.Reduce(context.Background(), benchmarkProjectInput, 9)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkProjectResult = result
	}
	if benchmarkProjectResult.Total != 44 {
		b.Fatal(benchmarkProjectResult)
	}
}
func BenchmarkHandwritten(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result, err := handwrittenReduce(context.Background(), benchmarkGeneratedInput, 9)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkGeneratedResult = result
	}
	if benchmarkGeneratedResult.Total != 44 {
		b.Fatalf("sink=%#v", benchmarkGeneratedResult)
	}
}

func TestDeterministicAllocationContract(t *testing.T) {
	program := proofProgram(t)
	evaluator := testing.AllocsPerRun(100, func() {
		result, err := program.Reduce(context.Background(), benchmarkInput, 9)
		if err != nil {
			panic(err)
		}
		benchmarkResult = result
	})
	native := testing.AllocsPerRun(100, func() {
		result, err := generated.Reduce(context.Background(), benchmarkGeneratedInput, 9)
		if err != nil {
			panic(err)
		}
		benchmarkGeneratedResult = result
	})
	handwritten := testing.AllocsPerRun(100, func() {
		result, err := handwrittenReduce(context.Background(), benchmarkGeneratedInput, 9)
		if err != nil {
			panic(err)
		}
		benchmarkGeneratedResult = result
	})
	if evaluator < 1 {
		t.Fatalf("evaluator allocations=%g, want at least one", evaluator)
	}
	if native != 0 || handwritten != 0 {
		t.Fatalf("allocations evaluator/generated/handwritten = %g/%g/%g, want generated and handwritten zero", evaluator, native, handwritten)
	}
	t.Logf("allocations evaluator/generated/handwritten = %.0f/%.0f/%.0f", evaluator, native, handwritten)
	rejectInput := []generated.Reading{{Value: 1, Divisor: 0}}
	nativeReject := testing.AllocsPerRun(100, func() {
		result, err := generated.Reduce(context.Background(), rejectInput, 2)
		if err != nil {
			panic(err)
		}
		benchmarkGeneratedResult = result
	})
	handReject := testing.AllocsPerRun(100, func() {
		result, err := handwrittenReduce(context.Background(), rejectInput, 2)
		if err != nil {
			panic(err)
		}
		benchmarkGeneratedResult = result
	})
	if nativeReject != 1 || handReject != 1 {
		t.Fatalf("rejection allocations generated/handwritten = %g/%g, want 1/1", nativeReject, handReject)
	}
}

func TestProjectDeterministicAllocationContract(t *testing.T) {
	project := proofProject(t)
	program, _ := project.Program("counter")
	evaluator := testing.AllocsPerRun(100, func() {
		result, err := program.Reduce(context.Background(), benchmarkInput, 9)
		if err != nil {
			panic(err)
		}
		benchmarkResult = result
	})
	native := testing.AllocsPerRun(100, func() {
		result, err := projectgenerated.Reduce(context.Background(), benchmarkProjectInput, 9)
		if err != nil {
			panic(err)
		}
		benchmarkProjectResult = result
	})
	if evaluator < 1 || native != 0 {
		t.Fatalf("project allocations evaluator/generated=%g/%g want >=1/0", evaluator, native)
	}
	reject := []projectgenerated.Reading{{Value: 1, Divisor: 0}}
	rejected := testing.AllocsPerRun(100, func() {
		result, err := projectgenerated.Reduce(context.Background(), reject, 2)
		if err != nil {
			panic(err)
		}
		benchmarkProjectResult = result
	})
	if rejected != 1 {
		t.Fatalf("project rejection allocations=%g want 1", rejected)
	}
}

func TestOptInProjectPerformanceGate(t *testing.T) {
	if os.Getenv("EMBER_SPRIG_PERF") == "" {
		t.Skip("set EMBER_SPRIG_PERF=1 for repeated performance gate")
	}
	if os.Getenv("CGO_ENABLED") != "0" || os.Getenv("GOMAXPROCS") != "1" {
		t.Fatal("performance gate requires CGO_ENABLED=0 GOMAXPROCS=1")
	}
	const repeats = 7
	speedups := make([]float64, repeats)
	overheads := make([]float64, repeats)
	for i := 0; i < repeats; i++ {
		var evaluator, native, handwritten float64
		switch i % 3 {
		case 0:
			evaluator = float64(testing.Benchmark(BenchmarkProjectEvaluator).NsPerOp())
			native = float64(testing.Benchmark(BenchmarkProjectGenerated).NsPerOp())
			handwritten = float64(testing.Benchmark(BenchmarkHandwritten).NsPerOp())
		case 1:
			native = float64(testing.Benchmark(BenchmarkProjectGenerated).NsPerOp())
			handwritten = float64(testing.Benchmark(BenchmarkHandwritten).NsPerOp())
			evaluator = float64(testing.Benchmark(BenchmarkProjectEvaluator).NsPerOp())
		case 2:
			handwritten = float64(testing.Benchmark(BenchmarkHandwritten).NsPerOp())
			evaluator = float64(testing.Benchmark(BenchmarkProjectEvaluator).NsPerOp())
			native = float64(testing.Benchmark(BenchmarkProjectGenerated).NsPerOp())
		}
		speedups[i] = evaluator / native
		overheads[i] = native / handwritten
		t.Logf("project round %d e/g/h=%.0f/%.0f/%.0f ratios=%.2fx/%.3fx", i+1, evaluator, native, handwritten, speedups[i], overheads[i])
	}
	sort.Float64s(speedups)
	sort.Float64s(overheads)
	speedup, overhead := speedups[repeats/2], overheads[repeats/2]
	if speedup < 2 {
		t.Fatalf("project generated speedup %.2fx < 2x", speedup)
	}
	if overhead > 1.10 {
		t.Fatalf("project generated/handwritten %.3fx > 1.10x", overhead)
	}
}

func TestOptInPerformanceGate(t *testing.T) {
	if os.Getenv("EMBER_SPRIG_PERF") == "" {
		t.Skip("set EMBER_SPRIG_PERF=1 for repeated performance gate")
	}
	if os.Getenv("CGO_ENABLED") != "0" || os.Getenv("GOMAXPROCS") != "1" {
		t.Fatal("performance gate requires CGO_ENABLED=0 GOMAXPROCS=1")
	}
	const repeats = 7
	speedups := make([]float64, repeats)
	overheads := make([]float64, repeats)
	for i := 0; i < repeats; i++ {
		var evaluator, native, handwritten float64
		var order string
		switch i % 3 {
		case 0:
			order = "e/g/h"
			evaluator = float64(testing.Benchmark(BenchmarkEvaluator).NsPerOp())
			native = float64(testing.Benchmark(BenchmarkGenerated).NsPerOp())
			handwritten = float64(testing.Benchmark(BenchmarkHandwritten).NsPerOp())
		case 1:
			order = "g/h/e"
			native = float64(testing.Benchmark(BenchmarkGenerated).NsPerOp())
			handwritten = float64(testing.Benchmark(BenchmarkHandwritten).NsPerOp())
			evaluator = float64(testing.Benchmark(BenchmarkEvaluator).NsPerOp())
		case 2:
			order = "h/e/g"
			handwritten = float64(testing.Benchmark(BenchmarkHandwritten).NsPerOp())
			evaluator = float64(testing.Benchmark(BenchmarkEvaluator).NsPerOp())
			native = float64(testing.Benchmark(BenchmarkGenerated).NsPerOp())
		}
		speedups[i] = evaluator / native
		overheads[i] = native / handwritten
		t.Logf("round %d order=%s ns/op e/g/h=%.0f/%.0f/%.0f ratios=%.2fx/%.3fx", i+1, order, evaluator, native, handwritten, speedups[i], overheads[i])
	}
	sort.Float64s(speedups)
	sort.Float64s(overheads)
	speedup, overhead := speedups[repeats/2], overheads[repeats/2]
	t.Logf("median paired ratios: generated speedup %.2fx; generated/handwritten %.3fx", speedup, overhead)
	if speedup < 2 {
		t.Fatalf("generated speedup %.2fx < 2x", speedup)
	}
	if overhead > 1.10 {
		t.Fatalf("generated/handwritten %.3fx > 1.10x", overhead)
	}
}

type handwrittenStep struct {
	tag      uint8
	by, code int64
}

func handwrittenPoll(ctx context.Context, remaining *uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if *remaining == 0 {
		return generated.ErrStepLimit
	}
	*remaining--
	return nil
}
func handwrittenReduce(ctx context.Context, readings []generated.Reading, limit uint64) (generated.Result, error) {
	if ctx == nil {
		return generated.Result{}, errors.New("sprig: nil context")
	}
	if err := handwrittenPoll(ctx, &limit); err != nil {
		return generated.Result{}, err
	}
	if len(readings) > 4096 {
		return generated.Result{}, fmt.Errorf("sprig: input length %d exceeds limit %d", len(readings), 4096)
	}
	var total int64
	var rejected []int64
	for _, reading := range readings {
		var step handwrittenStep
		if reading.Divisor == 0 {
			step = handwrittenStep{tag: 2, code: 7}
		} else {
			divisor := reading.Divisor
			if divisor == 0 {
				return generated.Result{Tag: generated.ResultError, Code: generated.DomainDivideByZero}, nil
			}
			if reading.Value == math.MinInt64 && divisor == -1 {
				return generated.Result{Tag: generated.ResultError, Code: generated.DomainOverflow}, nil
			}
			step = handwrittenStep{tag: 1, by: reading.Value / divisor}
		}
		switch step.tag {
		case 1:
			sum := total + step.by
			if step.by > 0 && sum < total || step.by < 0 && sum > total {
				return generated.Result{Tag: generated.ResultError, Code: generated.DomainOverflow}, nil
			}
			total = sum
		case 2:
			rejected = append(rejected, step.code)
		}
		if err := handwrittenPoll(ctx, &limit); err != nil {
			return generated.Result{}, err
		}
	}
	return generated.Result{Tag: generated.ResultOK, Total: total, Rejected: rejected}, nil
}
