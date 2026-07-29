package preparedworker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/besmpl/ember/internal/preparedworkerfixture"
)

func TestModuleRunsTypedJobAndCachesCommittedOperation(t *testing.T) {
	calls := 0
	module := newJobModule(t, &calls)
	candidate, err := module.Prepare(context.Background(), artifactFromIdentity(
		identityFor("job-generation-a"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.Activate(); err != nil {
		t.Fatal(err)
	}
	operation := Operation[preparedworkerfixture.BatchRequest]{
		Sequence:     1,
		BaseRevision: 0,
		Request: preparedworkerfixture.BatchRequest{
			Iterations: 3,
			Seed:       17,
		},
	}
	completion, err := module.Apply(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	if completion.Position != (Position{Sequence: 1, Revision: 1}) {
		t.Fatalf("completion position = %#v, want sequence/revision 1/1", completion.Position)
	}
	if completion.Result.Checksum != 90152 {
		t.Fatalf("job checksum = %d, want 90152", completion.Result.Checksum)
	}

	replayed, err := module.Apply(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	if replayed != completion || calls != 1 {
		t.Fatalf("cached completion/calls = %#v/%d, want %#v/1", replayed, calls, completion)
	}
	resolution, err := module.Resolve(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != ResolveCommitted || resolution.Completion != completion {
		t.Fatalf("resolution = %#v, want committed %#v", resolution, completion)
	}
	absent, err := module.Resolve(context.Background(), Operation[preparedworkerfixture.BatchRequest]{
		Sequence:     2,
		BaseRevision: 1,
		Request:      preparedworkerfixture.BatchRequest{Iterations: 1, Seed: 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	if absent.Status != ResolveNotCommitted {
		t.Fatalf("absent resolution = %#v, want not committed", absent)
	}
}

func TestModuleRejectsConflictGapAndRetiredGeneration(t *testing.T) {
	calls := 0
	module := newJobModule(t, &calls)
	firstCandidate, err := module.Prepare(context.Background(), artifactFromIdentity(
		identityFor("job-generation-a"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := firstCandidate.Activate(); err != nil {
		t.Fatal(err)
	}
	operation := Operation[preparedworkerfixture.BatchRequest]{
		Sequence:     1,
		BaseRevision: 0,
		Request:      preparedworkerfixture.BatchRequest{Iterations: 3, Seed: 17},
	}
	if _, err := module.Apply(context.Background(), operation); err != nil {
		t.Fatal(err)
	}

	conflict := operation
	conflict.Request.Seed++
	if _, err := module.Apply(context.Background(), conflict); !IsFailure(err, FailureConflict) {
		t.Fatalf("conflicting duplicate error = %v, want conflict", err)
	}
	gap := Operation[preparedworkerfixture.BatchRequest]{
		Sequence:     3,
		BaseRevision: 1,
		Request:      preparedworkerfixture.BatchRequest{Iterations: 1, Seed: 9},
	}
	if _, err := module.Apply(context.Background(), gap); !IsFailure(err, FailureGap) {
		t.Fatalf("gap error = %v, want gap", err)
	}
	if calls != 1 {
		t.Fatalf("rejected operations entered handler %d times, want 1 total", calls)
	}

	secondCandidate, err := module.Prepare(context.Background(), artifactFromIdentity(
		identityFor("job-generation-b"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := secondCandidate.Activate(); err != nil {
		t.Fatal(err)
	}
	next := Operation[preparedworkerfixture.BatchRequest]{
		Sequence:     2,
		BaseRevision: 1,
		Request:      preparedworkerfixture.BatchRequest{Iterations: 1, Seed: 9},
	}
	if _, err := module.Apply(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if err := firstCandidate.Activate(); !IsFailure(err, FailureStale) {
		t.Fatalf("reused candidate error = %v, want stale", err)
	}
}

func TestModuleReplaysCommittedOperationAfterGenerationLoss(t *testing.T) {
	calls := 0
	module := newJobModule(t, &calls)
	candidate, err := module.Prepare(context.Background(), artifactFromIdentity(
		identityFor("job-generation-a"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.Activate(); err != nil {
		t.Fatal(err)
	}
	committed := Operation[preparedworkerfixture.BatchRequest]{
		Sequence:     1,
		BaseRevision: 0,
		Request:      preparedworkerfixture.BatchRequest{Iterations: 3, Seed: 17},
	}
	want, err := module.Apply(context.Background(), committed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := module.Apply(context.Background(), Operation[preparedworkerfixture.BatchRequest]{
		Sequence:     2,
		BaseRevision: 1,
		Request:      preparedworkerfixture.BatchRequest{},
	}); !IsFailure(err, FailureGuest) {
		t.Fatalf("failed operation error = %v, want guest failure", err)
	}
	got, err := module.Apply(context.Background(), committed)
	if err != nil {
		t.Fatal(err)
	}
	if got != want || calls != 2 {
		t.Fatalf("replayed completion/calls = %#v/%d, want %#v/2", got, calls, want)
	}
}

type jobCheckpoint struct {
	Position Position `json:"position"`
}

type jobOutbox struct{}

type jobHandler struct {
	calls *int
}

func (handler *jobHandler) Apply(
	_ context.Context,
	operation Operation[preparedworkerfixture.BatchRequest],
) (Decision[preparedworkerfixture.BatchResult, jobCheckpoint, jobOutbox], error) {
	(*handler.calls)++
	request := operation.Request
	if request.Iterations == 0 {
		return Decision[preparedworkerfixture.BatchResult, jobCheckpoint, jobOutbox]{}, errors.New("iterations must be positive")
	}
	var checksum int64
	for iteration := uint32(1); iteration <= request.Iterations; iteration++ {
		threshold := int64(2 + (request.Seed+int64(iteration))%3)
		checksum += recursiveFibonacciWithThreshold(20, threshold) * int64(iteration%7+1)
	}
	position := Position{
		Sequence: operation.Sequence,
		Revision: operation.BaseRevision + 1,
	}
	checkpoint := jobCheckpoint{Position: position}
	return Decision[preparedworkerfixture.BatchResult, jobCheckpoint, jobOutbox]{
		Result:     preparedworkerfixture.BatchResult{Checksum: checksum},
		Checkpoint: &checkpoint,
	}, nil
}

func (*jobHandler) Close(context.Context) error { return nil }

func recursiveFibonacciWithThreshold(value, threshold int64) int64 {
	if value < threshold {
		return value
	}
	return recursiveFibonacciWithThreshold(value-1, threshold) +
		recursiveFibonacciWithThreshold(value-2, threshold)
}

func newJobModule(
	t *testing.T,
	calls *int,
) *transactionModule[
	preparedworkerfixture.BatchRequest,
	preparedworkerfixture.BatchResult,
	jobCheckpoint,
	jobOutbox,
] {
	t.Helper()
	requestCodec := jsonCodec[preparedworkerfixture.BatchRequest]{}
	resultCodec := jsonCodec[preparedworkerfixture.BatchResult]{}
	checkpointCodec := jsonCodec[jobCheckpoint]{}
	outboxCodec := jsonCodec[jobOutbox]{}
	preparer := newMemoryPreparer(
		requestCodec,
		resultCodec,
		checkpointCodec,
		outboxCodec,
		func(
			_ context.Context,
			_ Position,
			_ jobCheckpoint,
		) (Handler[preparedworkerfixture.BatchRequest, preparedworkerfixture.BatchResult, jobCheckpoint, jobOutbox], error) {
			return &jobHandler{calls: calls}, nil
		},
	)
	module, err := newTransactionModule(
		moduleConfig[
			preparedworkerfixture.BatchRequest,
			preparedworkerfixture.BatchResult,
			jobCheckpoint,
			jobOutbox,
		]{
			Stream:   identityFor("job-stream"),
			Contract: testContract("job"),
			Limits: transactionLimits{
				MaxRequestBytes:    64 << 10,
				MaxResultBytes:     64 << 10,
				MaxCheckpointBytes: 64 << 10,
				MaxOutboxItems:     64,
				MaxOutboxBytes:     64 << 10,
				MaxCandidates:      4,
				MaxRetired:         4,
			},
			RequestCodec:    requestCodec,
			ResultCodec:     resultCodec,
			CheckpointCodec: checkpointCodec,
			OutboxCodec:     outboxCodec,
			Preparer:        preparer,
			Journal:         newMemoryJournal(),
			Publisher:       newMemoryPublisher(),
		},
		initialState[jobCheckpoint]{
			Checkpoint: jobCheckpoint{},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := module.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return module
}

type jsonCodec[T any] struct{}

func testContract(name string) wireContract {
	return wireContract{
		RequestSchema:    identityFor(name + "-request-v1"),
		ResultSchema:     identityFor(name + "-result-v1"),
		CheckpointSchema: identityFor(name + "-checkpoint-v1"),
		EffectSchema:     identityFor(name + "-effect-v1"),
		CodecVersion:     1,
	}
}

func (jsonCodec[T]) Encode(value T) ([]byte, error) { return json.Marshal(value) }

func (jsonCodec[T]) Decode(data []byte) (T, error) {
	var value T
	err := json.Unmarshal(data, &value)
	return value, err
}
