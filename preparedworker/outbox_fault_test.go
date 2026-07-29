package preparedworker

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestApplyReturnsCommittedCompletionWhenOutboxDeliveryFails(t *testing.T) {
	fixture := newOutboxFaultFixture(t)

	completion, err := fixture.module.Apply(context.Background(), fixture.operation)
	if !IsFailure(err, FailureDelivery) {
		t.Errorf("Apply error = %v, want delivery failure", err)
	}
	if completion != fixture.completion {
		t.Errorf("Apply completion = %#v, want committed %#v", completion, fixture.completion)
	}
	stored, ok, lookupErr := fixture.journal.Lookup(
		context.Background(),
		fixture.stream,
		fixture.operation.Sequence,
	)
	if lookupErr != nil {
		t.Fatal(lookupErr)
	}
	if !ok {
		t.Fatal("Apply delivery failure did not leave a committed journal record")
	}
	if stored.Sequence != fixture.completion.Position.Sequence ||
		stored.Revision != fixture.completion.Position.Revision {
		t.Fatalf("stored position = %d/%d, want %#v", stored.Sequence, stored.Revision, fixture.completion.Position)
	}
}

func TestCommittedOutboxRetryKeepsStableIDsOrderAndGuestCompletion(t *testing.T) {
	for _, test := range []struct {
		name  string
		retry func(*outboxFaultFixture) (Completion[outboxResult], error)
	}{
		{
			name: "duplicate Apply",
			retry: func(fixture *outboxFaultFixture) (Completion[outboxResult], error) {
				return fixture.module.Apply(context.Background(), fixture.operation)
			},
		},
		{
			name: "Resolve",
			retry: func(fixture *outboxFaultFixture) (Completion[outboxResult], error) {
				resolution, err := fixture.module.Resolve(context.Background(), fixture.operation)
				if resolution.Status != ResolveCommitted {
					return Completion[outboxResult]{}, errors.New("Resolve did not report committed")
				}
				return resolution.Completion, err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOutboxFaultFixture(t)

			_, firstErr := fixture.module.Apply(context.Background(), fixture.operation)
			if firstErr == nil {
				t.Fatal("initial Apply succeeded, want injected publication failure")
			}
			completion, err := test.retry(fixture)
			if err != nil {
				t.Fatal(err)
			}
			if completion != fixture.completion {
				t.Fatalf("retried completion = %#v, want %#v", completion, fixture.completion)
			}
			if fixture.handler.calls != 1 {
				t.Fatalf("handler calls = %d, want 1", fixture.handler.calls)
			}

			wantIDs := []outboxID{
				{Stream: fixture.stream, Sequence: 1, Ordinal: 1},
				{Stream: fixture.stream, Sequence: 1, Ordinal: 1},
				{Stream: fixture.stream, Sequence: 1, Ordinal: 2},
				{Stream: fixture.stream, Sequence: 1, Ordinal: 3},
			}
			if got := fixture.publisher.attemptedIDs(); !reflect.DeepEqual(got, wantIDs) {
				t.Fatalf("publication attempts = %#v, want stable ordered %#v", got, wantIDs)
			}
		})
	}
}

func TestResolveReturnsOneDeliveryFailureLayerWithCommittedCompletion(t *testing.T) {
	fixture := newOutboxFaultFixture(t)
	if _, err := fixture.module.Apply(context.Background(), fixture.operation); err == nil {
		t.Fatal("initial Apply succeeded, want delivery failure")
	}
	fixture.publisher.failNext()
	resolution, err := fixture.module.Resolve(context.Background(), fixture.operation)
	if resolution.Status != ResolveCommitted || resolution.Completion != fixture.completion {
		t.Fatalf("Resolve = %#v, want committed %#v", resolution, fixture.completion)
	}
	if !IsFailure(err, FailureDelivery) {
		t.Fatalf("Resolve error = %v, want delivery failure", err)
	}
	layers := 0
	for current := err; current != nil; current = errors.Unwrap(current) {
		var failure *Failure
		if errors.As(current, &failure) && current == error(failure) && failure.Kind == FailureDelivery {
			layers++
		}
	}
	if layers != 1 {
		t.Fatalf("Resolve delivery failure layers = %d, want 1: %v", layers, err)
	}
}

type outboxRequest struct {
	Value int `json:"value"`
}

type outboxResult struct {
	Value int `json:"value"`
}

type outboxCheckpoint struct {
	Value int `json:"value"`
}

type outboxEffect struct {
	Value int `json:"value"`
}

type outboxHandler struct {
	calls int
}

