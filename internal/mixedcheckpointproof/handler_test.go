package mixedcheckpointproof

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	ruby "github.com/besmpl/ember/internal/rubyproof/generated"
	sprig "github.com/besmpl/ember/internal/sprigproof/generated"
)

func TestHandlerRestoresDetachedRubyAndSprigState(t *testing.T) {
	first := mustHandler(t, InitialCheckpoint(), ProofPolicy())
	result, checkpoint, effect, hasEffect, err := first.Apply(context.Background(), Request{
		Reading: sprig.Reading{Value: 10, Divisor: 2}, AdvanceRuby: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCheckpoint := Checkpoint{Version: 1, Ruby: ruby.State{Value: 105, Trace: 532}, SprigTotal: 5}
	if checkpoint != wantCheckpoint || result != (Result{Ruby: wantCheckpoint.Ruby, SprigTotal: 5}) {
		t.Fatalf("first result/checkpoint = %#v / %#v, want %#v", result, checkpoint, wantCheckpoint)
	}
	if !hasEffect || effect != (Effect{RubyValue: 105, RubyTrace: 532, SprigTotal: 5}) {
		t.Fatalf("first effect = %#v, present %v", effect, hasEffect)
	}
	firstOwner := first.ruby
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := firstOwner.Hot(context.Background()); !errors.Is(err, ruby.ErrClosed) {
		t.Fatalf("retired Ruby owner = %v, want closed", err)
	}

	second := mustHandler(t, checkpoint, ProofPolicy())
	if second.ruby == firstOwner {
		t.Fatal("restored generation reused the retired Ruby owner")
	}
	result, checkpoint, effect, hasEffect, err = second.Apply(context.Background(), Request{
		Reading: sprig.Reading{Value: 8, Divisor: 2}, AdvanceRuby: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCheckpoint = Checkpoint{Version: 1, Ruby: ruby.State{Value: 206, Trace: 532532}, SprigTotal: 9}
	if checkpoint != wantCheckpoint || result != (Result{Ruby: wantCheckpoint.Ruby, SprigTotal: 9}) {
		t.Fatalf("restored result/checkpoint = %#v / %#v, want %#v", result, checkpoint, wantCheckpoint)
	}
	if !hasEffect || effect != (Effect{RubyValue: 206, RubyTrace: 532532, SprigTotal: 9}) {
		t.Fatalf("restored effect = %#v, present %v", effect, hasEffect)
	}
}

func TestHandlerCommitsTypedSprigFailuresBeforeRuby(t *testing.T) {
	handler := mustHandler(t, InitialCheckpoint(), ProofPolicy())
	initialOwner := handler.ruby

	result, checkpoint, effect, hasEffect, err := handler.Apply(context.Background(), Request{
		Reading: sprig.Reading{Value: 99, Divisor: 0}, AdvanceRuby: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Rejected || result.RejectionCode != 7 || result.Domain != 0 || checkpoint != InitialCheckpoint() || hasEffect || effect != (Effect{}) {
		t.Fatalf("rejected decision = %#v / %#v / %#v / %v", result, checkpoint, effect, hasEffect)
	}
	if state, err := initialOwner.Snapshot(context.Background()); err != nil || state != ruby.InitialState() {
		t.Fatalf("Ruby state after rejection = %#v, %v", state, err)
	}

	overflowCheckpoint := InitialCheckpoint()
	overflowCheckpoint.SprigTotal = math.MaxInt64
	overflow := mustHandler(t, overflowCheckpoint, ProofPolicy())
	result, checkpoint, effect, hasEffect, err = overflow.Apply(context.Background(), Request{
		Reading: sprig.Reading{Value: 1, Divisor: 1}, AdvanceRuby: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Domain != sprig.DomainOverflow || result.Rejected || checkpoint != overflowCheckpoint || hasEffect || effect != (Effect{}) {
		t.Fatalf("domain decision = %#v / %#v / %#v / %v", result, checkpoint, effect, hasEffect)
	}
	if state, err := overflow.ruby.Snapshot(context.Background()); err != nil || state != ruby.InitialState() {
		t.Fatalf("Ruby state after domain failure = %#v, %v", state, err)
	}

	// A typed rejection is a committed application outcome, not generation
	// poisoning; the same handler can execute the next valid transaction.
	if _, _, _, hasEffect, err := handler.Apply(context.Background(), Request{
		Reading: sprig.Reading{Value: 6, Divisor: 2}, AdvanceRuby: false,
	}); err != nil || !hasEffect {
		t.Fatalf("reuse after typed rejection = effect %v, error %v", hasEffect, err)
	}
}

func TestHandlerHostFailuresPoisonWithoutCheckpoint(t *testing.T) {
	tests := []struct {
		name   string
		policy Policy
		ctx    func() context.Context
		want   error
	}{
		{
			name: "pre-canceled", policy: ProofPolicy(), want: context.Canceled,
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
		},
		{name: "sprig limit", policy: Policy{Ruby: ruby.ProofLimits()}, ctx: context.Background, want: sprig.ErrStepLimit},
		{name: "ruby limit", policy: Policy{Ruby: ruby.Limits{Frames: 16, Objects: 4}, SprigSteps: 4}, ctx: context.Background, want: ruby.ErrStepLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := mustHandler(t, InitialCheckpoint(), test.policy)
			result, checkpoint, effect, hasEffect, err := handler.Apply(test.ctx(), Request{
				Reading: sprig.Reading{Value: 4, Divisor: 2}, AdvanceRuby: true,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if result != (Result{}) || checkpoint != (Checkpoint{}) || effect != (Effect{}) || hasEffect {
				t.Fatalf("failed output = %#v / %#v / %#v / %v", result, checkpoint, effect, hasEffect)
			}
			if _, _, _, _, err := handler.Apply(context.Background(), Request{}); !errors.Is(err, ErrPoisoned) {
				t.Fatalf("reuse after host failure = %v", err)
			}
		})
	}
}

func TestHandlerRestoreFailureCleansConstructedOwnerAndCloseIsIdempotent(t *testing.T) {
	if handler, err := NewHandler(context.Background(), Checkpoint{}, ProofPolicy()); handler != nil || !errors.Is(err, ErrCheckpoint) {
		t.Fatalf("zero checkpoint restore = %#v, %v", handler, err)
	}
	invalid := InitialCheckpoint()
	invalid.Ruby.Trace = -1
	if handler, err := NewHandler(context.Background(), invalid, ProofPolicy()); handler != nil || !errors.Is(err, ErrCheckpoint) {
		t.Fatalf("invalid Ruby restore = %#v, %v", handler, err)
	}

	sentinel := errors.New("injected post-restore cancellation")
	var owner *ruby.Engine
	handler, err := newHandler(InitialCheckpoint(), ProofPolicy(), func(handler *Handler) error {
		owner = handler.ruby
		return sentinel
	})
	if handler != nil || !errors.Is(err, sentinel) || owner == nil {
		t.Fatalf("partial restore = %#v, %v, owner %#v", handler, err, owner)
	}
	if _, err := owner.Hot(context.Background()); !errors.Is(err, ruby.ErrClosed) {
		t.Fatalf("partially restored owner = %v, want closed", err)
	}

	handler = mustHandler(t, InitialCheckpoint(), ProofPolicy())
	owner = handler.ruby
	if err := handler.Close(); err != nil {
		t.Fatal(err)
	}
	if err := handler.Close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if _, _, _, _, err := handler.Apply(context.Background(), Request{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("apply after close = %v", err)
	}
	if _, err := owner.Hot(context.Background()); !errors.Is(err, ruby.ErrClosed) {
		t.Fatalf("owned Ruby after close = %v", err)
	}
}

func TestHandlerCloseRetainsOwnerUntilRubyCloseSucceeds(t *testing.T) {
	handler := mustHandler(t, InitialCheckpoint(), ProofPolicy())
	owner := handler.ruby
	blocking := &blockingErrContext{
		Context: context.Background(),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	hotDone := make(chan error, 1)
	go func() {
		_, err := owner.Hot(blocking)
		hotDone <- err
	}()
	<-blocking.entered

	if err := handler.Close(); !errors.Is(err, ruby.ErrBusy) {
		t.Fatalf("close while Ruby is active = %v, want busy", err)
	}
	if handler.closed || handler.ruby != owner || handler.checkpoint != InitialCheckpoint() {
		t.Fatalf("failed close discarded retry state: closed=%v owner=%p checkpoint=%#v", handler.closed, handler.ruby, handler.checkpoint)
	}
	close(blocking.release)
	if err := <-hotDone; err != nil {
		t.Fatalf("blocked Ruby call: %v", err)
	}
	if err := handler.Close(); err != nil {
		t.Fatalf("retry close: %v", err)
	}
	if !handler.closed || handler.ruby != nil || handler.checkpoint != (Checkpoint{}) {
		t.Fatalf("successful retry did not retire state: closed=%v owner=%p checkpoint=%#v", handler.closed, handler.ruby, handler.checkpoint)
	}
}

func TestConcreteCodecsRoundTripAndFailClosed(t *testing.T) {
	request := Request{Reading: sprig.Reading{Value: -7, Divisor: 3}, AdvanceRuby: true}
	requestData, err := (requestCodec{}).Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := (requestCodec{}).Decode(requestData); err != nil || got != request {
		t.Fatalf("request round trip = %#v, %v", got, err)
	}
	requestData[16] = 2
	if _, err := (requestCodec{}).Decode(requestData); err == nil {
		t.Fatal("non-canonical request boolean decoded")
	}

	result := Result{Ruby: ruby.State{Value: 105, Trace: 532}, SprigTotal: 5}
	resultData, err := (resultCodec{}).Encode(result)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := (resultCodec{}).Decode(resultData); err != nil || got != result {
		t.Fatalf("result round trip = %#v, %v", got, err)
	}
	if _, err := (resultCodec{}).Encode(Result{Domain: 99}); err == nil {
		t.Fatal("unknown domain encoded")
	}

	checkpoint := Checkpoint{Version: 1, Ruby: result.Ruby, SprigTotal: 5}
	checkpointData, err := (checkpointCodec{}).Encode(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := (checkpointCodec{}).Decode(checkpointData); err != nil || got != checkpoint {
		t.Fatalf("checkpoint round trip = %#v, %v", got, err)
	}
	checkpointData[0] = 2
	if _, err := (checkpointCodec{}).Decode(checkpointData); !errors.Is(err, ErrCheckpoint) {
		t.Fatalf("checkpoint version error = %v", err)
	}

	effect := Effect{RubyValue: 105, RubyTrace: 532, SprigTotal: 5}
	effectData, err := (effectCodec{}).Encode(effect)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := (effectCodec{}).Decode(effectData); err != nil || got != effect {
		t.Fatalf("effect round trip = %#v, %v", got, err)
	}
	for _, decode := range []func([]byte) error{
		func(data []byte) error { _, err := (requestCodec{}).Decode(data); return err },
		func(data []byte) error { _, err := (resultCodec{}).Decode(data); return err },
		func(data []byte) error { _, err := (checkpointCodec{}).Decode(data); return err },
		func(data []byte) error { _, err := (effectCodec{}).Decode(data); return err },
	} {
		if err := decode(nil); err == nil {
			t.Fatal("empty closed record decoded")
		}
	}
}

func mustHandler(t *testing.T, checkpoint Checkpoint, policy Policy) *Handler {
	t.Helper()
	handler, err := NewHandler(context.Background(), checkpoint, policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handler.Close(); err != nil {
			t.Errorf("close handler: %v", err)
		}
	})
	return handler
}

type blockingErrContext struct {
	context.Context
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (ctx *blockingErrContext) Err() error {
	ctx.once.Do(func() { close(ctx.entered) })
	<-ctx.release
	return nil
}
