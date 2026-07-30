package rubyproof

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/besmpl/ember/internal/rubyprojectionproof"
	rubyv2 "github.com/besmpl/ember/internal/rubyprojectionproof/generated/rubyv2"
)

var (
	projectionBenchmarkValue int64
	projectionBenchmarkError error
)

type explicitProjectionNode struct {
	edge   *explicitProjectionNode
	scalar int64
}

type explicitProjectionClosure struct {
	kind explicitProjectionBehavior
	self *explicitProjectionNode
}

type explicitProjectionBehavior uint8

const explicitProjectionCapturedSelf explicitProjectionBehavior = 1

// explicitProjectionOwner is the equivalent handwritten application owner.
// It matches the generated call-relevant graph, admission, cancellation, and
// lifetime work; it omits no dispatch by replacing the call with `return 49`.
type explicitProjectionOwner struct {
	active, closed     atomic.Bool
	nodes              [3]*explicitProjectionNode
	root, alias, other *explicitProjectionNode
	behavior           explicitProjectionClosure
}

func newExplicitProjectionOwner() *explicitProjectionOwner {
	owner := &explicitProjectionOwner{}
	owner.nodes[0] = &explicitProjectionNode{scalar: 40}
	owner.nodes[1] = &explicitProjectionNode{scalar: 40}
	owner.nodes[2] = &explicitProjectionNode{scalar: 7}
	owner.nodes[0].edge = owner.nodes[0]
	owner.nodes[1].edge = owner.nodes[2]
	owner.nodes[2].edge = owner.nodes[0]
	owner.root, owner.alias, owner.other = owner.nodes[0], owner.nodes[0], owner.nodes[1]
	owner.behavior = explicitProjectionClosure{kind: explicitProjectionCapturedSelf, self: owner.nodes[0]}
	return owner
}

func (owner *explicitProjectionOwner) enter(ctx context.Context) error {
	if ctx == nil {
		return rubyv2.ErrNilContext
	}
	if owner == nil || owner.closed.Load() {
		return rubyv2.ErrClosed
	}
	if !owner.active.CompareAndSwap(false, true) {
		return rubyv2.ErrBusy
	}
	if owner.closed.Load() {
		owner.active.Store(false)
		return rubyv2.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		owner.active.Store(false)
		return err
	}
	return nil
}

func (owner *explicitProjectionOwner) Call(ctx context.Context) (int64, error) {
	if err := owner.enter(ctx); err != nil {
		return 0, err
	}
	defer owner.active.Store(false)
	if owner.behavior.kind != explicitProjectionCapturedSelf || owner.behavior.self == nil {
		return 0, rubyv2.ErrNonCheckpointable
	}
	return owner.behavior.self.scalar + 9, nil
}

func (owner *explicitProjectionOwner) clear() {
	if owner == nil {
		return
	}
	owner.nodes = [3]*explicitProjectionNode{}
	owner.root, owner.alias, owner.other = nil, nil, nil
	owner.behavior = explicitProjectionClosure{}
}

func (owner *explicitProjectionOwner) Close() error {
	if owner == nil || owner.closed.Load() {
		return nil
	}
	if !owner.active.CompareAndSwap(false, true) {
		return rubyv2.ErrBusy
	}
	defer owner.active.Store(false)
	owner.closed.Store(true)
	owner.clear()
	return nil
}

