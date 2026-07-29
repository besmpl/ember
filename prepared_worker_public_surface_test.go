package ember_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/besmpl/ember/preparedworker"
)

func TestPreparedWorkerPublicRunnerReplaysCommittedOperation(t *testing.T) {
	handler := &publicRunnerHandler{total: 2}
	runner, err := preparedworker.OpenEmbedded(
		context.Background(),
		preparedworker.Options[publicRunnerRequest, publicRunnerResult, publicRunnerCheckpoint, string]{
			Stream: "public-runner",
			Contract: preparedworker.Contract[publicRunnerRequest, publicRunnerResult, publicRunnerCheckpoint, string]{
				CodecVersion: 1,
				Request:      preparedworker.Schema[publicRunnerRequest]{Name: "public-request-v1", MaxBytes: 128, Codec: publicRunnerCodec[publicRunnerRequest]{}},
				Result:       preparedworker.Schema[publicRunnerResult]{Name: "public-result-v1", MaxBytes: 128, Codec: publicRunnerCodec[publicRunnerResult]{}},
				Checkpoint:   preparedworker.Schema[publicRunnerCheckpoint]{Name: "public-checkpoint-v1", MaxBytes: 128, Codec: publicRunnerCodec[publicRunnerCheckpoint]{}},
				Effect:       preparedworker.Schema[string]{Name: "public-effect-v1", MaxBytes: 128, Codec: publicRunnerCodec[string]{}},
				MaxEffects:   1,
			},
			Initial:       publicRunnerCheckpoint{Total: 2},
			StatePath:     filepath.Join(t.TempDir(), "state.journal"),
			MaxStateBytes: 1 << 20,
			Deliver: func(context.Context, preparedworker.Delivery[string]) error {
				return nil
			},
		},
		func(context.Context, preparedworker.Restore[publicRunnerCheckpoint]) (
			preparedworker.Handler[publicRunnerRequest, publicRunnerResult, publicRunnerCheckpoint, string],
			error,
		) {
			return handler, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runner.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})

	operation := preparedworker.Operation[publicRunnerRequest]{Sequence: 1, Request: publicRunnerRequest{Delta: 3}}
	first, err := runner.Apply(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.Apply(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := runner.Resolve(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}

	want := preparedworker.Completion[publicRunnerResult]{
		Position: preparedworker.Position{Sequence: 1, Revision: 1},
		Result:   publicRunnerResult{Total: 5},
	}
	if first != want || second != want || resolution.Status != preparedworker.ResolveCommitted || resolution.Completion != want {
		t.Fatalf("first=%#v second=%#v resolution=%#v, want committed %#v", first, second, resolution, want)
	}
	if handler.calls != 1 {
		t.Fatalf("handler calls = %d, want one guest entry", handler.calls)
	}
}

type publicRunnerCodec[T any] struct{}

type publicRunnerRequest struct {
	Delta int `json:"delta"`
}

type publicRunnerResult struct {
	Total int `json:"total"`
}

type publicRunnerCheckpoint struct {
	Total int `json:"total"`
}

func (publicRunnerCodec[T]) Encode(value T) ([]byte, error) { return json.Marshal(value) }

func (publicRunnerCodec[T]) Decode(encoded []byte) (T, error) {
	var value T
	err := json.Unmarshal(encoded, &value)
	return value, err
}

type publicRunnerHandler struct {
	total int
	calls int
}

func (handler *publicRunnerHandler) Apply(
	_ context.Context,
	operation preparedworker.Operation[publicRunnerRequest],
) (preparedworker.Decision[publicRunnerResult, publicRunnerCheckpoint, string], error) {
	handler.calls++
	handler.total += operation.Request.Delta
	result := publicRunnerResult{Total: handler.total}
	checkpoint := publicRunnerCheckpoint{Total: handler.total}
	return preparedworker.Decision[publicRunnerResult, publicRunnerCheckpoint, string]{
		Result: result, Checkpoint: &checkpoint,
	}, nil
}

func (*publicRunnerHandler) Close(context.Context) error { return nil }
