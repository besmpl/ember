package preparedworker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func TestApplyResolvesUnknownCommitWhenLookupProvesExactRecordStored(t *testing.T) {
	journal := newUncertainJournal(uncertainStoreThenUnknown)
	fixture := newUncertaintyFixture(t, journal)

	first, err := fixture.module.Apply(context.Background(), fixture.operation)
	if err != nil {
		t.Fatal(err)
	}
	if first != fixture.completion {
		t.Fatalf("first completion = %#v, want %#v", first, fixture.completion)
	}
	duplicate, err := fixture.module.Apply(context.Background(), fixture.operation)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate != fixture.completion {
		t.Fatalf("duplicate completion = %#v, want %#v", duplicate, fixture.completion)
	}
	if got := fixture.calls.count(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
}

func TestUnknownCommitWithUnavailableLookupQuarantinesAndResolveStaysGuestFree(t *testing.T) {
	journal := newUncertainJournal(uncertainUnknownLookupError)
	fixture := newUncertaintyFixture(t, journal)

	completion, err := fixture.module.Apply(context.Background(), fixture.operation)
	if !IsFailure(err, FailureUnknownCommit) {
		t.Fatalf("Apply error = %v, want unknown commit", err)
	}
	if completion != (Completion[uncertaintyResult]{}) {
		t.Fatalf("uncertain completion = %#v, want zero completion", completion)
	}
	if got := fixture.calls.count(); got != 1 {
		t.Fatalf("handler calls after Apply = %d, want 1", got)
	}

	resolution, err := fixture.module.Resolve(context.Background(), fixture.operation)
	if !IsFailure(err, FailureUnknownCommit) {
		t.Fatalf("Resolve error = %v, want unknown commit", err)
	}
	if resolution.Status != ResolveUnresolved {
		t.Fatalf("Resolve status = %v, want unresolved", resolution.Status)
	}
	if got := fixture.calls.count(); got != 1 {
		t.Fatalf("handler calls after Resolve = %d, want 1", got)
	}

	_, err = fixture.module.Apply(context.Background(), fixture.operation)
	if !IsFailure(err, FailureLost) {
		t.Fatalf("second Apply error = %v, want quarantined generation failure", err)
	}
	if got := fixture.calls.count(); got != 1 {
		t.Fatalf("handler calls after quarantined Apply = %d, want 1", got)
	}
}

func TestAuthoritativeAbsentResolutionStillRequiresReprepareAfterQuarantine(t *testing.T) {
	journal := newUncertainJournal(uncertainUnknownLookupError)
	fixture := newUncertaintyFixture(t, journal)

	if _, err := fixture.module.Apply(context.Background(), fixture.operation); !IsFailure(err, FailureUnknownCommit) {
		t.Fatalf("Apply error = %v, want unknown commit", err)
	}
	journal.setMode(uncertainAuthoritativeAbsent)
	resolution, err := fixture.module.Resolve(context.Background(), fixture.operation)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != ResolveNotCommitted {
		t.Fatalf("Resolve status = %v, want not committed", resolution.Status)
	}
	if got := fixture.calls.count(); got != 1 {
		t.Fatalf("handler calls after Resolve = %d, want 1", got)
	}

	if _, err := fixture.module.Apply(context.Background(), fixture.operation); !IsFailure(err, FailureLost) {
		t.Fatalf("Apply before reprepare error = %v, want quarantined generation failure", err)
	}
	if got := fixture.calls.count(); got != 1 {
		t.Fatalf("handler calls before reprepare = %d, want 1", got)
	}

	journal.setMode(uncertainNormal)
	candidate, err := fixture.module.Prepare(context.Background(), artifactFromIdentity(
		identityFor("uncertainty-generation-b"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.Activate(); err != nil {
		t.Fatal(err)
	}
	completion, err := fixture.module.Apply(context.Background(), fixture.operation)
	if err != nil {
		t.Fatal(err)
	}
	if completion != fixture.completion {
		t.Fatalf("completion after reprepare = %#v, want %#v", completion, fixture.completion)
	}
	if got := fixture.calls.count(); got != 2 {
		t.Fatalf("handler calls after explicit reprepare = %d, want 2", got)
	}
}

type uncertainJournalMode uint8

const (
	uncertainNormal uncertainJournalMode = iota
	uncertainStoreThenUnknown
	uncertainUnknownLookupError
	uncertainAuthoritativeAbsent
)

var (
	errUncertainCommit = errors.New("injected uncertain commit")
	errUncertainLookup = errors.New("injected unavailable lookup")
)

type uncertainJournal struct {
	mu   sync.Mutex
	mode uncertainJournalMode
	next transactionJournal
}

func newUncertainJournal(mode uncertainJournalMode) *uncertainJournal {
	return &uncertainJournal{mode: mode, next: newMemoryJournal()}
}

func (journal *uncertainJournal) setMode(mode uncertainJournalMode) {
	journal.mu.Lock()
	journal.mode = mode
	journal.mu.Unlock()
}

func (journal *uncertainJournal) currentMode() uncertainJournalMode {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return journal.mode
}

func (journal *uncertainJournal) Latest(
	ctx context.Context,
	stream contentIdentity,
) (transactionRecord, bool, error) {
	return journal.next.Latest(ctx, stream)
}

func (journal *uncertainJournal) LatestQuiescent(
	ctx context.Context,
	stream contentIdentity,
) (transactionRecord, bool, error) {
	if journal.currentMode() == uncertainUnknownLookupError {
		return transactionRecord{}, false, errUncertainLookup
	}
	return journal.next.LatestQuiescent(ctx, stream)
}

func (journal *uncertainJournal) Lookup(
	ctx context.Context,
	stream contentIdentity,
	sequence uint64,
) (transactionRecord, bool, error) {
	switch journal.currentMode() {
	case uncertainUnknownLookupError:
		return transactionRecord{}, false, errUncertainLookup
	case uncertainAuthoritativeAbsent:
		return transactionRecord{}, false, nil
	default:
		return journal.next.Lookup(ctx, stream, sequence)
	}
}

func (journal *uncertainJournal) Commit(
	ctx context.Context,
	expected Position,
	record transactionRecord,
) (commitDisposition, error) {
	switch journal.currentMode() {
	case uncertainStoreThenUnknown:
		disposition, err := journal.next.Commit(ctx, expected, record)
		if err != nil || disposition != commitStored {
			return disposition, err
		}
		return commitUnknown, errUncertainCommit
	case uncertainUnknownLookupError, uncertainAuthoritativeAbsent:
		return commitUnknown, errUncertainCommit
	default:
		return journal.next.Commit(ctx, expected, record)
	}
}

func (journal *uncertainJournal) MarkPublished(
	ctx context.Context,
	stream contentIdentity,
	id outboxID,
) error {
	return journal.next.MarkPublished(ctx, stream, id)
}

type uncertaintyRequest struct {
	Value int `json:"value"`
}

type uncertaintyResult struct {
	Value int `json:"value"`
}

type uncertaintyCheckpoint struct {
	Value int `json:"value"`
}

type uncertaintyEffect struct{}

type uncertaintyCallCounter struct {
	mu    sync.Mutex
	calls int
}

func (counter *uncertaintyCallCounter) increment() {
	counter.mu.Lock()
	counter.calls++
	counter.mu.Unlock()
}

func (counter *uncertaintyCallCounter) count() int {
	counter.mu.Lock()
	defer counter.mu.Unlock()
	return counter.calls
}

type uncertaintyHandler struct {
	calls *uncertaintyCallCounter
}

func (handler *uncertaintyHandler) Apply(
	_ context.Context,
	operation Operation[uncertaintyRequest],
) (Decision[uncertaintyResult, uncertaintyCheckpoint, uncertaintyEffect], error) {
	handler.calls.increment()
	checkpoint := uncertaintyCheckpoint{Value: operation.Request.Value}
	return Decision[uncertaintyResult, uncertaintyCheckpoint, uncertaintyEffect]{
		Result:     uncertaintyResult{Value: operation.Request.Value * 3},
		Checkpoint: &checkpoint,
	}, nil
}

func (*uncertaintyHandler) Close(context.Context) error { return nil }

type uncertaintyFixture struct {
	module     *transactionModule[uncertaintyRequest, uncertaintyResult, uncertaintyCheckpoint, uncertaintyEffect]
	calls      *uncertaintyCallCounter
	operation  Operation[uncertaintyRequest]
	completion Completion[uncertaintyResult]
}

func newUncertaintyFixture(t *testing.T, journal transactionJournal) *uncertaintyFixture {
	t.Helper()
	requestCodec := uncertaintyJSONCodec[uncertaintyRequest]{}
	resultCodec := uncertaintyJSONCodec[uncertaintyResult]{}
	checkpointCodec := uncertaintyJSONCodec[uncertaintyCheckpoint]{}
	effectCodec := uncertaintyJSONCodec[uncertaintyEffect]{}
	calls := &uncertaintyCallCounter{}
	preparer := newMemoryPreparer(
		requestCodec,
		resultCodec,
		checkpointCodec,
		effectCodec,
		func(
			context.Context,
			Position,
			uncertaintyCheckpoint,
		) (Handler[uncertaintyRequest, uncertaintyResult, uncertaintyCheckpoint, uncertaintyEffect], error) {
			return &uncertaintyHandler{calls: calls}, nil
		},
	)
	module, err := newTransactionModule(
		moduleConfig[uncertaintyRequest, uncertaintyResult, uncertaintyCheckpoint, uncertaintyEffect]{
			Stream:   identityFor("uncertainty-stream"),
			Contract: testContract("uncertainty"),
			Limits: transactionLimits{
				MaxRequestBytes:    1024,
				MaxResultBytes:     1024,
				MaxCheckpointBytes: 1024,
				MaxOutboxItems:     1,
				MaxOutboxBytes:     1024,
				MaxCandidates:      2,
				MaxRetired:         2,
			},
			RequestCodec:    requestCodec,
			ResultCodec:     resultCodec,
			CheckpointCodec: checkpointCodec,
			OutboxCodec:     effectCodec,
			Preparer:        preparer,
			Journal:         journal,
			Publisher:       newMemoryPublisher(),
		},
		initialState[uncertaintyCheckpoint]{},
	)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := module.Prepare(context.Background(), artifactFromIdentity(
		identityFor("uncertainty-generation-a"),
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
	return &uncertaintyFixture{
		module: module,
		calls:  calls,
		operation: Operation[uncertaintyRequest]{
			Sequence:     1,
			BaseRevision: 0,
			Request:      uncertaintyRequest{Value: 14},
		},
		completion: Completion[uncertaintyResult]{
			Position: Position{Sequence: 1, Revision: 1},
			Result:   uncertaintyResult{Value: 42},
		},
	}
}

type uncertaintyJSONCodec[T any] struct{}

func (uncertaintyJSONCodec[T]) Encode(value T) ([]byte, error) { return json.Marshal(value) }

func (uncertaintyJSONCodec[T]) Decode(data []byte) (T, error) {
	var value T
	err := json.Unmarshal(data, &value)
	return value, err
}
