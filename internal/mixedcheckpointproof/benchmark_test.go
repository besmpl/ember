package mixedcheckpointproof

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"testing"

	ruby "github.com/besmpl/ember/internal/rubyproof/generated"
	sprig "github.com/besmpl/ember/internal/sprigproof/generated"
)

var (
	benchmarkResult    Result
	benchmarkState     Checkpoint
	benchmarkEffect    Effect
	benchmarkHasEffect bool
	benchmarkError     error
)

var benchmarkObserveRequest = Request{
	Reading: sprig.Reading{Value: 0, Divisor: 1},
}

func BenchmarkMixedHandlerObserve(b *testing.B) {
	handler, err := NewHandler(context.Background(), InitialCheckpoint(), ProofPolicy())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		b.StopTimer()
		if err := handler.Close(); err != nil {
			b.Errorf("close handler: %v", err)
		}
	})
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		benchmarkResult, benchmarkState, benchmarkEffect, benchmarkHasEffect, benchmarkError = handler.Apply(ctx, benchmarkObserveRequest)
	}
	b.StopTimer()
	if benchmarkError != nil || !benchmarkHasEffect {
		b.Fatalf("Apply = effect %v, error %v", benchmarkHasEffect, benchmarkError)
	}
}

func BenchmarkMixedDirectCompositionObserve(b *testing.B) {
	composition, err := newDirectComposition(InitialCheckpoint(), ProofPolicy())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		b.StopTimer()
		if err := composition.close(); err != nil {
			b.Errorf("close direct composition: %v", err)
		}
	})
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		benchmarkResult, benchmarkState, benchmarkEffect, benchmarkHasEffect, benchmarkError = composition.apply(ctx, benchmarkObserveRequest)
	}
	b.StopTimer()
	if benchmarkError != nil || !benchmarkHasEffect {
		b.Fatalf("apply = effect %v, error %v", benchmarkHasEffect, benchmarkError)
	}
}

func TestMixedHandlerAllocationContract(t *testing.T) {
	handlerObserve := mustHandler(t, InitialCheckpoint(), ProofPolicy())
	handlerObserveAllocations := testing.AllocsPerRun(100, func() {
		benchmarkResult, benchmarkState, benchmarkEffect, benchmarkHasEffect, benchmarkError = handlerObserve.Apply(context.Background(), benchmarkObserveRequest)
		if benchmarkError != nil || !benchmarkHasEffect {
			panic(errors.Join(benchmarkError, errors.New("missing effect")))
		}
	})

	directObserve := mustDirectComposition(t, InitialCheckpoint(), ProofPolicy())
	directObserveAllocations := testing.AllocsPerRun(100, func() {
		benchmarkResult, benchmarkState, benchmarkEffect, benchmarkHasEffect, benchmarkError = directObserve.apply(context.Background(), benchmarkObserveRequest)
		if benchmarkError != nil || !benchmarkHasEffect {
			panic(errors.Join(benchmarkError, errors.New("missing effect")))
		}
	})

	advanceRequest := Request{Reading: sprig.Reading{Value: 2, Divisor: 1}, AdvanceRuby: true}
	handlerAdvance := mustHandler(t, InitialCheckpoint(), ProofPolicy())
	handlerAdvanceAllocations := testing.AllocsPerRun(1, func() {
		benchmarkResult, benchmarkState, benchmarkEffect, benchmarkHasEffect, benchmarkError = handlerAdvance.Apply(context.Background(), advanceRequest)
		if benchmarkError != nil || !benchmarkHasEffect {
			panic(errors.Join(benchmarkError, errors.New("missing effect")))
		}
	})

	directAdvance := mustDirectComposition(t, InitialCheckpoint(), ProofPolicy())
	directAdvanceAllocations := testing.AllocsPerRun(1, func() {
		benchmarkResult, benchmarkState, benchmarkEffect, benchmarkHasEffect, benchmarkError = directAdvance.apply(context.Background(), advanceRequest)
		if benchmarkError != nil || !benchmarkHasEffect {
			panic(errors.Join(benchmarkError, errors.New("missing effect")))
		}
	})

	if handlerObserveAllocations != 0 || directObserveAllocations != 0 ||
		handlerAdvanceAllocations != 0 || directAdvanceAllocations != 0 {
		t.Fatalf("allocations handler observe/advance = %.0f/%.0f, direct observe/advance = %.0f/%.0f, want 0/0 and 0/0",
			handlerObserveAllocations, handlerAdvanceAllocations, directObserveAllocations, directAdvanceAllocations)
	}
	t.Logf("allocations handler observe/advance = %.0f/%.0f, direct observe/advance = %.0f/%.0f",
		handlerObserveAllocations, handlerAdvanceAllocations, directObserveAllocations, directAdvanceAllocations)
}

