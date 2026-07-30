package rubyproof

import (
	"context"
	"os"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/besmpl/ember/internal/rubyproof/generated"
)

var rubyHotSink int64

type canonicalHotLane struct {
	runtime  *Runtime
	receiver *rubyObject
	selector selectorID
}

func newCanonicalHotLane(b testing.TB) canonicalHotLane {
	b.Helper()
	program := proofProgram(b)
	runtime := newRuntime(program.checked)
	lookup, err := newLookupOwner(program.checked.lookup)
	if err != nil {
		b.Fatal(err)
	}
	runtime.state = runtimeState{
		lookup:     lookup,
		topMethods: make([]*checkedMethod, len(program.checked.topMethodNames)),
		top:        make(map[bindingID]rubyValue),
	}
	class := program.checked.classes["Alpha"]
	for _, member := range program.checked.module.statements[0].body {
		operation := program.checked.classOperations[member]
		mutation, err := program.checked.lookup.mutation(operation.id)
		if err != nil {
			b.Fatal(err)
		}
		if err := lookup.apply(context.Background(), mutation); err != nil {
			b.Fatal(err)
		}
	}
	return canonicalHotLane{
		runtime:  runtime,
		selector: program.checked.selectors["hot"],
		receiver: &rubyObject{id: 1, class: class, dispatch: class.dispatch, shape: class.shape, fields: map[string]rubyValue{
			"@value": {kind: rubyInteger, integer: 4},
			"@trace": {kind: rubyInteger, integer: 0},
		}},
	}
}

func (lane canonicalHotLane) hot(ctx context.Context) (int64, error) {
	runtime := lane.runtime
	if runtime.closed.Load() {
		return 0, ErrClosed
	}
	if runtime.poisoned.Load() {
		return 0, ErrPoisoned
	}
	if !runtime.active.CompareAndSwap(false, true) {
		return 0, ErrBusy
	}
	defer runtime.active.Store(false)
	if runtime.closed.Load() {
		return 0, ErrClosed
	}
	if runtime.poisoned.Load() {
		return 0, ErrPoisoned
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	runtime.state.control = executionControl{ctx: ctx, remainingSteps: 8, maximumFrames: 4}
	result, err := runtime.send(lane.receiver, lane.selector, nil, nil, checkedCallAnyVisibility)
	if err != nil {
		return 0, err
	}
	if result.flow != flowNormal || result.value.kind != rubyInteger {
		return 0, &internalRuntimeError{message: "canonical hot result"}
	}
	return result.value.integer, nil
}

type handwrittenHotLane struct {
	active, closed, poisoned atomic.Bool
	dispatch, shape          uint32
	value                    int64
}

func newHandwrittenHotLane() *handwrittenHotLane {
	return &handwrittenHotLane{dispatch: 1, shape: 1, value: 4}
}

func (lane *handwrittenHotLane) enter(ctx context.Context) error {
	if ctx == nil {
		return generated.ErrNilContext
	}
	if lane.closed.Load() {
		return generated.ErrClosed
	}
	if lane.poisoned.Load() {
		return generated.ErrPoisoned
	}
	if !lane.active.CompareAndSwap(false, true) {
		return generated.ErrBusy
	}
	if lane.closed.Load() {
		lane.active.Store(false)
		return generated.ErrClosed
	}
	if lane.poisoned.Load() {
		lane.active.Store(false)
		return generated.ErrPoisoned
	}
	if err := ctx.Err(); err != nil {
		lane.poisoned.Store(true)
		lane.active.Store(false)
		return err
	}
	return nil
}

func (lane *handwrittenHotLane) hot(ctx context.Context) (int64, error) {
	if err := lane.enter(ctx); err != nil {
		return 0, err
	}
	defer lane.active.Store(false)
	if lane.dispatch != 1 || lane.shape != 1 {
		lane.poisoned.Store(true)
		return 0, generated.ErrInternal
	}
	return lane.value, nil
}

func BenchmarkRubyCanonicalHotSend(b *testing.B) {
	lane := newCanonicalHotLane(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		value, err := lane.hot(ctx)
		if err != nil {
			b.Fatal(err)
		}
		rubyHotSink = value
	}
}

func BenchmarkRubyPreparedHotSend(b *testing.B) {
	lane := generated.NewEngine()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		value, err := lane.Hot(ctx)
		if err != nil {
			b.Fatal(err)
		}
		rubyHotSink = value
	}
}

func BenchmarkRubyHandwrittenHotSend(b *testing.B) {
	lane := newHandwrittenHotLane()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		value, err := lane.hot(ctx)
		if err != nil {
			b.Fatal(err)
		}
		rubyHotSink = value
	}
}

