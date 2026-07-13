package ember

import (
	"context"
	"fmt"
)

const maxInt64Uint = uint64(1<<63 - 1)

type executionController struct {
	ctx       context.Context
	limits    ExecutionLimits
	remaining int64
}

func newExecutionController(ctx context.Context, limits ExecutionLimits) (*executionController, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	normalized, err := normalizeExecutionLimits(0, limits)
	if err != nil {
		return nil, err
	}
	remaining, err := executionControllerRemaining(normalized)
	if err != nil {
		return nil, err
	}
	return &executionController{
		ctx:       ctx,
		limits:    normalized,
		remaining: remaining,
	}, nil
}

func validateExecutionLimits(limits ExecutionLimits) error {
	_, err := executionControllerRemaining(limits)
	return err
}

func executionControllerRemaining(limits ExecutionLimits) (int64, error) {
	if limits.MaxInstructions == 0 {
		return -1, nil
	}
	if limits.MaxInstructions > maxInt64Uint {
		return 0, fmt.Errorf("execution controller: max instructions %d exceeds int64", limits.MaxInstructions)
	}
	return int64(limits.MaxInstructions), nil
}

func (controller *executionController) checkContext() error {
	if controller == nil || controller.ctx == nil {
		return nil
	}
	return controller.ctx.Err()
}

func (controller *executionController) chargeInstructions(count uint64) error {
	if controller == nil || controller.remaining < 0 || count == 0 {
		return nil
	}
	if count <= uint64(controller.remaining) {
		controller.remaining -= int64(count)
		return nil
	}
	used := controller.limits.MaxInstructions - uint64(controller.remaining)
	if count > ^uint64(0)-used {
		used = ^uint64(0)
	} else {
		used += count
	}
	controller.remaining = 0
	return &LimitError{
		Kind:  LimitInstructions,
		Limit: controller.limits.MaxInstructions,
		Used:  used,
	}
}

func (controller *executionController) configuredLimits() ExecutionLimits {
	if controller == nil {
		return ExecutionLimits{}
	}
	return controller.limits
}