func (handler *outboxHandler) Apply(
	_ context.Context,
	operation Operation[outboxRequest],
) (Decision[outboxResult, outboxCheckpoint, outboxEffect], error) {
	handler.calls++
	checkpoint := outboxCheckpoint{Value: operation.Request.Value}
	return Decision[outboxResult, outboxCheckpoint, outboxEffect]{
		Result:     outboxResult{Value: operation.Request.Value * 2},
		Checkpoint: &checkpoint,
		Effects: []outboxEffect{
			{Value: 10},
			{Value: 20},
			{Value: 30},
		},
	}, nil
}

func (*outboxHandler) Close(context.Context) error { return nil }

type recordingJournal struct {
	transactionJournal
}

type failOncePublisher struct {
	mu       sync.Mutex
	failed   bool
	attempts []outboxRecord
	next     effectPublisher
}

func (publisher *failOncePublisher) Publish(
	ctx context.Context,
	record outboxRecord,
) error {
	publisher.mu.Lock()
	publisher.attempts = append(publisher.attempts, record)
	if !publisher.failed {
		publisher.failed = true
		publisher.mu.Unlock()
		return errors.New("injected publication failure")
	}
	publisher.mu.Unlock()
	return publisher.next.Publish(ctx, record)
}

func (publisher *failOncePublisher) attemptedIDs() []outboxID {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	ids := make([]outboxID, len(publisher.attempts))
	for index, record := range publisher.attempts {
		ids[index] = record.ID
	}
	return ids
}

func (publisher *failOncePublisher) failNext() {
	publisher.mu.Lock()
	publisher.failed = false
	publisher.mu.Unlock()
}

type outboxFaultFixture struct {
	module     *transactionModule[outboxRequest, outboxResult, outboxCheckpoint, outboxEffect]
	handler    *outboxHandler
	journal    *recordingJournal
	publisher  *failOncePublisher
	stream     contentIdentity
	operation  Operation[outboxRequest]
	completion Completion[outboxResult]
}

func newOutboxFaultFixture(t *testing.T) *outboxFaultFixture {
	t.Helper()
	requestCodec := outboxJSONCodec[outboxRequest]{}
	resultCodec := outboxJSONCodec[outboxResult]{}
	checkpointCodec := outboxJSONCodec[outboxCheckpoint]{}
	effectCodec := outboxJSONCodec[outboxEffect]{}
	handler := &outboxHandler{}
	preparer := newMemoryPreparer(
		requestCodec,
		resultCodec,
		checkpointCodec,
		effectCodec,
		func(
			context.Context,
			Position,
			outboxCheckpoint,
		) (Handler[outboxRequest, outboxResult, outboxCheckpoint, outboxEffect], error) {
			return handler, nil
		},
	)
	stream := identityFor("outbox-fault-stream")
	journal := &recordingJournal{transactionJournal: newMemoryJournal()}
	publisher := &failOncePublisher{next: newMemoryPublisher()}
	module, err := newTransactionModule(
		moduleConfig[outboxRequest, outboxResult, outboxCheckpoint, outboxEffect]{
			Stream:   stream,
			Contract: testContract("outbox-fault"),
			Limits: transactionLimits{
				MaxRequestBytes:    1024,
				MaxResultBytes:     1024,
				MaxCheckpointBytes: 1024,
				MaxOutboxItems:     8,
				MaxOutboxBytes:     1024,
				MaxCandidates:      1,
				MaxRetired:         1,
			},
			RequestCodec:    requestCodec,
			ResultCodec:     resultCodec,
			CheckpointCodec: checkpointCodec,
			OutboxCodec:     effectCodec,
			Preparer:        preparer,
			Journal:         journal,
			Publisher:       publisher,
		},
		initialState[outboxCheckpoint]{},
	)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := module.Prepare(context.Background(), artifactFromIdentity(
		identityFor("outbox-fault-generation"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.Activate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := module.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return &outboxFaultFixture{
		module:    module,
		handler:   handler,
		journal:   journal,
		publisher: publisher,
		stream:    stream,
		operation: Operation[outboxRequest]{
			Sequence:     1,
			BaseRevision: 0,
			Request:      outboxRequest{Value: 21},
		},
		completion: Completion[outboxResult]{
			Position: Position{Sequence: 1, Revision: 1},
			Result:   outboxResult{Value: 42},
		},
	}
}

type outboxJSONCodec[T any] struct{}

func (outboxJSONCodec[T]) Encode(value T) ([]byte, error) { return json.Marshal(value) }

func (outboxJSONCodec[T]) Decode(data []byte) (T, error) {
	var value T
	err := json.Unmarshal(data, &value)
	return value, err
}
