package preparedworkerparity

import (
	"context"
	"fmt"
	"math"

	"github.com/besmpl/ember"
	preparedworkerparitygenerated "github.com/besmpl/ember/internal/preparedworkerparity/generated"
	"github.com/besmpl/ember/preparedworker"
)

type Handler struct {
	runtime *ember.Runtime
}

func NewHandler(
	ctx context.Context,
	_ preparedworker.Restore[Checkpoint],
) (preparedworker.Handler[Request, Result, Checkpoint, Effect], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	program, _, err := preparedworkerparitygenerated.LoadProgram(
		ctx,
		ember.ProgramOptions{Parallelism: 1},
	)
	if err != nil {
		return nil, fmt.Errorf("prepared worker parity: load Program: %w", err)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{
		Prepared: preparedworkerparitygenerated.Bundle,
	})
	if err != nil {
		return nil, fmt.Errorf("prepared worker parity: bind bundle: %w", err)
	}
	return &Handler{runtime: runtime}, nil
}

func (handler *Handler) Apply(
	ctx context.Context,
	operation preparedworker.Operation[Request],
) (preparedworker.Decision[Result, Checkpoint, Effect], error) {
	if handler == nil || handler.runtime == nil {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf("prepared worker parity: closed")
	}
	request := operation.Request
	if request.Case >= CaseCount {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf(
			"prepared worker parity: case %d is out of bounds",
			request.Case,
		)
	}
	if request.Iterations == 0 || request.Iterations > MaxIterations {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf(
			"prepared worker parity: iterations %d are out of bounds",
			request.Iterations,
		)
	}
	const maxExactInteger = int64(1 << 53)
	if request.Seed <= -maxExactInteger || request.Seed >= maxExactInteger {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf(
			"prepared worker parity: seed is outside the exact integer range",
		)
	}
	values, err := handler.runtime.Invoke(
		ctx,
		ember.Invocation{
			Module: ember.LogicalModule(fmt.Sprintf("prepared-worker-parity/case-%02d", request.Case)),
			Export: "run",
		},
		ember.NumberValue(float64(request.Iterations)),
		ember.NumberValue(float64(request.Seed)),
	)
	if err != nil {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, err
	}
	if len(values) != 1 {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf(
			"prepared worker parity: got %d results, want one",
			len(values),
		)
	}
	number, ok := values[0].Number()
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number ||
		number <= -float64(maxExactInteger) || number >= float64(maxExactInteger) {
		return preparedworker.Decision[Result, Checkpoint, Effect]{}, fmt.Errorf(
			"prepared worker parity: result is not an exact integer",
		)
	}
	checkpoint := Checkpoint{}
	return preparedworker.Decision[Result, Checkpoint, Effect]{
		Result: Result{Checksum: int64(number)}, Checkpoint: &checkpoint,
	}, nil
}

func (handler *Handler) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if handler == nil || handler.runtime == nil {
		return nil
	}
	err := handler.runtime.Close()
	handler.runtime = nil
	return err
}
