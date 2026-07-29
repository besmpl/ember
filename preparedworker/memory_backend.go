package preparedworker

import (
	"context"
	"fmt"
	"sync"
)

// newMemoryPreparer builds the invariant-equivalent in-memory backend used by
// contract tests and embedded deployments.
func newMemoryPreparer[Q, R, C, E any](
	requestCodec Codec[Q],
	resultCodec Codec[R],
	checkpointCodec Codec[C],
	effectCodec Codec[E],
	factory func(context.Context, Position, C) (Handler[Q, R, C, E], error),
) backendPreparer {
	return &memoryPreparer[Q, R, C, E]{
		requestCodec:    requestCodec,
		resultCodec:     resultCodec,
		checkpointCodec: checkpointCodec,
		effectCodec:     effectCodec,
		factory:         factory,
	}
}

type memoryPreparer[Q, R, C, E any] struct {
	requestCodec    Codec[Q]
	resultCodec     Codec[R]
	checkpointCodec Codec[C]
	effectCodec     Codec[E]
	factory         func(context.Context, Position, C) (Handler[Q, R, C, E], error)
}

func (preparer *memoryPreparer[Q, R, C, E]) Prepare(
	ctx context.Context,
	spec backendSpec,
) (generationBackend, backendReady, error) {
	if preparer == nil || preparer.requestCodec == nil || preparer.resultCodec == nil ||
		preparer.checkpointCodec == nil || preparer.effectCodec == nil || preparer.factory == nil {
		return nil, backendReady{}, fmt.Errorf("prepared worker memory backend: incomplete preparer")
	}
	checkpoint, err := decodeCanonical(
		preparer.checkpointCodec,
		spec.Checkpoint,
		spec.Limits.MaxCheckpointBytes,
		"checkpoint",
	)
	if err != nil {
		return nil, backendReady{}, err
	}
	handler, err := preparer.factory(ctx, spec.Position, checkpoint)
	if err != nil {
		return nil, backendReady{}, err
	}
	if handler == nil {
		return nil, backendReady{}, fmt.Errorf("prepared worker memory backend: nil handler")
	}
	return &memoryBackend[Q, R, C, E]{
		requestCodec:    preparer.requestCodec,
		resultCodec:     preparer.resultCodec,
		checkpointCodec: preparer.checkpointCodec,
		effectCodec:     preparer.effectCodec,
		handler:         handler,
		limits:          spec.Limits,
	}, readyFor(spec), nil
}

type memoryBackend[Q, R, C, E any] struct {
	mu              sync.Mutex
	requestCodec    Codec[Q]
	resultCodec     Codec[R]
	checkpointCodec Codec[C]
	effectCodec     Codec[E]
	handler         Handler[Q, R, C, E]
	limits          transactionLimits
	closed          bool
}

func (backend *memoryBackend[Q, R, C, E]) Apply(
	ctx context.Context,
	encoded encodedOperation,
) (wireDecision, error) {
	backend.mu.Lock()
	if backend.closed || backend.handler == nil {
		backend.mu.Unlock()
		return wireDecision{}, fmt.Errorf("prepared worker memory backend: closed")
	}
	handler := backend.handler
	backend.mu.Unlock()

	request, err := decodeCanonical(
		backend.requestCodec,
		encoded.Request,
		backend.limits.MaxRequestBytes,
		"request",
	)
	if err != nil {
		return wireDecision{}, err
	}
	decision, err := handler.Apply(ctx, Operation[Q]{
		Sequence:     encoded.Sequence,
		BaseRevision: encoded.BaseRevision,
		Request:      request,
	})
	if err != nil {
		return wireDecision{}, err
	}
	result, _, err := encodeCanonical(
		backend.resultCodec,
		decision.Result,
		backend.limits.MaxResultBytes,
		"result",
	)
	if err != nil {
		return wireDecision{}, err
	}
	encodedDecision := wireDecision{
		Position: Position{Sequence: encoded.Sequence, Revision: encoded.BaseRevision + 1},
		Result:   result,
	}
	if decision.Checkpoint != nil {
		checkpoint, _, err := encodeCanonical(
			backend.checkpointCodec,
			*decision.Checkpoint,
			backend.limits.MaxCheckpointBytes,
			"checkpoint",
		)
		if err != nil {
			return wireDecision{}, err
		}
		encodedDecision.HasCheckpoint = true
		encodedDecision.Quiescent = true
		encodedDecision.Checkpoint = checkpoint
	}
	if len(decision.Effects) > backend.limits.MaxOutboxItems {
		return wireDecision{}, fmt.Errorf(
			"prepared worker memory backend: effects have %d items, limit %d",
			len(decision.Effects),
			backend.limits.MaxOutboxItems,
		)
	}
	total := 0
	for _, effect := range decision.Effects {
		payload, _, err := encodeCanonical(
			backend.effectCodec,
			effect,
			backend.limits.MaxOutboxBytes,
			"effect",
		)
		if err != nil {
			return wireDecision{}, err
		}
		total += len(payload)
		if total > backend.limits.MaxOutboxBytes {
			return wireDecision{}, fmt.Errorf(
				"prepared worker memory backend: effects use %d bytes, limit %d",
				total,
				backend.limits.MaxOutboxBytes,
			)
		}
		encodedDecision.Effects = append(encodedDecision.Effects, payload)
	}
	return encodedDecision, nil
}

func (backend *memoryBackend[Q, R, C, E]) Close(ctx context.Context) error {
	if backend == nil {
		return nil
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.closed || backend.handler == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := backend.handler.Close(ctx); err != nil {
		return err
	}
	backend.handler = nil
	backend.closed = true
	return nil
}
