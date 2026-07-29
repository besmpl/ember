package preparedworker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestJobQueueContractResolvesCommittedBatchAfterDeliveryFailure(t *testing.T) {
	fixture := newJobQueueFixture(t, 1)
	operation := jobQueueOperation(1, 0, jobQueueRequest{
		Queue: 7,
		Jobs: []jobQueueJob{
			{ID: 11, Units: 3},
			{ID: 4, Units: 5},
		},
	})
	want := Completion[jobQueueResult]{
		Position: Position{Sequence: 1, Revision: 1},
		Result: jobQueueResult{
			Queue: 7, Accepted: 2, Completed: 2, Units: 8, Cursor: 1508,
		},
	}

	completion, err := fixture.module.Apply(context.Background(), operation)
	if !IsFailure(err, FailureDelivery) {
		t.Fatalf("Apply error = %v, want delivery failure", err)
	}
	if completion != want {
		t.Fatalf("Apply completion = %#v, want %#v", completion, want)
	}
	if got := fixture.factory.applyCalls(); got != 1 {
		t.Fatalf("job handler calls = %d, want 1", got)
	}
	fixture.assertCommittedBytes(t, jobQueueRecordExpectation{
		Sequence:   1,
		Request:    jobQueueWire(t, "a1010007020000000b0003000000040005"),
		Result:     jobQueueWire(t, "b1010007000200000002000000000000000800000000000005e4"),
		Checkpoint: jobQueueWire(t, "c101000700000002000000000000000800000000000005e4"),
		Effects: [][]byte{
			jobQueueWire(t, "d1010000000b000000000000044f"),
			jobQueueWire(t, "d1010000000400000000000005e4"),
		},
	})

	resolution, err := fixture.module.Resolve(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != ResolveCommitted || resolution.Completion != want {
		t.Fatalf("Resolve = %#v, want committed %#v", resolution, want)
	}
	if got := fixture.factory.applyCalls(); got != 1 {
		t.Fatalf("job handler calls after Resolve = %d, want 1", got)
	}
	if got := fixture.publisher.attemptsCount(); got != 3 {
		t.Fatalf("publication attempts = %d, want one failed plus two recovered", got)
	}
	published := fixture.publisher.records()
	if len(published) != 2 {
		t.Fatalf("published effects = %d, want 2", len(published))
	}
	for index, wantPayload := range [][]byte{
		jobQueueWire(t, "d1010000000b000000000000044f"),
		jobQueueWire(t, "d1010000000400000000000005e4"),
	} {
		if published[index].ID.Sequence != 1 || published[index].ID.Ordinal != uint32(index+1) ||
			!bytes.Equal(published[index].Payload, wantPayload) {
			t.Fatalf("published effect %d = %#v, want sequence 1 ordinal %d payload %x", index, published[index], index+1, wantPayload)
		}
	}
	fixture.assertCommittedBytes(t, jobQueueRecordExpectation{
		Sequence:   1,
		Request:    jobQueueWire(t, "a1010007020000000b0003000000040005"),
		Result:     jobQueueWire(t, "b1010007000200000002000000000000000800000000000005e4"),
		Checkpoint: jobQueueWire(t, "c101000700000002000000000000000800000000000005e4"),
		Effects:    [][]byte{jobQueueWire(t, "d1010000000b000000000000044f"), jobQueueWire(t, "d1010000000400000000000005e4")},
		Published:  true,
	})

	absent, err := fixture.module.Resolve(context.Background(), jobQueueOperation(2, 1, jobQueueRequest{
		Queue: 7,
		Jobs:  []jobQueueJob{{ID: 8, Units: 2}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if absent.Status != ResolveNotCommitted {
		t.Fatalf("absent Resolve = %#v, want not committed", absent)
	}
}

func TestJobQueueContractReloadsFromCheckpointAndReplaysPriorBatch(t *testing.T) {
	fixture := newJobQueueFixture(t, 0)
	firstOperation := jobQueueOperation(1, 0, jobQueueRequest{
		Queue: 7,
		Jobs: []jobQueueJob{
			{ID: 11, Units: 3},
			{ID: 4, Units: 5},
		},
	})
	first, err := fixture.module.Apply(context.Background(), firstOperation)
	if err != nil {
		t.Fatal(err)
	}

	secondGeneration := identityFor("job-queue-generation-b")
	candidate, err := fixture.module.Prepare(
		context.Background(),
		artifactFromIdentity(secondGeneration),
	)
	if err != nil {
		t.Fatal(err)
	}
	restored := fixture.factory.lastRestore()
	if restored.Position != (Position{Sequence: 1, Revision: 1}) ||
		restored.Checkpoint != (jobQueueCheckpoint{Queue: 7, Completed: 2, Units: 8, Cursor: 1508}) {
		t.Fatalf("replacement restore = %#v, want 1/1 with first batch state", restored)
	}
	if err := candidate.Activate(); err != nil {
		t.Fatal(err)
	}

	secondOperation := jobQueueOperation(2, 1, jobQueueRequest{
		Queue: 7,
		Jobs:  []jobQueueJob{{ID: 8, Units: 2}},
	})
	second, err := fixture.module.Apply(context.Background(), secondOperation)
	if err != nil {
		t.Fatal(err)
	}
	wantSecond := Completion[jobQueueResult]{
		Position: Position{Sequence: 2, Revision: 2},
		Result: jobQueueResult{
			Queue: 7, Accepted: 1, Completed: 3, Units: 10, Cursor: 2310,
		},
	}
	if second != wantSecond {
		t.Fatalf("continued completion = %#v, want %#v", second, wantSecond)
	}
	fixture.assertCommittedBytes(t, jobQueueRecordExpectation{
		Sequence:   2,
		Request:    jobQueueWire(t, "a101000701000000080002"),
		Result:     jobQueueWire(t, "b1010007000100000003000000000000000a0000000000000906"),
		Checkpoint: jobQueueWire(t, "c101000700000003000000000000000a0000000000000906"),
		Effects:    [][]byte{jobQueueWire(t, "d101000000080000000000000906")},
		Published:  true,
	})

	callsBeforeReplay := fixture.factory.applyCalls()
	publicationsBeforeReplay := fixture.publisher.records()
	replayed, err := fixture.module.Apply(context.Background(), firstOperation)
	if err != nil {
		t.Fatal(err)
	}
	if replayed != first {
		t.Fatalf("replayed first completion = %#v, want %#v", replayed, first)
	}
	if got := fixture.factory.applyCalls(); got != callsBeforeReplay {
		t.Fatalf("job handler calls after old-batch replay = %d, want %d", got, callsBeforeReplay)
	}
	if got := fixture.publisher.records(); !reflect.DeepEqual(got, publicationsBeforeReplay) {
		t.Fatalf("publications after old-batch replay = %#v, want unchanged %#v", got, publicationsBeforeReplay)
	}

	resolution, err := fixture.module.Resolve(context.Background(), secondOperation)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Status != ResolveCommitted || resolution.Completion != wantSecond {
		t.Fatalf("Resolve after reload = %#v, want committed %#v", resolution, wantSecond)
	}
}

type jobQueueJob struct {
	ID    uint32
	Units uint16
}

type jobQueueRequest struct {
	Queue uint16
	Jobs  []jobQueueJob
}

type jobQueueResult struct {
	Queue     uint16
	Accepted  uint16
	Completed uint32
	Units     uint64
	Cursor    uint64
}

type jobQueueCheckpoint struct {
	Queue     uint16
	Completed uint32
	Units     uint64
	Cursor    uint64
}

type jobQueueEffect struct {
	JobID  uint32
	Ticket uint64
}

type jobQueueHandler struct {
	factory *jobQueueFactory
	state   jobQueueCheckpoint
}

func (handler *jobQueueHandler) Apply(
	ctx context.Context,
	operation Operation[jobQueueRequest],
) (Decision[jobQueueResult, jobQueueCheckpoint, jobQueueEffect], error) {
	if err := ctx.Err(); err != nil {
		return Decision[jobQueueResult, jobQueueCheckpoint, jobQueueEffect]{}, err
	}
	handler.factory.recordApply()
	request := operation.Request
	if request.Queue == 0 || len(request.Jobs) == 0 {
		return Decision[jobQueueResult, jobQueueCheckpoint, jobQueueEffect]{}, fmt.Errorf("queue and jobs are required")
	}
	if handler.state.Queue != 0 && request.Queue != handler.state.Queue {
		return Decision[jobQueueResult, jobQueueCheckpoint, jobQueueEffect]{}, fmt.Errorf("queue %d differs from restored queue %d", request.Queue, handler.state.Queue)
	}
	if handler.state.Queue == 0 {
		handler.state.Queue = request.Queue
	}
	effects := make([]jobQueueEffect, 0, len(request.Jobs))
	for _, job := range request.Jobs {
		if job.ID == 0 || job.Units == 0 {
			return Decision[jobQueueResult, jobQueueCheckpoint, jobQueueEffect]{}, fmt.Errorf("job ID and units are required")
		}
		handler.state.Completed++
		handler.state.Units += uint64(job.Units)
		handler.state.Cursor += uint64(job.ID)*100 + uint64(job.Units)
		effects = append(effects, jobQueueEffect{JobID: job.ID, Ticket: handler.state.Cursor})
	}
	checkpoint := handler.state
	return Decision[jobQueueResult, jobQueueCheckpoint, jobQueueEffect]{
		Result: jobQueueResult{
			Queue:     handler.state.Queue,
			Accepted:  uint16(len(request.Jobs)),
			Completed: handler.state.Completed,
			Units:     handler.state.Units,
			Cursor:    handler.state.Cursor,
		},
		Checkpoint: &checkpoint,
		Effects:    effects,
	}, nil
}

func (*jobQueueHandler) Close(context.Context) error { return nil }

type jobQueueRestore struct {
	Position   Position
	Checkpoint jobQueueCheckpoint
}

type jobQueueFactory struct {
	mu       sync.Mutex
	calls    int
	restores []jobQueueRestore
}

func (factory *jobQueueFactory) prepare(
	ctx context.Context,
	position Position,
	checkpoint jobQueueCheckpoint,
) (Handler[jobQueueRequest, jobQueueResult, jobQueueCheckpoint, jobQueueEffect], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	factory.mu.Lock()
	factory.restores = append(factory.restores, jobQueueRestore{
		Position: position, Checkpoint: checkpoint,
	})
	factory.mu.Unlock()
	return &jobQueueHandler{factory: factory, state: checkpoint}, nil
}

func (factory *jobQueueFactory) recordApply() {
	factory.mu.Lock()
	factory.calls++
	factory.mu.Unlock()
}

func (factory *jobQueueFactory) applyCalls() int {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return factory.calls
}

func (factory *jobQueueFactory) lastRestore() jobQueueRestore {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	return factory.restores[len(factory.restores)-1]
}

type jobQueuePublisher struct {
	mu       sync.Mutex
	failures int
	attempts int
	next     *memoryPublisher
}

func (publisher *jobQueuePublisher) Publish(ctx context.Context, record outboxRecord) error {
	publisher.mu.Lock()
	publisher.attempts++
	if publisher.failures > 0 {
		publisher.failures--
		publisher.mu.Unlock()
		return fmt.Errorf("injected job publication failure")
	}
	publisher.mu.Unlock()
	return publisher.next.Publish(ctx, record)
}

func (publisher *jobQueuePublisher) attemptsCount() int {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	return publisher.attempts
}

func (publisher *jobQueuePublisher) records() []outboxRecord {
	return publisher.next.Records()
}

type jobQueueFixture struct {
	module *transactionModule[jobQueueRequest, jobQueueResult, jobQueueCheckpoint, jobQueueEffect]

	stream          contentIdentity
	firstGeneration contentIdentity
	journal         *memoryJournal
	publisher       *jobQueuePublisher
	factory         *jobQueueFactory
}

func newJobQueueFixture(t *testing.T, publicationFailures int) *jobQueueFixture {
	t.Helper()
	factory := &jobQueueFactory{}
	preparer := newMemoryPreparer(
		jobQueueRequestCodec{},
		jobQueueResultCodec{},
		jobQueueCheckpointCodec{},
		jobQueueEffectCodec{},
		factory.prepare,
	)
	stream := identityFor("job-queue-stream")
	journal := newMemoryJournal()
	publisher := &jobQueuePublisher{
		failures: publicationFailures,
		next:     newMemoryPublisher(),
	}
	module, err := newTransactionModule(
		moduleConfig[jobQueueRequest, jobQueueResult, jobQueueCheckpoint, jobQueueEffect]{
			Stream:   stream,
			Contract: testContract("job-queue-binary"),
			Limits: transactionLimits{
				MaxRequestBytes:    4 << 10,
				MaxResultBytes:     256,
				MaxCheckpointBytes: 256,
				MaxOutboxItems:     64,
				MaxOutboxBytes:     4 << 10,
				MaxCandidates:      2,
				MaxRetired:         2,
			},
			RequestCodec:    jobQueueRequestCodec{},
			ResultCodec:     jobQueueResultCodec{},
			CheckpointCodec: jobQueueCheckpointCodec{},
			OutboxCodec:     jobQueueEffectCodec{},
			Preparer:        preparer,
			Journal:         journal,
			Publisher:       publisher,
		},
		initialState[jobQueueCheckpoint]{},
	)
	if err != nil {
		t.Fatal(err)
	}
	firstGeneration := identityFor("job-queue-generation-a")
	candidate, err := module.Prepare(
		context.Background(),
		artifactFromIdentity(firstGeneration),
	)
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
	return &jobQueueFixture{
		module:          module,
		stream:          stream,
		firstGeneration: firstGeneration,
		journal:         journal,
		publisher:       publisher,
		factory:         factory,
	}
}

type jobQueueRecordExpectation struct {
	Sequence   uint64
	Request    []byte
	Result     []byte
	Checkpoint []byte
	Effects    [][]byte
	Published  bool
}

func (fixture *jobQueueFixture) assertCommittedBytes(t *testing.T, want jobQueueRecordExpectation) {
	t.Helper()
	record, ok, err := fixture.journal.Lookup(context.Background(), fixture.stream, want.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("committed job batch %d is missing", want.Sequence)
	}
	if !bytes.Equal(record.Request, want.Request) || !bytes.Equal(record.Result, want.Result) {
		t.Fatalf("canonical request/result = %x/%x, want %x/%x", record.Request, record.Result, want.Request, want.Result)
	}
	if record.Checkpoint == nil || !record.Quiescent || !bytes.Equal(record.Checkpoint.Payload, want.Checkpoint) {
		t.Fatalf("canonical checkpoint = %#v, quiescent %v; want %x/true", record.Checkpoint, record.Quiescent, want.Checkpoint)
	}
	if len(record.Outbox) != len(want.Effects) {
		t.Fatalf("outbox items = %d, want %d", len(record.Outbox), len(want.Effects))
	}
	for index, effect := range record.Outbox {
		if effect.ID.Sequence != want.Sequence || effect.ID.Ordinal != uint32(index+1) ||
			effect.Published != want.Published || !bytes.Equal(effect.Payload, want.Effects[index]) {
			t.Fatalf("outbox item %d = %#v, want sequence %d ordinal %d published %v payload %x", index, effect, want.Sequence, index+1, want.Published, want.Effects[index])
		}
	}
}

func jobQueueOperation(
	sequence uint64,
	baseRevision uint64,
	request jobQueueRequest,
) Operation[jobQueueRequest] {
	return Operation[jobQueueRequest]{
		Sequence: sequence, BaseRevision: baseRevision, Request: request,
	}
}

func jobQueueWire(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

type jobQueueRequestCodec struct{}

func (jobQueueRequestCodec) Encode(request jobQueueRequest) ([]byte, error) {
	if len(request.Jobs) > 255 {
		return nil, fmt.Errorf("job count %d exceeds wire limit", len(request.Jobs))
	}
	encoded := make([]byte, 0, 5+len(request.Jobs)*6)
	encoded = append(encoded, 0xa1, 1)
	encoded = appendJobQueueUint16(encoded, request.Queue)
	encoded = append(encoded, byte(len(request.Jobs)))
	for _, job := range request.Jobs {
		encoded = appendJobQueueUint32(encoded, job.ID)
		encoded = appendJobQueueUint16(encoded, job.Units)
	}
	return encoded, nil
}

func (jobQueueRequestCodec) Decode(encoded []byte) (jobQueueRequest, error) {
	if len(encoded) < 5 || encoded[0] != 0xa1 || encoded[1] != 1 {
		return jobQueueRequest{}, fmt.Errorf("invalid job request header")
	}
	count := int(encoded[4])
	if len(encoded) != 5+count*6 {
		return jobQueueRequest{}, fmt.Errorf("job request length %d does not match count %d", len(encoded), count)
	}
	request := jobQueueRequest{
		Queue: binary.BigEndian.Uint16(encoded[2:4]),
		Jobs:  make([]jobQueueJob, count),
	}
	offset := 5
	for index := range request.Jobs {
		request.Jobs[index] = jobQueueJob{
			ID:    binary.BigEndian.Uint32(encoded[offset : offset+4]),
			Units: binary.BigEndian.Uint16(encoded[offset+4 : offset+6]),
		}
		offset += 6
	}
	return request, nil
}

type jobQueueResultCodec struct{}

func (jobQueueResultCodec) Encode(result jobQueueResult) ([]byte, error) {
	encoded := []byte{0xb1, 1}
	encoded = appendJobQueueUint16(encoded, result.Queue)
	encoded = appendJobQueueUint16(encoded, result.Accepted)
	encoded = appendJobQueueUint32(encoded, result.Completed)
	encoded = appendJobQueueUint64(encoded, result.Units)
	encoded = appendJobQueueUint64(encoded, result.Cursor)
	return encoded, nil
}

func (jobQueueResultCodec) Decode(encoded []byte) (jobQueueResult, error) {
	if len(encoded) != 26 || encoded[0] != 0xb1 || encoded[1] != 1 {
		return jobQueueResult{}, fmt.Errorf("invalid job result record")
	}
	return jobQueueResult{
		Queue:     binary.BigEndian.Uint16(encoded[2:4]),
		Accepted:  binary.BigEndian.Uint16(encoded[4:6]),
		Completed: binary.BigEndian.Uint32(encoded[6:10]),
		Units:     binary.BigEndian.Uint64(encoded[10:18]),
		Cursor:    binary.BigEndian.Uint64(encoded[18:26]),
	}, nil
}

type jobQueueCheckpointCodec struct{}

func (jobQueueCheckpointCodec) Encode(checkpoint jobQueueCheckpoint) ([]byte, error) {
	encoded := []byte{0xc1, 1}
	encoded = appendJobQueueUint16(encoded, checkpoint.Queue)
	encoded = appendJobQueueUint32(encoded, checkpoint.Completed)
	encoded = appendJobQueueUint64(encoded, checkpoint.Units)
	encoded = appendJobQueueUint64(encoded, checkpoint.Cursor)
	return encoded, nil
}

func (jobQueueCheckpointCodec) Decode(encoded []byte) (jobQueueCheckpoint, error) {
	if len(encoded) != 24 || encoded[0] != 0xc1 || encoded[1] != 1 {
		return jobQueueCheckpoint{}, fmt.Errorf("invalid job checkpoint record")
	}
	return jobQueueCheckpoint{
		Queue:     binary.BigEndian.Uint16(encoded[2:4]),
		Completed: binary.BigEndian.Uint32(encoded[4:8]),
		Units:     binary.BigEndian.Uint64(encoded[8:16]),
		Cursor:    binary.BigEndian.Uint64(encoded[16:24]),
	}, nil
}

type jobQueueEffectCodec struct{}

func (jobQueueEffectCodec) Encode(effect jobQueueEffect) ([]byte, error) {
	encoded := []byte{0xd1, 1}
	encoded = appendJobQueueUint32(encoded, effect.JobID)
	encoded = appendJobQueueUint64(encoded, effect.Ticket)
	return encoded, nil
}

func (jobQueueEffectCodec) Decode(encoded []byte) (jobQueueEffect, error) {
	if len(encoded) != 14 || encoded[0] != 0xd1 || encoded[1] != 1 {
		return jobQueueEffect{}, fmt.Errorf("invalid job effect record")
	}
	return jobQueueEffect{
		JobID:  binary.BigEndian.Uint32(encoded[2:6]),
		Ticket: binary.BigEndian.Uint64(encoded[6:14]),
	}, nil
}

func appendJobQueueUint16(encoded []byte, value uint16) []byte {
	var field [2]byte
	binary.BigEndian.PutUint16(field[:], value)
	return append(encoded, field[:]...)
}

func appendJobQueueUint32(encoded []byte, value uint32) []byte {
	var field [4]byte
	binary.BigEndian.PutUint32(field[:], value)
	return append(encoded, field[:]...)
}

func appendJobQueueUint64(encoded []byte, value uint64) []byte {
	var field [8]byte
	binary.BigEndian.PutUint64(field[:], value)
	return append(encoded, field[:]...)
}