func TestExplicitProjectionOwnerMatchesRestoredV2CallAndLifetime(t *testing.T) {
	ctx := context.Background()
	prepared, err := rubyprojectionproof.RestoreV2(ctx, rubyprojectionproof.InitialCheckpoint(), rubyv2.ProjectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	explicit := newExplicitProjectionOwner()

	checkpoint, err := rubyprojectionproof.SnapshotV2(ctx, prepared)
	if err != nil || checkpoint != rubyprojectionproof.InitialCheckpoint() {
		t.Fatalf("restored checkpoint = %#v, %v", checkpoint, err)
	}
	if explicit.root == nil || explicit.root != explicit.alias || explicit.root == explicit.other ||
		explicit.root.edge != explicit.root || explicit.other.edge != explicit.nodes[2] || explicit.nodes[2].edge != explicit.root ||
		explicit.behavior.self != explicit.root {
		t.Fatal("explicit comparator topology does not match the admitted restored graph")
	}
	preparedValue, preparedErr := prepared.Call(ctx)
	explicitValue, explicitErr := explicit.Call(ctx)
	if preparedValue != 49 || preparedValue != explicitValue || preparedErr != nil || explicitErr != nil {
		t.Fatalf("calls prepared/explicit = %d,%v / %d,%v", preparedValue, preparedErr, explicitValue, explicitErr)
	}
	if _, err := prepared.Call(nil); !errors.Is(err, rubyv2.ErrNilContext) {
		t.Fatalf("prepared nil context = %v", err)
	}
	if _, err := explicit.Call(nil); !errors.Is(err, rubyv2.ErrNilContext) {
		t.Fatalf("explicit nil context = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := prepared.Call(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("prepared canceled call = %v", err)
	}
	if _, err := explicit.Call(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("explicit canceled call = %v", err)
	}
	if value, err := prepared.Call(ctx); value != 49 || err != nil {
		t.Fatalf("prepared reuse after cancellation = %d, %v", value, err)
	}
	if value, err := explicit.Call(ctx); value != 49 || err != nil {
		t.Fatalf("explicit reuse after cancellation = %d, %v", value, err)
	}

	assertProjectionBusyClose(t, "prepared", prepared.Call, prepared.Close)
	assertProjectionBusyClose(t, "explicit", explicit.Call, explicit.Close)
	if err := prepared.Close(); err != nil {
		t.Fatalf("prepared close retry: %v", err)
	}
	if err := explicit.Close(); err != nil {
		t.Fatalf("explicit close retry: %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatalf("prepared idempotent close: %v", err)
	}
	if err := explicit.Close(); err != nil {
		t.Fatalf("explicit idempotent close: %v", err)
	}
	if _, err := prepared.Call(ctx); !errors.Is(err, rubyv2.ErrClosed) {
		t.Fatalf("prepared call after close = %v", err)
	}
	if _, err := explicit.Call(ctx); !errors.Is(err, rubyv2.ErrClosed) {
		t.Fatalf("explicit call after close = %v", err)
	}

	unknown := newExplicitProjectionOwner()
	unknown.behavior.kind = 99
	if _, err := unknown.Call(ctx); !errors.Is(err, rubyv2.ErrNonCheckpointable) {
		t.Fatalf("unknown explicit behavior = %v", err)
	}
	if err := unknown.Close(); err != nil {
		t.Fatal(err)
	}
	missing := newExplicitProjectionOwner()
	missing.behavior.self = nil
	if _, err := missing.Call(ctx); !errors.Is(err, rubyv2.ErrNonCheckpointable) {
		t.Fatalf("missing explicit capture = %v", err)
	}
	if err := missing.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertProjectionBusyClose(t *testing.T, name string, call func(context.Context) (int64, error), closeOwner func() error) {
	t.Helper()
	blocking := &projectionBlockingContext{
		Context: context.Background(),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		_, err := call(blocking)
		done <- err
	}()
	<-blocking.entered
	if err := closeOwner(); !errors.Is(err, rubyv2.ErrBusy) {
		t.Fatalf("%s busy close = %v", name, err)
	}
	close(blocking.release)
	if err := <-done; err != nil {
		t.Fatalf("%s blocked call = %v", name, err)
	}
}

func TestRubyRestoredV2ProjectionCallAllocationContract(t *testing.T) {
	prepared, err := rubyprojectionproof.RestoreV2(context.Background(), rubyprojectionproof.InitialCheckpoint(), rubyv2.ProjectionLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.Close(); err != nil {
			t.Errorf("close prepared: %v", err)
		}
	}()
	explicit := newExplicitProjectionOwner()
	defer func() {
		if err := explicit.Close(); err != nil {
			t.Errorf("close explicit: %v", err)
		}
	}()
	ctx := context.Background()
	preparedAllocations := testing.AllocsPerRun(1000, func() {
		projectionBenchmarkValue, projectionBenchmarkError = prepared.Call(ctx)
		if projectionBenchmarkError != nil {
			panic(projectionBenchmarkError)
		}
	})
	explicitAllocations := testing.AllocsPerRun(1000, func() {
		projectionBenchmarkValue, projectionBenchmarkError = explicit.Call(ctx)
		if projectionBenchmarkError != nil {
			panic(projectionBenchmarkError)
		}
	})
	if preparedAllocations != 0 || explicitAllocations != 0 {
		t.Fatalf("allocations prepared/explicit = %.0f/%.0f, want 0/0", preparedAllocations, explicitAllocations)
	}
}

func BenchmarkRubyRestoredV2ProjectionCall(b *testing.B) {
	owner, err := rubyprojectionproof.RestoreV2(context.Background(), rubyprojectionproof.InitialCheckpoint(), rubyv2.ProjectionLimits())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		projectionBenchmarkValue, projectionBenchmarkError = owner.Call(ctx)
	}
	b.StopTimer()
	if projectionBenchmarkError != nil || projectionBenchmarkValue != 49 {
		b.Fatalf("Call = %d, %v", projectionBenchmarkValue, projectionBenchmarkError)
	}
	if err := owner.Close(); err != nil {
		b.Fatal(err)
	}
}

func BenchmarkRubyExplicitV2ProjectionCall(b *testing.B) {
	owner := newExplicitProjectionOwner()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		projectionBenchmarkValue, projectionBenchmarkError = owner.Call(ctx)
	}
	b.StopTimer()
	if projectionBenchmarkError != nil || projectionBenchmarkValue != 49 {
		b.Fatalf("Call = %d, %v", projectionBenchmarkValue, projectionBenchmarkError)
	}
	if err := owner.Close(); err != nil {
		b.Fatal(err)
	}
}

func TestOptInRubyProjectionPerformanceGate(t *testing.T) {
	if os.Getenv("EMBER_RUBY_PROJECTION_PERF") == "" {
		t.Skip("set EMBER_RUBY_PROJECTION_PERF=1 for the rotated Ruby projection performance gate")
	}
	if os.Getenv("CGO_ENABLED") != "0" || os.Getenv("GOMAXPROCS") != "1" {
		t.Fatal("performance gate requires CGO_ENABLED=0 GOMAXPROCS=1")
	}
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Fatalf("performance gate requires runtime GOMAXPROCS 1, got %d", got)
	}

	const rounds = 7
	ratios := make([]float64, rounds)
	for index := 0; index < rounds; index++ {
		var prepared, explicit testing.BenchmarkResult
		var order string
		if index%2 == 0 {
			order = "P/E"
			prepared = testing.Benchmark(BenchmarkRubyRestoredV2ProjectionCall)
			explicit = testing.Benchmark(BenchmarkRubyExplicitV2ProjectionCall)
		} else {
			order = "E/P"
			explicit = testing.Benchmark(BenchmarkRubyExplicitV2ProjectionCall)
			prepared = testing.Benchmark(BenchmarkRubyRestoredV2ProjectionCall)
		}
		if prepared.AllocsPerOp() != 0 || explicit.AllocsPerOp() != 0 || prepared.AllocedBytesPerOp() != 0 || explicit.AllocedBytesPerOp() != 0 {
			t.Fatalf("round %d allocations prepared=%d B/%d alloc explicit=%d B/%d alloc", index+1,
				prepared.AllocedBytesPerOp(), prepared.AllocsPerOp(), explicit.AllocedBytesPerOp(), explicit.AllocsPerOp())
		}
		preparedNS := float64(prepared.T.Nanoseconds()) / float64(prepared.N)
		explicitNS := float64(explicit.T.Nanoseconds()) / float64(explicit.N)
		if explicitNS == 0 {
			t.Fatalf("round %d explicit owner measured 0 ns/op", index+1)
		}
		ratios[index] = preparedNS / explicitNS
		t.Logf("round %d order=%s ns/op prepared/explicit=%.3f/%.3f ratio=%.3fx", index+1, order, preparedNS, explicitNS, ratios[index])
	}
	sort.Float64s(ratios)
	median := ratios[rounds/2]
	t.Logf("median prepared/explicit ratio: %.3fx", median)
	if median > 1.05 {
		t.Fatalf("prepared/explicit %.3fx > 1.05x", median)
	}
}
