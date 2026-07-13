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
	remaining := int64(-1)
	if normalized.MaxInstructions != 0 {
		if normalized.MaxInstructions > maxInt64Uint {
			return nil, fmt.Errorf("execution controller: max instructions %d exceeds int64", normalized.MaxInstructions)
		}
		remaining = int64(normalized.MaxInstructions)
	}
	return &executionController{
		ctx:       ctx,
		limits:    normalized,
		remaining: remaining,
	}, nil
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