func TestDirectCompositionMatchesHandlerTransactions(t *testing.T) {
	handler := mustHandler(t, InitialCheckpoint(), ProofPolicy())
	direct := mustDirectComposition(t, InitialCheckpoint(), ProofPolicy())
	requests := []Request{
		benchmarkObserveRequest,
		{Reading: sprig.Reading{Value: 10, Divisor: 2}, AdvanceRuby: true},
		{Reading: sprig.Reading{Value: 99, Divisor: 0}, AdvanceRuby: true},
		{Reading: sprig.Reading{Value: 8, Divisor: 2}},
	}
	for index, request := range requests {
		handlerResult, handlerState, handlerEffect, handlerHasEffect, handlerErr := handler.Apply(context.Background(), request)
		directResult, directState, directEffect, directHasEffect, directErr := direct.apply(context.Background(), request)
		if handlerResult != directResult || handlerState != directState || handlerEffect != directEffect ||
			handlerHasEffect != directHasEffect || !errors.Is(directErr, handlerErr) {
			t.Fatalf("transaction %d differs: handler=(%#v, %#v, %#v, %v, %v), direct=(%#v, %#v, %#v, %v, %v)",
				index, handlerResult, handlerState, handlerEffect, handlerHasEffect, handlerErr,
				directResult, directState, directEffect, directHasEffect, directErr)
		}
	}
}

func TestOptInMixedCheckpointPerformanceGate(t *testing.T) {
	if os.Getenv("EMBER_MIXED_CHECKPOINT_PERF") == "" {
		t.Skip("set EMBER_MIXED_CHECKPOINT_PERF=1 for the rotated mixed-checkpoint performance gate")
	}
	if os.Getenv("CGO_ENABLED") != "0" || os.Getenv("GOMAXPROCS") != "1" {
		t.Fatal("performance gate requires CGO_ENABLED=0 GOMAXPROCS=1")
	}
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Fatalf("performance gate requires runtime GOMAXPROCS 1, got %d", got)
	}

	const repeats = 7
	ratios := make([]float64, repeats)
	for index := 0; index < repeats; index++ {
		var handler, direct float64
		var order string
		if index%2 == 0 {
			order = "H/D"
			handler = benchmarkNanosecondsPerOperation(BenchmarkMixedHandlerObserve)
			direct = benchmarkNanosecondsPerOperation(BenchmarkMixedDirectCompositionObserve)
		} else {
			order = "D/H"
			direct = benchmarkNanosecondsPerOperation(BenchmarkMixedDirectCompositionObserve)
			handler = benchmarkNanosecondsPerOperation(BenchmarkMixedHandlerObserve)
		}
		if direct == 0 {
			t.Fatalf("round %d direct composition measured 0 ns/op", index+1)
		}
		ratios[index] = handler / direct
		t.Logf("round %d order=%s ns/op handler/direct=%.0f/%.0f ratio=%.3fx", index+1, order, handler, direct, ratios[index])
	}
	sort.Float64s(ratios)
	ratio := ratios[repeats/2]
	t.Logf("median handler/direct-composition ratio: %.3fx", ratio)
	if ratio > 1.05 {
		t.Fatalf("handler/direct-composition %.3fx > 1.05x", ratio)
	}
}

