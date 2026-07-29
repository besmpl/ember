package preparedworker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/besmpl/ember/internal/preparedworkerfixture"
	"github.com/besmpl/ember/internal/preparedworkerprobe"
	"github.com/besmpl/ember/preparedworker"
)

func TestRichGameContractRestoresLatestQuiescentAndFollowsToNextSafePoint(t *testing.T) {
	fixture := newRichGameFixture(t)

	firstRequest := richGameFirstRequest()
	first, err := fixture.module.Apply(context.Background(), richGameOperation(firstRequest))
	if err != nil {
		t.Fatal(err)
	}
	firstResult := richGameFirstResult()
	if !reflect.DeepEqual(first.Result, firstResult) {
		t.Fatalf("first result = %#v, want %#v", first.Result, firstResult)
	}
	if len(first.Result.Pending) != 1 {
		t.Fatalf("first pending = %v, want one retained callback continuation", first.Result.Pending)
	}
	candidate, err := fixture.module.Prepare(context.Background(), preparedworker.ExportArtifactFromIdentity(
		preparedworker.ExportIdentityFor("rich-game-generation-b"),
	))
	if err != nil {
		t.Fatalf("Prepare from latest quiescent checkpoint: %v", err)
	}
	if restored := fixture.factory.lastCheckpoint(); restored != (preparedworkerfixture.Checkpoint{}) {
		t.Fatalf("initial candidate restore = %#v, want initial quiescent checkpoint", restored)
	}
	if err := activateRichGameCandidateAfterFollowing(candidate); !preparedworker.IsFailure(err, preparedworker.FailureNotQuiescent) {
		t.Fatalf("Activate at nonquiescent head = %v, want not-quiescent", err)
	}

	secondRequest := richGameSecondRequest(firstResult.State)
	second, err := fixture.module.Apply(context.Background(), richGameOperation(secondRequest))
	if err != nil {
		t.Fatal(err)
	}
	secondResult := richGameSecondResult()
	if !reflect.DeepEqual(second.Result, secondResult) {
		t.Fatalf("second result = %#v, want %#v", second.Result, secondResult)
	}
	if len(second.Result.Pending) != 0 {
		t.Fatalf("second pending = %v, want detached quiescent state", second.Result.Pending)
	}
	wantCheckpoint, err := preparedworkerprobe.QuiescentCheckpoint(secondResult)
	if err != nil {
		t.Fatal(err)
	}

	if err := activateRichGameCandidateAfterFollowing(candidate); err != nil {
		t.Fatalf("Activate after following quiescent head: %v", err)
	}
	record, ok, err := fixture.journal.Lookup(context.Background(), fixture.stream, 2)
	if err != nil || !ok {
		t.Fatalf("quiescent record = present %v, error %v", ok, err)
	}
	wantCheckpointBytes, err := json.Marshal(wantCheckpoint)
	if err != nil {
		t.Fatal(err)
	}
	if record.Checkpoint == nil || !bytes.Equal(record.Checkpoint.Payload, wantCheckpointBytes) {
		t.Fatalf("quiescent checkpoint = %#v, want %s", record.Checkpoint, wantCheckpointBytes)
	}

	thirdRequest := richGameThirdRequest(secondResult.State)
	third, err := fixture.module.Apply(context.Background(), richGameOperation(thirdRequest))
	if err != nil {
		t.Fatal(err)
	}
	thirdResult := richGameThirdResult()
	if !reflect.DeepEqual(third.Result, thirdResult) {
		t.Fatalf("continued result = %#v, want %#v", third.Result, thirdResult)
	}
	fixture.assertCommittedBytes(t, 3, thirdResult)
}

func TestRichGameContractDuplicateReplaysWithoutHandlerOrEffectReentry(t *testing.T) {
	fixture := newRichGameFixture(t)
	requests := []preparedworkerfixture.TurnRequest{
		richGameFirstRequest(),
		richGameSecondRequest(richGameFirstResult().State),
	}
	var completion preparedworker.Completion[preparedworkerfixture.TurnResult]
	for _, request := range requests {
		var err error
		completion, err = fixture.module.Apply(context.Background(), richGameOperation(request))
		if err != nil {
			t.Fatal(err)
		}
	}
	callsBefore := fixture.factory.applyCalls()
	publicationsBefore := fixture.publisher.records()

	replayed, err := fixture.module.Apply(context.Background(), richGameOperation(requests[1]))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replayed, completion) {
		t.Fatalf("replayed completion = %#v, want %#v", replayed, completion)
	}
	if got := fixture.factory.applyCalls(); got != callsBefore {
		t.Fatalf("game handler calls after replay = %d, want unchanged %d", got, callsBefore)
	}
	if got := fixture.publisher.records(); !reflect.DeepEqual(got, publicationsBefore) {
		t.Fatalf("publications after replay = %#v, want unchanged %#v", got, publicationsBefore)
	}
}

