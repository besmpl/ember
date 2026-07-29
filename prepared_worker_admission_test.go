package ember_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/besmpl/ember/internal/preparedworkerfixture"
	"github.com/besmpl/ember/internal/preparedworkerprobe"
	"github.com/besmpl/ember/preparedworker"
)

func TestPreparedWorkerEmbeddedTurnTrace(t *testing.T) {
	handler, err := preparedworkerprobe.NewHandler()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handler.Close(); err != nil {
			t.Error(err)
		}
	})

	got, err := handler.Transact(context.Background(), preparedworkerfixture.TurnRequest{
		Sequence: 1,
		Revision: 0,
		Projection: preparedworkerfixture.Projection{
			Step: 1,
			Seed: 5,
			Work: 1,
		},
		Events: []preparedworkerfixture.DamageEvent{{
			Route:  preparedworkerfixture.RouteDamage,
			Entity: 7,
			Amount: 5,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := preparedworkerfixture.TurnResult{
		Sequence: 1,
		Revision: 1,
		State: preparedworkerfixture.StateSnapshot{
			Tick:        1,
			Total:       6,
			Ready:       0,
			Entity7:     0,
			ModuleCalls: 1,
		},
		Commands: []preparedworkerfixture.Command{{
			Kind:   preparedworkerfixture.CommandDraw,
			Entity: 0,
			A:      1,
			B:      6,
		}},
		Effects: []preparedworkerfixture.Effect{
			{
				ID:              preparedWorkerEffectID(1, 1),
				Kind:            preparedworkerfixture.EffectLoad,
				Entity:          7,
				NeedsCompletion: true,
			},
			{
				ID:     preparedWorkerEffectID(1, 2),
				Kind:   preparedworkerfixture.EffectAudio,
				Entity: 0,
				Value:  5,
			},
		},
		Pending: []uint64{preparedWorkerEffectID(1, 1)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("turn result = %#v, want %#v", got, want)
	}

	got, err = handler.Transact(context.Background(), preparedworkerfixture.TurnRequest{
		Sequence: 2,
		Revision: 1,
		State:    want.State,
		Projection: preparedworkerfixture.Projection{
			Step: 1,
			Seed: 6,
			Work: 1,
		},
		Completions: []preparedworkerfixture.Completion{{
			EffectID: preparedWorkerEffectID(1, 1),
			Status:   preparedworkerfixture.CompletionOK,
			Value:    11,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want = preparedworkerfixture.TurnResult{
		Sequence: 2,
		Revision: 2,
		State: preparedworkerfixture.StateSnapshot{
			Tick:        2,
			Total:       34,
			Ready:       11,
			Entity7:     5,
			ModuleCalls: 2,
		},
		Commands: []preparedworkerfixture.Command{
			{
				Kind:   preparedworkerfixture.CommandFlash,
				Entity: 7,
				A:      5,
				B:      19,
			},
			{
				Kind:   preparedworkerfixture.CommandDraw,
				Entity: 0,
				A:      2,
				B:      34,
			},
		},
		Effects: []preparedworkerfixture.Effect{{
			ID:     preparedWorkerEffectID(2, 1),
			Kind:   preparedworkerfixture.EffectAudio,
			Entity: 0,
			Value:  8,
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("turn result = %#v, want %#v", got, want)
	}

	got, err = handler.Transact(context.Background(), preparedworkerfixture.TurnRequest{
		Sequence: 3,
		Revision: 2,
		State:    want.State,
		Projection: preparedworkerfixture.Projection{
			Step: 1,
			Seed: 4,
			Work: 1,
		},
		Events: []preparedworkerfixture.DamageEvent{{
			Route:  preparedworkerfixture.RouteDamage,
			Entity: 7,
			Amount: 4,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want = preparedworkerfixture.TurnResult{
		Sequence: 3,
		Revision: 3,
		State: preparedworkerfixture.StateSnapshot{
			Tick:        3,
			Total:       38,
			Ready:       11,
			Entity7:     5,
			ModuleCalls: 3,
		},
		Commands: []preparedworkerfixture.Command{{
			Kind:   preparedworkerfixture.CommandDraw,
			Entity: 0,
			A:      3,
			B:      38,
		}},
		Effects: []preparedworkerfixture.Effect{
			{
				ID:              preparedWorkerEffectID(3, 1),
				Kind:            preparedworkerfixture.EffectLoad,
				Entity:          7,
				NeedsCompletion: true,
			},
			{
				ID:     preparedWorkerEffectID(3, 2),
				Kind:   preparedworkerfixture.EffectAudio,
				Entity: 0,
				Value:  3,
			},
		},
		Pending: []uint64{preparedWorkerEffectID(3, 1)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("turn result = %#v, want %#v", got, want)
	}

	got, err = handler.Transact(context.Background(), preparedworkerfixture.TurnRequest{
		Sequence: 4,
		Revision: 3,
		State:    want.State,
		Projection: preparedworkerfixture.Projection{
			Step: 1,
			Seed: 3,
			Work: 1,
		},
		Completions: []preparedworkerfixture.Completion{{
			EffectID: preparedWorkerEffectID(3, 1),
			Status:   preparedworkerfixture.CompletionOK,
			Value:    2,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want = preparedworkerfixture.TurnResult{
		Sequence: 4,
		Revision: 4,
		State: preparedworkerfixture.StateSnapshot{
			Tick:        4,
			Total:       50,
			Ready:       13,
			Entity7:     9,
			ModuleCalls: 4,
		},
		Commands: []preparedworkerfixture.Command{
			{
				Kind:   preparedworkerfixture.CommandFlash,
				Entity: 7,
				A:      4,
				B:      9,
			},
			{
				Kind:   preparedworkerfixture.CommandDraw,
				Entity: 0,
				A:      4,
				B:      50,
			},
		},
		Effects: []preparedworkerfixture.Effect{{
			ID:     preparedWorkerEffectID(4, 1),
			Kind:   preparedworkerfixture.EffectAudio,
			Entity: 0,
			Value:  2,
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("turn result = %#v, want %#v", got, want)
	}
}

func TestPreparedWorkerProcessMatchesEmbeddedTurn(t *testing.T) {
	publication := buildPreparedWorkerHostPublication(
		t,
		"./internal/preparedworkerprobe/cmd/worker",
		"ember-rich-game-worker",
	)
	embedded := openPreparedWorkerEmbeddedRunner(t, "process-match-embedded")
	worker := openPreparedWorkerProcessRunner(t, publication, "process-match-worker")
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if pid := worker.ProcessID(t); pid <= 0 {
			t.Fatalf("supervised worker PID = %d", pid)
		}
	}

	for _, request := range preparedWorkerTraceRequests() {
		want, err := embedded.Transact(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		got, err := worker.Transact(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("worker turn %d = %#v, embedded = %#v", request.Sequence, got, want)
		}
	}
}

func TestPreparedWorkerRestartsOnlyFromQuiescentApplicationState(t *testing.T) {
	continuing, err := preparedworkerprobe.NewHandler()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = continuing.Close() })

	requests := preparedWorkerTraceRequests()
	var checkpoint preparedworkerfixture.TurnResult
	checkpoint, err = continuing.Transact(context.Background(), requests[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := preparedworkerprobe.QuiescentCheckpoint(checkpoint); err == nil {
		t.Fatal("created a reload checkpoint with a generation-owned suspension")
	}
	for _, request := range requests[1:2] {
		checkpoint, err = continuing.Transact(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(checkpoint.Pending) != 0 {
		t.Fatalf("checkpoint has generation-owned suspensions: %v", checkpoint.Pending)
	}
	quiescent, err := preparedworkerprobe.QuiescentCheckpoint(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := preparedworkerprobe.NewHandlerAt(quiescent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })

	for _, request := range requests[2:] {
		want, err := continuing.Transact(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		got, err := restarted.Transact(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("restarted turn %d = %#v, continuing = %#v", request.Sequence, got, want)
		}
	}
}

func TestPreparedWorkerRejectsLossyInputBeforeAdvancing(t *testing.T) {
	handler, err := preparedworkerprobe.NewHandler()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handler.Close() })

	request := preparedWorkerTraceRequests()[0]
	request.Projection.Step = 1<<53 + 1
	if _, err := handler.Transact(context.Background(), request); err == nil {
		t.Fatal("accepted an integer that Luau cannot represent exactly")
	}
	if _, err := handler.Transact(context.Background(), preparedWorkerTraceRequests()[0]); err != nil {
		t.Fatalf("boundary rejection advanced or poisoned the handler: %v", err)
	}
}

func TestPreparedWorkerMaximumTurnAlwaysFitsResponse(t *testing.T) {
	handler, err := preparedworkerprobe.NewHandler()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handler.Close() })

	events := make([]preparedworkerfixture.DamageEvent, 63)
	for index := range events {
		events[index] = preparedworkerfixture.DamageEvent{
			Route:  preparedworkerfixture.RouteDamage,
			Entity: uint32(index),
			Amount: 1,
		}
	}
	first, err := handler.Transact(context.Background(), preparedworkerfixture.TurnRequest{
		Sequence:   1,
		Revision:   0,
		Projection: preparedworkerfixture.Projection{Step: 1, Seed: 3, Work: 1},
		Events:     events,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Effects) != 64 || len(first.Pending) != 63 {
		t.Fatalf("maximum event turn produced %d effects and %d pending, want 64 and 63", len(first.Effects), len(first.Pending))
	}

	completions := make([]preparedworkerfixture.Completion, len(first.Pending))
	for index, effectID := range first.Pending {
		completions[index] = preparedworkerfixture.Completion{
			EffectID: effectID,
			Status:   preparedworkerfixture.CompletionOK,
			Value:    1,
		}
	}
	second, err := handler.Transact(context.Background(), preparedworkerfixture.TurnRequest{
		Sequence:    2,
		Revision:    1,
		State:       first.State,
		Projection:  preparedworkerfixture.Projection{Step: 1, Seed: 3, Work: 1},
		Completions: completions,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Commands) != 64 || len(second.Pending) != 0 {
		t.Fatalf("maximum completion turn produced %d commands and %d pending, want 64 and 0", len(second.Commands), len(second.Pending))
	}
}

func TestPreparedWorkerRejectsOverflowingTurnAndReactivatesFromDurableHead(t *testing.T) {
	publication := buildPreparedWorkerHostPublication(
		t,
		"./internal/preparedworkerprobe/cmd/worker",
		"ember-rich-game-overflow-worker",
	)
	worker := openPreparedWorkerProcessRunner(t, publication, "overflow-worker")
	events := make([]preparedworkerfixture.DamageEvent, 64)
	for index := range events {
		events[index] = preparedworkerfixture.DamageEvent{
			Route:  preparedworkerfixture.RouteDamage,
			Entity: uint32(index),
			Amount: 1,
		}
	}
	invalid := preparedWorkerTraceRequests()[0]
	invalid.Events = events
	if _, err := worker.Transact(context.Background(), invalid); !preparedworker.IsFailure(err, preparedworker.FailureGuest) {
		t.Fatalf("overflowing turn error = %v, want guest rejection", err)
	}
	resolution, err := worker.runner.Resolve(
		context.Background(),
		preparedworker.Operation[preparedworkerfixture.TurnRequest]{
			Sequence: invalid.Sequence, BaseRevision: invalid.Revision, Request: invalid,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != preparedworker.ResolveNotCommitted {
		t.Fatalf("overflowing turn resolution = %v, want not committed", resolution.Status)
	}
	if _, err := worker.Transact(context.Background(), preparedWorkerTraceRequests()[0]); !preparedworker.IsFailure(err, preparedworker.FailureLost) {
		t.Fatalf("apply before reactivation error = %v, want quarantined generation", err)
	}
	if err := worker.Reactivate(context.Background(), publication); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Transact(context.Background(), preparedWorkerTraceRequests()[0]); err != nil {
		t.Fatalf("apply after reactivation: %v", err)
	}
}

func TestPreparedWorkerDeadlineTerminatesStalledGeneration(t *testing.T) {
	publication := buildPreparedWorkerHostPublication(
		t,
		"./internal/preparedworkerprobe/cmd/stallworker",
		"ember-rich-game-stall-worker",
	)
	worker := openPreparedWorkerProcessRunner(t, publication, "stall-worker")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := worker.Transact(ctx, preparedWorkerTraceRequests()[0])
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stalled transaction error = %v, want deadline exceeded", err)
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedWorkerTerminalFailureCannotBeRetried(t *testing.T) {
	state := preparedworkerfixture.StateSnapshot{Total: (1 << 53) - 1}
	handler, err := preparedworkerprobe.NewHandlerAt(preparedworkerfixture.Checkpoint{State: state})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handler.Close() })

	request := preparedWorkerTraceRequests()[0]
	request.State = state
	if _, err := handler.Transact(context.Background(), request); err == nil {
		t.Fatal("accepted a result outside the exact guest integer domain")
	}
	if _, err := handler.Transact(context.Background(), request); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("retry error = %v, want terminal handler failure", err)
	}
}
