package preparedworkerprobe

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/besmpl/ember/internal/preparedworkerfixture"
	"github.com/besmpl/ember/preparedworker"
)

const transactionCodecVersion = 1

// TransactionRunnerContract is the complete caller-facing rich-game record
// contract shared by embedded and process deployment adapters.
func TransactionRunnerContract() preparedworker.Contract[
	preparedworkerfixture.TurnRequest,
	preparedworkerfixture.TurnResult,
	preparedworkerfixture.Checkpoint,
	preparedworkerfixture.Effect,
] {
	return preparedworker.Contract[
		preparedworkerfixture.TurnRequest,
		preparedworkerfixture.TurnResult,
		preparedworkerfixture.Checkpoint,
		preparedworkerfixture.Effect,
	]{
		CodecVersion: transactionCodecVersion,
		Request: preparedworker.Schema[preparedworkerfixture.TurnRequest]{
			Name: "rich-game-request-v1", MaxBytes: 64 << 10,
			Codec: JSONCodec[preparedworkerfixture.TurnRequest]{},
		},
		Result: preparedworker.Schema[preparedworkerfixture.TurnResult]{
			Name: "rich-game-result-v1", MaxBytes: 64 << 10,
			Codec: JSONCodec[preparedworkerfixture.TurnResult]{},
		},
		Checkpoint: preparedworker.Schema[preparedworkerfixture.Checkpoint]{
			Name: "rich-game-checkpoint-v1", MaxBytes: 64 << 10,
			Codec: JSONCodec[preparedworkerfixture.Checkpoint]{},
		},
		Effect: preparedworker.Schema[preparedworkerfixture.Effect]{
			Name: "rich-game-effect-v1", MaxBytes: 64 << 10,
			Codec: JSONCodec[preparedworkerfixture.Effect]{},
		},
		MaxEffects: 64,
	}
}

// NewTransactionHandler restores the fixture's typed embedded handler through
// the same checkpoint path used inside the process worker.
func NewTransactionHandler(
	ctx context.Context,
	restore preparedworker.Restore[preparedworkerfixture.Checkpoint],
) (preparedworker.Handler[
	preparedworkerfixture.TurnRequest,
	preparedworkerfixture.TurnResult,
	preparedworkerfixture.Checkpoint,
	preparedworkerfixture.Effect,
], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	handler, err := NewHandlerAt(restore.Checkpoint)
	if err != nil {
		return nil, err
	}
	return &TransactionHandler{handler: handler}, nil
}

// TransactionHandler adapts the fixture's Ember runtime to one atomic typed
// application decision.
type TransactionHandler struct {
	handler *Handler
}

func (handler *TransactionHandler) Apply(
	ctx context.Context,
	operation preparedworker.Operation[preparedworkerfixture.TurnRequest],
) (preparedworker.Decision[
	preparedworkerfixture.TurnResult,
	preparedworkerfixture.Checkpoint,
	preparedworkerfixture.Effect,
], error) {
	request := operation.Request
	if request.Sequence != operation.Sequence || request.Revision != operation.BaseRevision {
		return preparedworker.Decision[
			preparedworkerfixture.TurnResult,
			preparedworkerfixture.Checkpoint,
			preparedworkerfixture.Effect,
		]{}, fmt.Errorf("prepared worker fixture: request envelope differs")
	}
	result, err := handler.handler.Transact(ctx, request)
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
		Result: result, Effects: append([]preparedworkerfixture.Effect(nil), result.Effects...),
	}
	if checkpoint, err := QuiescentCheckpoint(result); err == nil {
		decision.Checkpoint = &checkpoint
	}
	return decision, nil
}

func (handler *TransactionHandler) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if handler == nil || handler.handler == nil {
		return nil
	}
	if err := handler.handler.Close(); err != nil {
		return err
	}
	handler.handler = nil
	return nil
}

// JSONCodec is the fixture's canonical closed-record codec.
type JSONCodec[T any] struct{}

func (JSONCodec[T]) Encode(value T) ([]byte, error) { return json.Marshal(value) }

func (JSONCodec[T]) Decode(data []byte) (T, error) {
	var value T
	err := json.Unmarshal(data, &value)
	return value, err
}