type richGameHandler struct {
	handler *preparedworkerprobe.Handler
	factory *richGameFactory
}

func (handler *richGameHandler) Apply(
	ctx context.Context,
	operation preparedworker.Operation[preparedworkerfixture.TurnRequest],
) (preparedworker.Decision[
	preparedworkerfixture.TurnResult,
	preparedworkerfixture.Checkpoint,
	preparedworkerfixture.Effect,
], error) {
	handler.factory.recordApply()
	result, err := handler.handler.Transact(ctx, operation.Request)
	if err != nil {
		return preparedworker.Decision[
			preparedworkerfixture.TurnResult,
			preparedworkerfixture.Checkpoint,
			preparedworkerfixture.Effect,
		]{}, err
	}
	decision := preparedworker.Decision[
		preparedworkerfixture.TurnResult,
		preparedworkerfixture.Checkpoint,
		preparedworkerfixture.Effect,
	]{
		Result:  result,
		Effects: append([]preparedworkerfixture.Effect(nil), result.Effects...),
	}
	checkpoint, checkpointErr := preparedworkerprobe.QuiescentCheckpoint(result)
	if checkpointErr == nil {
		decision.Checkpoint = &checkpoint
	}
	return decision, nil
}

func (handler *richGameHandler) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return handler.handler.Close()
}

type richGameFactory struct {
	mu          sync.Mutex
	checkpoints []preparedworkerfixture.Checkpoint
	calls       int
}

func (factory *richGameFactory) prepare(
	_ context.Context,
	_ preparedworker.Position,
	checkpoint preparedworkerfixture.Checkpoint,
) (preparedworker.Handler[
	preparedworkerfixture.TurnRequest,
	preparedworkerfixture.TurnResult,
	preparedworkerfixture.Checkpoint,
	preparedworkerfixture.Effect,
], error) {
	handler, err := preparedworkerprobe.NewHandlerAt(checkpoint)
	if err != nil {
		return nil, err
	}
	factory.mu.Lock()
	factory.checkpoints = append(factory.checkpoints, checkpoint)
	factory.mu.Unlock()
	return &richGameHandler{handler: handler, factory: factory}, nil
}

func (factory *richGameFactory) recordApply() {
	factory.mu.Lock()
	factory.calls++
	factory.mu.Unlock()
}

func (factory *richGameFactory) applyCalls() int {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return factory.calls
}

func (factory *richGameFactory) lastCheckpoint() preparedworkerfixture.Checkpoint {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return factory.checkpoints[len(factory.checkpoints)-1]
}

type richGamePublisher struct {
	mu           sync.Mutex
	recordsValue []preparedworker.TestOutboxRecord
	next         preparedworker.TestEffectPublisher
}

func (publisher *richGamePublisher) Publish(ctx context.Context, record preparedworker.TestOutboxRecord) error {
	if err := publisher.next.Publish(ctx, record); err != nil {
		return err
	}
	publisher.mu.Lock()
	cloned := record
	cloned.Payload = append([]byte(nil), record.Payload...)
	publisher.recordsValue = append(publisher.recordsValue, cloned)
	publisher.mu.Unlock()
	return nil
}

func (publisher *richGamePublisher) records() []preparedworker.TestOutboxRecord {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	records := make([]preparedworker.TestOutboxRecord, len(publisher.recordsValue))
	for index, record := range publisher.recordsValue {
		records[index] = record
		records[index].Payload = append([]byte(nil), record.Payload...)
	}
	return records
}

type richGameFixture struct {
	module *preparedworker.TestTransactionModule[
		preparedworkerfixture.TurnRequest,
		preparedworkerfixture.TurnResult,
		preparedworkerfixture.Checkpoint,
		preparedworkerfixture.Effect,
	]
	stream    preparedworker.TestIdentity
	journal   preparedworker.TestTransactionJournal
	publisher *richGamePublisher
	factory   *richGameFactory
}