func benchmarkNanosecondsPerOperation(benchmark func(*testing.B)) float64 {
	result := testing.Benchmark(benchmark)
	return float64(result.T.Nanoseconds()) / float64(result.N)
}

// directComposition performs the same application transaction as Handler.
// It deliberately omits only Handler's lifecycle admission and poisoning so
// that the performance gate measures the cost of that dispatch.
type directComposition struct {
	ruby       *ruby.Engine
	checkpoint Checkpoint
	policy     Policy
}

func newDirectComposition(checkpoint Checkpoint, policy Policy) (*directComposition, error) {
	if checkpoint.Version != CheckpointVersion {
		return nil, fmt.Errorf("%w: version %d", ErrCheckpoint, checkpoint.Version)
	}
	engine, err := ruby.NewEngineFromState(checkpoint.Ruby)
	if err != nil {
		return nil, fmt.Errorf("%w: Ruby state: %v", ErrCheckpoint, err)
	}
	return &directComposition{ruby: engine, checkpoint: checkpoint, policy: policy}, nil
}

func (composition *directComposition) apply(ctx context.Context, request Request) (Result, Checkpoint, Effect, bool, error) {
	readings := [...]sprig.Reading{
		{Value: composition.checkpoint.SprigTotal, Divisor: 1},
		request.Reading,
	}
	sprigResult, err := sprig.Reduce(ctx, readings[:], composition.policy.SprigSteps)
	if err != nil {
		return Result{}, Checkpoint{}, Effect{}, false, err
	}
	if sprigResult.Tag == sprig.ResultError {
		result := composition.result()
		result.Domain = sprigResult.Code
		return result, composition.checkpoint, Effect{}, false, nil
	}
	if len(sprigResult.Rejected) != 0 {
		if len(sprigResult.Rejected) != 1 {
			return Result{}, Checkpoint{}, Effect{}, false, fmt.Errorf("mixed checkpoint proof: unexpected Sprig rejection count %d", len(sprigResult.Rejected))
		}
		result := composition.result()
		result.Rejected = true
		result.RejectionCode = sprigResult.Rejected[0]
		return result, composition.checkpoint, Effect{}, false, nil
	}

	rubyState := composition.checkpoint.Ruby
	if request.AdvanceRuby {
		rubyState, err = composition.ruby.ApplyRescuedRaise(ctx, composition.policy.Ruby)
		if err != nil {
			return Result{}, Checkpoint{}, Effect{}, false, err
		}
	} else {
		value, hotErr := composition.ruby.Hot(ctx)
		if hotErr != nil {
			return Result{}, Checkpoint{}, Effect{}, false, hotErr
		}
		if value != rubyState.Value {
			return Result{}, Checkpoint{}, Effect{}, false, errors.New("mixed checkpoint proof: Ruby state projection differs from owner")
		}
	}
	composition.checkpoint = Checkpoint{
		Version:    CheckpointVersion,
		Ruby:       rubyState,
		SprigTotal: sprigResult.Total,
	}
	result := composition.result()
	effect := Effect{RubyValue: rubyState.Value, RubyTrace: rubyState.Trace, SprigTotal: sprigResult.Total}
	return result, composition.checkpoint, effect, true, nil
}

func (composition *directComposition) result() Result {
	return Result{Ruby: composition.checkpoint.Ruby, SprigTotal: composition.checkpoint.SprigTotal}
}

func (composition *directComposition) close() error {
	if composition == nil || composition.ruby == nil {
		return nil
	}
	if err := composition.ruby.Close(); err != nil {
		return err
	}
	composition.ruby = nil
	composition.checkpoint = Checkpoint{}
	return nil
}

func mustDirectComposition(t *testing.T, checkpoint Checkpoint, policy Policy) *directComposition {
	t.Helper()
	composition, err := newDirectComposition(checkpoint, policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := composition.close(); err != nil {
			t.Errorf("close direct composition: %v", err)
		}
	})
	return composition
}
