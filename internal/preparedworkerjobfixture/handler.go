package preparedworkerjobfixture

import (
	"context"
	"fmt"
	"math"

	"github.com/besmpl/ember"
	preparedworkerfixturegenerated "github.com/besmpl/ember/internal/preparedworkerfixture/generated"
	"github.com/besmpl/ember/preparedworker"
)

const (
	maximumSeed       = uint16(32)
	maximumWork       = uint16(256)
	checkpointFormat1 = uint8(1)
	checkpointFormat2 = uint8(2)
)

// NewHandler restores the typed embedded static-AOT job handler.
func NewHandler(
	ctx context.Context,
	restore preparedworker.Restore[Checkpoint],
) (preparedworker.Handler[Request, Result, Checkpoint, Effect], error) {
	return NewHandlerV2(ctx, restore)
}

// NewHandlerV1 restores the first durable checkpoint-envelope format.
func NewHandlerV1(
	ctx context.Context,
	restore preparedworker.Restore[Checkpoint],
) (preparedworker.Handler[Request, Result, Checkpoint, Effect], error) {
	return newHandler(ctx, restore, checkpointFormat1)
}

// NewHandlerV2 migrates format-1 checkpoints to the current format before the
// worker is admitted. The surrounding contract and codec remain unchanged.
func NewHandlerV2(
	ctx context.Context,
	restore preparedworker.Restore[Checkpoint],
) (preparedworker.Handler[Request, Result, Checkpoint, Effect], error) {
	return newHandler(ctx, restore, checkpointFormat2)
}

func newHandler(
	ctx context.Context,
	restore preparedworker.Restore[Checkpoint],
	targetFormat uint8,
) (preparedworker.Handler[Request, Result, Checkpoint, Effect], error) {
	position := restore.Position
	checkpoint := restore.Checkpoint
	if checkpoint.Sequence != position.Sequence || checkpoint.Revision != position.Revision {
		return nil, fmt.Errorf("prepared job fixture: checkpoint position differs from restore point")
	}
	if err := validateCheckpoint(checkpoint); err != nil {
		return nil, err
	}
	if checkpoint.Format > targetFormat {
		return nil, fmt.Errorf(
			"prepared job fixture: checkpoint format %d cannot be restored by worker format %d",
			checkpoint.Format,
			targetFormat,
		)
	}
	checkpoint.Format = targetFormat
	program, _, err := preparedworkerfixturegenerated.LoadProgram(ctx, ember.ProgramOptions{Parallelism: 1})
	if err != nil {
		return nil, fmt.Errorf("prepared job fixture: load static program: %w", err)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{Prepared: preparedworkerfixturegenerated.Bundle})
	if err != nil {
		return nil, fmt.Errorf("prepared job fixture: bind static bundle: %w", err)
	}
	return &handler{runtime: runtime, state: checkpoint}, nil
}

type handler struct {
	runtime *ember.Runtime
	state   Checkpoint
}

func (handler *handler) Apply(
	ctx context.Context,
	operation preparedworker.Operation[Request],
) (preparedworker.Decision[Result, Checkpoint, Effect], error) {
	if handler == nil || handler.runtime == nil {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf("prepared job fixture: closed handler")
	}
	if err := ctx.Err(); err != nil {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, err
	}
	if handler.state.Sequence == math.MaxUint64 || handler.state.Revision == math.MaxUint64 {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf("prepared job fixture: position exhausted")
	}
	if operation.Sequence != handler.state.Sequence+1 || operation.BaseRevision != handler.state.Revision {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf(
			"prepared job fixture: operation %d/%d does not follow %d/%d",
			operation.Sequence, operation.BaseRevision, handler.state.Sequence, handler.state.Revision,
		)
	}
	request := operation.Request
	if request.Queue == 0 || len(request.Jobs) == 0 {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf("prepared job fixture: queue and jobs are required")
	}
	if handler.state.Queue != 0 && request.Queue != handler.state.Queue {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf(
			"prepared job fixture: queue %d differs from restored queue %d",
			request.Queue, handler.state.Queue,
		)
	}
	if len(request.Jobs) > math.MaxUint16 {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf("prepared job fixture: too many jobs")
	}

	next := handler.state
	if next.Queue == 0 {
		next.Queue = request.Queue
	}
	effects := make([]Effect, 0, len(request.Jobs))
	for _, job := range request.Jobs {
		checksum, err := handler.runJob(ctx, job)
		if err != nil {
			return preparedworker.Decision[Result, Checkpoint, Effect]{}, err
		}
		if next.Completed == math.MaxUint32 || checksum > math.MaxInt64-next.Checksum {
			return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf("prepared job fixture: checkpoint totals exhausted")
		}
		delta := uint64(job.ID)*1000 + uint64(checksum)
		if delta < uint64(checksum) || next.Cursor > math.MaxUint64-delta {
			return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf("prepared job fixture: ticket space exhausted")
		}
		next.Completed++
		next.Checksum += checksum
		next.Cursor += delta
		effects = append(effects, Effect{JobID: job.ID, Ticket: next.Cursor, Checksum: checksum})
	}
	next.Sequence = operation.Sequence
	next.Revision = operation.BaseRevision + 1
	handler.state = next
	checkpoint := next
	return preparedworker.Decision[Result, Checkpoint, Effect]{
		Result: Result{
			Queue: next.Queue, Accepted: uint16(len(request.Jobs)), Completed: next.Completed,
			Checksum: next.Checksum, Cursor: next.Cursor,
		},
		Checkpoint: &checkpoint, Effects: effects,
	}, nil
}

func (handler *handler) runJob(ctx context.Context, job Job) (int64, error) {
	if job.ID == 0 || job.Seed == 0 || job.Seed > maximumSeed || job.Work == 0 || job.Work > maximumWork {
		return 0, fmt.Errorf("prepared job fixture: invalid job %d", job.ID)
	}
	values, err := handler.runtime.Invoke(
		ctx,
		ember.Invocation{Module: ember.LogicalModule("prepared-worker/numeric")},
		ember.NumberValue(float64(job.Seed)),
		ember.NumberValue(float64(job.Work)),
	)
	if err != nil {
		return 0, fmt.Errorf("prepared job fixture: run static job %d: %w", job.ID, err)
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("prepared job fixture: job %d returned %d values", job.ID, len(values))
	}
	number, ok := values[0].Number()
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > math.MaxInt64 || math.Trunc(number) != number {
		return 0, fmt.Errorf("prepared job fixture: job %d returned a non-integer checksum", job.ID)
	}
	return int64(number), nil
}

func (handler *handler) Close(ctx context.Context) error {
	if handler == nil || handler.runtime == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := handler.runtime.Close()
	if err == nil {
		handler.runtime = nil
	}
	return err
}

func validateCheckpoint(checkpoint Checkpoint) error {
	if checkpoint.Format > checkpointFormat2 {
		return fmt.Errorf("prepared job fixture: unsupported checkpoint format %d", checkpoint.Format)
	}
	if checkpoint.Checksum < 0 {
		return fmt.Errorf("prepared job fixture: negative checkpoint checksum")
	}
	if checkpoint.Queue == 0 && (checkpoint.Completed != 0 || checkpoint.Checksum != 0 || checkpoint.Cursor != 0) {
		return fmt.Errorf("prepared job fixture: unbound queue has nonzero state")
	}
	if checkpoint.Format == 0 && (checkpoint.Sequence != 0 || checkpoint.Revision != 0 || checkpoint.Queue != 0) {
		return fmt.Errorf("prepared job fixture: only the empty initial checkpoint may omit a format")
	}
	return nil
}