func newRichGameFixture(t *testing.T) *richGameFixture {
	t.Helper()
	requestCodec := richGameJSONCodec[preparedworkerfixture.TurnRequest]{}
	resultCodec := richGameJSONCodec[preparedworkerfixture.TurnResult]{}
	checkpointCodec := richGameJSONCodec[preparedworkerfixture.Checkpoint]{}
	effectCodec := richGameJSONCodec[preparedworkerfixture.Effect]{}
	factory := &richGameFactory{}
	preparer := preparedworker.NewTestMemoryPreparer(
		requestCodec,
		resultCodec,
		checkpointCodec,
		effectCodec,
		factory.prepare,
	)
	stream := preparedworker.ExportIdentityFor("rich-game-stream")
	journal := preparedworker.NewTestMemoryJournal()
	publisher := &richGamePublisher{next: preparedworker.NewTestMemoryPublisher()}
	module, err := preparedworker.NewTestTransactionModule(
		preparedworker.TestModuleConfig[
			preparedworkerfixture.TurnRequest,
			preparedworkerfixture.TurnResult,
			preparedworkerfixture.Checkpoint,
			preparedworkerfixture.Effect,
		]{
			Stream:   stream,
			Contract: richGameTestContract("rich-game"),
			Limits: preparedworker.TestTransactionLimits{
				MaxRequestBytes:    64 << 10,
				MaxResultBytes:     64 << 10,
				MaxCheckpointBytes: 64 << 10,
				MaxOutboxItems:     64,
				MaxOutboxBytes:     64 << 10,
				MaxCandidates:      2,
				MaxRetired:         2,
			},
			RequestCodec:    requestCodec,
			ResultCodec:     resultCodec,
			CheckpointCodec: checkpointCodec,
			OutboxCodec:     effectCodec,
			Preparer:        preparer,
			Journal:         journal,
			Publisher:       publisher,
		},
		preparedworker.TestInitialState[preparedworkerfixture.Checkpoint]{},
	)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := module.Prepare(context.Background(), preparedworker.ExportArtifactFromIdentity(
		preparedworker.ExportIdentityFor("rich-game-generation-a"),
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
	return &richGameFixture{
		module:    module,
		stream:    stream,
		journal:   journal,
		publisher: publisher,
		factory:   factory,
	}
}

func activateRichGameCandidateAfterFollowing(candidate *preparedworker.Candidate) error {
	deadline := time.Now().Add(time.Second)
	for {
		err := candidate.Activate()
		if !preparedworker.IsFailure(err, preparedworker.FailureBehind) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}

func richGameTestContract(name string) preparedworker.TestWireContract {
	return preparedworker.TestWireContract{
		RequestSchema:    preparedworker.ExportIdentityFor(name + "-request-v1"),
		ResultSchema:     preparedworker.ExportIdentityFor(name + "-result-v1"),
		CheckpointSchema: preparedworker.ExportIdentityFor(name + "-checkpoint-v1"),
		EffectSchema:     preparedworker.ExportIdentityFor(name + "-effect-v1"),
		CodecVersion:     1,
	}
}

func (fixture *richGameFixture) assertCommittedBytes(
	t *testing.T,
	sequence uint64,
	want preparedworkerfixture.TurnResult,
) {
	t.Helper()
	record, ok, err := fixture.journal.Lookup(context.Background(), fixture.stream, sequence)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("committed turn %d is missing", sequence)
	}
	wantResultBytes, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(record.Result, wantResultBytes) {
		t.Fatalf("committed result bytes = %q, want %q", record.Result, wantResultBytes)
	}
	if len(record.Outbox) != len(want.Effects) {
		t.Fatalf("committed effects = %d, want %d", len(record.Outbox), len(want.Effects))
	}
	for index, effect := range want.Effects {
		wantEffectBytes, err := json.Marshal(effect)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(record.Outbox[index].Payload, wantEffectBytes) {
			t.Fatalf("effect %d bytes = %q, want %q", index, record.Outbox[index].Payload, wantEffectBytes)
		}
	}
}

func richGameOperation(request preparedworkerfixture.TurnRequest) preparedworker.Operation[preparedworkerfixture.TurnRequest] {
	return preparedworker.Operation[preparedworkerfixture.TurnRequest]{
		Sequence:     request.Sequence,
		BaseRevision: request.Revision,
		Request:      request,
	}
}

func richGameFirstRequest() preparedworkerfixture.TurnRequest {
	return preparedworkerfixture.TurnRequest{
		Sequence:   1,
		Revision:   0,
		Projection: preparedworkerfixture.Projection{Step: 1, Seed: 5, Work: 1},
		Events: []preparedworkerfixture.DamageEvent{{
			Route: preparedworkerfixture.RouteDamage, Entity: 7, Amount: 5,
		}},
	}
}

func richGameSecondRequest(state preparedworkerfixture.StateSnapshot) preparedworkerfixture.TurnRequest {
	return preparedworkerfixture.TurnRequest{
		Sequence:   2,
		Revision:   1,
		State:      state,
		Projection: preparedworkerfixture.Projection{Step: 1, Seed: 6, Work: 1},
		Completions: []preparedworkerfixture.Completion{{
			EffectID: richGameEffectID(1, 1), Status: preparedworkerfixture.CompletionOK, Value: 11,
		}},
	}
}

func richGameThirdRequest(state preparedworkerfixture.StateSnapshot) preparedworkerfixture.TurnRequest {
	return preparedworkerfixture.TurnRequest{
		Sequence:   3,
		Revision:   2,
		State:      state,
		Projection: preparedworkerfixture.Projection{Step: 1, Seed: 4, Work: 1},
		Events: []preparedworkerfixture.DamageEvent{{
			Route: preparedworkerfixture.RouteDamage, Entity: 7, Amount: 4,
		}},
	}
}

func richGameFirstResult() preparedworkerfixture.TurnResult {
	return preparedworkerfixture.TurnResult{
		Sequence: 1,
		Revision: 1,
		State: preparedworkerfixture.StateSnapshot{
			Tick: 1, Total: 6, ModuleCalls: 1,
		},
		Commands: []preparedworkerfixture.Command{{
			Kind: preparedworkerfixture.CommandDraw, Entity: 0, A: 1, B: 6,
		}},
		Effects: []preparedworkerfixture.Effect{
			{ID: richGameEffectID(1, 1), Kind: preparedworkerfixture.EffectLoad, Entity: 7, NeedsCompletion: true},
			{ID: richGameEffectID(1, 2), Kind: preparedworkerfixture.EffectAudio, Entity: 0, Value: 5},
		},
		Pending: []uint64{richGameEffectID(1, 1)},
	}
}

func richGameSecondResult() preparedworkerfixture.TurnResult {
	return preparedworkerfixture.TurnResult{
		Sequence: 2,
		Revision: 2,
		State: preparedworkerfixture.StateSnapshot{
			Tick: 2, Total: 34, Ready: 11, Entity7: 5, ModuleCalls: 2,
		},
		Commands: []preparedworkerfixture.Command{
			{Kind: preparedworkerfixture.CommandFlash, Entity: 7, A: 5, B: 19},
			{Kind: preparedworkerfixture.CommandDraw, Entity: 0, A: 2, B: 34},
		},
		Effects: []preparedworkerfixture.Effect{{
			ID: richGameEffectID(2, 1), Kind: preparedworkerfixture.EffectAudio, Entity: 0, Value: 8,
		}},
	}
}

func richGameThirdResult() preparedworkerfixture.TurnResult {
	return preparedworkerfixture.TurnResult{
		Sequence: 3,
		Revision: 3,
		State: preparedworkerfixture.StateSnapshot{
			Tick: 3, Total: 38, Ready: 11, Entity7: 5, ModuleCalls: 3,
		},
		Commands: []preparedworkerfixture.Command{{
			Kind: preparedworkerfixture.CommandDraw, Entity: 0, A: 3, B: 38,
		}},
		Effects: []preparedworkerfixture.Effect{
			{ID: richGameEffectID(3, 1), Kind: preparedworkerfixture.EffectLoad, Entity: 7, NeedsCompletion: true},
			{ID: richGameEffectID(3, 2), Kind: preparedworkerfixture.EffectAudio, Entity: 0, Value: 3},
		},
		Pending: []uint64{richGameEffectID(3, 1)},
	}
}

func richGameEffectID(sequence, ordinal uint64) uint64 {
	return sequence<<8 | ordinal
}

type richGameJSONCodec[T any] struct{}

func (richGameJSONCodec[T]) Encode(value T) ([]byte, error) { return json.Marshal(value) }

func (richGameJSONCodec[T]) Decode(data []byte) (T, error) {
	var value T
	err := json.Unmarshal(data, &value)
	return value, err
}
