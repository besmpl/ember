package mixedcheckpointproof

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	ruby "github.com/besmpl/ember/internal/rubyproof/generated"
	sprig "github.com/besmpl/ember/internal/sprigproof/generated"
	"github.com/besmpl/ember/preparedworker"
)

func TestProductionEmbeddedRunnerRestoresOneMixedCheckpointAndReplaysExactly(t *testing.T) {
	statePath := t.TempDir() + "/mixed.journal"
	deliveries := &effectDeliveries{}
	options := preparedworker.Options[Request, Result, Checkpoint, Effect]{
		Stream:          "mixed-ruby-sprig-proof",
		Contract:        Contract(),
		Initial:         InitialCheckpoint(),
		StatePath:       statePath,
		MaxStateBytes:   1 << 20,
		ShutdownTimeout: time.Second,
		Deliver:         deliveries.deliver,
	}
	restores := &restoreLog{}
	factory := func(ctx context.Context, restore preparedworker.Restore[Checkpoint]) (
		preparedworker.Handler[Request, Result, Checkpoint, Effect], error,
	) {
		restores.append(restore)
		return NewTransactionHandler(ctx, restore)
	}

	first, err := preparedworker.OpenEmbedded(context.Background(), options, factory)
	if err != nil {
		t.Fatal(err)
	}
	firstOperation := preparedworker.Operation[Request]{
		Sequence: 1, BaseRevision: 0,
		Request: Request{Reading: sprig.Reading{Value: 10, Divisor: 2}, AdvanceRuby: true},
	}
	firstCompletion, err := first.Apply(context.Background(), firstOperation)
	if err != nil {
		t.Fatal(err)
	}
	wantFirstResult := Result{Ruby: rubyState(105, 532), SprigTotal: 5}
	if firstCompletion.Position != (preparedworker.Position{Sequence: 1, Revision: 1}) || firstCompletion.Result != wantFirstResult {
		t.Fatalf("first completion = %#v, want position 1/1 and %#v", firstCompletion, wantFirstResult)
	}
	if got := deliveries.effects(); !reflect.DeepEqual(got, []Effect{{RubyValue: 105, RubyTrace: 532, SprigTotal: 5}}) {
		t.Fatalf("first deliveries = %#v", got)
	}

	replayed, err := first.Apply(context.Background(), firstOperation)
	if err != nil {
		t.Fatal(err)
	}
	if replayed != firstCompletion {
		t.Fatalf("duplicate completion = %#v, want %#v", replayed, firstCompletion)
	}
	if got := deliveries.effects(); len(got) != 1 {
		t.Fatalf("duplicate redelivered effects = %#v", got)
	}
	if err := first.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	second, err := preparedworker.OpenEmbedded(context.Background(), options, factory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := second.Close(context.Background()); err != nil {
			t.Errorf("close reopened runner: %v", err)
		}
	})
	wantRestore := preparedworker.Restore[Checkpoint]{
		Position:   preparedworker.Position{Sequence: 1, Revision: 1},
		Checkpoint: Checkpoint{Version: 1, Ruby: rubyState(105, 532), SprigTotal: 5},
	}
	if got := restores.last(); got != wantRestore {
		t.Fatalf("reopened restore = %#v, want %#v", got, wantRestore)
	}

	secondOperation := preparedworker.Operation[Request]{
		Sequence: 2, BaseRevision: 1,
		Request: Request{Reading: sprig.Reading{Value: 8, Divisor: 2}, AdvanceRuby: true},
	}
	secondCompletion, err := second.Apply(context.Background(), secondOperation)
	if err != nil {
		t.Fatal(err)
	}
	wantSecondResult := Result{Ruby: rubyState(206, 532532), SprigTotal: 9}
	if secondCompletion.Position != (preparedworker.Position{Sequence: 2, Revision: 2}) || secondCompletion.Result != wantSecondResult {
		t.Fatalf("second completion = %#v, want position 2/2 and %#v", secondCompletion, wantSecondResult)
	}
	if got := deliveries.effects(); !reflect.DeepEqual(got, []Effect{
		{RubyValue: 105, RubyTrace: 532, SprigTotal: 5},
		{RubyValue: 206, RubyTrace: 532532, SprigTotal: 9},
	}) {
		t.Fatalf("reopened deliveries = %#v", got)
	}
	resolution, err := second.Resolve(context.Background(), secondOperation)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != preparedworker.ResolveCommitted || resolution.Completion != secondCompletion {
		t.Fatalf("resolution = %#v, want committed %#v", resolution, secondCompletion)
	}
}

