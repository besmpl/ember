package mixedcheckpointproof

import (
	"context"
	"errors"

	"github.com/besmpl/ember/preparedworker"
)

const transactionCodecVersion uint32 = 1

// Contract is the application-specific closed-record transaction contract.
// The generic worker sees only these codecs and byte bounds, never Ruby or
// Sprig identities.
func Contract() preparedworker.Contract[Request, Result, Checkpoint, Effect] {
	return preparedworker.Contract[Request, Result, Checkpoint, Effect]{
		CodecVersion: transactionCodecVersion,
		Request: preparedworker.Schema[Request]{
			Name: "mixed-checkpoint-request-v1", MaxBytes: requestBytes, Codec: requestCodec{},
		},
		Result: preparedworker.Schema[Result]{
			Name: "mixed-checkpoint-result-v1", MaxBytes: resultBytes, Codec: resultCodec{},
		},
		Checkpoint: preparedworker.Schema[Checkpoint]{
			Name: "mixed-checkpoint-state-v1", MaxBytes: checkpointBytes, Codec: checkpointCodec{},
		},
		Effect: preparedworker.Schema[Effect]{
			Name: "mixed-checkpoint-effect-v1", MaxBytes: effectBytes, Codec: effectCodec{},
		},
		MaxEffects: 1,
	}
}

// NewTransactionHandler restores the concrete mixed application behind the
// production embedded transaction seam.
func NewTransactionHandler(
	ctx context.Context,
	restore preparedworker.Restore[Checkpoint],
) (preparedworker.Handler[Request, Result, Checkpoint, Effect], error) {
	handler, err := NewHandler(ctx, restore.Checkpoint, ProofPolicy())
	if err != nil {
		return nil, err
	}
	return &TransactionHandler{handler: handler}, nil
}

// TransactionHandler is the thin application-owned preparedworker adapter.
// Language execution and checkpoint policy stay in the concrete Handler.
type TransactionHandler struct {
	handler *Handler
}

func (handler *TransactionHandler) Apply(
	ctx context.Context,
	operation preparedworker.Operation[Request],
) (preparedworker.Decision[Result, Checkpoint, Effect], error) {
	if handler == nil || handler.handler == nil {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, ErrClosed
	}
	result, checkpoint, effect, hasEffect, err := handler.handler.Apply(ctx, operation.Request)
	if err != nil {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, err
	}
	decision := preparedworker.Decision[Result, Checkpoint, Effect]{Result: result, Checkpoint: &checkpoint}
	if hasEffect {
		decision.Effects = []Effect{effect}
	}
	return decision, nil
}

func (handler *TransactionHandler) Close(ctx context.Context) error {
	if handler == nil || handler.handler == nil {
		return nil
	}
	closeErr := handler.handler.Close()
	if closeErr == nil {
		handler.handler = nil
	}
	if ctx == nil {
		return errors.Join(closeErr, ErrNilContext)
	}
	return errors.Join(closeErr, ctx.Err())
}
