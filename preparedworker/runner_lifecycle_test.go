package preparedworker_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/besmpl/ember/preparedworker"
)

func TestRunnerCloseRetainsJournalOwnershipUntilRetrySucceeds(t *testing.T) {
	closeFailure := errors.New("injected handler close failure")
	handler := &runnerLifecycleHandler{closeErrors: []error{closeFailure}}
	options := runnerLifecycleOptions(filepath.Join(t.TempDir(), "owned.journal"), time.Second)
	runner, err := preparedworker.OpenEmbedded(
		context.Background(),
		options,
		func(context.Context, preparedworker.Restore[runnerLifecycleCheckpoint]) (
			preparedworker.Handler[string, string, runnerLifecycleCheckpoint, string],
			error,
		) {
			return handler, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Close(context.Background()); !errors.Is(err, closeFailure) {
		t.Fatalf("first Close error = %v, want injected failure", err)
	}

	other, err := preparedworker.OpenEmbedded(
		context.Background(),
		options,
		newRunnerLifecycleHandler,
	)
	if err == nil {
		_ = other.Close(context.Background())
		t.Fatal("second runner opened state still owned by failed Close")
	}
	if err := runner.Close(context.Background()); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	if err := runner.Close(context.Background()); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}

	reopened, err := preparedworker.OpenEmbedded(
		context.Background(),
		options,
		newRunnerLifecycleHandler,
	)
	if err != nil {
		t.Fatalf("reopen after successful Close: %v", err)
	}
	if err := reopened.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerCloseAppliesConfiguredBoundAndCanRetry(t *testing.T) {
	handler := &runnerLifecycleHandler{waitForFirstCloseContext: true}
	options := runnerLifecycleOptions(filepath.Join(t.TempDir(), "bounded.journal"), 20*time.Millisecond)
	runner, err := preparedworker.OpenEmbedded(
		context.Background(),
		options,
		func(context.Context, preparedworker.Restore[runnerLifecycleCheckpoint]) (
			preparedworker.Handler[string, string, runnerLifecycleCheckpoint, string],
			error,
		) {
			return handler, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := runner.Close(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded Close error = %v, want deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bounded Close took %s", elapsed)
	}
	if err := runner.Close(context.Background()); err != nil {
		t.Fatalf("retry bounded Close: %v", err)
	}
}

type runnerLifecycleCheckpoint struct {
	Sequence uint64 `json:"sequence"`
}

type runnerLifecycleCodec[T any] struct{}

func (runnerLifecycleCodec[T]) Encode(value T) ([]byte, error) { return json.Marshal(value) }
func (runnerLifecycleCodec[T]) Decode(encoded []byte) (T, error) {
	var value T
	err := json.Unmarshal(encoded, &value)
	return value, err
}

type runnerLifecycleHandler struct {
	mu                       sync.Mutex
	closeErrors              []error
	waitForFirstCloseContext bool
	closeCalls               int
}

func newRunnerLifecycleHandler(
	context.Context,
	preparedworker.Restore[runnerLifecycleCheckpoint],
) (preparedworker.Handler[string, string, runnerLifecycleCheckpoint, string], error) {
	return &runnerLifecycleHandler{}, nil
}

func (handler *runnerLifecycleHandler) Apply(
	context.Context,
	preparedworker.Operation[string],
) (preparedworker.Decision[string, runnerLifecycleCheckpoint, string], error) {
	checkpoint := runnerLifecycleCheckpoint{Sequence: 1}
	return preparedworker.Decision[string, runnerLifecycleCheckpoint, string]{
		Result: "ok", Checkpoint: &checkpoint,
	}, nil
}

func (handler *runnerLifecycleHandler) Close(ctx context.Context) error {
	handler.mu.Lock()
	handler.closeCalls++
	call := handler.closeCalls
	if len(handler.closeErrors) != 0 {
		err := handler.closeErrors[0]
		handler.closeErrors = handler.closeErrors[1:]
		handler.mu.Unlock()
		return err
	}
	wait := handler.waitForFirstCloseContext && call == 1
	handler.mu.Unlock()
	if wait {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func runnerLifecycleOptions(
	statePath string,
	timeout time.Duration,
) preparedworker.Options[string, string, runnerLifecycleCheckpoint, string] {
	codecString := runnerLifecycleCodec[string]{}
	return preparedworker.Options[string, string, runnerLifecycleCheckpoint, string]{
		Stream: "runner-lifecycle",
		Contract: preparedworker.Contract[string, string, runnerLifecycleCheckpoint, string]{
			CodecVersion: 1,
			Request:      preparedworker.Schema[string]{Name: "runner-request-v1", MaxBytes: 1024, Codec: codecString},
			Result:       preparedworker.Schema[string]{Name: "runner-result-v1", MaxBytes: 1024, Codec: codecString},
			Checkpoint: preparedworker.Schema[runnerLifecycleCheckpoint]{
				Name: "runner-checkpoint-v1", MaxBytes: 1024,
				Codec: runnerLifecycleCodec[runnerLifecycleCheckpoint]{},
			},
			Effect:     preparedworker.Schema[string]{Name: "runner-effect-v1", MaxBytes: 1024, Codec: codecString},
			MaxEffects: 4,
		},
		StatePath: statePath, MaxStateBytes: 1 << 20, ShutdownTimeout: timeout,
		Deliver: func(context.Context, preparedworker.Delivery[string]) error { return nil },
	}
}