func TestRubyHotPathAllocationContract(t *testing.T) {
	canonicalLane := newCanonicalHotLane(t)
	preparedLane := generated.NewEngine()
	handwrittenLane := newHandwrittenHotLane()
	ctx := context.Background()
	canonical := testing.AllocsPerRun(100, func() {
		value, err := canonicalLane.hot(ctx)
		if err != nil {
			panic(err)
		}
		rubyHotSink = value
	})
	prepared := testing.AllocsPerRun(100, func() {
		value, err := preparedLane.Hot(ctx)
		if err != nil {
			panic(err)
		}
		rubyHotSink = value
	})
	handwritten := testing.AllocsPerRun(100, func() {
		value, err := handwrittenLane.hot(ctx)
		if err != nil {
			panic(err)
		}
		rubyHotSink = value
	})
	preparedRunLane := generated.NewEngine()
	preparedRun := testing.AllocsPerRun(100, func() {
		result, err := preparedRunLane.Run(ctx, generated.ProofLimits())
		if err != nil {
			panic(err)
		}
		rubyHotSink = result.Trace
	})
	if canonical < 1 || prepared != 0 || handwritten != 0 || preparedRun != 0 {
		t.Fatalf("allocations canonical/prepared/handwritten/prepared-run = %.0f/%.0f/%.0f/%.0f, want >=1/0/0/0", canonical, prepared, handwritten, preparedRun)
	}
	t.Logf("allocations canonical/prepared/handwritten/prepared-run = %.0f/%.0f/%.0f/%.0f", canonical, prepared, handwritten, preparedRun)
}

// TestOptInRubyPreparedDirectHotCalibration keeps the older monomorphic Hot
// calibration attributable. It does not measure the D1 polymorphic label site
// and therefore cannot promote D1 or open-world Ruby performance claims.
func TestOptInRubyPreparedDirectHotCalibration(t *testing.T) {
	if os.Getenv("EMBER_RUBY_PERF") == "" {
		t.Skip("set EMBER_RUBY_PERF=1 for the rotated Ruby direct-Hot calibration")
	}
	if os.Getenv("CGO_ENABLED") != "0" || os.Getenv("GOMAXPROCS") != "1" {
		t.Fatal("performance gate requires CGO_ENABLED=0 GOMAXPROCS=1")
	}
	const repeats = 7
	speedups := make([]float64, repeats)
	overheads := make([]float64, repeats)
	for index := 0; index < repeats; index++ {
		var canonical, prepared, handwritten float64
		var order string
		switch index % 3 {
		case 0:
			order = "c/p/h"
			canonical = float64(testing.Benchmark(BenchmarkRubyCanonicalHotSend).NsPerOp())
			prepared = float64(testing.Benchmark(BenchmarkRubyPreparedHotSend).NsPerOp())
			handwritten = float64(testing.Benchmark(BenchmarkRubyHandwrittenHotSend).NsPerOp())
		case 1:
			order = "p/h/c"
			prepared = float64(testing.Benchmark(BenchmarkRubyPreparedHotSend).NsPerOp())
			handwritten = float64(testing.Benchmark(BenchmarkRubyHandwrittenHotSend).NsPerOp())
			canonical = float64(testing.Benchmark(BenchmarkRubyCanonicalHotSend).NsPerOp())
		case 2:
			order = "h/c/p"
			handwritten = float64(testing.Benchmark(BenchmarkRubyHandwrittenHotSend).NsPerOp())
			canonical = float64(testing.Benchmark(BenchmarkRubyCanonicalHotSend).NsPerOp())
			prepared = float64(testing.Benchmark(BenchmarkRubyPreparedHotSend).NsPerOp())
		}
		speedups[index] = canonical / prepared
		overheads[index] = prepared / handwritten
		t.Logf("round %d order=%s ns/op c/p/h=%.0f/%.0f/%.0f ratios=%.2fx/%.3fx", index+1, order, canonical, prepared, handwritten, speedups[index], overheads[index])
	}
	sort.Float64s(speedups)
	sort.Float64s(overheads)
	speedup, overhead := speedups[repeats/2], overheads[repeats/2]
	t.Logf("median paired ratios: canonical/prepared %.2fx; prepared/handwritten %.3fx", speedup, overhead)
	if speedup < 2 {
		t.Fatalf("prepared speedup %.2fx < 2x", speedup)
	}
	if overhead > 1.15 {
		t.Fatalf("prepared/handwritten %.3fx > 1.15x", overhead)
	}
}