func TestProductionEmbeddedRunnerQuarantinesPartialRubyFailureWithoutDecision(t *testing.T) {
	statePath := t.TempDir() + "/mixed-failure.journal"
	deliveries := &effectDeliveries{}
	options := preparedworker.Options[Request, Result, Checkpoint, Effect]{
		Stream:          "mixed-ruby-sprig-failure-proof",
		Contract:        Contract(),
		Initial:         InitialCheckpoint(),
		StatePath:       statePath,
		MaxStateBytes:   1 << 20,
		ShutdownTimeout: time.Second,
		Deliver:         deliveries.deliver,
	}
	policy := Policy{
		// One step lets ApplyRescuedRaise append its first raise mark, then
		// aborts before the rescue. The owner has begun mutating, but the
		// application must not publish its detached projection.
		Ruby:       ruby.Limits{Steps: 1, Frames: 16, Objects: 4},
		SprigSteps: 4,
	}
	var rubyOwner *ruby.Engine
	factory := func(ctx context.Context, restore preparedworker.Restore[Checkpoint]) (
		preparedworker.Handler[Request, Result, Checkpoint, Effect], error,
	) {
		handler, err := NewHandler(ctx, restore.Checkpoint, policy)
		if err != nil {
			return nil, err
		}
		rubyOwner = handler.ruby
		return &TransactionHandler{handler: handler}, nil
	}

	runner, err := preparedworker.OpenEmbedded(context.Background(), options, factory)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			if err := runner.Close(context.Background()); err != nil {
				t.Errorf("close failed runner: %v", err)
			}
		}
	})
	operation := preparedworker.Operation[Request]{
		Sequence: 1, BaseRevision: 0,
		Request: Request{Reading: sprig.Reading{Value: 4, Divisor: 2}, AdvanceRuby: true},
	}
	completion, err := runner.Apply(context.Background(), operation)
	if !preparedworker.IsFailure(err, preparedworker.FailureGuest) || !errors.Is(err, ruby.ErrStepLimit) {
		t.Fatalf("partial Ruby Apply error = %v, want guest step-limit failure", err)
	}
	if completion != (preparedworker.Completion[Result]{}) {
		t.Fatalf("partial Ruby completion = %#v, want zero", completion)
	}
	if got := deliveries.effects(); len(got) != 0 {
		t.Fatalf("partial Ruby deliveries = %#v, want none", got)
	}

	resolution, err := runner.Resolve(context.Background(), operation)
	if err != nil || resolution.Status != preparedworker.ResolveNotCommitted {
		t.Fatalf("partial Ruby resolution = %#v, %v, want not committed", resolution, err)
	}
	if _, err := runner.Apply(context.Background(), operation); !preparedworker.IsFailure(err, preparedworker.FailureLost) {
		t.Fatalf("Apply after generation quarantine = %v, want lost", err)
	}
	if rubyOwner == nil {
		t.Fatal("factory did not expose the generation-local Ruby owner")
	}
	if err := runner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	closed = true
	if _, err := rubyOwner.Hot(context.Background()); !errors.Is(err, ruby.ErrClosed) {
		t.Fatalf("quarantined Ruby owner after runner close = %v, want closed", err)
	}
}

type effectDeliveries struct {
	mu      sync.Mutex
	values  []Effect
	lastIDs []string
}

func (deliveries *effectDeliveries) deliver(_ context.Context, delivery preparedworker.Delivery[Effect]) error {
	deliveries.mu.Lock()
	defer deliveries.mu.Unlock()
	deliveries.values = append(deliveries.values, delivery.Effect)
	deliveries.lastIDs = append(deliveries.lastIDs, delivery.ID.String())
	return nil
}

func (deliveries *effectDeliveries) effects() []Effect {
	deliveries.mu.Lock()
	defer deliveries.mu.Unlock()
	return append([]Effect(nil), deliveries.values...)
}

type restoreLog struct {
	mu     sync.Mutex
	values []preparedworker.Restore[Checkpoint]
}

func (log *restoreLog) append(value preparedworker.Restore[Checkpoint]) {
	log.mu.Lock()
	defer log.mu.Unlock()
	log.values = append(log.values, value)
}

func (log *restoreLog) last() preparedworker.Restore[Checkpoint] {
	log.mu.Lock()
	defer log.mu.Unlock()
	return log.values[len(log.values)-1]
}
