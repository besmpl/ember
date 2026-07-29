package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/besmpl/ember/internal/preparedworkerfixture"
	"github.com/besmpl/ember/internal/preparedworkerprobe"
	"github.com/besmpl/ember/preparedworker"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	return preparedworker.ServeWorker(
		context.Background(),
		preparedworkerprobe.TransactionRunnerContract(),
		newStallingHandler,
	)
}

func newStallingHandler(
	ctx context.Context,
	restore preparedworker.Restore[preparedworkerfixture.Checkpoint],
) (preparedworker.Handler[
	preparedworkerfixture.TurnRequest,
	preparedworkerfixture.TurnResult,
	preparedworkerfixture.Checkpoint,
	preparedworkerfixture.Effect,
], error) {
	inner, err := preparedworkerprobe.NewTransactionHandler(ctx, restore)
	if err != nil {
		return nil, err
	}
	return &stallingHandler{inner: inner}, nil
}

// stallingHandler exists only to exercise the production supervisor's hard
// cancellation path with a real EPW2 executable. The timer keeps this a live
// blocked process until the parent terminates its owned process tree.
type stallingHandler struct {
	inner preparedworker.Handler[
		preparedworkerfixture.TurnRequest,
		preparedworkerfixture.TurnResult,
		preparedworkerfixture.Checkpoint,
		preparedworkerfixture.Effect,
	]
}

func (handler *stallingHandler) Apply(
	ctx context.Context,
	_ preparedworker.Operation[preparedworkerfixture.TurnRequest],
) (preparedworker.Decision[
	preparedworkerfixture.TurnResult,
	preparedworkerfixture.Checkpoint,
	preparedworkerfixture.Effect,
], error) {
	timer := time.NewTimer(24 * time.Hour)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return preparedworker.Decision[
			preparedworkerfixture.TurnResult,
			preparedworkerfixture.Checkpoint,
			preparedworkerfixture.Effect,
		]{}, ctx.Err()
	case <-timer.C:
		return preparedworker.Decision[
			preparedworkerfixture.TurnResult,
			preparedworkerfixture.Checkpoint,
			preparedworkerfixture.Effect,
		]{}, fmt.Errorf("prepared worker stall fixture: timer elapsed")
	}
}

func (handler *stallingHandler) Close(ctx context.Context) error {
	if handler == nil || handler.inner == nil {
		return nil
	}
	err := handler.inner.Close(ctx)
	if err == nil {
		handler.inner = nil
	}
	return err
}
